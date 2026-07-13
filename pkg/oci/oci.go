package oci

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// withArtifactType wraps a v1.Image to inject an OCI ArtifactType into its
// manifest. go-containerregistry's v1.Image has no setter for ArtifactType,
// so this recomputes the manifest (and therefore Digest/Size, since both
// depend on the manifest bytes) with the field set.
type withArtifactType struct {
	v1.Image
	artifactType string
}

// NewArtifactTypeImage wraps img so its manifest advertises the given OCI
// artifactType, working around go-containerregistry's lack of a setter.
func NewArtifactTypeImage(img v1.Image, artifactType string) v1.Image {
	return &withArtifactType{Image: img, artifactType: artifactType}
}

func (w *withArtifactType) Manifest() (*v1.Manifest, error) {
	m, err := w.Image.Manifest()
	if err != nil {
		return nil, err
	}
	mCopy := *m
	mCopy.ArtifactType = w.artifactType
	return &mCopy, nil
}

func (w *withArtifactType) RawManifest() ([]byte, error) {
	m, err := w.Manifest()
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

func (w *withArtifactType) Digest() (v1.Hash, error) {
	b, err := w.RawManifest()
	if err != nil {
		return v1.Hash{}, err
	}
	h := sha256.Sum256(b)
	return v1.Hash{
		Algorithm: "sha256",
		Hex:       fmt.Sprintf("%x", h),
	}, nil
}

func (w *withArtifactType) Size() (int64, error) {
	b, err := w.RawManifest()
	if err != nil {
		return 0, err
	}
	return int64(len(b)), nil
}

// ParseAeroflareMetadata converts a narinfo's "Key: value" lines into
// lowercase "aeroflare.<key>" annotations suitable for an OCI manifest.
func ParseAeroflareMetadata(content string) (map[string]string, error) {
	annotations := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(content))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid metadata format on line: %s", line)
		}
		rawKey := strings.TrimSpace(parts[0])
		key := fmt.Sprintf("aeroflare.%s", strings.ToLower(rawKey))
		val := strings.TrimSpace(parts[1])
		annotations[key] = val
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return annotations, nil
}

// FetchAeroflareAnnotations fetches an OCI manifest by tag and returns its
// annotations merged with its labels (labels are checked as a fallback for
// registries/tools that surface metadata under "labels" instead). A nil auth
// means anonymous access.
func FetchAeroflareAnnotations(ctx context.Context, registry, repository, tag string, auth authn.Authenticator) (map[string]string, error) {
	imageRef := fmt.Sprintf("%s/%s:%s", registry, repository, tag)
	ref, err := name.ParseReference(imageRef, nameOptions(registry)...)
	if err != nil {
		return nil, fmt.Errorf("failed to parse reference: %w", err)
	}

	img, err := remote.Image(ref,
		remote.WithContext(ctx),
		remote.WithTransport(optimizedTransport),
		remoteAuth(auth),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image: %w", err)
	}

	raw, err := img.RawManifest()
	if err != nil {
		return nil, fmt.Errorf("failed to read raw manifest: %w", err)
	}

	var rawManifest struct {
		Annotations map[string]string `json:"annotations"`
		Labels      map[string]string `json:"labels"`
	}
	if err := json.Unmarshal(raw, &rawManifest); err != nil {
		return nil, fmt.Errorf("failed to unmarshal raw manifest: %w", err)
	}

	result := make(map[string]string)
	for key, val := range rawManifest.Annotations {
		result[key] = val
	}
	for key, val := range rawManifest.Labels {
		result[key] = val
	}

	return result, nil
}
