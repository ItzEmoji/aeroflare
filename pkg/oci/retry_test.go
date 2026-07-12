package oci

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDoWithRetryOpts_HonorsGivenBaseDelay(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	start := time.Now()
	resp, err := DoWithRetryOpts(srv.Client(), func() (*http.Request, error) {
		return http.NewRequest("PUT", srv.URL, bytes.NewReader([]byte("body")))
	}, time.Millisecond)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if attempts.Load() != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts.Load())
	}
	// The backoff must come from the argument, not a package global. With the
	// 500ms default this loop would take well over a second.
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("elapsed %v: base delay was not honored", elapsed)
	}
}

func TestDoWithRetry_RecoversFromTransient5xx(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	resp, err := DoWithRetryOpts(srv.Client(), func() (*http.Request, error) {
		return http.NewRequest("PUT", srv.URL, bytes.NewReader([]byte("body")))
	}, time.Millisecond)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if attempts.Load() != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts.Load())
	}
}

func TestDoWithRetry_DoesNotRetryClientErrors(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	resp, err := DoWithRetryOpts(srv.Client(), func() (*http.Request, error) {
		return http.NewRequest("GET", srv.URL, nil)
	}, time.Millisecond)
	if err != nil {
		t.Fatalf("4xx is a valid response, not an error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if attempts.Load() != 1 {
		t.Fatalf("404 must not be retried; got %d attempts", attempts.Load())
	}
}

func TestDoWithRetry_RetriesConnectionErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // refuse all connections

	start := time.Now()
	_, err := DoWithRetryOpts(http.DefaultClient, func() (*http.Request, error) {
		return http.NewRequest("GET", url, nil)
	}, time.Millisecond)
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
	// With 3 attempts at millisecond backoff this should be nearly instant;
	// the assertion just guards against a runaway retry loop.
	if time.Since(start) > 5*time.Second {
		t.Fatal("retry loop took too long")
	}
}

func TestDoWithRetry_Retries429(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	resp, err := DoWithRetryOpts(srv.Client(), func() (*http.Request, error) {
		return http.NewRequest("GET", srv.URL, nil)
	}, time.Millisecond)
	if err != nil {
		t.Fatalf("expected success after 429 retry, got: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
