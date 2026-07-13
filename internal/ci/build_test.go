package ci

import (
	"reflect"
	"testing"
)

func TestScrapeStorePaths(t *testing.T) {
	in := "" +
		"warning: something\n" +
		"/nix/store/aaa-foo\n" +
		"# a comment /nix/store/ignored\n" +
		"  /nix/store/bbb-bar  \n" +
		"not a store path\n" +
		"\n"
	got := scrapeStorePaths(in)
	want := []string{"/nix/store/aaa-foo", "/nix/store/bbb-bar"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scrapeStorePaths = %v, want %v", got, want)
	}
}

func TestScrapeStorePaths_None(t *testing.T) {
	if got := scrapeStorePaths("nothing here\n"); len(got) != 0 {
		t.Errorf("expected no paths, got %v", got)
	}
}

func TestProxyNixConfig(t *testing.T) {
	if got := proxyNixConfig("", 0); got != "" {
		t.Errorf("port 0 with empty existing = %q, want empty", got)
	}
	if got := proxyNixConfig("keep-me = 1", 0); got != "keep-me = 1" {
		t.Errorf("port 0 should return existing unchanged, got %q", got)
	}
	if got := proxyNixConfig("", 38411); got != "extra-substituters = http://127.0.0.1:38411" {
		t.Errorf("got %q", got)
	}
	if got := proxyNixConfig("a = 1", 38411); got != "a = 1\nextra-substituters = http://127.0.0.1:38411" {
		t.Errorf("got %q", got)
	}
}

func TestDedupPaths(t *testing.T) {
	got := dedupPaths([]string{"/a", "/b", "/a", "/c", "/b"})
	want := []string{"/a", "/b", "/c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
		}
	}
}
