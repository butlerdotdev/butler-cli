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

// Package serverhttp is the CLI's authenticated HTTP client for protected
// butler-server endpoints (cert rotation, GitOps lifecycle, ProviderConfig
// image/network discovery, IdP create). It reads the session JWT from the
// CLI credential file, attaches it as a Bearer, and centralizes the 401
// invalid-session envelope handling: a single silent refresh, a single
// retry, and ErrSessionExpired if the refresh itself fails.
//
// See ADR-016 amendment (2026-05-28) for the design rationale and the
// documented wire contract for the 401 envelope.
package serverhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/butlerdotdev/butler/internal/common/auth"
)

// ErrSessionExpired is returned when the server returns the documented 401
// invalid-session envelope and the silent refresh attempt also fails. The
// CLI should surface this with a "run 'butlerctl login' to re-authenticate"
// prompt rather than a stack trace.
var ErrSessionExpired = errors.New("butler session expired, please run 'butlerctl login'")

// ServerError wraps a non-2xx response. Message holds the canonical
// {"error": "..."} body emitted by butler-server's writeError helper.
// Feature code should pattern-match via the Is* accessors rather than
// string-comparing the message.
type ServerError struct {
	StatusCode int
	Message    string
	raw        []byte
}

// Error implements the error interface.
func (e *ServerError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("butler-server returned %d", e.StatusCode)
	}
	return fmt.Sprintf("butler-server returned %d: %s", e.StatusCode, e.Message)
}

// IsInvalidSession reports the documented 401 invalid-session envelope.
// Verified against SessionMiddleware on 2026-05-28; the envelope is
// {"error":"invalid session"} at status 401. See ADR-016 amendment.
func (e *ServerError) IsInvalidSession() bool {
	return e.StatusCode == http.StatusUnauthorized && e.Message == "invalid session"
}

// IsForbidden reports a 403 response (typically insufficient role).
func (e *ServerError) IsForbidden() bool {
	return e.StatusCode == http.StatusForbidden
}

// IsConflict reports a 409 response (typically a state conflict such as
// "rotation already in progress").
func (e *ServerError) IsConflict() bool {
	return e.StatusCode == http.StatusConflict
}

// IsNotFound reports a 404 response.
func (e *ServerError) IsNotFound() bool {
	return e.StatusCode == http.StatusNotFound
}

// IsBadRequest reports a 400 response (typically malformed body or
// failed server-side validation).
func (e *ServerError) IsBadRequest() bool {
	return e.StatusCode == http.StatusBadRequest
}

// Client is an authenticated HTTP client bound to a single butler-server.
type Client struct {
	serverURL string
	creds     *auth.CredentialFile
	http      *http.Client
}

// New constructs a Client from the active butler credential. Errors if no
// active credential exists, the credential is missing a session token
// (e.g., issued by a pre-amendment server), or the server URL is empty.
func New() (*Client, error) {
	creds, err := auth.LoadCredentials()
	if err != nil {
		return nil, fmt.Errorf("loading credentials: %w", err)
	}
	if creds.ActiveServer == "" {
		return nil, fmt.Errorf("no active butler server (run 'butlerctl login' first)")
	}
	sc := creds.ActiveCredential()
	if sc == nil {
		return nil, fmt.Errorf("no active butler credential (run 'butlerctl login' first)")
	}
	if sc.SessionToken == "" {
		return nil, fmt.Errorf("active credential has no session token; re-run 'butlerctl login' to refresh")
	}
	return &Client{
		serverURL: creds.ActiveServer,
		creds:     creds,
		http:      &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// ServerURL returns the active butler-server URL the client talks to.
func (c *Client) ServerURL() string {
	return c.serverURL
}

// Get issues an authenticated GET against path (joined to the server URL).
// If out is non-nil and the response carries a JSON body, that body is
// unmarshaled into out. See package doc for 401 handling.
func (c *Client) Get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

// Post issues an authenticated POST. body, if non-nil, is JSON-encoded.
// See Get for response and error semantics.
func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	statusCode, raw, err := c.sendAndRead(ctx, method, path, body)
	if err != nil {
		return err
	}

	if statusCode != http.StatusUnauthorized {
		return c.handleResponse(statusCode, raw, out)
	}

	// 401: single-retry discipline. Only the documented invalid-session
	// envelope triggers a silent refresh. Any other 401 (e.g., a server-
	// returned envelope we did not author) is surfaced as-is so the
	// operator sees the real reason.
	serverErr := parseError(statusCode, raw)
	if !serverErr.IsInvalidSession() {
		return serverErr
	}

	if refreshErr := c.refreshSession(); refreshErr != nil {
		return fmt.Errorf("%w: %v", ErrSessionExpired, refreshErr)
	}

	statusCode, raw, err = c.sendAndRead(ctx, method, path, body)
	if err != nil {
		return err
	}
	return c.handleResponse(statusCode, raw, out)
}

// sendAndRead issues the request, reads the full response body, and closes
// the body. Returning (statusCode, raw, error) lets the caller inspect the
// body twice (once for envelope decoding on errors, once for out
// unmarshaling on success) without retaining a live Body.
func (c *Client) sendAndRead(ctx context.Context, method, path string, body any) (int, []byte, error) {
	resp, err := c.send(ctx, method, path, body)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return resp.StatusCode, nil, fmt.Errorf("reading response body: %w", readErr)
	}
	return resp.StatusCode, raw, nil
}

func (c *Client) send(ctx context.Context, method, path string, body any) (*http.Response, error) {
	fullURL := strings.TrimRight(c.serverURL, "/") + path
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshaling request body: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, reader)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	sc := c.creds.ActiveCredential()
	if sc != nil && sc.SessionToken != "" {
		req.Header.Set("Authorization", "Bearer "+sc.SessionToken)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling butler-server: %w", err)
	}
	return resp, nil
}

func (c *Client) handleResponse(statusCode int, raw []byte, out any) error {
	if statusCode >= 400 {
		return parseError(statusCode, raw)
	}
	if statusCode == http.StatusNoContent || len(raw) == 0 || out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("parsing response body: %w", err)
	}
	return nil
}

// refreshSession exchanges the stored refresh token for a fresh credential
// and persists the result. Returns an error if the refresh token is absent,
// expired, or rejected by the server.
func (c *Client) refreshSession() error {
	sc := c.creds.ActiveCredential()
	if sc == nil || !sc.CanRefresh() {
		return fmt.Errorf("no usable refresh token")
	}
	tr, err := auth.RefreshAccessToken(c.serverURL, sc.RefreshToken)
	if err != nil {
		return err
	}
	sc.User = tr.User
	sc.Kubeconfig = tr.Kubeconfig
	sc.ExpiresAt = tr.ExpiresAt
	sc.SessionToken = tr.SessionToken
	sc.SessionExpiresAt = tr.SessionExpiresAt
	sc.RefreshToken = tr.RefreshToken
	sc.RefreshExpiresAt = tr.RefreshExpiresAt
	return c.creds.Save()
}

// parseError decodes butler-server's {"error": "..."} envelope. If the
// body is not valid JSON or the envelope is missing, the raw trimmed body
// becomes the message so the operator still sees something useful.
func parseError(statusCode int, raw []byte) *ServerError {
	se := &ServerError{StatusCode: statusCode, raw: raw}
	var env struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &env) == nil && env.Error != "" {
		se.Message = env.Error
		return se
	}
	se.Message = strings.TrimSpace(string(raw))
	return se
}
