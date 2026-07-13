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
// Registry credentials are supplied as an authn.Authenticator (see pkg/oci),
// which StartProxy takes as a parameter rather than reading from the
// environment. The proxy performs no token bookkeeping of its own: the
// credential is negotiated with the registry on first use and refreshed
// whenever the registry says it has expired, inside the transport. A nil
// authenticator reads anonymously, which is all a public cache needs.
package proxy
