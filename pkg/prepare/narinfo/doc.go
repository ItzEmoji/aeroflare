// Package narinfo parses and renders the .narinfo metadata files a Nix binary
// cache serves.
//
// A Narinfo describes one store path: where its NAR lives, its compression, its
// hashes and sizes, its references, and its signatures. Aeroflare stores these
// fields in an OCI manifest's annotations rather than as separate files, so this
// type is both what gets written to a .narinfo file and what is round-tripped
// through the registry.
package narinfo
