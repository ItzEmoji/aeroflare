package main

import (
	"aeroflare/cmd"
	_ "embed"
)

//go:embed version.json
var versionJSON []byte

func main() {
	cmd.VersionJSON = versionJSON
	cmd.Execute()
}
