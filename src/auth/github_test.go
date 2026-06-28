package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestDeviceCode(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"device_code": "dc123", "user_code": "123-456", "verification_uri": "https://github.com/login/device", "interval": 5}`))
	}))
	defer ts.Close()

	originalURL := githubBaseURL
	githubBaseURL = ts.URL
	defer func() { githubBaseURL = originalURL }()

	res, err := RequestDeviceCode("test-client")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UserCode != "123-456" {
		t.Errorf("expected 123-456, got %s", res.UserCode)
	}
}

func TestPollAccessTokenSuccess(t *testing.T) {
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			w.Write([]byte(`{"error": "authorization_pending"}`))
			return
		}
		if requests == 2 {
			w.Write([]byte(`{"error": "slow_down"}`))
			return
		}
		w.Write([]byte(`{"access_token": "gho_12345"}`))
	}))
	defer ts.Close()

	originalURL := githubBaseURL
	githubBaseURL = ts.URL
	defer func() { githubBaseURL = originalURL }()

	originalTimeUnit := pollTimeUnit
	pollTimeUnit = time.Millisecond
	defer func() { pollTimeUnit = originalTimeUnit }()

	token, err := PollAccessToken("client_id", "device_code", 1)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if token != "gho_12345" {
		t.Fatalf("expected token gho_12345, got: %s", token)
	}
}

func TestPollAccessTokenTransientError(t *testing.T) {
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte(`<html>502 Bad Gateway</html>`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token": "gho_12345"}`))
	}))
	defer ts.Close()

	originalURL := githubBaseURL
	githubBaseURL = ts.URL
	defer func() { githubBaseURL = originalURL }()

	originalTimeUnit := pollTimeUnit
	pollTimeUnit = time.Millisecond
	defer func() { pollTimeUnit = originalTimeUnit }()

	token, err := PollAccessToken("client_id", "device_code", 1)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if token != "gho_12345" {
		t.Fatalf("expected token gho_12345, got: %s", token)
	}
}

func TestPollAccessTokenError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"error": "expired_token"}`))
	}))
	defer ts.Close()

	originalURL := githubBaseURL
	githubBaseURL = ts.URL
	defer func() { githubBaseURL = originalURL }()

	originalTimeUnit := pollTimeUnit
	pollTimeUnit = time.Millisecond
	defer func() { pollTimeUnit = originalTimeUnit }()

	_, err := PollAccessToken("client_id", "device_code", 1)
	if err == nil || err.Error() != "expired_token" {
		t.Fatalf("expected expired_token error, got: %v", err)
	}
}

func TestPollAccessTokenClientError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "access_denied"}`))
	}))
	defer ts.Close()

	originalURL := githubBaseURL
	githubBaseURL = ts.URL
	defer func() { githubBaseURL = originalURL }()

	originalTimeUnit := pollTimeUnit
	pollTimeUnit = time.Millisecond
	defer func() { pollTimeUnit = originalTimeUnit }()

	_, err := PollAccessToken("client_id", "device_code", 1)
	if err == nil || err.Error() != "access_denied" {
		t.Fatalf("expected access_denied, got: %v", err)
	}
}
