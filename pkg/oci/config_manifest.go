package oci

import (
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// PushConfigManifest pushes the cache-config manifest with the provided
// annotations (cache-wide settings such as the public signing key).
func PushConfigManifest(registry, repository string, auth authn.Authenticator, annotations map[string]string) error {
	refStr := fmt.Sprintf("%s/%s:cache-config", registry, repository)
	tagRef, err := name.NewTag(refStr, nameOptions(registry)...)
	if err != nil {
		return fmt.Errorf("failed to create tag: %w", err)
	}

	cacheImg := empty.Image
	cacheImg = mutate.MediaType(cacheImg, types.OCIManifestSchema1)
	cacheImg = mutate.ConfigMediaType(cacheImg, "application/vnd.oci.empty.v1+json")

	emptyLayer := static.NewLayer([]byte("{}"), "application/vnd.oci.empty.v1+json")
	cacheImg, err = mutate.AppendLayers(cacheImg, emptyLayer)
	if err != nil {
		return fmt.Errorf("failed to append empty layer: %w", err)
	}

	cacheImg = mutate.Annotations(cacheImg, annotations).(v1.Image)

	finalCacheImg := NewArtifactTypeImage(cacheImg, "application/vnd.aeroflare.cache-config.v1")

	if err := remote.Write(tagRef, finalCacheImg,
		remote.WithTransport(WriteTransport()),
		remoteAuth(auth),
	); err != nil {
		return fmt.Errorf("failed to push config manifest: %w", err)
	}

	return nil
}
