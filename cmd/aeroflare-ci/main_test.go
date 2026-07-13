package main

import (
	"reflect"
	"testing"
)

func TestSplitEnvList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{".#a\n.#b", []string{".#a", ".#b"}},
		{".#a, .#b , .#c", []string{".#a", ".#b", ".#c"}},
		{"\n\n.#only\n", []string{".#only"}},
	}
	for _, c := range cases {
		if got := splitEnvList(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitEnvList(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestStringListSet(t *testing.T) {
	var s stringList
	_ = s.Set("a")
	_ = s.Set("b")
	if !reflect.DeepEqual([]string(s), []string{"a", "b"}) {
		t.Errorf("got %v", s)
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("AEROFLARE_CI_TESTVAR", "x")
	if got := envOr("AEROFLARE_CI_TESTVAR", "def"); got != "x" {
		t.Errorf("got %q", got)
	}
	if got := envOr("AEROFLARE_CI_UNSET_VAR", "def"); got != "def" {
		t.Errorf("got %q", got)
	}
}

func TestSplitEnvList_UpstreamCaches(t *testing.T) {
	got := splitEnvList("https://cache.nixos.org\nhttps://my.cache\n")
	if len(got) != 2 || got[0] != "https://cache.nixos.org" || got[1] != "https://my.cache" {
		t.Errorf("got %v, want two upstream URLs", got)
	}
}

func TestSplitEnvList_SingleNone(t *testing.T) {
	got := splitEnvList("none")
	if len(got) != 1 || got[0] != "none" {
		t.Errorf("got %v, want [none]", got)
	}
}
