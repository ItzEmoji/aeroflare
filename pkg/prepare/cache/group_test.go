package cache

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serverHolding returns a cache server that answers 200 for exactly the given
// hashes and 404 for everything else.
func serverHolding(hashes ...string) *httptest.Server {
	held := make(map[string]bool, len(hashes))
	for _, h := range hashes {
		held[h] = true
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		hash := strings.TrimSuffix(name, ".narinfo")
		if held[hash] {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

// brokenServer always returns 500, which Cache.checkRemote reports as an error.
func brokenServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
}

func TestGroupExistsBatch_UnionAcrossMembers(t *testing.T) {
	a := serverHolding("aaa")
	defer a.Close()
	b := serverHolding("bbb")
	defer b.Close()

	g := NewGroup([]string{a.URL, b.URL})
	got, err := g.ExistsBatch(context.Background(), []string{"aaa", "bbb", "ccc"}, 4)
	if err != nil {
		t.Fatalf("ExistsBatch: %v", err)
	}
	if !got["aaa"] || !got["bbb"] {
		t.Errorf("expected aaa and bbb present, got %v", got)
	}
	if got["ccc"] {
		t.Errorf("expected ccc absent, got %v", got)
	}
}

func TestGroupExistsBatch_BrokenMemberDoesNotSuppressHit(t *testing.T) {
	good := serverHolding("aaa")
	defer good.Close()
	bad := brokenServer()
	defer bad.Close()

	g := NewGroup([]string{bad.URL, good.URL})
	var warn bytes.Buffer
	g.SetWarnWriter(&warn)

	got, err := g.ExistsBatch(context.Background(), []string{"aaa"}, 4)
	if err != nil {
		t.Fatalf("ExistsBatch: %v", err)
	}
	if !got["aaa"] {
		t.Errorf("a broken member must not hide a healthy member's hit: %v", got)
	}
	if !strings.Contains(warn.String(), bad.URL) {
		t.Errorf("expected a warning naming %s, got %q", bad.URL, warn.String())
	}
}

func TestGroupExistsBatch_AllMembersBrokenReportsAbsent(t *testing.T) {
	bad := brokenServer()
	defer bad.Close()

	g := NewGroup([]string{bad.URL})
	g.SetWarnWriter(&bytes.Buffer{})

	got, err := g.ExistsBatch(context.Background(), []string{"aaa"}, 4)
	if err != nil {
		t.Fatalf("all-broken must not be a hard error, got %v", err)
	}
	if got["aaa"] {
		t.Errorf("unreachable upstream must never mark a path present: %v", got)
	}
}

func TestGroupExistsBatch_EmptyGroup(t *testing.T) {
	g := NewGroup(nil)
	if g.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", g.Len())
	}
	got, err := g.ExistsBatch(context.Background(), []string{"aaa"}, 4)
	if err != nil {
		t.Fatalf("ExistsBatch: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty group must report nothing upstream, got %v", got)
	}
}
