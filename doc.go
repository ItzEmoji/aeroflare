// Package aeroflare is a high-performance OCI-backed Nix binary cache.
//
// Aeroflare bridges the Nix ecosystem and standard container registries
// (GitHub Container Registry, Docker Hub, GitLab) so a binary cache needs no
// dedicated infrastructure. Each Nix store path becomes one OCI image tagged
// with its 32-character store hash: the NAR is a layer and the narinfo metadata
// lives in the manifest annotations. Lookups by store hash are therefore O(1)
// and no separate metadata store exists.
//
// The command-line tool is the primary interface; see the README for usage.
// The packages under pkg/ are also importable as a Go library:
//
//	pkg/oci      registry client: token exchange, blobs, manifests
//	pkg/prepare  Nix store path -> NAR + narinfo (hashing, compression, signing)
//	pkg/push     the NAR/narinfo -> registry pipeline
//	pkg/proxy    the Nix substituter HTTP server
//	pkg/cmd/...  the cobra command tree, built on top of the above
//
// These packages take their configuration as explicit parameters. They do not
// read viper, the environment, or the OS keyring, and they do not write to
// stdout: resolving credentials and rendering progress are the caller's job.
// pkg/cmdutil holds the CLI's own answers to those questions and is a good
// worked example.
//
// # API stability
//
// The Go API under pkg/ is NOT covered by this module's semantic version.
//
// Aeroflare is versioned as a command-line tool. A release may change, rename,
// or remove any exported Go symbol without a major version bump, and the
// project makes no compatibility promise to importers. If these packages are
// useful to you, import them, but pin an exact version and expect to make
// adjustments when you upgrade.
package aeroflare
