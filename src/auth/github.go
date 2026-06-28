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
var pollTimeUnit = time.Second

type DeviceCodeRequest struct {
	ClientID string `json:"client_id"`
}

type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	Interval        int    `json:"interval"`
}

func RequestDeviceCode(clientID string) (*DeviceCodeResponse, error) {
	reqBodyBytes, err := json.Marshal(DeviceCodeRequest{ClientID: clientID})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", githubBaseURL+"/login/device/code", bytes.NewBuffer(reqBodyBytes))
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
			req, err := http.NewRequestWithContext(ctx, "POST", githubBaseURL+"/login/oauth/access_token", bytes.NewBuffer(reqBodyBytes))
			if err != nil {
				return "", err
			}
			req.Header.Set("Accept", "application/json")
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				continue // retry on network error
			}

			if resp.StatusCode >= 500 {
				resp.Body.Close()
				continue // transient error, retry
			}

			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				continue
			}

			var result TokenResponse
			if err := json.Unmarshal(body, &result); err != nil {
				return "", err
			}

			if result.AccessToken != "" {
				return result.AccessToken, nil
			}

			if result.Error == "authorization_pending" {
				continue
			}
			if result.Error == "slow_down" {
				interval += 5
				ticker.Reset(time.Duration(interval) * pollTimeUnit)
				continue
			}

			if result.Error != "" {
				return "", errors.New(result.Error)
			}

			return "", errors.New("invalid response from server")
		}
	}
}
