// Package build holds version metadata for aeroflare binaries. Version and
// Date are set at link time via `-ldflags -X`, computed by scripts/build.go
// and baked in by the Makefile, the release workflow, and default.nix.
package build

import "runtime/debug"

// Version defaults to "dev" for unversioned local builds. It falls back to
// the module version recorded by `go install pkg@version` when no ldflags
// were provided at build time.
var Version = "dev"

// Date is the build date (YYYY-MM-DD), set via `-ldflags -X` at build time.
var Date = ""

func init() {
	if Version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			Version = info.Main.Version
		}
	}
}
