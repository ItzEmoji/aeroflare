// Package compress wraps the compression algorithms a Nix binary cache may use
// for NAR files: zstd, xz, gzip, or none.
//
// ParseType maps a user-supplied name to a Type, and a Type knows the file
// extension and the narinfo Compression field value that go with it, so a
// caller does not have to keep those three spellings in sync by hand.
package compress
