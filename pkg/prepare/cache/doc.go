// Package cache queries upstream Nix binary caches, such as
// https://cache.nixos.org.
//
// Its purpose is negative lookup: before preparing and uploading a store path,
// ask whether an upstream cache already serves it, and skip it if so. ExistsBatch
// checks many hashes concurrently, and a Group fans the question out across
// several upstream caches at once.
package cache
