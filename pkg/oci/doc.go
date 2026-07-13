// Package oci stores and retrieves Nix binary cache artifacts in an OCI
// registry.
//
// Each Nix store path is one OCI image tagged with its 32-character store hash.
// The compressed NAR is pushed as a layer and the narinfo metadata is carried in
// the manifest annotations, so a substituter can resolve a store hash to its
// metadata with a single manifest fetch and no separate index.
//
// Every function takes the registry, repository, and credential explicitly. The
// package reads no configuration file, no environment variable, and no keyring:
// resolving credentials is the caller's job. The CLI's own resolution lives in
// pkg/cmdutil (RegistryAndRepository, RegistryAuth) and is a working example of
// how to feed this package.
//
// # Credentials
//
// A credential is an authn.Authenticator. Build one with PasswordAuth from a
// password or personal access token; the registry exchange, and the refresh when
// the resulting token expires, happen inside the transport, so no caller here
// tracks token lifetimes. BearerAuth exists for a caller that already holds an
// exchanged token. A nil Authenticator reads anonymously.
//
// Aeroflare implements none of that exchange itself: it is a protocol, and
// go-containerregistry implements it. That is also what makes a registry other
// than ghcr.io a matter of configuration rather than code -- see auth.go.
package oci
