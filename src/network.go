package network

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// PushBlob natively hashes and streams a file to any OCI registry.
// repository should be the full repository path (e.g. "itzemoji/nix-cache-test/nix-cache")
func PushBlob(filePath, registry, repository, token string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := stat.Size()

	// Compute sha256
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	digest := "sha256:" + hex.EncodeToString(h.Sum(nil))

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 1. Check if blob already exists
	proto := GetProtocol(registry)
	checkURL := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", proto, registry, repository, digest)
	req, err := http.NewRequest("HEAD", checkURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "aeroflare/1.0")

	resp, err := client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return digest, nil
		}
	} else {
		return "", fmt.Errorf("failed to check blob existence: %v", err)
	}

	// 2. Initiate upload
	proto = GetProtocol(registry)
	initURL := fmt.Sprintf("%s://%s/v2/%s/blobs/uploads/", proto, registry, repository)
	req, err = http.NewRequest("POST", initURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "aeroflare/1.0")

	resp, err = client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to initiate upload: %v", err)
	}
	defer func(body io.ReadCloser) { _ = body.Close() }(resp.Body)

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to initiate blob upload (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
	}

	uploadURL := resp.Header.Get("Location")
	if uploadURL == "" {
		return "", fmt.Errorf("failed to initiate blob upload (HTTP %d): missing Location header", resp.StatusCode)
	}

	// Make URL absolute if relative
	if strings.HasPrefix(uploadURL, "/") {
		proto = GetProtocol(registry)
		uploadURL = fmt.Sprintf("%s://%s%s", proto, registry, uploadURL)
	}

	// 3. Upload blob in single PUT
	sep := "?"
	if strings.Contains(uploadURL, "?") {
		sep = "&"
	}
	putURL := fmt.Sprintf("%s%sdigest=%s", uploadURL, sep, digest)

	// Seek back to start of file for uploading
	if _, err := f.Seek(0, 0); err != nil {
		return "", err
	}

	req, err = http.NewRequest("PUT", putURL, f)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("User-Agent", "aeroflare/1.0")
	req.ContentLength = size

	// Reset client timeout for the actual upload as it may take longer
	putClient := &http.Client{
		Timeout: 10 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err = putClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to upload blob: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("blob upload failed with HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return digest, nil
}

// PullBlob fetches a blob from any OCI registry and writes it to outFile.
// repository should be the full repository path (e.g. "itzemoji/nix-cache-test/nix-cache")
func PullBlob(digest, outFile, registry, repository, token string) error {
	proto := GetProtocol(registry)
	getURL := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", proto, registry, repository, digest)
	req, err := http.NewRequest("GET", getURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "aeroflare/1.0")

	// Create a new client that WILL follow redirects, as pulling blobs often redirects to cloud storage
	client := &http.Client{Timeout: 5 * time.Minute}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	f, err := os.Create(outFile)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	_, err = io.Copy(f, resp.Body)
	return err
}
