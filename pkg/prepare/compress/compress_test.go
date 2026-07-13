package compress

import (
	"bytes"
	"compress/gzip"
	"io"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

func TestCompressRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		typ  Type
	}{
		{"none", None},
		{"gzip", Gzip},
		{"xz", Xz},
		{"zstd", Zstd},
	}

	data := bytes.Repeat([]byte("The quick brown fox jumps over the lazy dog. "), 100)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			w, err := NewWriter(&buf, tt.typ)
			if err != nil {
				t.Fatalf("NewWriter: %v", err)
			}

			if _, err := w.Write(data); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if err := w.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			// Verify hash and size
			if len(w.Hash()) != 32 {
				t.Errorf("expected 32-byte hash, got %d", len(w.Hash()))
			}
			if w.Size() == 0 {
				t.Error("expected non-zero compressed size")
			}

			// Decompress and verify content
			var reader io.Reader
			switch tt.typ {
			case None:
				reader = bytes.NewReader(buf.Bytes())
			case Gzip:
				r, err := gzip.NewReader(&buf)
				if err != nil {
					t.Fatalf("gzip reader: %v", err)
				}
				defer func() { _ = r.Close() }()
				reader = r
			case Xz:
				r, err := xz.NewReader(&buf)
				if err != nil {
					t.Fatalf("xz reader: %v", err)
				}
				reader = r
			case Zstd:
				r, err := zstd.NewReader(&buf)
				if err != nil {
					t.Fatalf("zstd reader: %v", err)
				}
				defer r.Close()
				reader = r
			}

			decompressed, err := io.ReadAll(reader)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}

			if !bytes.Equal(data, decompressed) {
				t.Errorf("round-trip data mismatch")
			}
		})
	}
}

func TestParseType(t *testing.T) {
	tests := []struct {
		input string
		want  Type
		err   bool
	}{
		{"none", None, false},
		{"xz", Xz, false},
		{"gzip", Gzip, false},
		{"gz", Gzip, false},
		{"zstd", Zstd, false},
		{"zst", Zstd, false},
		{"invalid", "", true},
	}
	for _, tt := range tests {
		got, err := ParseType(tt.input)
		if tt.err {
			if err == nil {
				t.Errorf("ParseType(%q) expected error, got none", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseType(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseType(%q) = %s, want %s", tt.input, got, tt.want)
		}
	}
}

func TestExtension(t *testing.T) {
	tests := []struct {
		typ  Type
		want string
	}{
		{None, "nar"},
		{Xz, "nar.xz"},
		{Gzip, "nar.gz"},
		{Zstd, "nar.zst"},
	}
	for _, tt := range tests {
		if got := tt.typ.Extension(); got != tt.want {
			t.Errorf("%s.Extension() = %s, want %s", tt.typ, got, tt.want)
		}
	}
}

func TestCompressEmptyInput(t *testing.T) {
	for _, typ := range []Type{None, Gzip, Xz, Zstd} {
		t.Run(string(typ), func(t *testing.T) {
			var buf bytes.Buffer
			w, err := NewWriter(&buf, typ)
			if err != nil {
				t.Fatalf("NewWriter: %v", err)
			}
			if err := w.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			// Even with empty input, hash should be valid
			if len(w.Hash()) != 32 {
				t.Errorf("expected 32-byte hash, got %d", len(w.Hash()))
			}
		})
	}
}
