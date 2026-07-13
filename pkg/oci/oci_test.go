package oci

import (
	"encoding/json"
	"reflect"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
)

func TestParseAeroflareMetadata(t *testing.T) {
	content := `
Key1: value1
  key2  : value2  
Key3:value3:with:colons
`
	expected := map[string]string{
		"aeroflare.key1": "value1",
		"aeroflare.key2": "value2",
		"aeroflare.key3": "value3:with:colons",
	}

	result, err := ParseAeroflareMetadata(content)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}
}

func TestParseAeroflareMetadata_Error(t *testing.T) {
	content := `
Key1: value1
invalid-line
`
	_, err := ParseAeroflareMetadata(content)
	if err == nil {
		t.Fatal("Expected error for invalid line, got nil")
	}
}

func TestWithArtifactType(t *testing.T) {
	baseImage := empty.Image
	artifactType := "application/vnd.aeroflare.test"

	wrapper := &withArtifactType{
		Image:        baseImage,
		artifactType: artifactType,
	}

	// Test Manifest
	m, err := wrapper.Manifest()
	if err != nil {
		t.Fatalf("Unexpected error from Manifest: %v", err)
	}
	if m.ArtifactType != artifactType {
		t.Errorf("Expected ArtifactType %q, got %q", artifactType, m.ArtifactType)
	}

	// Test RawManifest
	raw, err := wrapper.RawManifest()
	if err != nil {
		t.Fatalf("Unexpected error from RawManifest: %v", err)
	}

	var parsedManifest v1.Manifest
	if err := json.Unmarshal(raw, &parsedManifest); err != nil {
		t.Fatalf("Failed to parse RawManifest: %v", err)
	}

	if parsedManifest.ArtifactType != artifactType {
		t.Errorf("Expected ArtifactType in RawManifest %q, got %q", artifactType, parsedManifest.ArtifactType)
	}

	// Test Digest
	digest, err := wrapper.Digest()
	if err != nil {
		t.Fatalf("Unexpected error from Digest: %v", err)
	}
	if digest.Algorithm != "sha256" {
		t.Errorf("Expected digest algorithm sha256, got %s", digest.Algorithm)
	}
	if len(digest.Hex) != 64 {
		t.Errorf("Expected digest hex length 64, got %d", len(digest.Hex))
	}

	// Test Size
	size, err := wrapper.Size()
	if err != nil {
		t.Fatalf("Unexpected error from Size: %v", err)
	}
	if size != int64(len(raw)) {
		t.Errorf("Expected size %d, got %d", len(raw), size)
	}
}
