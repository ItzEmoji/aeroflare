package cmdutil

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"

	"github.com/itzemoji/aeroflare/internal/auth"
	"github.com/itzemoji/aeroflare/pkg/oci"

	"github.com/spf13/viper"
)

// Registry and token resolution lives here, in the CLI layer, rather than in
// pkg/oci. pkg/oci takes registry, repository, and token as explicit
// parameters so that it can be embedded, tested, and driven with more than one
// set of credentials in a single process. Reaching into viper, the environment,
// and the OS keyring is a command-line concern, so it belongs on this side of
// the boundary.

// RegistryAndRepository derives the target registry and repository from viper
// config and the environment. An explicit cache-url (oci://registry/repo) wins;
// otherwise the configured cache name becomes the repository and the registry
// defaults to ghcr.io. Returns an error if neither is configured.
func RegistryAndRepository() (string, string, error) {
	registry := viper.GetString("registry")
	if registry == "" {
		registry = os.Getenv("NIXCACHE_REGISTRY")
	}
	if registry == "" {
		registry = "ghcr.io"
	}

	ociURL := viper.GetString("cache-url")
	var repository string

	if ociURL != "" {
		ociURL = strings.TrimPrefix(ociURL, "oci://")
		parts := strings.SplitN(ociURL, "/", 2)
		if len(parts) == 2 && strings.Contains(parts[0], ".") {
			registry = parts[0]
			repository = parts[1]
		} else {
			repository = ociURL
		}
	} else {
		cacheName := viper.GetString("cache")
		if cacheName == "" {
			cacheName = os.Getenv("NIXCACHE_REPO")
		}
		if cacheName == "" {
			return "", "", errors.New("AEROFLARE_CACHE or AEROFLARE_CACHE_URL configuration is required")
		}
		repository = strings.ToLower(cacheName)
	}

	return registry, repository, nil
}

// RegistryAuth resolves the credential for a registry and returns it as an
// authn.Authenticator, ready to hand to pkg/oci, pkg/push, or pkg/proxy. It
// returns nil when no credential can be found at all, which means anonymous
// access — enough to read a public cache, never enough to push.
//
// explicitToken, when non-empty, takes precedence over the environment and the
// keyring.
//
// Nothing here inspects the token's shape. Every credential the CLI can find --
// a flag, oci_token, GITHUB_TOKEN, the keyring -- is a password, and the
// registry is what turns passwords into bearer tokens. Guessing from a "ghp_"
// or "eyJ" prefix whether a given string had already been exchanged is how the
// wrong credential used to end up in an Authorization header.
//
// There is deliberately no way to inject a pre-exchanged bearer from the
// environment. The old oci_token override meant that, but oci_token is also
// declared in the credential catalog as a *password* source (see
// internal/auth), and auth login writes raw PATs into it -- so the override
// spent most of its life being handed a PAT and having to detect it by prefix.
// Embedders that genuinely hold a bearer can build oci.BearerAuth themselves.
func RegistryAuth(registry, explicitToken string) authn.Authenticator {
	token := explicitToken
	if token == "" {
		var err error
		token, err = auth.ResolveRegistryToken(registry)
		if err != nil && !errors.Is(err, auth.ErrTokenNotFound) {
			fmt.Fprintf(os.Stderr, "Warning: failed to resolve registry token: %v\n", err)
		}
	}
	if token == "" {
		return nil
	}

	// The username is only consulted by registries that check it: ghcr.io
	// ignores it, Docker Hub requires it to be the real account name.
	username, _ := auth.NewResolver(fmt.Sprintf("oci-%s-username", registry)).Resolve()
	if username == "" {
		username = os.Getenv("AEROFLARE_GIT_USERNAME")
	}

	return oci.PasswordAuth(username, token)
}
