// Package hash implements the hashing and base32 encoding Nix uses for store
// paths and NAR digests.
//
// Nix's base32 is not RFC 4648: it uses its own 32-character alphabet and emits
// digits in reverse order. EncodeBase32 and DecodeBase32 implement that variant,
// so their output matches the hashes Nix itself prints.
package hash
