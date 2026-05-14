package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	GitHubAPIBase = "https://api.github.com"
	UserAgent     = "Arrflix"
)

type Client struct {
	httpClient *http.Client
	owner      string
	repo       string
}

// NewClient creates a new GitHub API client
func NewClient(owner, repo string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		owner:      owner,
		repo:       repo,
	}
}

// Release represents a GitHub release
type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
	Prerelease  bool      `json:"prerelease"`
}

// Commit represents a GitHub commit
type Commit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Author struct {
			Date time.Time `json:"date"`
		} `json:"author"`
	} `json:"commit"`
	HTMLURL string `json:"html_url"`
}

// GetLatestRelease fetches the latest stable release
func (c *Client) GetLatestRelease(ctx context.Context) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", GitHubAPIBase, c.owner, c.repo)
	return doRequest[Release](ctx, c.httpClient, url)
}

// GetMainHeadCommit fetches the latest commit on main branch
func (c *Client) GetMainHeadCommit(ctx context.Context) (*Commit, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/commits/main", GitHubAPIBase, c.owner, c.repo)
	return doRequest[Commit](ctx, c.httpClient, url)
}

func doRequest[T any](ctx context.Context, client *http.Client, url string) (*T, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github api error: %d %s", resp.StatusCode, string(body))
	}

	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}
