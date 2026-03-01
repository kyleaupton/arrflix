package guessit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultBaseURL = "http://127.0.0.1:8000"

type Client struct {
	httpClient *http.Client
	baseURL    string
}

func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    baseURL,
	}
}

// ParseResult holds the output from guessit for a single filename.
type ParseResult struct {
	Title        *string `json:"title"`
	Year         *int    `json:"year"`
	Season       *int    `json:"season"`
	Episode      *int    `json:"episode"`
	Type         *string `json:"type"`
	ScreenSize   *string `json:"screen_size"`
	Source       *string `json:"source"`
	VideoCodec   *string `json:"video_codec"`
	AudioCodec   *string `json:"audio_codec"`
	ReleaseGroup *string `json:"release_group"`
	Container    *string `json:"container"`
	EpisodeTitle *string `json:"episode_title"`
}

// Healthy returns true if the guessit sidecar is reachable.
func (c *Client) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ParseBatch sends a list of filenames to guessit and returns one ParseResult per input.
func (c *Client) ParseBatch(ctx context.Context, filenames []string) ([]ParseResult, error) {
	body, err := json.Marshal(filenames)
	if err != nil {
		return nil, fmt.Errorf("guessit: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/parse/batch", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("guessit: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("guessit: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("guessit: unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var results []ParseResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("guessit: decode response: %w", err)
	}

	if len(results) != len(filenames) {
		return nil, fmt.Errorf("guessit: result count mismatch: got %d, want %d", len(results), len(filenames))
	}

	return results, nil
}
