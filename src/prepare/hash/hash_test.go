package hash

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

func TestEncodeBase32Length(t *testing.T) {
	tests := []struct {
		inputLen  int
		outputLen int
	}{
		{20, 32}, // SHA-1
		{32, 52}, // SHA-256
	}
	for _, tt := range tests {
		data := make([]byte, tt.inputLen)
		encoded := EncodeBase32(data)
		if len(encoded) != tt.outputLen {
			t.Errorf("EncodeBase32(%d bytes) = %d chars, want %d", tt.inputLen, len(encoded), tt.outputLen)
		}
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	tests := [][]byte{
		make([]byte, 20), // SHA-1 zeros
		make([]byte, 32), // SHA-256 zeros
		bytes.Repeat([]byte{0xFF}, 20),
		bytes.Repeat([]byte{0xFF}, 32),
		{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a,
			0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14},
	}

	for _, data := range tests {
		encoded := EncodeBase32(data)
		decoded, err := DecodeBase32(encoded)
		if err != nil {
			t.Errorf("DecodeBase32(%s) error: %v", encoded, err)
			continue
		}
		if !bytes.Equal(data, decoded) {
			t.Errorf("round-trip mismatch:\n  input:  %x\n  encoded: %s\n  decoded: %x", data, encoded, decoded)
		}
	}
}

func TestEncodeBase32SHA256(t *testing.T) {
	// SHA-256 of empty string
	h := sha256.Sum256(nil)
	encoded := EncodeBase32(h[:])
	if len(encoded) != 52 {
		t.Fatalf("expected 52 chars, got %d", len(encoded))
	}

	// Verify each char is in the base32 alphabet
	validChars := make(map[byte]bool)
	for _, c := range []byte(base32Chars) {
		validChars[c] = true
	}
	for i, c := range []byte(encoded) {
		if !validChars[c] {
			t.Errorf("invalid char %c at position %d in %s", c, i, encoded)
		}
	}

	// Decode back and verify
	decoded, err := DecodeBase32(encoded)
	if err != nil {
		t.Fatalf("DecodeBase32 error: %v", err)
	}
	if !bytes.Equal(h[:], decoded) {
		t.Errorf("round-trip mismatch for SHA-256 of empty string")
	}
}

func TestDecodeBase32InvalidChar(t *testing.T) {
	_, err := DecodeBase32("eoooooooooooooooooooooooooooooooo") // 'e' is not in the alphabet
	if err == nil {
		t.Error("expected error for invalid char 'e'")
	}
}

func TestSHA256(t *testing.T) {
	data := []byte("hello")
	expected := sha256.Sum256(data)
	got := SHA256(data)
	if !bytes.Equal(expected[:], got) {
		t.Errorf("SHA256 mismatch: got %x, want %x", got, expected[:])
	}
}
