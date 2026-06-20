package store

import (
	"testing"
)

func TestParsePath(t *testing.T) {
	tests := []struct {
		input    string
		wantHash string
		wantName string
		wantErr  bool
	}{
		{
			input:    "/nix/store/0c2j6g2bxqzw7x9q6kbx3vrrj6yfj1vh-hello-2.10",
			wantHash: "0c2j6g2bxqzw7x9q6kbx3vrrj6yfj1vh",
			wantName: "hello-2.10",
		},
		{
			input:    "/nix/store/abc123-my-package-1.0.0-bin",
			wantHash: "abc123",
			wantName: "my-package-1.0.0-bin",
		},
		{
			input:   "/nix/store/nohyphenpath",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		hash, name, err := ParsePath(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParsePath(%q) expected error, got none", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParsePath(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if hash != tt.wantHash {
			t.Errorf("ParsePath(%q) hash = %s, want %s", tt.input, hash, tt.wantHash)
		}
		if name != tt.wantName {
			t.Errorf("ParsePath(%q) name = %s, want %s", tt.input, name, tt.wantName)
		}
	}
}

func TestParsePathTrailingSlash(t *testing.T) {
	hash, name, err := ParsePath("/nix/store/abc123-hello-2.10/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash != "abc123" {
		t.Errorf("hash = %s, want abc123", hash)
	}
	if name != "hello-2.10" {
		t.Errorf("name = %s, want hello-2.10", name)
	}
}
