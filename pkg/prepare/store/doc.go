// Package store reads the local Nix store.
//
// A StoreBackend answers what Nix itself would: the closure of a path, its
// references, its NAR hash and size. ParsePath splits a /nix/store entry into
// its 32-character hash and its human-readable name.
//
// The backend is an interface so the store can be faked in tests; the default
// implementation shells out to the nix and nix-store commands.
package store
