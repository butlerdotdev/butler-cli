/*
Copyright 2026 The Butler Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// DeviceFlowResponse is the server's response when initiating a device flow.
type DeviceFlowResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// TokenResponse is the server's response when a device flow completes
// successfully, or when a refresh token is exchanged.
type TokenResponse struct {
	User             UserInfo  `json:"user"`
	Kubeconfig       string    `json:"kubeconfig"`
	ExpiresAt        time.Time `json:"expires_at"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

// TokenError is returned by the token endpoint when the flow has not
// completed yet or has failed.
type TokenError struct {
	Error string `json:"error"`
}

// InitiateDeviceFlow starts a device authorization flow against the given
// Butler server. The caller should display the user code and verification URI,
// then call PollForToken.
func InitiateDeviceFlow(serverURL string) (*DeviceFlowResponse, error) {
	url := strings.TrimRight(serverURL, "/") + "/api/auth/cli/device"

	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		return nil, fmt.Errorf("requesting device code: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var dfr DeviceFlowResponse
	if err := json.Unmarshal(body, &dfr); err != nil {
		return nil, fmt.Errorf("parsing device flow response: %w", err)
	}

	return &dfr, nil
}

// PollForToken polls the token endpoint until the user approves (or denies)
// the device flow, or until the context is cancelled. The interval parameter
// controls how many seconds to wait between polls.
func PollForToken(ctx context.Context, serverURL, deviceCode string, interval int) (*TokenResponse, error) {
	url := strings.TrimRight(serverURL, "/") + "/api/auth/cli/token"

	if interval < 1 {
		interval = 5
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			tr, err := requestToken(url, deviceCode)
			if err != nil {
				return nil, err
			}
			if tr != nil {
				return tr, nil
			}
			// tr == nil means "authorization_pending", keep polling
		}
	}
}

// requestToken makes a single token request. It returns (nil, nil) when the
// server responds with "authorization_pending", indicating the caller should
// keep polling.
func requestToken(url, deviceCode string) (*TokenResponse, error) {
	payload, _ := json.Marshal(map[string]string{
		"device_code": deviceCode,
	})

	resp, err := http.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("polling token endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading token response: %w", err)
	}

	// 200 means the user approved
	if resp.StatusCode == http.StatusOK {
		var tr TokenResponse
		if err := json.Unmarshal(body, &tr); err != nil {
			return nil, fmt.Errorf("parsing token response: %w", err)
		}
		return &tr, nil
	}

	// Non-200: parse the error body
	var te TokenError
	if err := json.Unmarshal(body, &te); err != nil {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	switch te.Error {
	case "authorization_pending":
		return nil, nil // keep polling
	case "slow_down":
		return nil, nil // keep polling (our fixed interval handles this)
	case "expired_token":
		return nil, fmt.Errorf("device code expired; please run login again")
	case "access_denied":
		return nil, fmt.Errorf("login request was denied")
	default:
		return nil, fmt.Errorf("token error: %s", te.Error)
	}
}

// RefreshAccessToken exchanges a refresh token for a new access token.
func RefreshAccessToken(serverURL, refreshToken string) (*TokenResponse, error) {
	url := strings.TrimRight(serverURL, "/") + "/api/auth/cli/refresh"

	payload, _ := json.Marshal(map[string]string{
		"refresh_token": refreshToken,
	})

	resp, err := http.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("refreshing token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tr TokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("parsing refresh response: %w", err)
	}

	return &tr, nil
}

// OpenBrowser opens the given URL in the user's default browser.
// Errors are returned but are non-fatal; the caller should print the URL
// as a fallback.
func OpenBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}

	return cmd.Start()
}
