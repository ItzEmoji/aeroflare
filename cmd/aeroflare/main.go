// Command aeroflare is an OCI-backed Nix binary cache proxy and toolkit.
package main

import (
	"os"

	"github.com/itzemoji/aeroflare/v2/internal/aerocmd"
)

func main() {
	code := aerocmd.Main()
	os.Exit(int(code))
}
