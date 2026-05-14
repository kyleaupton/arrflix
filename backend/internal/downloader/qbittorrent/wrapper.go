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
	"strings"
	"time"

	"github.com/kyleaupton/arrflix/internal/downloader"
)

// qBittorrentClient implements downloader.Client using our custom HTTP client.
// Each instance is scoped to a single qBittorrent server.
type qBittorrentClient struct {
	instanceID downloader.InstanceID
	client     *Client
}

// NewQBittorrentClient creates a new qBittorrent client wrapper.
func NewQBittorrentClient(instanceID downloader.InstanceID, baseURL, username, password string) *qBittorrentClient {
	return &qBittorrentClient{
		instanceID: instanceID,
		client:     NewClient(baseURL, username, password),
	}
}

func (c *qBittorrentClient) Type() downloader.Type             { return downloader.TypeQbittorrent }
func (c *qBittorrentClient) InstanceID() downloader.InstanceID { return c.instanceID }

// Test tests the connection to qBittorrent.
func (c *qBittorrentClient) Test(ctx context.Context) (downloader.TestResult, error) {
	result := downloader.TestResult{}

	if err := c.client.Login(ctx); err != nil {
		errMsg := err.Error()
		var errorType string
		if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "connection refused") || strings.Contains(errMsg, "no such host") {
			errorType = "Unable to connect to qBittorrent. Check if qBittorrent is running and the URL is correct."
		} else if strings.Contains(errMsg, "auth error") || strings.Contains(errMsg, "invalid credentials") {
			errorType = "Authentication failed - check username and password"
		} else {
			errorType = "Connection test failed: " + errMsg
		}
		result.Success = false
		result.Error = errorType
		return result, nil
	}

	version, err := c.client.GetVersion(ctx)
	if err != nil {
		result.Success = false
		result.Error = "Connected but unable to retrieve version information: " + err.Error()
		return result, nil
	}

	webAPIVersion, _ := c.client.GetWebAPIVersion(ctx)

	result.Success = true
	result.Message = "Connection test successful"
	result.Version = version
	result.WebAPIVersion = webAPIVersion
	return result, nil
}

// Add adds a download (magnet URL or torrent file URL).
func (c *qBittorrentClient) Add(ctx context.Context, req downloader.AddRequest) (downloader.AddResult, error) {
	var result downloader.AddResult

	torrentURL := req.MagnetURL
	if torrentURL == "" {
		return result, fmt.Errorf("magnet URL or torrent file URL is required")
	}

	isMagnet := strings.HasPrefix(torrentURL, "magnet:")

	// Build common download options
	opts := map[string]string{}
	if req.SavePath != "" {
		opts["savepath"] = req.SavePath
	}
	if req.Category != "" {
		opts["category"] = req.Category
	}
	if req.Paused {
		opts["paused"] = "true"
	}

	if isMagnet {
		return c.addMagnet(ctx, torrentURL, opts, req.Tags)
	}
	return c.addTorrentFile(ctx, torrentURL, opts, req.Tags)
}

func (c *qBittorrentClient) addMagnet(ctx context.Context, magnetURL string, opts map[string]string, tags []string) (downloader.AddResult, error) {
	var result downloader.AddResult

	if err := c.client.AddMagnet(ctx, magnetURL, opts); err != nil {
		return result, fmt.Errorf("add magnet: %w", err)
	}

	hash, err := extractHashFromMagnet(magnetURL)
	if err != nil {
		return result, fmt.Errorf("extract hash from magnet URL: %w", err)
	}

	result.ExternalID = hash
	result.Name = extractNameFromMagnet(magnetURL)

	if len(tags) > 0 {
		// Best-effort, don't fail the add if tagging fails
		_ = c.client.AddTags(ctx, []string{hash}, tags)
	}

	return result, nil
}

func (c *qBittorrentClient) addTorrentFile(ctx context.Context, torrentURL string, opts map[string]string, tags []string) (downloader.AddResult, error) {
	var result downloader.AddResult

	// Fetch the .torrent file from the URL (e.g. Prowlarr proxy)
	torrentBytes, torrentFilename, err := c.fetchTorrentFile(ctx, torrentURL)
	if err != nil {
		return result, fmt.Errorf("fetch torrent file: %w", err)
	}

	// Snapshot existing torrents so we can identify the new one
	existingTorrents, _ := c.client.ListTorrents(ctx)
	existing := make(map[string]bool, len(existingTorrents))
	for _, t := range existingTorrents {
		existing[t.Hash] = true
	}

	// Build multipart form body
	body, contentType, err := buildTorrentUploadForm(torrentBytes, torrentFilename, opts)
	if err != nil {
		return result, fmt.Errorf("build upload form: %w", err)
	}

	if err := c.client.AddTorrentFile(ctx, body, contentType); err != nil {
		return result, fmt.Errorf("upload torrent: %w", err)
	}

	// Poll to find the newly-added torrent
	const pollAttempts = 10
	const pollDelay = 500 * time.Millisecond

	var newest *TorrentInfo
	for poll := 0; poll < pollAttempts; poll++ {
		if poll > 0 {
			time.Sleep(pollDelay)
		}

		torrents, err := c.client.ListTorrents(ctx)
		if err != nil {
			continue
		}

		for i := range torrents {
			t := &torrents[i]
			if existing[t.Hash] {
				continue
			}
			if newest == nil || t.AddedOn > newest.AddedOn {
				newest = t
			}
		}

		if newest != nil {
			break
		}
	}

	if newest == nil {
		return result, fmt.Errorf("torrent was uploaded but could not be found in qBittorrent")
	}

	result.ExternalID = newest.Hash
	result.Name = newest.Name

	if len(tags) > 0 {
		_ = c.client.AddTags(ctx, []string{newest.Hash}, tags)
	}

	return result, nil
}

// Get gets a torrent by external ID (hash).
func (c *qBittorrentClient) Get(ctx context.Context, externalID string) (downloader.Item, error) {
	var item downloader.Item

	torrents, err := c.client.ListTorrents(ctx, externalID)
	if err != nil {
		return item, fmt.Errorf("downloader get: %w", err)
	}

	if len(torrents) == 0 {
		return item, fmt.Errorf("torrent not found: %s", externalID)
	}

	return torrentInfoToItem(torrents[0]), nil
}

// List lists all torrents.
func (c *qBittorrentClient) List(ctx context.Context) ([]downloader.Item, error) {
	torrents, err := c.client.ListTorrents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list torrents: %w", err)
	}

	items := make([]downloader.Item, len(torrents))
	for i, t := range torrents {
		items[i] = torrentInfoToItem(t)
	}
	return items, nil
}

// ListFiles lists files for a torrent.
func (c *qBittorrentClient) ListFiles(ctx context.Context, externalID string) ([]downloader.File, error) {
	torrentFiles, err := c.client.ListTorrentFiles(ctx, externalID)
	if err != nil {
		return nil, fmt.Errorf("list torrent files: %w", err)
	}

	files := make([]downloader.File, len(torrentFiles))
	for i, f := range torrentFiles {
		files[i] = downloader.File{
			Path:     f.Name,
			Size:     f.Size,
			Progress: f.Progress,
			Priority: f.Priority,
		}
	}
	return files, nil
}

func (c *qBittorrentClient) Pause(ctx context.Context, externalID string) error {
	return c.client.Pause(ctx, []string{externalID})
}

func (c *qBittorrentClient) Resume(ctx context.Context, externalID string) error {
	return c.client.Resume(ctx, []string{externalID})
}

func (c *qBittorrentClient) Remove(ctx context.Context, externalID string, deleteData bool) error {
	return c.client.Delete(ctx, []string{externalID}, deleteData)
}

// --- Helpers ---

func torrentInfoToItem(t TorrentInfo) downloader.Item {
	return downloader.Item{
		ExternalID:    t.Hash,
		Name:          t.Name,
		Status:        mapStateToStatus(t.State),
		Progress:      t.Progress,
		SavePath:      t.SavePath,
		ContentPath:   t.ContentPath,
		DownloadSpeed: int64(t.Dlspeed),
		ETA:           int64(t.Eta),
		TotalSize:     t.TotalSize,
		AddedAt:       time.Unix(t.AddedOn, 0),
	}
}

func mapStateToStatus(state string) downloader.JobStatus {
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

func extractHashFromMagnet(magnetURL string) (string, error) {
	if !strings.HasPrefix(magnetURL, "magnet:") {
		return "", fmt.Errorf("not a magnet URL")
	}

	u, err := url.Parse(magnetURL)
	if err != nil {
		return "", fmt.Errorf("parse magnet URL: %w", err)
	}

	for _, xt := range u.Query()["xt"] {
		if strings.HasPrefix(xt, "urn:btih:") {
			hash := strings.ToLower(strings.TrimPrefix(xt, "urn:btih:"))
			if len(hash) != 40 && len(hash) != 32 {
				return "", fmt.Errorf("invalid hash length: got %d chars", len(hash))
			}
			return hash, nil
		}
	}

	return "", fmt.Errorf("no btih hash found in magnet URL")
}

func extractNameFromMagnet(magnetURL string) string {
	u, err := url.Parse(magnetURL)
	if err != nil {
		return ""
	}
	name := u.Query().Get("dn")
	if name != "" {
		if decoded, err := url.QueryUnescape(name); err == nil {
			return decoded
		}
		return name
	}
	return ""
}

// fetchTorrentFile downloads a .torrent file from the given URL.
func (c *qBittorrentClient) fetchTorrentFile(ctx context.Context, torrentURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", torrentURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Arrflix/1.0")

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch torrent: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}

	filename := "download.torrent"
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if fn, ok := params["filename"]; ok {
				filename = fn
			}
		}
	} else if u, err := url.Parse(torrentURL); err == nil {
		if base := path.Base(u.Path); base != "" && base != "." && base != "/" {
			filename = base
		}
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read response: %w", err)
	}

	if len(data) == 0 {
		return nil, "", fmt.Errorf("empty response")
	}

	return data, filename, nil
}

// buildTorrentUploadForm creates a multipart form body for uploading a .torrent file.
func buildTorrentUploadForm(torrentBytes []byte, filename string, opts map[string]string) ([]byte, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	formWriter, err := writer.CreateFormFile("torrents", filename)
	if err != nil {
		return nil, "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := formWriter.Write(torrentBytes); err != nil {
		return nil, "", fmt.Errorf("write torrent bytes: %w", err)
	}

	for k, v := range opts {
		if err := writer.WriteField(k, v); err != nil {
			return nil, "", fmt.Errorf("write form field %q: %w", k, err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("close writer: %w", err)
	}

	return buf.Bytes(), writer.FormDataContentType(), nil
}
