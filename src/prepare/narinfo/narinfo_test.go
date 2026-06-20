package narinfo

import (
	"strings"
	"testing"
)

func TestNarinfoRoundTrip(t *testing.T) {
	original := &Narinfo{
		StorePath:   "/nix/store/0c2j6g2bxqzw7x9q6kbx3vrrj6yfj1vh-hello-2.10",
		URL:         "nar/0c2j6g2bxqzw7x9q6kbx3vrrj6yfj1vh.nar.xz",
		Compression: "xz",
		FileHash:    "sha256:0abcdefghijklmnopqrstuvwxyz0abcdefghijklmnopqrstuv",
		FileSize:    12345,
		NarHash:     "sha256:1abcdefghijklmnopqrstuvwxyz1abcdefghijklmnopqrstuv",
		NarSize:     67890,
		References:  []string{"aaa111-hello-2.10", "bbb222-glibc-2.37"},
		Deriver:     "ccc333-hello-2.10.drv",
		System:      "x86_64-linux",
		Sig:         "cache.nixos.org-1:abc123",
	}

	text := original.String()
	parsed, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if parsed.StorePath != original.StorePath {
		t.Errorf("StorePath mismatch: got %s, want %s", parsed.StorePath, original.StorePath)
	}
	if parsed.URL != original.URL {
		t.Errorf("URL mismatch: got %s, want %s", parsed.URL, original.URL)
	}
	if parsed.Compression != original.Compression {
		t.Errorf("Compression mismatch: got %s, want %s", parsed.Compression, original.Compression)
	}
	if parsed.FileHash != original.FileHash {
		t.Errorf("FileHash mismatch: got %s, want %s", parsed.FileHash, original.FileHash)
	}
	if parsed.FileSize != original.FileSize {
		t.Errorf("FileSize mismatch: got %d, want %d", parsed.FileSize, original.FileSize)
	}
	if parsed.NarHash != original.NarHash {
		t.Errorf("NarHash mismatch: got %s, want %s", parsed.NarHash, original.NarHash)
	}
	if parsed.NarSize != original.NarSize {
		t.Errorf("NarSize mismatch: got %d, want %d", parsed.NarSize, original.NarSize)
	}
	if len(parsed.References) != len(original.References) {
		t.Errorf("References length mismatch: got %d, want %d", len(parsed.References), len(original.References))
	} else {
		for i, ref := range original.References {
			if parsed.References[i] != ref {
				t.Errorf("Reference[%d] mismatch: got %s, want %s", i, parsed.References[i], ref)
			}
		}
	}
	if parsed.Deriver != original.Deriver {
		t.Errorf("Deriver mismatch: got %s, want %s", parsed.Deriver, original.Deriver)
	}
	if parsed.System != original.System {
		t.Errorf("System mismatch: got %s, want %s", parsed.System, original.System)
	}
	if parsed.Sig != original.Sig {
		t.Errorf("Sig mismatch: got %s, want %s", parsed.Sig, original.Sig)
	}
}

func TestNarinfoEmptyReferences(t *testing.T) {
	ni := &Narinfo{
		StorePath:   "/nix/store/xxx-yyy",
		URL:         "nar/xxx.nar.zst",
		Compression: "zstd",
		FileHash:    "sha256:abc",
		FileSize:    100,
		NarHash:     "sha256:def",
		NarSize:     200,
		References:  nil,
	}

	text := ni.String()
	if !strings.Contains(text, "References:\n") {
		t.Errorf("expected empty References line, got:\n%s", text)
	}

	parsed, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(parsed.References) != 0 {
		t.Errorf("expected empty references, got %v", parsed.References)
	}
}

func TestNarinfoParseRealWorld(t *testing.T) {
	// Simulates a narinfo file from cache.nixos.org
	raw := `StorePath: /nix/store/abc123-hello-2.10
URL: nar/abc123.nar.xz
Compression: xz
FileHash: sha256:0abcdefghijklmnopqrstuvwxyz0abcdefghijklmnopqrstuv
FileSize: 42000
NarHash: sha256:1abcdefghijklmnopqrstuvwxyz1abcdefghijklmnopqrstuv
NarSize: 120000
References: def456-glibc-2.37 ghi789-hello-2.10
Deriver: jkl012-hello-2.10.drv
Sig: cache.nixos.org-1:signaturebase64data
System: x86_64-linux`

	parsed, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if parsed.StorePath != "/nix/store/abc123-hello-2.10" {
		t.Errorf("StorePath: got %s", parsed.StorePath)
	}
	if parsed.Compression != "xz" {
		t.Errorf("Compression: got %s", parsed.Compression)
	}
	if parsed.FileSize != 42000 {
		t.Errorf("FileSize: got %d", parsed.FileSize)
	}
	if parsed.NarSize != 120000 {
		t.Errorf("NarSize: got %d", parsed.NarSize)
	}
	if len(parsed.References) != 2 {
		t.Errorf("References: got %v", parsed.References)
	}
	if parsed.References[0] != "def456-glibc-2.37" {
		t.Errorf("Reference[0]: got %s", parsed.References[0])
	}
	if parsed.System != "x86_64-linux" {
		t.Errorf("System: got %s", parsed.System)
	}
}
