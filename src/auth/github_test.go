package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestDeviceCode(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"device_code": "dc123", "user_code": "123-456", "verification_uri": "https://github.com/login/device", "interval": 5}`))
	}))
	defer ts.Close()

	// Temporarily override the base URL for testing (we'll add a variable for this in implementation)
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
