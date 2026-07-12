// Package oci stores and retrieves Nix binary cache artifacts in an OCI
// registry.
//
// Each Nix store path is one OCI image tagged with its 32-character store hash.
// The compressed NAR is pushed as a layer and the narinfo metadata is carried in
// the manifest annotations, so a substituter can resolve a store hash to its
// metadata with a single manifest fetch and no separate index.
//
// Every function takes the registry, repository, and bearer token explicitly.
// The package reads no configuration file, no environment variable, and no
// keyring: resolving credentials is the caller's job. The CLI's own resolution
// lives in pkg/cmdutil (RegistryAndRepository, RegistryToken) and is a working
// example of how to feed this package.
//
// ExchangeToken performs the Basic-to-Bearer token exchange that registries
// like ghcr.io require. Retries use DoWithRetry, which backs off on network
// errors, 429, and 5xx.
package oci
