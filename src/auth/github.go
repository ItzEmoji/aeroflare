package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

var githubBaseURL = "https://github.com"

type DeviceCodeRequest struct {
	ClientID string `json:"client_id"`
	Scope    string `json:"scope,omitempty"`
}

type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	Interval        int    `json:"interval"`
}

func RequestDeviceCode(clientID string) (*DeviceCodeResponse, error) {
	return requestDeviceCode(clientID, githubBaseURL)
}

func requestDeviceCode(clientID, baseURL string) (*DeviceCodeResponse, error) {
	reqBodyBytes, err := json.Marshal(DeviceCodeRequest{
		ClientID: clientID,
		Scope:    "repo write:packages",
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/login/device/code", bytes.NewBuffer(reqBodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("unexpected status code: " + resp.Status + " body: " + string(body))
	}

	var result DeviceCodeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

type TokenRequest struct {
	ClientID   string `json:"client_id"`
	DeviceCode string `json:"device_code"`
	GrantType  string `json:"grant_type"`
}

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	Error       string `json:"error"`
}

func PollAccessToken(clientID string, deviceCode string, interval int) (string, error) {
	return pollAccessToken(clientID, deviceCode, interval, githubBaseURL, time.Second)
}

func pollAccessToken(clientID string, deviceCode string, interval int, baseURL string, pollTimeUnit time.Duration) (string, error) {
	if interval <= 0 {
		interval = 5
	}
	ticker := time.NewTicker(time.Duration(interval) * pollTimeUnit)
	defer ticker.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	reqBodyBytes, err := json.Marshal(TokenRequest{
		ClientID:   clientID,
		DeviceCode: deviceCode,
		GrantType:  "urn:ietf:params:oauth:grant-type:device_code",
	})
	if err != nil {
		return "", err
	}

	for {
		select {
		case <-ctx.Done():
			return "", errors.New("polling timed out")
		case <-ticker.C:
			reqCtx, reqCancel := context.WithTimeout(ctx, 10*time.Second)
			req, err := http.NewRequestWithContext(reqCtx, "POST", baseURL+"/login/oauth/access_token", bytes.NewBuffer(reqBodyBytes))
			if err != nil {
				reqCancel()
				return "", err
			}
			req.Header.Set("Accept", "application/json")
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				reqCancel()
				continue // retry on network error
			}

			if resp.StatusCode >= 500 {
				resp.Body.Close()
				reqCancel()
				continue // transient error, retry
			}

			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				reqCancel()
				continue
			}

			var result TokenResponse
			if err := json.Unmarshal(body, &result); err != nil {
				reqCancel()
				return "", err
			}

			if result.AccessToken != "" {
				reqCancel()
				return result.AccessToken, nil
			}

			if result.Error == "authorization_pending" {
				reqCancel()
				continue
			}
			if result.Error == "slow_down" {
				interval += 5
				ticker.Reset(time.Duration(interval) * pollTimeUnit)
				reqCancel()
				continue
			}

			if result.Error != "" {
				reqCancel()
				return "", errors.New(result.Error)
			}

			reqCancel()
			return "", errors.New("invalid response from server")
		}
	}
}
