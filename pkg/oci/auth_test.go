package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
)

// fakeRegistry speaks the Docker v2 token flow: /v2/ answers with a Bearer
// challenge naming a realm, the realm mints a bearer in exchange for Basic
// credentials, and the data endpoint accepts only that bearer.
//
// The realm sits at /auth/mint rather than the conventional /token, which is
// the point: a client must read the realm out of the challenge. The
// TokenManager this replaces hardcoded https://<registry>/token and never
// pinged, so it could not talk to any registry that advertises a realm
// elsewhere -- Docker Hub answers registry-1.docker.io challenges with a realm
// on auth.docker.io. (A realm on a genuinely different host cannot be
// exercised here: go-containerregistry refuses a realm pointing at a private
// address unless it is the registry itself, an SSRF guard that real registries
// satisfy by being https.)
type fakeRegistry struct {
	registry *httptest.Server
	// baseURL is the server's own URL, set once before any request is served
	// so the challenge can name a realm on it.
	baseURL string

	// basicSeen records the Basic credentials presented to the token endpoint.
	basicSeen atomic.Pointer[[2]string]
	// authSeen records the Authorization header the data endpoint received.
	authSeen atomic.Pointer[string]
	// exchanges counts token mints, to prove caching/refresh behavior.
	exchanges atomic.Int32
	// bearer is the token the data endpoint accepts. Changing it mid-test
	// simulates expiry: the old token stops working and must be re-fetched.
	bearer atomic.Pointer[string]
}

func newFakeRegistry(t *testing.T) *fakeRegistry {
	t.Helper()
	f := &fakeRegistry{}
	f.setBearer("bearer-v1")

	mux := http.NewServeMux()

	mux.HandleFunc("/auth/mint", func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); ok {
			f.basicSeen.Store(&[2]string{user, pass})
		}
		f.exchanges.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      *f.bearer.Load(),
			"expires_in": 300,
		})
	})

	mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		// The ping and every data request share the same challenge: a client
		// holding no valid bearer is told where to go and get one.
		got := r.Header.Get("Authorization")
		if r.URL.Path != "/v2/" {
			f.authSeen.Store(&got)
		}
		if got != "Bearer "+*f.bearer.Load() {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(
				`Bearer realm="%s/auth/mint",service="fake.registry",scope="repository:me/cache:pull"`,
				f.baseURL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	f.registry = httptest.NewServer(mux)
	t.Cleanup(f.registry.Close)
	f.baseURL = f.registry.URL

	return f
}

func (f *fakeRegistry) setBearer(tok string) { f.bearer.Store(&tok) }

// host returns the registry's host:port, which is a loopback address, so
// GetProtocol selects http and name.Insecure is applied.
func (f *fakeRegistry) host(t *testing.T) string {
	t.Helper()
	u, err := url.Parse(f.registry.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

// get issues an authenticated request to a repository path via oci.Client.
func (f *fakeRegistry) get(t *testing.T, auth authn.Authenticator) *http.Response {
	t.Helper()
	repo, err := Repository(f.host(t), "me/cache")
	if err != nil {
		t.Fatal(err)
	}
	client, err := Client(context.Background(), repo, auth, 10*time.Second)
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	resp, err := client.Get(f.registry.URL + "/v2/me/cache/manifests/abc")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// TestPasswordAuth_IsExchangedNotSentVerbatim is the regression test for the
// GitHub Action failure: CI resolved a GITHUB_TOKEN (a ghs_ PAT) and it was
// sent to the registry as "Authorization: Bearer ghs_...", which ghcr.io
// rejects. A password must be exchanged at the token endpoint, and the PAT
// itself must never appear in a request to the registry.
func TestPasswordAuth_IsExchangedNotSentVerbatim(t *testing.T) {
	f := newFakeRegistry(t)
	const pat = "ghs_actionsToken"

	resp := f.get(t, PasswordAuth("x-access-token", pat))

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	basic := f.basicSeen.Load()
	if basic == nil {
		t.Fatal("token endpoint never saw Basic credentials: the PAT was not exchanged")
	}
	if basic[0] != "x-access-token" || basic[1] != pat {
		t.Errorf("token endpoint got Basic %q/%q, want x-access-token/%s", basic[0], basic[1], pat)
	}
	sent := f.authSeen.Load()
	if sent == nil {
		t.Fatal("registry saw no Authorization header")
	}
	if strings.Contains(*sent, pat) {
		t.Errorf("PAT was sent verbatim to the registry: %q", *sent)
	}
	if *sent != "Bearer bearer-v1" {
		t.Errorf("registry got %q, want the exchanged bearer", *sent)
	}
}

// TestPasswordAuth_DoesNotSniffTokenPrefixes: every credential shape takes the
// same path. The old code branched on ghp_/glpat-/dckr_pat_/eyJ prefixes and
// guessed wrong for anything it had not enumerated.
func TestPasswordAuth_DoesNotSniffTokenPrefixes(t *testing.T) {
	for _, pw := range []string{"ghp_classic", "ghs_actions", "glpat-gitlab", "dckr_pat_hub", "eyJ.looks.like.a.jwt", "plain-harbor-password"} {
		t.Run(pw, func(t *testing.T) {
			f := newFakeRegistry(t)
			resp := f.get(t, PasswordAuth("user", pw))
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			basic := f.basicSeen.Load()
			if basic == nil || basic[1] != pw {
				t.Fatalf("%q was not exchanged as a password", pw)
			}
		})
	}
}

// TestBearerAuth_IsSentVerbatim covers the oci_token / NIXCACHE_TOKEN escape
// hatch: an already-exchanged token is used as-is, with no trip to the token
// endpoint.
func TestBearerAuth_IsSentVerbatim(t *testing.T) {
	f := newFakeRegistry(t)

	resp := f.get(t, BearerAuth("bearer-v1"))

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if n := f.exchanges.Load(); n != 0 {
		t.Errorf("token endpoint was called %d times; a pre-exchanged bearer must be sent verbatim", n)
	}
}

// TestAnonymous_ReadsPublicRepository: a nil Authenticator means anonymous, and
// a registry that does not require credentials still serves the request.
func TestAnonymous_ReadsPublicRepository(t *testing.T) {
	f := newFakeRegistry(t)
	// A public registry mints a bearer for anyone who asks, without Basic auth.
	resp := f.get(t, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if basic := f.basicSeen.Load(); basic != nil {
		t.Errorf("anonymous request presented Basic credentials %q", basic[0])
	}
}

// TestExpiredToken_IsRefreshedAndRetried replaces the hand-rolled expiry
// bookkeeping that TokenManager used to do (cache a token, guess a TTL, refetch
// before it lapses). The registry simply challenges again when a token stops
// working, and the transport re-authenticates and retries the request.
func TestExpiredToken_IsRefreshedAndRetried(t *testing.T) {
	f := newFakeRegistry(t)
	repo, err := Repository(f.host(t), "me/cache")
	if err != nil {
		t.Fatal(err)
	}
	client, err := Client(context.Background(), repo, PasswordAuth("user", "pw"), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Get(f.registry.URL + "/v2/me/cache/manifests/abc")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first request: status = %d, want 200", resp.StatusCode)
	}

	// The registry now rejects the token the client is holding.
	f.setBearer("bearer-v2")

	resp, err = client.Get(f.registry.URL + "/v2/me/cache/manifests/abc")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("after expiry: status = %d, want 200 (the token was not refreshed)", resp.StatusCode)
	}
	if sent := f.authSeen.Load(); sent == nil || *sent != "Bearer bearer-v2" {
		t.Errorf("registry got %v after expiry, want the refreshed bearer", sent)
	}
}
