package qbittorrent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/kyleaupton/arrflix/internal/downloader"
	qbt "github.com/superturkey650/go-qbittorrent/qbt"
)

const (
	maxRetries = 2
	retryDelay = 1 * time.Second
)

// qBittorrentClient wraps the library client to implement downloader.Client interface
type qBittorrentClient struct {
	instanceID downloader.InstanceID
	client     *qbt.Client
	username   string
	password   string
}

// NewQBittorrentClient creates a new qBittorrent client wrapper
func NewQBittorrentClient(instanceID downloader.InstanceID, baseURL, username, password string) *qBittorrentClient {
	return &qBittorrentClient{
		instanceID: instanceID,
		client:     qbt.NewClient(baseURL),
		username:   username,
		password:   password,
	}
}

// Type returns the downloader type
func (c *qBittorrentClient) Type() downloader.Type {
	return downloader.TypeQbittorrent
}

// InstanceID returns the instance ID
func (c *qBittorrentClient) InstanceID() downloader.InstanceID {
	return c.instanceID
}

// Test tests the connection to qBittorrent
func (c *qBittorrentClient) Test(ctx context.Context) (downloader.TestResult, error) {
	result := downloader.TestResult{}

	// Test 1: Attempt to login
	if err := c.ensureLoggedIn(ctx); err != nil {
		// Determine error type
		errMsg := err.Error()
		var errorType string
		if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "connection refused") || strings.Contains(errMsg, "no such host") {
			errorType = "Unable to connect to qBittorrent. Check if qBittorrent is running and the URL is correct."
		} else if strings.Contains(errMsg, "401") || strings.Contains(errMsg, "403") || strings.Contains(errMsg, "unauthorized") || strings.Contains(errMsg, "Fails") {
			errorType = "Authentication failed - check username and password"
		} else {
			errorType = "Connection test failed: " + errMsg
		}

		result.Success = false
		result.Error = errorType
		return result, nil // Return result with error, don't wrap in error
	}

	// Test 2: Get application version
	// Make direct HTTP call to check status code since library doesn't validate it
	version, err := c.getVersionWithStatusCheck(ctx)
	if err != nil {
		result.Success = false
		result.Error = "Connected but unable to retrieve version information: " + err.Error()
		return result, nil
	}

	// Test 3: Get Web API version (optional, don't fail if it errors)
	webAPIVersion, _ := c.getWebAPIVersionWithStatusCheck(ctx)

	// Success!
	result.Success = true
	result.Message = "Connection test successful"
	result.Version = version
	result.WebAPIVersion = webAPIVersion
	return result, nil
}

// Add adds a download (magnet URL or torrent file URL)
func (c *qBittorrentClient) Add(ctx context.Context, req downloader.AddRequest) (downloader.AddResult, error) {
	var err error
	var result downloader.AddResult

	// Determine the URL to use
	torrentURL := req.MagnetURL
	if torrentURL == "" {
		return result, fmt.Errorf("magnet URL or torrent file URL is required")
	}

	// Detect if this is a magnet URL or an HTTP URL to a .torrent file
	isMagnet := strings.HasPrefix(torrentURL, "magnet:")

	// For non-magnet URLs (e.g., Prowlarr proxy URLs), fetch the .torrent file BEFORE the retry loop.
	// This is necessary because qBittorrent may not have network access to fetch the file itself.
	var torrentBytes []byte
	var torrentFilename string
	if !isMagnet {
		torrentBytes, torrentFilename, err = c.fetchTorrentFile(ctx, torrentURL)
		if err != nil {
			return result, fmt.Errorf("fetch torrent file: %w", err)
		}
	}

	// Retry logic
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay)
		}

		// Ensure we're logged in
		if err = c.ensureLoggedIn(ctx); err != nil {
			continue
		}

		// Snapshot existing torrents so we can identify a newly-added torrent
		// (needed for .torrent files where we can't extract hash from URL)
		existing := map[string]bool{}
		if !isMagnet {
			torrents, listErr := c.listTorrentsWithAuth(ctx, qbt.TorrentsOptions{})
			if listErr == nil {
				for _, t := range torrents {
					existing[t.Hash] = true
				}
			}
		}

		// Build download options
		opts := qbt.DownloadOptions{}
		if req.SavePath != "" {
			opts.Savepath = &req.SavePath
		}
		if req.Category != "" {
			opts.Category = &req.Category
		}
		if req.Paused {
			paused := true
			opts.Paused = &paused
		}

		// Add the torrent using appropriate method
		if isMagnet {
			// For magnet URLs, use the library's DownloadLinks
			fmt.Printf("[DEBUG Add] Calling DownloadLinks with URL: %s...\n", torrentURL[:min(len(torrentURL), 80)])
			err = c.client.DownloadLinks([]string{torrentURL}, opts)
			fmt.Printf("[DEBUG Add] DownloadLinks returned err: %v\n", err)
		} else {
			// For .torrent files, upload bytes directly
			fmt.Printf("[DEBUG Add] Calling addTorrentFromBytes with filename: %s, bytes len: %d\n", torrentFilename, len(torrentBytes))
			err = c.addTorrentFromBytes(ctx, torrentBytes, torrentFilename, opts)
			fmt.Printf("[DEBUG Add] addTorrentFromBytes returned err: %v\n", err)
		}

		if err != nil {
			// If it's an auth error, clear session and retry
			if strings.Contains(err.Error(), "login") || strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "unauthorized") {
				c.client.Authenticated = false
				continue
			}
			continue
		}

		// For magnet URLs, extract hash directly
		if isMagnet {
			hash, hashErr := extractHashFromMagnet(torrentURL)
			if hashErr == nil {
				result.ExternalID = hash
				result.Name = extractNameFromMagnet(torrentURL)

				// Add tags if provided
				if len(req.Tags) > 0 {
					c.client.AddTorrentTags([]string{hash}, req.Tags)
				}

				return result, nil
			}
			// If hash extraction fails for a magnet URL, something is wrong
			return result, fmt.Errorf("failed to extract hash from magnet URL: %w", hashErr)
		}

		// For .torrent files, poll qBittorrent to find the newly-added torrent by diffing hashes
		const pollAttempts = 10
		const pollDelay = 500 * time.Millisecond

		fmt.Printf("[DEBUG Add] Polling for new torrent (existing count: %d)\n", len(existing))
		var newest *qbt.TorrentInfo
		for poll := 0; poll < pollAttempts; poll++ {
			if poll > 0 {
				time.Sleep(pollDelay)
			}

			torrents, listErr := c.listTorrentsWithAuth(ctx, qbt.TorrentsOptions{})
			if listErr != nil {
				fmt.Printf("[DEBUG Add] Poll %d: listTorrentsWithAuth error: %v\n", poll, listErr)
				continue
			}
			fmt.Printf("[DEBUG Add] Poll %d: found %d total torrents\n", poll, len(torrents))

			for i := range torrents {
				t := &torrents[i]
				if existing[t.Hash] {
					continue
				}
				fmt.Printf("[DEBUG Add] Poll %d: found NEW torrent hash=%s name=%s\n", poll, t.Hash, t.Name)
				// First unseen becomes candidate; if multiple, pick newest by AddedOn.
				if newest == nil || t.AddedOn > newest.AddedOn {
					newest = t
				}
			}

			if newest != nil {
				break
			}
		}

		if newest == nil {
			fmt.Printf("[DEBUG Add] FAILED: torrent was uploaded but could not be found after %d polls\n", pollAttempts)
			return result, fmt.Errorf("torrent was uploaded but could not be found in qBittorrent")
		}
		fmt.Printf("[DEBUG Add] SUCCESS: found torrent hash=%s name=%s\n", newest.Hash, newest.Name)

		result.ExternalID = newest.Hash
		result.Name = newest.Name

		// Add tags if provided
		if len(req.Tags) > 0 {
			c.client.AddTorrentTags([]string{newest.Hash}, req.Tags)
		}

		return result, nil
	}

	return result, fmt.Errorf("failed to add torrent after %d attempts: %w", maxRetries+1, err)
}

// Get gets a torrent by external ID (hash)
func (c *qBittorrentClient) Get(ctx context.Context, externalID string) (downloader.Item, error) {
	var err error
	var item downloader.Item

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay)
		}

		if err = c.ensureLoggedIn(ctx); err != nil {
			continue
		}

		// Use Torrents with hash filter
		opts := qbt.TorrentsOptions{
			Hashes: []string{externalID},
		}
		fmt.Printf("[DEBUG Get] Looking for hash: %s\n", externalID)
		var torrents []qbt.TorrentInfo
		torrents, err = c.client.Torrents(opts)
		fmt.Printf("[DEBUG Get] Found %d torrents, err: %v\n", len(torrents), err)
		if err != nil {
			if strings.Contains(err.Error(), "login") || strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "unauthorized") {
				c.client.Authenticated = false
				continue
			}
			continue
		}

		if len(torrents) == 0 {
			fmt.Printf("[DEBUG Get] Torrent not found for hash: %s\n", externalID)
			return item, fmt.Errorf("torrent not found: %s", externalID)
		}

		t := torrents[0]
		item.ExternalID = t.Hash
		item.Name = t.Name
		item.Status = mapStateToStatus(t.State)
		item.Progress = t.Progress
		item.SavePath = t.SavePath
		item.ContentPath = t.ContentPath
		item.DownloadSpeed = int64(t.Dlspeed)
		item.ETA = int64(t.Eta)
		item.TotalSize = int64(t.TotalSize)
		item.AddedAt = time.Unix(t.AddedOn, 0)

		return item, nil
	}

	return item, fmt.Errorf("failed to get torrent after %d attempts: %w", maxRetries+1, err)
}

// List lists all items (torrents) in the client
func (c *qBittorrentClient) List(ctx context.Context) ([]downloader.Item, error) {
	var err error
	var items []downloader.Item

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay)
		}

		if err = c.ensureLoggedIn(ctx); err != nil {
			continue
		}

		// Get all torrents
		torrents, err := c.client.Torrents(qbt.TorrentsOptions{})
		if err != nil {
			if strings.Contains(err.Error(), "login") || strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "unauthorized") {
				c.client.Authenticated = false
				continue
			}
			continue
		}

		items = make([]downloader.Item, len(torrents))
		for i, t := range torrents {
			items[i] = downloader.Item{
				ExternalID:    t.Hash,
				Name:          t.Name,
				Status:        mapStateToStatus(t.State),
				Progress:      t.Progress,
				SavePath:      t.SavePath,
				ContentPath:   t.ContentPath,
				DownloadSpeed: int64(t.Dlspeed),
				ETA:           int64(t.Eta),
				TotalSize:     int64(t.TotalSize),
				AddedAt:       time.Unix(t.AddedOn, 0),
			}
		}

		return items, nil
	}

	return nil, fmt.Errorf("failed to list torrents after %d attempts: %w", maxRetries+1, err)
}

// ListFiles lists files for a torrent
func (c *qBittorrentClient) ListFiles(ctx context.Context, externalID string) ([]downloader.File, error) {
	var err error
	var files []downloader.File

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay)
		}

		if err = c.ensureLoggedIn(ctx); err != nil {
			continue
		}

		// Use library's TorrentFiles method
		torrentFiles, err := c.client.TorrentFiles(externalID)
		if err != nil {
			if strings.Contains(err.Error(), "login") || strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "unauthorized") {
				c.client.Authenticated = false
				continue
			}
			continue
		}

		files = make([]downloader.File, len(torrentFiles))
		for i, f := range torrentFiles {
			files[i] = downloader.File{
				Path:     f.Name,
				Size:     int64(f.Size),
				Progress: f.Progress,
				Priority: f.Priority,
			}
		}

		return files, nil
	}

	return nil, fmt.Errorf("failed to list files after %d attempts: %w", maxRetries+1, err)
}

// Pause pauses a torrent
func (c *qBittorrentClient) Pause(ctx context.Context, externalID string) error {
	return c.withRetry(ctx, func() error {
		return c.client.Pause([]string{externalID})
	})
}

// Resume resumes a torrent
func (c *qBittorrentClient) Resume(ctx context.Context, externalID string) error {
	return c.withRetry(ctx, func() error {
		return c.client.Resume([]string{externalID})
	})
}

// Remove removes a torrent
func (c *qBittorrentClient) Remove(ctx context.Context, externalID string, deleteData bool) error {
	return c.withRetry(ctx, func() error {
		return c.client.Delete([]string{externalID}, deleteData)
	})
}

// ensureLoggedIn ensures the client is logged in
func (c *qBittorrentClient) ensureLoggedIn(ctx context.Context) error {
	if !c.client.Authenticated {
		return c.client.Login(c.username, c.password)
	}
	return nil
}

// listTorrentsWithAuth lists torrents with authentication retry logic
// This prevents JSON parsing errors when qBittorrent returns HTML error pages (e.g., "Forbidden")
func (c *qBittorrentClient) listTorrentsWithAuth(ctx context.Context, opts qbt.TorrentsOptions) ([]qbt.TorrentInfo, error) {
	var err error
	var torrents []qbt.TorrentInfo

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay)
		}

		if err = c.ensureLoggedIn(ctx); err != nil {
			continue
		}

		torrents, err = c.client.Torrents(opts)
		if err != nil {
			// If it's an auth error, clear session and retry
			if strings.Contains(err.Error(), "login") ||
				strings.Contains(err.Error(), "401") ||
				strings.Contains(err.Error(), "403") ||
				strings.Contains(err.Error(), "unauthorized") ||
				strings.Contains(err.Error(), "Forbidden") ||
				strings.Contains(err.Error(), "invalid character") { // JSON parse errors often indicate auth issues
				c.client.Authenticated = false
				continue
			}
			return nil, err
		}

		return torrents, nil
	}

	return nil, fmt.Errorf("failed to list torrents after %d attempts: %w", maxRetries+1, err)
}

// withRetry executes a function with retry logic
func (c *qBittorrentClient) withRetry(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay)
		}

		if err = c.ensureLoggedIn(ctx); err != nil {
			continue
		}

		err = fn()
		if err == nil {
			return nil
		}

		if strings.Contains(err.Error(), "login") || strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "unauthorized") {
			c.client.Authenticated = false
			continue
		}
	}
	return fmt.Errorf("failed after %d attempts: %w", maxRetries+1, err)
}

// fetchTorrentFile downloads a .torrent file from the given URL (e.g., Prowlarr proxy URL)
// Returns the torrent bytes and extracted filename
func (c *qBittorrentClient) fetchTorrentFile(ctx context.Context, torrentURL string) ([]byte, string, error) {
	fmt.Printf("[DEBUG fetchTorrentFile] Fetching from URL: %s\n", torrentURL)
	req, err := http.NewRequestWithContext(ctx, "GET", torrentURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", "Arrflix/1.0")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[DEBUG fetchTorrentFile] Fetch error: %v\n", err)
		return nil, "", fmt.Errorf("fetch torrent: %w", err)
	}
	defer resp.Body.Close()

	fmt.Printf("[DEBUG fetchTorrentFile] Response status: %d, Content-Type: %s\n", resp.StatusCode, resp.Header.Get("Content-Type"))
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("[DEBUG fetchTorrentFile] Error body: %s\n", string(body[:min(len(body), 200)]))
		return nil, "", fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	// Extract filename from Content-Disposition header or fall back to URL path
	filename := "download.torrent"
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if fn, ok := params["filename"]; ok {
				filename = fn
			}
		}
	} else {
		// Try to extract filename from URL path
		if u, err := url.Parse(torrentURL); err == nil {
			if base := path.Base(u.Path); base != "" && base != "." && base != "/" {
				filename = base
			}
		}
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read response: %w", err)
	}

	// Basic validation - torrent files start with "d" (bencoded dictionary)
	if len(data) == 0 {
		return nil, "", fmt.Errorf("empty response")
	}

	return data, filename, nil
}

// addTorrentFromBytes uploads torrent file bytes directly to qBittorrent
// This bypasses the library's DownloadFiles which requires a local file path
func (c *qBittorrentClient) addTorrentFromBytes(ctx context.Context, torrentBytes []byte, filename string, opts qbt.DownloadOptions) error {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)

	// Create form file for torrent bytes
	formWriter, err := writer.CreateFormFile("torrents", filename)
	if err != nil {
		return fmt.Errorf("create form file: %w", err)
	}

	if _, err := formWriter.Write(torrentBytes); err != nil {
		return fmt.Errorf("write torrent bytes: %w", err)
	}

	// Add optional parameters matching qBittorrent API
	if opts.Savepath != nil && *opts.Savepath != "" {
		writer.WriteField("savepath", *opts.Savepath)
	}
	if opts.Category != nil && *opts.Category != "" {
		writer.WriteField("category", *opts.Category)
	}
	if opts.Paused != nil {
		writer.WriteField("paused", strconv.FormatBool(*opts.Paused))
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("close writer: %w", err)
	}

	// Make HTTP request using the client's cookie jar for auth
	apiURL := strings.TrimSuffix(c.client.URL, "/") + "/api/v2/torrents/add"
	fmt.Printf("[DEBUG addTorrentFromBytes] Posting to: %s\n", apiURL)
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, &buffer)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	httpClient := &http.Client{Jar: c.client.Jar}
	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Printf("[DEBUG addTorrentFromBytes] Request error: %v\n", err)
		return fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("[DEBUG addTorrentFromBytes] Response status: %d, body: %s\n", resp.StatusCode, string(body))

	if resp.StatusCode == 415 {
		return fmt.Errorf("torrent file is not valid")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// mapStateToStatus maps qBittorrent state to JobStatus
func mapStateToStatus(state string) downloader.JobStatus {
	// qBittorrent states mapping based on official documentation/API
	switch state {
	case "downloading", "metaDL", "stalledDL", "checkingDL", "forcedDL", "allocating":
		return downloader.StatusDownloading
	case "uploading", "stalledUP", "checkingUP", "forcedUP", "seeding":
		return downloader.StatusSeeding
	case "completed":
		return downloader.StatusCompleted
	case "pausedDL", "pausedUP":
		return downloader.StatusPaused
	case "queuedDL", "queuedUP", "checkingResumeData", "moving":
		return downloader.StatusQueued
	case "error", "missingFiles":
		return downloader.StatusErrored
	default:
		return downloader.StatusUnknown
	}
}

// extractHashFromMagnet extracts the hash from a magnet URL using proper URL parsing
func extractHashFromMagnet(magnetURL string) (string, error) {
	// Parse magnet URL
	if !strings.HasPrefix(magnetURL, "magnet:") {
		return "", fmt.Errorf("not a magnet URL")
	}

	u, err := url.Parse(magnetURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse magnet URL: %w", err)
	}

	// The xt parameter contains the hash in format: urn:btih:HASH
	xtValues := u.Query()["xt"]
	for _, xt := range xtValues {
		if strings.HasPrefix(xt, "urn:btih:") {
			hash := strings.TrimPrefix(xt, "urn:btih:")
			// Normalize hash to lowercase
			hash = strings.ToLower(hash)
			// Hash can be 40 chars (hex) or 32 chars (base32)
			if len(hash) != 40 && len(hash) != 32 {
				return "", fmt.Errorf("invalid hash length: got %d chars", len(hash))
			}
			return hash, nil
		}
	}

	return "", fmt.Errorf("no btih hash found in magnet URL")
}

// extractNameFromMagnet extracts the name from a magnet URL
func extractNameFromMagnet(magnetURL string) string {
	// Parse magnet URL to extract dn parameter
	u, err := url.Parse(magnetURL)
	if err != nil {
		return ""
	}

	// Get dn parameter from query string
	name := u.Query().Get("dn")
	if name != "" {
		// URL decode the name
		decoded, err := url.QueryUnescape(name)
		if err == nil {
			return decoded
		}
		return name
	}
	return ""
}

// getVersionWithStatusCheck makes a direct HTTP request to check status code
// This works around the library's issue where it returns "Forbidden" as a string instead of an error
func (c *qBittorrentClient) getVersionWithStatusCheck(ctx context.Context) (string, error) {
	// Build the URL
	versionURL := strings.TrimSuffix(c.client.URL, "/") + "/api/v2/app/version"

	req, err := http.NewRequestWithContext(ctx, "GET", versionURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	// Create HTTP client with same cookie jar to preserve authentication
	httpClient := &http.Client{
		Jar: c.client.Jar,
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code - this is the key fix
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("authentication failed: received status code %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	version := strings.TrimSpace(string(body))
	// Additional validation: version should not be empty and should look like a version
	if version == "" {
		return "", fmt.Errorf("empty version response")
	}

	return version, nil
}

// getWebAPIVersionWithStatusCheck makes a direct HTTP request to check status code
func (c *qBittorrentClient) getWebAPIVersionWithStatusCheck(ctx context.Context) (string, error) {
	// Build the URL
	versionURL := strings.TrimSuffix(c.client.URL, "/") + "/api/v2/app/webapiVersion"

	req, err := http.NewRequestWithContext(ctx, "GET", versionURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	// Create HTTP client with same cookie jar to preserve authentication
	httpClient := &http.Client{
		Jar: c.client.Jar,
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("authentication failed: received status code %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	return strings.TrimSpace(string(body)), nil
}
