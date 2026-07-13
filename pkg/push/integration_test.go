package push_test

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/itzemoji/aeroflare/pkg/oci"
	"github.com/itzemoji/aeroflare/pkg/prepare/narinfo"
	"github.com/itzemoji/aeroflare/pkg/proxy"
)

// authGatedRegistry is go-containerregistry's real registry implementation
// behind the Docker v2 token flow: unauthenticated requests get a Bearer
// challenge, the realm mints a token in exchange for Basic credentials, and only
// that token opens the registry.
//
// Crucially it rejects the PAT presented as a bearer, which is exactly what the
// GitHub Action used to do and what ghcr.io answered with a 401.
type authGatedRegistry struct {
	srv     *httptest.Server
	baseURL string

	// sawPATAsBearer records the bug: the credential arriving as a bearer token
	// rather than being exchanged for one.
	sawPATAsBearer atomic.Bool
}

func newAuthGatedRegistry(t *testing.T, pat string) *authGatedRegistry {
	t.Helper()

	g := &authGatedRegistry{}
	const minted = "minted-bearer-token"
	inner := registry.New(registry.Logger(nopLogger(t)))

	mux := http.NewServeMux()

	mux.HandleFunc("/auth/token", func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || pass != pat {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if user == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprintf(w, `{"token":%q,"expires_in":300}`, minted); err != nil {
			t.Errorf("writing token response: %v", err)
		}
	})

	mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")

		// The PAT sent verbatim as a bearer: the registry has no idea what it
		// is. This is the failure the Action hit.
		if auth == "Bearer "+pat {
			g.sawPATAsBearer.Store(true)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if auth != "Bearer "+minted {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(
				`Bearer realm="%s/auth/token",service="gated.registry"`, g.baseURL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		inner.ServeHTTP(w, r)
	})

	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)
	g.baseURL = g.srv.URL
	return g
}

// host returns the registry's host:port, a loopback address, so aeroflare talks
// to it over http.
func (g *authGatedRegistry) host(t *testing.T) string {
	t.Helper()
	u, err := url.Parse(g.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

func nopLogger(t *testing.T) *log.Logger {
	t.Helper()
	return log.New(io.Discard, "", 0)
}

// TestEndToEnd_PATIsExchanged_PushThenServe drives the whole path CI drives: a
// personal access token (the shape GitHub Actions hands out) pushes a package to
// a registry that demands a proper token exchange, and the proxy then serves
// that package's narinfo and NAR back over the Nix binary cache protocol.
//
// Before this change the push half failed: the PAT went to the registry as
// "Authorization: Bearer ghs_...", and the registry rejected it.
func TestEndToEnd_PATIsExchanged_PushThenServe(t *testing.T) {
	const (
		pat       = "ghs_actionsPersonalAccessToken"
		storeHash = "xn2nlmvng2im9mgrq46y3wkbz4ll1hnp"
		repo      = "me/nix-cache"
	)
	narBody := []byte("this is a compressed nar, honest")

	reg := newAuthGatedRegistry(t, pat)
	host := reg.host(t)

	// The credential as CI actually holds it: a PAT, which the registry must
	// exchange. Nothing here pre-exchanges it or inspects its prefix.
	auth := oci.PasswordAuth("x-access-token", pat)

	// --- push -------------------------------------------------------------
	layer := static.NewLayer(narBody, types.MediaType("application/vnd.aeroflare.nar.v1+zstd"))
	ni := &narinfo.Narinfo{
		StorePath:   "/nix/store/" + storeHash + "-hello-2.12.1",
		URL:         "nar/" + storeHash + ".nar.zst",
		Compression: "zstd",
		FileHash:    "sha256:1dyb7crbf67wyngrdgy8y1i09fhlkw6d3la2zkia75sm4qq8w1xh",
		FileSize:    int64(len(narBody)),
		NarHash:     "sha256:06v4v63xc818bc4csj49ri30my24hmpddhr2a2452q7jm10ijaim",
		NarSize:     4096,
	}

	if err := oci.PushNarPackage(layer, ni, storeHash, host, repo, auth); err != nil {
		t.Fatalf("push with a PAT failed: %v", err)
	}
	if reg.sawPATAsBearer.Load() {
		t.Fatal("the PAT was sent to the registry as a bearer token instead of being exchanged")
	}

	// --- serve ------------------------------------------------------------
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	port, err := proxy.StartProxy(ctx, 0, "127.0.0.1", host, repo, nil, auth)
	if err != nil {
		t.Fatalf("StartProxy: %v", err)
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	// The narinfo is reconstructed from the manifest annotations.
	body := getBody(t, base+"/"+storeHash+".narinfo")
	for _, want := range []string{
		"StorePath: /nix/store/" + storeHash + "-hello-2.12.1",
		"Compression: zstd",
		"URL: nar/" + storeHash + ".nar.zst",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("narinfo missing %q; got:\n%s", want, body)
		}
	}

	// The NAR itself streams straight out of the registry blob.
	if got := getBody(t, base+"/nar/"+storeHash+".nar.zst"); got != string(narBody) {
		t.Errorf("NAR body = %q, want %q", got, string(narBody))
	}

	if reg.sawPATAsBearer.Load() {
		t.Fatal("the proxy sent the PAT as a bearer token instead of exchanging it")
	}
}

func getBody(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d, body %s", url, resp.StatusCode, b)
	}
	return string(b)
}
