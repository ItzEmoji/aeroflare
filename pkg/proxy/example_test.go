package proxy_test

import (
	"context"
	"fmt"
	"log"

	"github.com/itzemoji/aeroflare/pkg/proxy"
)

// Running the substituter in-process. StartProxy returns as soon as the server
// is listening, reporting the port it actually bound — pass 0 to let the OS
// choose one, which is what makes this safe to run inside a test or a build
// wrapper.
func ExampleStartProxy() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // shuts the server down

	port, err := proxy.StartProxy(
		ctx,
		0,           // port: 0 means "pick a free one"
		"127.0.0.1", // listen address
		"ghcr.io",
		"itzemoji/aeroflare-cache",
		[]string{"https://cache.nixos.org"}, // upstreams for anything not in the registry
		"",                                  // a GitHub PAT, to be exchanged for a bearer
		"",                                  // or a verbatim bearer token, used as-is
	)
	if err != nil {
		log.Fatal(err)
	}

	// Point Nix at it: nix build --substituters http://127.0.0.1:<port>
	fmt.Printf("substituter listening on http://127.0.0.1:%d\n", port)
}

// A raw personal access token is not an OCI bearer token. Anything that fails
// this check falls through to normal token exchange rather than being sent as
// an invalid Authorization header.
func ExampleIsBearerToken() {
	fmt.Println(proxy.IsBearerToken("ghp_arawpersonalaccesstoken"))
	fmt.Println(proxy.IsBearerToken("eyJhbGciOiJSUzI1NiJ9.a-real-bearer"))
	// Output:
	// false
	// true
}
