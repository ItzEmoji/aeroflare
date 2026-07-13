// Package push uploads Nix store paths to an OCI registry as binary cache
// artifacts.
//
// The pipeline prepares each store path into a compressed NAR plus narinfo
// metadata (see pkg/prepare), filters out paths the registry or an upstream
// cache already has, and uploads the remainder in chunks. Receipts are flushed
// after each chunk, so an interrupted push keeps whatever it already uploaded.
// A per-path upload failure does not abort the run: failures are collected in
// PushResult.Failed and reported at the end.
//
// There are two entry points. RunPushTo drives the whole pipeline from a
// PushPlan. PreparedSet, from Prepare, generates the artifacts once and can
// then PushTo several registries without regenerating them.
//
// # Configuration and output
//
// A Target carries the destination and the credential, as an authn.Authenticator
// (see pkg/oci). Hand over the password or personal access token itself: the
// registry exchange happens in the transport, which also refreshes the resulting
// token when it expires, so a push large enough to outlive a token still
// finishes without the caller doing anything about it.
//
// This package writes nothing to stdout. All progress, warnings, and per-path
// failures go through a Reporter that the caller supplies, so an embedding
// program controls presentation entirely; pass a Reporter with empty methods
// for silence. The CLI's terminal implementation is pkg/cmd/push.NewUIReporter.
package push
