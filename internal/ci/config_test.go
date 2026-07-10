package ci

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestResolve_FileBaseWithDefaults(t *testing.T) {
	fc := FileConfig{Builds: []string{".#a"}, Caches: []string{"ghcr.io;o/c"}}
	spec, err := Resolve(fc, Inputs{})
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Builds) != 1 || spec.Builds[0] != ".#a" {
		t.Errorf("builds = %v", spec.Builds)
	}
	if len(spec.Caches) != 1 || spec.Caches[0].Registry != "ghcr.io" {
		t.Errorf("caches = %+v", spec.Caches)
	}
	if spec.Compression != "zstd" {
		t.Errorf("compression = %q, want zstd", spec.Compression)
	}
	if spec.Workers != 50 {
		t.Errorf("workers = %d, want 50", spec.Workers)
	}
	if len(spec.UpstreamCaches) != 1 || spec.UpstreamCaches[0] != "https://cache.nixos.org" {
		t.Errorf("upstream = %q", spec.UpstreamCaches)
	}
}

func TestResolve_InlineReplacesLists(t *testing.T) {
	fc := FileConfig{Builds: []string{".#a"}, Caches: []string{"ghcr.io;o/c"}, Compression: "xz"}
	in := Inputs{Builds: []string{".#b", ".#c"}, Caches: []string{"docker.io;o/d"}, Compression: "gzip", Workers: 4}
	spec, err := Resolve(fc, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Builds) != 2 || spec.Builds[0] != ".#b" {
		t.Errorf("builds = %v (inline should replace)", spec.Builds)
	}
	if len(spec.Caches) != 1 || spec.Caches[0].Registry != "docker.io" {
		t.Errorf("caches = %+v (inline should replace)", spec.Caches)
	}
	if spec.Compression != "gzip" {
		t.Errorf("compression = %q, want gzip", spec.Compression)
	}
	if spec.Workers != 4 {
		t.Errorf("workers = %d, want 4", spec.Workers)
	}
}

func TestResolve_MissingBuilds(t *testing.T) {
	_, err := Resolve(FileConfig{Caches: []string{"ghcr.io;o/c"}}, Inputs{})
	if err == nil {
		t.Fatal("expected error for missing builds")
	}
}

func TestResolve_MissingCaches(t *testing.T) {
	_, err := Resolve(FileConfig{Builds: []string{".#a"}}, Inputs{})
	if err == nil {
		t.Fatal("expected error for missing caches")
	}
}

func TestResolve_BadCache(t *testing.T) {
	_, err := Resolve(FileConfig{Builds: []string{".#a"}, Caches: []string{"nope"}}, Inputs{})
	if err == nil {
		t.Fatal("expected error for malformed cache")
	}
}

func TestLoadFile_MissingOptional(t *testing.T) {
	fc, found, err := LoadFile("/nonexistent/aeroflare-ci.yaml", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("found should be false")
	}
	if len(fc.Builds) != 0 {
		t.Error("expected empty config")
	}
}

func TestStringListUnmarshal_Scalar(t *testing.T) {
	var fc FileConfig
	if err := yaml.Unmarshal([]byte("upstream-cache: none\n"), &fc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(fc.UpstreamCaches) != 1 || fc.UpstreamCaches[0] != "none" {
		t.Errorf("got %v, want [none]", fc.UpstreamCaches)
	}
}

func TestStringListUnmarshal_Sequence(t *testing.T) {
	var fc FileConfig
	y := "upstream-cache:\n  - https://a\n  - https://b\n"
	if err := yaml.Unmarshal([]byte(y), &fc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(fc.UpstreamCaches) != 2 || fc.UpstreamCaches[1] != "https://b" {
		t.Errorf("got %v, want [https://a https://b]", fc.UpstreamCaches)
	}
}

func TestStringListUnmarshal_RejectsMapping(t *testing.T) {
	var fc FileConfig
	err := yaml.Unmarshal([]byte("upstream-cache:\n  url: https://a\n"), &fc)
	if err == nil {
		t.Fatal("expected an error for a mapping node")
	}
}

func TestResolveUpstreams(t *testing.T) {
	tests := []struct {
		name    string
		raw     []string
		want    []string
		wantErr bool
	}{
		{name: "unset defaults to cache.nixos.org", raw: nil, want: []string{"https://cache.nixos.org"}},
		{name: "empty list is treated as unset", raw: []string{}, want: []string{"https://cache.nixos.org"}},
		{name: "none disables filtering", raw: []string{"none"}, want: nil},
		{name: "none is case insensitive", raw: []string{"NONE"}, want: nil},
		{name: "explicit list replaces the default", raw: []string{"https://my.cache"}, want: []string{"https://my.cache"}},
		{name: "several upstreams are kept in order", raw: []string{"https://a", "https://b"}, want: []string{"https://a", "https://b"}},
		{name: "blank entries are dropped", raw: []string{"  ", "https://a"}, want: []string{"https://a"}},
		{name: "none plus a url is an error", raw: []string{"none", "https://a"}, wantErr: true},
		{name: "url plus none is an error", raw: []string{"https://a", "none"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveUpstreams(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				if !strings.Contains(err.Error(), "cannot be combined") {
					t.Errorf("unexpected message: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveUpstreams: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestResolve_InputsOverrideFileUpstreams(t *testing.T) {
	fc := FileConfig{
		Builds:         []string{".#default"},
		Caches:         []string{"ghcr.io;me/cache"},
		UpstreamCaches: StringList{"https://from-file"},
	}
	spec, err := Resolve(fc, Inputs{UpstreamCaches: []string{"none"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(spec.UpstreamCaches) != 0 {
		t.Errorf("inline none must override the file, got %v", spec.UpstreamCaches)
	}
}
