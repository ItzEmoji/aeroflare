package oci

import (
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"
)

// DefaultRetryBaseDelay is the first backoff delay used by DoWithRetry.
const DefaultRetryBaseDelay = 500 * time.Millisecond

const retryMaxAttempts = 3

// DoWithRetry performs an idempotent HTTP request with bounded exponential
// backoff, retrying on network errors, 429, and 5xx responses. build is
// called once per attempt so each retry gets a fresh request (and body).
// Non-retryable statuses (e.g. 404) are returned as normal responses for the
// caller to interpret.
//
// It backs off starting at DefaultRetryBaseDelay. Use DoWithRetryOpts to
// choose a different delay.
func DoWithRetry(client *http.Client, build func() (*http.Request, error)) (*http.Response, error) {
	return DoWithRetryOpts(client, build, DefaultRetryBaseDelay)
}

// DoWithRetryOpts is DoWithRetry with an explicit initial backoff delay, which
// doubles (with jitter) on each attempt.
func DoWithRetryOpts(client *http.Client, build func() (*http.Request, error), baseDelay time.Duration) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt < retryMaxAttempts; attempt++ {
		if attempt > 0 {
			backoff := baseDelay << (attempt - 1)
			jitter := time.Duration(rand.Int63n(int64(backoff)/2 + 1))
			time.Sleep(backoff + jitter)
		}

		req, err := build()
		if err != nil {
			return nil, err
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if !retryableStatus(resp.StatusCode) {
			return resp, nil
		}

		lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, req.URL)
		// Drain so the connection can be reused, then retry.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
	}

	return nil, fmt.Errorf("request failed after %d attempts: %w", retryMaxAttempts, lastErr)
}

func retryableStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	}
	// 5xx except 501 Not Implemented, which never changes on retry.
	return code >= 500 && code != http.StatusNotImplemented
}
