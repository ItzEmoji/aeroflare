package auth_test

import (
	"fmt"
	"testing"

	"github.com/itzemoji/aeroflare/internal/auth"
	"github.com/itzemoji/aeroflare/internal/secrets"
)

// A wrapped "not found" from the secrets manager must be treated as an absent
// field (skipped), not surfaced as a hard error from Service.Resolve.
func TestServiceResolve_WrappedNotFoundSkipsField(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	mock := &mockSecretsManager{
		data:   map[string]string{},
		getErr: fmt.Errorf("keychain read failed: %w", secrets.ErrNotFound),
	}
	svc, _ := auth.ServiceByID("github")

	vals, err := svc.Resolve(mock)
	if err != nil {
		t.Fatalf("wrapped not-found should be treated as absent, got error: %v", err)
	}
	if len(vals) != 0 {
		t.Errorf("expected no resolved fields, got %v", vals)
	}
}

func TestServiceByID_Known(t *testing.T) {
	for _, id := range []string{"github", "gitlab", "cloudflare"} {
		svc, ok := auth.ServiceByID(id)
		if !ok {
			t.Fatalf("expected service %q to exist", id)
		}
		if svc.ID != id {
			t.Errorf("expected ID %q, got %q", id, svc.ID)
		}
		if len(svc.Fields) == 0 {
			t.Errorf("service %q has no fields", id)
		}
	}
}

func TestServiceByID_Unknown(t *testing.T) {
	if _, ok := auth.ServiceByID("nope"); ok {
		t.Fatalf("expected unknown service to return ok=false")
	}
}

func TestGithubServiceShape(t *testing.T) {
	svc, _ := auth.ServiceByID("github")
	if len(svc.Fields) != 1 {
		t.Fatalf("expected github to have 1 field, got %d", len(svc.Fields))
	}
	f := svc.Fields[0]
	if f.SecretKey != "github-token" {
		t.Errorf("expected secret key github-token, got %s", f.SecretKey)
	}
	if !f.Secret {
		t.Errorf("expected github token to be marked secret")
	}
	if len(f.EnvVars) < 2 || f.EnvVars[0] != "GITHUB_TOKEN" || f.EnvVars[1] != "GH_TOKEN" {
		t.Errorf("expected env vars [GITHUB_TOKEN GH_TOKEN], got %v", f.EnvVars)
	}
}

func TestCloudflareServiceShape(t *testing.T) {
	svc, _ := auth.ServiceByID("cloudflare")
	if len(svc.Fields) != 2 {
		t.Fatalf("expected cloudflare to have 2 fields, got %d", len(svc.Fields))
	}
	keys := map[string]auth.Field{}
	for _, f := range svc.Fields {
		keys[f.Name] = f
	}
	if keys["token"].SecretKey != "cf-token" || !keys["token"].Secret {
		t.Errorf("unexpected cloudflare token field: %+v", keys["token"])
	}
	if keys["account_id"].SecretKey != "cf-account-id" || keys["account_id"].Secret {
		t.Errorf("unexpected cloudflare account_id field: %+v", keys["account_id"])
	}
}

func TestServiceForRegistry_KnownAliases(t *testing.T) {
	if svc := auth.ServiceForRegistry("ghcr.io"); svc.ID != "github" {
		t.Errorf("expected ghcr.io -> github, got %s", svc.ID)
	}
	if svc := auth.ServiceForRegistry("registry.gitlab.com"); svc.ID != "gitlab" {
		t.Errorf("expected registry.gitlab.com -> gitlab, got %s", svc.ID)
	}
}

func TestServiceForRegistry_Generic(t *testing.T) {
	svc := auth.ServiceForRegistry("docker.io")
	if svc.ID != "oci:docker.io" {
		t.Errorf("expected oci:docker.io, got %s", svc.ID)
	}
	byName := map[string]auth.Field{}
	for _, f := range svc.Fields {
		byName[f.Name] = f
	}
	if byName["username"].SecretKey != "oci-docker.io-username" {
		t.Errorf("expected username key oci-docker.io-username, got %s", byName["username"].SecretKey)
	}
	if byName["token"].SecretKey != "oci-docker.io-token" || !byName["token"].Secret {
		t.Errorf("unexpected generic oci token field: %+v", byName["token"])
	}
}

func TestServiceForSecretKey(t *testing.T) {
	cases := map[string]string{
		"github-token":                   "github",
		"gitlab-token":                   "gitlab",
		"cf-token":                       "cloudflare",
		"cf-account-id":                  "cloudflare",
		"oci-docker.io-username":         "oci:docker.io",
		"oci-registry.example.com-token": "oci:registry.example.com",
	}
	for key, wantID := range cases {
		svc, ok := auth.ServiceForSecretKey(key)
		if !ok {
			t.Errorf("expected key %q to map to a service", key)
			continue
		}
		if svc.ID != wantID {
			t.Errorf("key %q: expected service %q, got %q", key, wantID, svc.ID)
		}
	}
	if _, ok := auth.ServiceForSecretKey("random-legacy-key"); ok {
		t.Errorf("expected unknown key to return ok=false")
	}
}

func TestFieldResolve_EnvBeatsSecret(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-tok")
	mock := &mockSecretsManager{data: map[string]string{"github-token": "secret-tok"}}
	svc, _ := auth.ServiceByID("github")
	val, err := svc.Fields[0].Resolve(mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "env-tok" {
		t.Errorf("expected env-tok, got %s", val)
	}
}

func TestServiceResolve_ReturnsFoundFields(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "")
	mock := &mockSecretsManager{data: map[string]string{"cf-token": "tok"}}
	svc, _ := auth.ServiceByID("cloudflare")
	vals, err := svc.Resolve(mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vals["token"] != "tok" {
		t.Errorf("expected token=tok, got %q", vals["token"])
	}
	if _, ok := vals["account_id"]; ok {
		t.Errorf("expected account_id to be absent, got %q", vals["account_id"])
	}
}
