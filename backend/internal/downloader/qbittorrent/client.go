package qbittorrent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Client is a low-level HTTP client for the qBittorrent Web API.
// Each instance manages its own SID (session cookie) and handles
// transparent re-authentication when the session expires.
type Client struct {
	baseURL  string
	username string
	password string

	sid  string       // SID cookie value; empty means not logged in
	mu   sync.Mutex   // protects sid during login/re-auth
	http *http.Client // plain client, no cookie jar
}

// NewClient creates a new qBittorrent API client.
func NewClient(baseURL, username, password string) *Client {
	return &Client{
		baseURL:  strings.TrimSuffix(baseURL, "/"),
		username: username,
		password: password,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// Login authenticates with qBittorrent and stores the session cookie.
func (c *Client) Login(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loginLocked(ctx)
}

// loginLocked performs login while the caller already holds c.mu.
func (c *Client) loginLocked(ctx context.Context) error {
	form := url.Values{
		"username": {c.username},
		"password": {c.password},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode == http.StatusForbidden {
		c.sid = ""
		return &AuthError{StatusCode: resp.StatusCode, Body: "IP is banned for too many failed login attempts"}
	}

	if resp.StatusCode != http.StatusOK {
		c.sid = ""
		return &APIError{StatusCode: resp.StatusCode, Body: "login failed"}
	}

	// Extract SID from Set-Cookie header
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "SID" {
			c.sid = cookie.Value
			return nil
		}
	}

	// qBittorrent returns 200 with body "Fails." on bad credentials (no SID set)
	c.sid = ""
	return &AuthError{StatusCode: http.StatusUnauthorized, Body: "login failed: invalid credentials (no SID returned)"}
}

// do executes an API request with automatic session management.
// It accepts body as []byte so the request can be safely retried on auth failure.
// Returns the raw response body bytes.
func (c *Client) do(ctx context.Context, method, path string, body []byte, contentType string) ([]byte, error) {
	// Ensure we have a session before the first request
	c.mu.Lock()
	if c.sid == "" {
		if err := c.loginLocked(ctx); err != nil {
			c.mu.Unlock()
			return nil, err
		}
	}
	sid := c.sid
	c.mu.Unlock()

	// First attempt
	respBody, err := c.doOnce(ctx, method, path, body, contentType, sid)
	if err == nil {
		return respBody, nil
	}

	// If it's an auth error, re-login and retry once
	var authErr *AuthError
	if errors.As(err, &authErr) {
		c.mu.Lock()
		// Only re-login if nobody else already did
		if c.sid == sid || c.sid == "" {
			c.sid = ""
			if loginErr := c.loginLocked(ctx); loginErr != nil {
				c.mu.Unlock()
				return nil, loginErr
			}
		}
		sid = c.sid
		c.mu.Unlock()

		return c.doOnce(ctx, method, path, body, contentType, sid)
	}

	return nil, err
}

// doOnce executes a single HTTP request without retry logic.
// Returns the raw response body on success.
func (c *Client) doOnce(ctx context.Context, method, path string, body []byte, contentType string, sid string) ([]byte, error) {
	reqURL := c.baseURL + path

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if sid != "" {
		req.Header.Set("Cookie", "SID="+sid)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Auth errors — signal the caller to re-auth
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, &AuthError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	// Other HTTP errors
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	return respBody, nil
}

// doJSON executes a request and JSON-decodes the response into result.
func (c *Client) doJSON(ctx context.Context, method, path string, body []byte, contentType string, result any) error {
	respBody, err := c.do(ctx, method, path, body, contentType)
	if err != nil {
		return err
	}
	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("decode response from %s: %w", path, err)
		}
	}
	return nil
}

// doRaw executes a request and returns the response as a trimmed string.
func (c *Client) doRaw(ctx context.Context, method, path string) (string, error) {
	respBody, err := c.do(ctx, method, path, nil, "")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(respBody)), nil
}

// doForm is a convenience wrapper for POST requests with form-encoded body.
func (c *Client) doForm(ctx context.Context, path string, form url.Values, result any) error {
	return c.doJSON(ctx, "POST", path, []byte(form.Encode()), "application/x-www-form-urlencoded", result)
}

// doGet is a convenience wrapper for GET requests with query parameters.
func (c *Client) doGet(ctx context.Context, path string, params url.Values, result any) error {
	if len(params) > 0 {
		path = path + "?" + params.Encode()
	}
	return c.doJSON(ctx, "GET", path, nil, "", result)
}

// doMultipart executes a multipart POST request (used for uploading .torrent files).
func (c *Client) doMultipart(ctx context.Context, path string, body []byte, contentType string) error {
	_, err := c.do(ctx, "POST", path, body, contentType)
	return err
}

// GetVersion returns the qBittorrent application version string.
func (c *Client) GetVersion(ctx context.Context) (string, error) {
	return c.doRaw(ctx, "GET", "/api/v2/app/version")
}

// GetWebAPIVersion returns the Web API version string.
func (c *Client) GetWebAPIVersion(ctx context.Context) (string, error) {
	return c.doRaw(ctx, "GET", "/api/v2/app/webapiVersion")
}

// ListTorrents returns torrents matching the given options.
func (c *Client) ListTorrents(ctx context.Context, hashes ...string) ([]TorrentInfo, error) {
	params := url.Values{}
	if len(hashes) > 0 {
		params.Set("hashes", strings.Join(hashes, "|"))
	}

	var torrents []TorrentInfo
	if err := c.doGet(ctx, "/api/v2/torrents/info", params, &torrents); err != nil {
		return nil, err
	}
	return torrents, nil
}

// ListTorrentFiles returns the files within a torrent.
func (c *Client) ListTorrentFiles(ctx context.Context, hash string) ([]TorrentFile, error) {
	params := url.Values{"hash": {hash}}

	var files []TorrentFile
	if err := c.doGet(ctx, "/api/v2/torrents/files", params, &files); err != nil {
		return nil, err
	}
	return files, nil
}

// AddMagnet adds a magnet link to qBittorrent.
func (c *Client) AddMagnet(ctx context.Context, magnetURL string, opts map[string]string) error {
	form := url.Values{"urls": {magnetURL}}
	for k, v := range opts {
		form.Set(k, v)
	}
	return c.doForm(ctx, "/api/v2/torrents/add", form, nil)
}

// AddTorrentFile uploads a .torrent file to qBittorrent.
// body should be the raw bytes of a multipart form, contentType the matching content type.
func (c *Client) AddTorrentFile(ctx context.Context, body []byte, contentType string) error {
	return c.doMultipart(ctx, "/api/v2/torrents/add", body, contentType)
}

// Pause pauses torrents by hash.
func (c *Client) Pause(ctx context.Context, hashes []string) error {
	form := url.Values{"hashes": {strings.Join(hashes, "|")}}
	return c.doForm(ctx, "/api/v2/torrents/pause", form, nil)
}

// Resume resumes torrents by hash.
func (c *Client) Resume(ctx context.Context, hashes []string) error {
	form := url.Values{"hashes": {strings.Join(hashes, "|")}}
	return c.doForm(ctx, "/api/v2/torrents/resume", form, nil)
}

// Delete deletes torrents by hash, optionally removing data.
func (c *Client) Delete(ctx context.Context, hashes []string, deleteFiles bool) error {
	form := url.Values{
		"hashes":      {strings.Join(hashes, "|")},
		"deleteFiles": {fmt.Sprintf("%t", deleteFiles)},
	}
	return c.doForm(ctx, "/api/v2/torrents/delete", form, nil)
}

// AddTags adds tags to torrents.
func (c *Client) AddTags(ctx context.Context, hashes, tags []string) error {
	form := url.Values{
		"hashes": {strings.Join(hashes, "|")},
		"tags":   {strings.Join(tags, ",")},
	}
	return c.doForm(ctx, "/api/v2/torrents/addTags", form, nil)
}

// BaseURL returns the base URL of the qBittorrent instance.
func (c *Client) BaseURL() string {
	return c.baseURL
}
