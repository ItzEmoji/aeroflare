package oci

import (
	"context"
	"net/http"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

// Aeroflare does not implement the registry token exchange itself. A caller
// supplies an authn.Authenticator describing the credential it holds, and
// go-containerregistry's transport does the rest: it pings /v2/, reads the
// realm and service out of the WWW-Authenticate challenge, exchanges a
// username/password for a bearer token, and re-authenticates whenever the
// registry challenges again (an expired token, or a scope it needs to widen).
//
// This is what makes a registry other than ghcr.io a matter of configuration
// rather than code: the exchange is a protocol, and the protocol is already
// implemented. The three credential shapes below cover it.
//
//   - PasswordAuth: a username and a password/PAT, exchanged for a bearer.
//     GitHub's ghp_/ghs_, GitLab's glpat-, Docker Hub's dckr_pat_ and a plain
//     registry password all take this path. Nothing inspects the prefix.
//   - BearerAuth: a token that has already been exchanged, sent verbatim.
//   - authn.Anonymous: no credential; reads from a public cache still work.

// PasswordAuth returns an Authenticator for a username and password (or
// personal access token), which the registry exchanges for a bearer token.
//
// username matters to registries that check it (Docker Hub) and is ignored by
// those that do not (ghcr.io accepts any non-empty value); it defaults to
// "token" when empty, which is what ghcr.io's own documentation uses.
func PasswordAuth(username, password string) authn.Authenticator {
	if username == "" {
		username = "token"
	}
	return authn.FromConfig(authn.AuthConfig{Username: username, Password: password})
}

// BearerAuth returns an Authenticator for a token that is already a registry
// bearer token, so no exchange is performed. This is the escape hatch for a
// caller that did its own exchange, or holds a token the registry accepts
// directly (for ghcr.io, a base64-encoded PAT).
//
// Prefer PasswordAuth: a credential that needs exchanging and is sent as a
// bearer is rejected by the registry, and that mistake is not detectable here.
func BearerAuth(token string) authn.Authenticator {
	return &authn.Bearer{Token: token}
}

// authOrAnonymous substitutes anonymous access for a nil Authenticator, so
// every caller can treat "no credential" as a nil value.
func authOrAnonymous(auth authn.Authenticator) authn.Authenticator {
	if auth == nil {
		return authn.Anonymous
	}
	return auth
}

// remoteAuth returns the remote.Option carrying auth, treating nil as anonymous.
func remoteAuth(auth authn.Authenticator) remote.Option {
	return remote.WithAuth(authOrAnonymous(auth))
}

// Puller returns a remote.Puller that reads with auth over the shared tuned
// transport, so a run of manifest and blob fetches shares one auth handshake
// instead of re-challenging on each.
func Puller(auth authn.Authenticator) (*remote.Puller, error) {
	return remote.NewPuller(
		remote.WithTransport(optimizedTransport),
		remoteAuth(auth),
	)
}

// Repository parses registry and repository into a name.Repository, marking it
// insecure for loopback registries so http is used (see GetProtocol).
func Repository(registry, repository string) (name.Repository, error) {
	var opts []name.Option
	if GetProtocol(registry) == "http" {
		opts = append(opts, name.Insecure)
	}
	return name.NewRepository(registry+"/"+repository, opts...)
}

// retryBase wraps t so idempotent requests retry with bounded exponential
// backoff on connection errors, 429, and 5xx. Registry auth is layered on top
// of this, matching how go-containerregistry's own remote package orders them.
func retryBase(t http.RoundTripper) http.RoundTripper {
	return transport.NewRetry(t,
		transport.WithRetryBackoff(transport.Backoff{
			Duration: 500 * time.Millisecond,
			Factor:   2.0,
			Jitter:   0.5,
			Steps:    3,
		}),
		transport.WithRetryStatusCodes(
			http.StatusRequestTimeout,
			http.StatusTooManyRequests,
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout,
		),
	)
}

// Transport returns a RoundTripper that authenticates every request to repo
// with auth, negotiating the credential with the registry on first use and
// refreshing it whenever the registry challenges. scopes defaults to pull.
//
// The returned transport is bound to repo: it must not be used to talk to a
// different repository, whose scopes it has not requested.
func Transport(ctx context.Context, repo name.Repository, auth authn.Authenticator, scopes ...string) (http.RoundTripper, error) {
	if len(scopes) == 0 {
		scopes = []string{repo.Scope(transport.PullScope)}
	}
	return transport.NewWithContext(ctx, repo.Registry, authOrAnonymous(auth), retryBase(optimizedTransport), scopes)
}

// Client is Transport wrapped in an *http.Client with the given timeout (zero
// means no timeout). Requests made with it carry registry credentials, so it
// must not be pointed at a host other than repo's registry.
func Client(ctx context.Context, repo name.Repository, auth authn.Authenticator, timeout time.Duration, scopes ...string) (*http.Client, error) {
	rt, err := Transport(ctx, repo, auth, scopes...)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: rt, Timeout: timeout}, nil
}

// PullScope and PushScope name the access levels a Transport can request.
// PushScope implies pull.
var (
	PullScope = transport.PullScope
	PushScope = transport.PushScope
)
