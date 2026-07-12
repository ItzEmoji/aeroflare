// Package signing creates and verifies the ed25519 signatures Nix uses to
// authenticate binary cache entries.
//
// A signature covers a fingerprint (see Fingerprint) derived from the store
// path, its NAR hash and size, and its references, so a signature cannot be
// lifted from one store path onto another. Keys are the "name:base64" format
// nix-store --generate-binary-cache-key produces.
package signing
