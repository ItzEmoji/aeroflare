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
func PushConfigManifest(registry, repository, token string, annotations map[string]string) error {
	opts := []name.Option{}
	if GetProtocol(registry) == "http" {
		opts = append(opts, name.Insecure)
	}

	refStr := fmt.Sprintf("%s/%s:cache-config", registry, repository)
	tagRef, err := name.NewTag(refStr, opts...)
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

	remoteOpts := []remote.Option{
		remote.WithTransport(WriteTransport()),
	}
	if token != "" {
		remoteOpts = append(remoteOpts, remote.WithAuth(&authn.Bearer{Token: token}))
	}

	if err := remote.Write(tagRef, finalCacheImg, remoteOpts...); err != nil {
		return fmt.Errorf("failed to push config manifest: %w", err)
	}

	return nil
}
