package plex

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	plexBaseURL  = "https://plex.tv/api/v2"
	plexAuthURL  = "https://app.plex.tv/auth#"
	plexClientID = "b5e6b3e5-c145-4a80-b592-e63d2c5e1e36"
	plexProduct  = "Arrflix"
)

type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

type PinResponse struct {
	ID        int    `json:"id"`
	Code      string `json:"code"`
	AuthToken string `json:"authToken"`
}

type UserResponse struct {
	ID       int    `json:"id"`
	UUID     string `json:"uuid"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Thumb    string `json:"thumb"`
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Client-Identifier", plexClientID)
	req.Header.Set("X-Plex-Product", plexProduct)
}

// CreatePin creates a new Plex PIN for authentication.
func (c *Client) CreatePin() (*PinResponse, error) {
	body := strings.NewReader("strong=true&X-Plex-Product=" + url.QueryEscape(plexProduct) + "&X-Plex-Client-Identifier=" + url.QueryEscape(plexClientID))
	req, err := http.NewRequest("POST", plexBaseURL+"/pins", body)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("plex create pin: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("plex create pin: status %d: %s", resp.StatusCode, b)
	}

	var pin PinResponse
	if err := json.NewDecoder(resp.Body).Decode(&pin); err != nil {
		return nil, fmt.Errorf("plex create pin decode: %w", err)
	}
	return &pin, nil
}

// CheckPin checks whether a PIN has been claimed and returns the auth token if so.
func (c *Client) CheckPin(pinID int) (*PinResponse, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/pins/%d", plexBaseURL, pinID), nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("plex check pin: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("plex check pin: status %d: %s", resp.StatusCode, b)
	}

	var pin PinResponse
	if err := json.NewDecoder(resp.Body).Decode(&pin); err != nil {
		return nil, fmt.Errorf("plex check pin decode: %w", err)
	}
	return &pin, nil
}

// GetUser fetches the Plex user profile using an auth token.
func (c *Client) GetUser(authToken string) (*UserResponse, error) {
	req, err := http.NewRequest("GET", plexBaseURL+"/user", nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	req.Header.Set("X-Plex-Token", authToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("plex get user: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("plex get user: status %d: %s", resp.StatusCode, b)
	}

	var user UserResponse
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("plex get user decode: %w", err)
	}
	return &user, nil
}

// AuthURL builds the Plex authentication URL that the user should be redirected to.
func AuthURL(code, forwardURL string) string {
	params := url.Values{}
	params.Set("clientID", plexClientID)
	params.Set("code", code)
	params.Set("forwardUrl", forwardURL)
	params.Set("context[device][product]", plexProduct)
	return plexAuthURL + "?" + params.Encode()
}
