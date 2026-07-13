package ci

import "testing"

func TestParseCacheSpec(t *testing.T) {
	cases := []struct {
		in      string
		reg     string
		repo    string
		wantErr bool
	}{
		{"ghcr.io;itzemoji/nix-cache", "ghcr.io", "itzemoji/nix-cache", false},
		{"docker.io;org/repo", "docker.io", "org/repo", false},
		{"localhost:5000;foo/bar", "localhost:5000", "foo/bar", false},
		{" ghcr.io ; itzemoji/cache ", "ghcr.io", "itzemoji/cache", false},
		{"https://ghcr.io;itzemoji/nix-cache", "ghcr.io", "itzemoji/nix-cache", false},
		{"http://localhost:5000;foo/bar", "localhost:5000", "foo/bar", false},
		{"ghcr.io/itzemoji/nix-cache", "", "", true},
		{"ghcr.io;", "", "", true},
		{";repo", "", "", true},
	}
	for _, c := range cases {
		got, err := ParseCacheSpec(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseCacheSpec(%q): expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseCacheSpec(%q): unexpected error %v", c.in, err)
			continue
		}
		if got.Registry != c.reg || got.Repository != c.repo {
			t.Errorf("ParseCacheSpec(%q) = %+v, want reg=%s repo=%s", c.in, got, c.reg, c.repo)
		}
	}
}

func TestTokenEnvVar(t *testing.T) {
	cases := map[string]string{
		"ghcr.io":        "AEROFLARE_TOKEN_GHCR_IO",
		"docker.io":      "AEROFLARE_TOKEN_DOCKER_IO",
		"localhost:5000": "AEROFLARE_TOKEN_LOCALHOST_5000",
	}
	for in, want := range cases {
		if got := TokenEnvVar(in); got != want {
			t.Errorf("TokenEnvVar(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveToken(t *testing.T) {
	t.Run("override wins", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "gh")
		t.Setenv("AEROFLARE_TOKEN_GHCR_IO", "override")
		if got := ResolveToken("ghcr.io"); got != "override" {
			t.Errorf("got %q, want override", got)
		}
	})
	t.Run("ghcr default", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "gh")
		if got := ResolveToken("ghcr.io"); got != "gh" {
			t.Errorf("got %q, want gh", got)
		}
	})
	t.Run("none", func(t *testing.T) {
		if got := ResolveToken("docker.io"); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}
