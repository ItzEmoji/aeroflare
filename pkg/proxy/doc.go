// Package proxy serves a Nix binary cache backed by an OCI registry.
//
// StartProxy runs an HTTP server that speaks the Nix substituter protocol
// (/nix-cache-info, /<hash>.narinfo, /nar/<...>) and answers each request from
// the registry: a narinfo lookup reads the manifest annotations for the store
// hash, and a NAR fetch streams the layer blob straight through to the client
// without buffering it on disk. Requests the registry cannot satisfy fall
// through to the configured upstream caches.
//
// The server holds no local binary state, which is what makes it viable to run
// as a sidecar or inside a Cloudflare Worker.
//
// TokenManager handles registry authentication, caching the bearer token until
// shortly before the registry says it expires. It does not read the
// environment: a caller that already has a verbatim bearer token passes it to
// SetOverrideToken (StartProxy takes one as a parameter), and anything that is
// not a usable bearer, such as a raw GitHub PAT, is rejected by IsBearerToken
// so it falls through to normal token exchange instead of being sent as an
// invalid Authorization header.
package proxy
