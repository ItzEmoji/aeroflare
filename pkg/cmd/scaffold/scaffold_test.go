package scaffold

import "testing"

func TestSetWranglerVar(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		key   string
		value string
		want  string
	}{
		{
			name:  "an existing assignment is replaced in place",
			body:  "name = \"proxy\"\nNIXCACHE_REPO = \"old/repo\"\n",
			key:   "NIXCACHE_REPO",
			value: "new/repo",
			want:  "name = \"proxy\"\nNIXCACHE_REPO = \"new/repo\"\n",
		},
		{
			name:  "a commented placeholder is replaced too",
			body:  "name = \"proxy\"\n# NIXCACHE_REPO = \"foo/bar\"\n",
			key:   "NIXCACHE_REPO",
			value: "new/repo",
			want:  "name = \"proxy\"\nNIXCACHE_REPO = \"new/repo\"\n",
		},
		{
			name:  "an absent key is appended",
			body:  "name = \"proxy\"\n",
			key:   "NIXCACHE_REPO",
			value: "new/repo",
			want:  "name = \"proxy\"\nNIXCACHE_REPO = \"new/repo\"\n",
		},
		{
			name:  "a value containing quotes is written through verbatim",
			body:  "name = \"proxy\"\n",
			key:   "NIXCACHE_REPO",
			value: `foo"bar`,
			want:  "name = \"proxy\"\nNIXCACHE_REPO = \"foo\"bar\"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := setWranglerVar(tt.body, tt.key, tt.value); got != tt.want {
				t.Errorf("setWranglerVar() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWorkerRegistryURL(t *testing.T) {
	tests := []struct {
		name     string
		registry string
		want     string
	}{
		{name: "a bare host gains an https scheme", registry: "ghcr.io", want: "https://ghcr.io"},
		{name: "a non-ghcr host gains one too", registry: "registry.example.com", want: "https://registry.example.com"},
		{name: "an existing scheme is preserved", registry: "https://registry.example.com", want: "https://registry.example.com"},
		{name: "a plain-http scheme is preserved", registry: "http://localhost:5000", want: "http://localhost:5000"},
		{name: "a trailing slash is trimmed", registry: "ghcr.io/", want: "https://ghcr.io"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := workerRegistryURL(tt.registry); got != tt.want {
				t.Errorf("workerRegistryURL(%q) = %q, want %q", tt.registry, got, tt.want)
			}
		})
	}
}
