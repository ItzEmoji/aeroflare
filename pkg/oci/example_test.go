package oci_test

import (
	"fmt"
	"log"

	"github.com/itzemoji/aeroflare/pkg/oci"
)

// Resolving a Nix store path's metadata takes a single manifest fetch: the
// store hash is the image tag, and the narinfo lives in the annotations.
func ExamplePullOCINativeManifest() {
	// The 32-character store hash of /nix/store/<hash>-<name>, used as the tag.
	const storeHash = "0nlp2xwzavr9dyrsdhcgnq2h4qxsi8bp"

	ni, err := oci.PullOCINativeManifest(storeHash, "ghcr.io", "itzemoji/aeroflare-cache", "")
	if err != nil {
		// A miss is an error here: the tag simply does not exist.
		fmt.Println("not cached:", err)
		return
	}

	fmt.Println(ni.StorePath, ni.NarSize)
}

// Registries such as ghcr.io will not accept a personal access token directly;
// they hand out a short-lived bearer token in exchange for one. The token comes
// back scoped to a single repository.
func ExampleExchangeToken() {
	bearer, err := oci.ExchangeToken("ghcr.io", "itzemoji/aeroflare-cache", "itzemoji", "ghp_examplepersonalaccesstoken")
	if err != nil {
		log.Fatal(err)
	}

	// Pass the bearer to any of this package's functions as the token argument.
	// It expires: fetch a new one for a long-running operation rather than
	// holding this one.
	_ = bearer
}
