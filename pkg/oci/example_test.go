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

	// A nil Authenticator reads anonymously, which is all a public cache needs.
	ni, err := oci.PullOCINativeManifest(storeHash, "ghcr.io", "itzemoji/aeroflare-cache", nil)
	if err != nil {
		// A miss is an error here: the tag simply does not exist.
		fmt.Println("not cached:", err)
		return
	}

	fmt.Println(ni.StorePath, ni.NarSize)
}

// Registries such as ghcr.io will not accept a personal access token as a
// bearer credential; they hand out a short-lived bearer token in exchange for
// one. Aeroflare does not perform that exchange itself. Describe the credential
// you hold, and pass the result to any function in this package that takes an
// authn.Authenticator: the exchange, and the refresh when the token expires,
// happen inside the transport.
func ExamplePasswordAuth() {
	auth := oci.PasswordAuth("itzemoji", "ghp_examplepersonalaccesstoken")

	ni, err := oci.PullOCINativeManifest("xn2nlmvng2im9mgrq46y3wkbz4ll1hnp", "ghcr.io", "itzemoji/aeroflare-cache", auth)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(ni.StorePath)
}
