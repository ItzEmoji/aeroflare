package oci

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func TestPushConfigManifest(t *testing.T) {
	// Start an in-memory registry
	regHandler := registry.New()
	mockRegistry := httptest.NewServer(regHandler)
	defer mockRegistry.Close()

	registryURL := strings.TrimPrefix(mockRegistry.URL, "http://")

	repository := "test/cache-config"
	annotations := map[string]string{
		"org.opencontainers.image.source": "https://github.com/itzemoji/aeroflare",
		"aeroflare.test":                  "true",
	}

	err := PushConfigManifest(registryURL, repository, nil, annotations)
	if err != nil {
		t.Fatalf("PushConfigManifest failed: %v", err)
	}

	// Verify the push by pulling it back
	refStr := fmt.Sprintf("%s/%s:cache-config", registryURL, repository)
	ref, err := name.ParseReference(refStr, name.Insecure)
	if err != nil {
		t.Fatalf("Failed to parse reference: %v", err)
	}

	img, err := remote.Image(ref)
	if err != nil {
		t.Fatalf("Failed to pull image: %v", err)
	}

	manifest, err := img.Manifest()
	if err != nil {
		t.Fatalf("Failed to get manifest: %v", err)
	}

	if manifest.Config.MediaType != "application/vnd.oci.empty.v1+json" {
		t.Errorf("Unexpected Config.MediaType in manifest: %s", manifest.Config.MediaType)
	}

	if manifest.Annotations["aeroflare.test"] != "true" {
		t.Errorf("Expected annotation aeroflare.test to be true, got %s", manifest.Annotations["aeroflare.test"])
	}
}
