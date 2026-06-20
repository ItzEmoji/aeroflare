package hash

import (
	"crypto/sha256"
	"fmt"
)

const base32Chars = "0123456789abcdfghijklmnpqrsvwxyz"

var base32Index [256]byte

func init() {
	for i := range base32Index {
		base32Index[i] = 0xFF
	}
	for i, c := range []byte(base32Chars) {
		base32Index[c] = byte(i)
	}
}

// EncodeBase32 encodes a byte slice to Nix base32 encoding.
//
// Nix base32 is LSB-first: for each output character n, the bit position
// is (outputLen - n - 1) * 5, counting from the end of the input.
// The alphabet omits e, o, t, u to avoid visual ambiguity.
func EncodeBase32(data []byte) string {
	hashLen := len(data)
	if hashLen == 0 {
		return ""
	}
	outputLen := (hashLen*8-1)/5 + 1

	result := make([]byte, outputLen)
	for n := 0; n < outputLen; n++ {
		b := (outputLen - n - 1) * 5
		i := b / 8
		j := b % 8
		var c byte
		if i >= hashLen-1 {
			c = data[i] >> j
		} else {
			c = (data[i] >> j) | (data[i+1] << (8 - j))
		}
		result[n] = base32Chars[c&0x1f]
	}
	return string(result)
}

// DecodeBase32 decodes a Nix base32 string back to raw bytes.
func DecodeBase32(s string) ([]byte, error) {
	if len(s) == 0 {
		return nil, nil
	}
	outputLen := len(s) * 5 / 8
	result := make([]byte, outputLen)

	for n := 0; n < len(s); n++ {
		c := base32Index[s[n]]
		if c == 0xFF {
			return nil, fmt.Errorf("invalid base32 character: %c", s[n])
		}
		b := (len(s) - n - 1) * 5
		i := b / 8
		j := b % 8

		result[i] |= c << j
		if i+1 < outputLen && j > 3 {
			result[i+1] |= c >> (8 - j)
		}
	}

	return result, nil
}

// SHA256 computes the SHA-256 hash of data and returns the raw 32-byte digest.
func SHA256(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}
