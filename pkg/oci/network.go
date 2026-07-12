package oci

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync/atomic"

	narhash "github.com/itzemoji/aeroflare/pkg/prepare/hash"
	"github.com/itzemoji/aeroflare/pkg/prepare/narinfo"
	"strconv"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"time"

	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"golang.org/x/sync/errgroup"
)

// debugHTTP gates per-request debug logging. It is a process-wide diagnostic
// switch rather than a field because the transport it guards is itself a
// process-wide singleton; atomic access keeps it safe for concurrent callers.
var debugHTTP atomic.Bool

// SetDebugHTTP turns per-request debug logging on or off. Logs are written to
// standard error. It is safe to call from any goroutine at any time.
//
// This is a diagnostic switch for the whole process, in the spirit of
// log.SetOutput; it is not per-request configuration.
func SetDebugHTTP(enabled bool) { debugHTTP.Store(enabled) }

type loggingTransport struct {
	Transport http.RoundTripper
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if debugHTTP.Load() {
		// stderr, never stdout: a library must not corrupt its caller's output.
		fmt.Fprintf(os.Stderr, "[DEBUG] %s %s\n", req.Method, req.URL.String())
	}
	return t.Transport.RoundTrip(req)
}

var optimizedTransport = &loggingTransport{
	Transport: &http.Transport{
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
	},
}

// WriteTransport returns the shared, connection-tuned HTTP transport used for
// registry writes, so callers in other packages can reuse it without touching
// the unexported transport value.
func WriteTransport() http.RoundTripper { return optimizedTransport }

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
// repository should be the full repository path (e.g. "itzemoji/nix-cache-test")
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

// PushBlobBytes pushes an in-memory blob to the OCI registry and returns its digest.
func PushBlobBytes(data []byte, registry, repository, token string) (string, error) {
	layer := static.NewLayer(data, types.MediaType("application/octet-stream"))
	digest, err := layer.Digest()
	if err != nil {
		return "", err
	}
	if err := PushLayer(layer, registry, repository, token); err != nil {
		return "", err
	}
	return digest.String(), nil
}

// PushLayer pushes an existing v1.Layer to the OCI registry.
func PushLayer(layer v1.Layer, registry, repository, token string) error {
	opts := []name.Option{}
	if GetProtocol(registry) == "http" {
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

// newPusher builds a remote.Pusher that authenticates once and can be reused
// across many Upload/Push calls, so a batch of concurrent operations shares a
// single registry auth handshake instead of repeating the /v2/ 401 challenge
// and token exchange each time.
func newPusher(token string) (*remote.Pusher, error) {
	remoteOpts := []remote.Option{
		remote.WithTransport(optimizedTransport),
	}
	if token != "" {
		remoteOpts = append(remoteOpts, remote.WithAuth(&authn.Bearer{Token: token}))
	}
	return remote.NewPusher(remoteOpts...)
}

// NewLayerPusher returns a shared pusher plus the target repository to pass to
// pusher.Upload, for uploading many NAR layers under one auth handshake.
func NewLayerPusher(registry, repository, token string) (*remote.Pusher, name.Repository, error) {
	opts := []name.Option{}
	if GetProtocol(registry) == "http" {
		opts = append(opts, name.Insecure)
	}

	repoStr := fmt.Sprintf("%s/%s", registry, repository)
	repo, err := name.NewRepository(repoStr, opts...)
	if err != nil {
		return nil, name.Repository{}, err
	}

	pusher, err := newPusher(token)
	if err != nil {
		return nil, name.Repository{}, err
	}
	return pusher, repo, nil
}

// NewImagePusher returns a shared pusher for pushing per-package OCI images via
// PushNarPackageWith, so a batch of image pushes shares one auth handshake.
func NewImagePusher(token string) (*remote.Pusher, error) {
	return newPusher(token)
}

// PullBlob fetches a blob from any OCI registry and writes it to outFile.
// repository should be the full repository path (e.g. "itzemoji/nix-cache-test")
func PullBlob(digest, outFile, registry, repository, token string) error {
	opts := []name.Option{}
	if GetProtocol(registry) == "http" {
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
	pusher, err := newPusher(token)
	if err != nil {
		return err
	}
	return PushNarPackageWith(context.Background(), pusher, layer, ni, tag, registry, repository)
}

// PushNarPackageWith builds the per-package OCI image and pushes it with a
// caller-supplied shared pusher, so a batch of packages reuses one auth
// handshake instead of re-authenticating per image. See PushNarPackage for the
// O(1)-lookup tagging rule that tag must follow.
func PushNarPackageWith(ctx context.Context, pusher *remote.Pusher, layer v1.Layer, ni *narinfo.Narinfo, tag, registry, repository string) error {
	// Create Image
	img := mutate.MediaType(empty.Image, types.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, types.OCIConfigJSON)

	layerMediaType, _ := layer.MediaType()
	img, err := mutate.Append(img, mutate.Addendum{
		Layer:     layer,
		MediaType: layerMediaType,
	})
	if err != nil {
		return err
	}

	// Reconstruct Narinfo text and parse to aeroflare.* annotations
	annotations, err := ParseAeroflareMetadata(ni.String())
	if err != nil {
		return err
	}

	img = mutate.Annotations(img, annotations).(v1.Image)

	finalNarImg := &withArtifactType{
		Image:        img,
		artifactType: "application/vnd.aeroflare.nar.v1",
	}

	opts := []name.Option{}
	if GetProtocol(registry) == "http" {
		opts = append(opts, name.Insecure)
	}

	refStr := fmt.Sprintf("%s/%s:%s", registry, repository, tag)
	ref, err := name.NewTag(refStr, opts...)
	if err != nil {
		return err
	}

	return pusher.Push(ctx, ref, finalNarImg)
}

// PullOCINativeManifest pulls the OCI image manifest by tag (e.g., <nix-hash>)
// and reconstructs the Narinfo metadata from the manifest annotations.
func PullOCINativeManifest(tag, registry, repository, token string) (*narinfo.Narinfo, error) {
	anns, err := FetchAeroflareAnnotations(context.Background(), nil, registry, repository, tag, token)
	if err != nil {
		return nil, err
	}

	// aeroflare.* keys are lowercase
	fileSize, _ := strconv.ParseInt(anns["aeroflare.filesize"], 10, 64)
	narSize, _ := strconv.ParseInt(anns["aeroflare.narsize"], 10, 64)

	var refs []string
	if r, ok := anns["aeroflare.references"]; ok && r != "" {
		refs = strings.Split(r, " ")
	}

	ni := &narinfo.Narinfo{
		StorePath:   anns["aeroflare.storepath"],
		URL:         anns["aeroflare.url"],
		Compression: anns["aeroflare.compression"],
		FileHash:    anns["aeroflare.filehash"],
		FileSize:    fileSize,
		NarHash:     anns["aeroflare.narhash"],
		NarSize:     narSize,
		References:  refs,
		Deriver:     anns["aeroflare.deriver"],
		System:      anns["aeroflare.system"],
		Sig:         anns["aeroflare.sig"],
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
	var eg errgroup.Group
	eg.SetLimit(maxWorkers)

	for _, job := range jobs {
		job := job // Create a local copy for the goroutine
		eg.Go(func() error {
			compVal := job.NarinfoAnnotations["aeroflare.compression"]

			layer, _, err := NewLayer(job.FilePath, types.MediaType("application/vnd.aeroflare.nar.v1+"+compVal))
			if err != nil {
				return err
			}

			img := mutate.MediaType(empty.Image, types.OCIManifestSchema1)
			img = mutate.ConfigMediaType(img, types.OCIConfigJSON)

			layerMediaType, err := layer.MediaType()
			if err != nil {
				return err
			}

			img, err = mutate.Append(img, mutate.Addendum{
				Layer:     layer,
				MediaType: layerMediaType,
			})
			if err != nil {
				return err
			}

			img = mutate.Annotations(img, job.NarinfoAnnotations).(v1.Image)

			finalNarImg := &withArtifactType{
				Image:        img,
				artifactType: "application/vnd.aeroflare.nar.v1",
			}

			opts := []name.Option{}
			if GetProtocol(registry) == "http" {
				opts = append(opts, name.Insecure)
			}

			refStr := fmt.Sprintf("%s/%s:%s", registry, repository, job.Tag)
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

			if err := remote.Write(ref, finalNarImg, remoteOpts...); err != nil {
				return err
			}
			return nil
		})
	}

	return eg.Wait()
}

// GetProtocol chooses http for localhost/loopback registries (e.g. mock
// registries used in tests, or a registry proxy on 127.0.0.1) and https for
// everything else. It is used throughout the package, not only in tests.
func GetProtocol(registry string) string {
	host := registry
	if h, _, err := net.SplitHostPort(registry); err == nil {
		host = h
	} else {
		host = strings.Trim(host, "[]") // bare IPv6 literal without port
	}
	if host == "localhost" {
		return "http"
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return "http"
	}
	return "https"
}
