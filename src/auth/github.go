package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

var githubBaseURL = "https://github.com"

type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	Interval        int    `json:"interval"`
}

func RequestDeviceCode(clientID string) (*DeviceCodeResponse, error) {
	reqBody := []byte(fmt.Sprintf(`{"client_id":"%s"}`, clientID))
	req, err := http.NewRequest("POST", githubBaseURL+"/login/device/code", bytes.NewBuffer(reqBody))
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

	body, _ := io.ReadAll(resp.Body)
	var result DeviceCodeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	Error       string `json:"error"`
}

func PollAccessToken(clientID, deviceCode string, interval int) (string, error) {
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		<-ticker.C
		reqBody := []byte(fmt.Sprintf(`{"client_id":"%s","device_code":"%s","grant_type":"urn:ietf:params:oauth:grant-type:device_code"}`, clientID, deviceCode))
		req, _ := http.NewRequest("POST", githubBaseURL+"/login/oauth/access_token", bytes.NewBuffer(reqBody))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue // retry on network error
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var result TokenResponse
		json.Unmarshal(body, &result)

		if result.AccessToken != "" {
			return result.AccessToken, nil
		}

		if result.Error == "authorization_pending" || result.Error == "slow_down" {
			continue
		}
		
		if result.Error != "" {
			return "", errors.New(result.Error)
		}
	}
}
