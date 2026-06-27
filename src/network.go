package network

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"

	narhash "aeroflare/src/prepare/hash"
	"aeroflare/src/prepare/narinfo"
	"aeroflare/src/proxy"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"strconv"
	"strings"

	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"sync"
	"time"
)

var optimizedTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          1000,
	MaxIdleConnsPerHost:   100,
	MaxConnsPerHost:       100,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
}

type fileLayer struct {
	path      string
	digest    v1.Hash
	size      int64
	mediaType types.MediaType
}

func (l *fileLayer) Digest() (v1.Hash, error)             { return l.digest, nil }
func (l *fileLayer) DiffID() (v1.Hash, error)             { return l.digest, nil }
func (l *fileLayer) Compressed() (io.ReadCloser, error)   { return os.Open(l.path) }
func (l *fileLayer) Uncompressed() (io.ReadCloser, error) { return os.Open(l.path) }
func (l *fileLayer) Size() (int64, error)                 { return l.size, nil }
func (l *fileLayer) MediaType() (types.MediaType, error) {
	if l.mediaType != "" {
		return l.mediaType, nil
	}
	return types.MediaType("application/octet-stream"), nil
}

// NewLayer creates a v1.Layer from a file path, computing its size and sha256 digest once.
func NewLayer(filePath string, mediaType types.MediaType) (v1.Layer, string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		return nil, "", err
	}
	size := stat.Size()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, "", err
	}
	digestStr := "sha256:" + hex.EncodeToString(h.Sum(nil))
	hash, err := v1.NewHash(digestStr)
	if err != nil {
		return nil, "", err
	}

	if mediaType == "" {
		mediaType = types.MediaType("application/octet-stream")
	}

	layer := &fileLayer{
		path:      filePath,
		digest:    hash,
		size:      size,
		mediaType: mediaType,
	}

	return layer, digestStr, nil
}

// NewLayerFast creates a v1.Layer instantly without reading the file from disk,
// using the metadata stored in the .narinfo file (FileHash and FileSize).
func NewLayerFast(filePath string, mediaType types.MediaType, ni *narinfo.Narinfo) (v1.Layer, string, error) {
	if !strings.HasPrefix(ni.FileHash, "sha256:") {
		return nil, "", fmt.Errorf("unsupported hash format in narinfo: %s", ni.FileHash)
	}
	base32Hash := strings.TrimPrefix(ni.FileHash, "sha256:")

	hashBytes, err := narhash.DecodeBase32(base32Hash)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode base32 hash: %w", err)
	}

	digestStr := "sha256:" + hex.EncodeToString(hashBytes)
	hash, err := v1.NewHash(digestStr)
	if err != nil {
		return nil, "", err
	}

	if mediaType == "" {
		mediaType = types.MediaType("application/octet-stream")
	}

	layer := &fileLayer{
		path:      filePath,
		digest:    hash,
		size:      ni.FileSize,
		mediaType: mediaType,
	}

	return layer, digestStr, nil
}

// PushBlob natively hashes and streams a file to any OCI registry.
// repository should be the full repository path (e.g. "itzemoji/nix-cache-test/nix-cache")
func PushBlob(filePath, registry, repository, token string) (string, error) {
	layer, digestStr, err := NewLayer(filePath, "")
	if err != nil {
		return "", err
	}

	err = PushLayer(layer, registry, repository, token)
	if err != nil {
		return "", err
	}

	return digestStr, nil
}

// PushLayer pushes an existing v1.Layer to the OCI registry.
func PushLayer(layer v1.Layer, registry, repository, token string) error {
	opts := []name.Option{}
	if proxy.GetProtocol(registry) == "http" {
		opts = append(opts, name.Insecure)
	}

	repoStr := fmt.Sprintf("%s/%s", registry, repository)
	repo, err := name.NewRepository(repoStr, opts...)
	if err != nil {
		return err
	}

	remoteOpts := []remote.Option{
		remote.WithTransport(optimizedTransport),
	}
	if token != "" {
		remoteOpts = append(remoteOpts, remote.WithAuth(&authn.Bearer{Token: token}))
	}

	return remote.WriteLayer(repo, layer, remoteOpts...)
}

// PullBlob fetches a blob from any OCI registry and writes it to outFile.
// repository should be the full repository path (e.g. "itzemoji/nix-cache-test/nix-cache")
func PullBlob(digest, outFile, registry, repository, token string) error {
	opts := []name.Option{}
	if proxy.GetProtocol(registry) == "http" {
		opts = append(opts, name.Insecure)
	}

	refStr := fmt.Sprintf("%s/%s@%s", registry, repository, digest)
	ref, err := name.NewDigest(refStr, opts...)
	if err != nil {
		return err
	}

	remoteOpts := []remote.Option{
		remote.WithTransport(optimizedTransport),
	}
	if token != "" {
		remoteOpts = append(remoteOpts, remote.WithAuth(&authn.Bearer{Token: token}))
	}

	layer, err := remote.Layer(ref, remoteOpts...)
	if err != nil {
		return err
	}

	rc, err := layer.Compressed()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	out, err := os.Create(outFile)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, rc)
	return err
}

// PushNarPackage creates an OCI image from an existing layer, annotates it with narinfo metadata, and pushes it.
// O(1) Lookup Tagging Rule: The tag passed to this function MUST strictly be the 32-character Nix hash
// (e.g., xn2nlmvng2im9mgrq46y3wkbz4ll1hnp) to ensure O(1) lookups during pulls later.
func PushNarPackage(layer v1.Layer, ni *narinfo.Narinfo, tag, registry, repository, token string) error {
	// Create Image
	img := mutate.MediaType(empty.Image, types.OCIManifestSchema1).(v1.Image)
	img = mutate.ConfigMediaType(img, types.OCIConfigJSON).(v1.Image)
	
	layerMediaType, _ := layer.MediaType()
	img, err := mutate.Append(img, mutate.Addendum{
		Layer:     layer,
		MediaType: layerMediaType,
	})
	if err != nil {
		return err
	}

	// Annotate Manifest
	annotations := map[string]string{
		"vnd.aeroflare.nar.storepath":   ni.StorePath,
		"vnd.aeroflare.nar.url":         ni.URL,
		"vnd.aeroflare.nar.compression": ni.Compression,
		"vnd.aeroflare.nar.filehash":    ni.FileHash,
		"vnd.aeroflare.nar.filesize":    strconv.FormatInt(ni.FileSize, 10),
		"vnd.aeroflare.nar.narhash":     ni.NarHash,
		"vnd.aeroflare.nar.narsize":     strconv.FormatInt(ni.NarSize, 10),
		"vnd.aeroflare.nar.system":      ni.System,
	}
	if len(ni.References) > 0 {
		annotations["vnd.aeroflare.nar.references"] = strings.Join(ni.References, " ")
	}
	if ni.Deriver != "" {
		annotations["vnd.aeroflare.nar.deriver"] = ni.Deriver
	}
	if ni.Sig != "" {
		annotations["vnd.aeroflare.nar.sig"] = ni.Sig
	}

	img = mutate.Annotations(img, annotations).(v1.Image)

	opts := []name.Option{}
	if proxy.GetProtocol(registry) == "http" {
		opts = append(opts, name.Insecure)
	}

	refStr := fmt.Sprintf("%s/%s:%s", registry, repository, tag)
	ref, err := name.NewTag(refStr, opts...)
	if err != nil {
		return err
	}

	remoteOpts := []remote.Option{
		remote.WithTransport(optimizedTransport),
	}
	if token != "" {
		remoteOpts = append(remoteOpts, remote.WithAuth(&authn.Bearer{Token: token}))
	}

	return remote.Write(ref, img, remoteOpts...)
}

// PullOCINativeManifest pulls the OCI image manifest by tag (e.g., <nix-hash>)
// and reconstructs the Narinfo metadata from the manifest annotations.
func PullOCINativeManifest(tag, registry, repository, token string) (*narinfo.Narinfo, error) {
	opts := []name.Option{}
	if proxy.GetProtocol(registry) == "http" {
		opts = append(opts, name.Insecure)
	}

	refStr := fmt.Sprintf("%s/%s:%s", registry, repository, tag)
	ref, err := name.NewTag(refStr, opts...)
	if err != nil {
		return nil, err
	}

	remoteOpts := []remote.Option{
		remote.WithTransport(optimizedTransport),
	}
	if token != "" {
		remoteOpts = append(remoteOpts, remote.WithAuth(&authn.Bearer{Token: token}))
	}

	desc, err := remote.Get(ref, remoteOpts...)
	if err != nil {
		return nil, err
	}

	img, err := desc.Image()
	if err != nil {
		return nil, err
	}

	manifest, err := img.Manifest()
	if err != nil {
		return nil, err
	}

	anns := manifest.Annotations

	fileSize, _ := strconv.ParseInt(anns["vnd.aeroflare.nar.filesize"], 10, 64)
	narSize, _ := strconv.ParseInt(anns["vnd.aeroflare.nar.narsize"], 10, 64)

	var refs []string
	if r, ok := anns["vnd.aeroflare.nar.references"]; ok && r != "" {
		refs = strings.Split(r, " ")
	}

	ni := &narinfo.Narinfo{
		StorePath:   anns["vnd.aeroflare.nar.storepath"],
		URL:         anns["vnd.aeroflare.nar.url"],
		Compression: anns["vnd.aeroflare.nar.compression"],
		FileHash:    anns["vnd.aeroflare.nar.filehash"],
		FileSize:    fileSize,
		NarHash:     anns["vnd.aeroflare.nar.narhash"],
		NarSize:     narSize,
		References:  refs,
		Deriver:     anns["vnd.aeroflare.nar.deriver"],
		System:      anns["vnd.aeroflare.nar.system"],
		Sig:         anns["vnd.aeroflare.nar.sig"],
	}

	return ni, nil
}

type PushJob struct {
	FilePath           string
	Tag                string
	NarinfoAnnotations map[string]string
}

// PushNarPackagesBatch uploads multiple Nix packages concurrently.
// O(1) Lookup Tagging Rule: The Tag passed in PushJob MUST strictly be the 32-character Nix hash
// (e.g., xn2nlmvng2im9mgrq46y3wkbz4ll1hnp) to ensure O(1) lookups during pulls later.
func PushNarPackagesBatch(registry, repository, token string, jobs []PushJob, maxWorkers int) error {
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxWorkers)
	var firstErr error
	var errOnce sync.Once

	for _, job := range jobs {
		wg.Add(1)
		sem <- struct{}{} // acquire

		go func(j PushJob) {
			defer wg.Done()
			defer func() { <-sem }() // release

			comp := j.NarinfoAnnotations["vnd.aeroflare.nar.compression"]
			layer, _, err := NewLayer(j.FilePath, types.MediaType("application/vnd.aeroflare.nar.v1+"+comp))
			if err != nil {
				errOnce.Do(func() { firstErr = err })
				return
			}

			img := mutate.MediaType(empty.Image, types.OCIManifestSchema1).(v1.Image)
			img = mutate.ConfigMediaType(img, types.OCIConfigJSON).(v1.Image)
			
			layerMediaType, _ := layer.MediaType()
			img, err = mutate.Append(img, mutate.Addendum{
				Layer:     layer,
				MediaType: layerMediaType,
			})
			if err != nil {
				errOnce.Do(func() { firstErr = err })
				return
			}

			img = mutate.Annotations(img, j.NarinfoAnnotations).(v1.Image)

			opts := []name.Option{}
			if proxy.GetProtocol(registry) == "http" {
				opts = append(opts, name.Insecure)
			}

			refStr := fmt.Sprintf("%s/%s:%s", registry, repository, j.Tag)
			ref, err := name.NewTag(refStr, opts...)
			if err != nil {
				errOnce.Do(func() { firstErr = err })
				return
			}

			remoteOpts := []remote.Option{
				remote.WithTransport(optimizedTransport),
			}
			if token != "" {
				remoteOpts = append(remoteOpts, remote.WithAuth(&authn.Bearer{Token: token}))
			}

			err = remote.Write(ref, img, remoteOpts...)
			if err != nil {
				errOnce.Do(func() { firstErr = err })
				return
			}
		}(job)
	}

	wg.Wait()
	return firstErr
}

// DeleteTag deletes a tag from the OCI registry.
func DeleteTag(tag, registry, repository, token string) error {
	opts := []name.Option{}
	if proxy.GetProtocol(registry) == "http" {
		opts = append(opts, name.Insecure)
	}

	refStr := fmt.Sprintf("%s/%s:%s", registry, repository, tag)
	ref, err := name.NewTag(refStr, opts...)
	if err != nil {
		return err
	}

	remoteOpts := []remote.Option{
		remote.WithTransport(optimizedTransport),
	}
	if token != "" {
		remoteOpts = append(remoteOpts, remote.WithAuth(&authn.Bearer{Token: token}))
	}

	return remote.Delete(ref, remoteOpts...)
}
