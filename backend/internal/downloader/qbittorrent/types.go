package qbittorrent

import "fmt"

// TorrentInfo represents a torrent from the qBittorrent API.
// Fields match the JSON keys from GET /api/v2/torrents/info.
type TorrentInfo struct {
	Hash        string  `json:"hash"`
	Name        string  `json:"name"`
	State       string  `json:"state"`
	Progress    float64 `json:"progress"`
	SavePath    string  `json:"save_path"`
	ContentPath string  `json:"content_path"`
	Dlspeed     int     `json:"dlspeed"`
	Eta         int     `json:"eta"`
	TotalSize   int64   `json:"total_size"`
	AddedOn     int64   `json:"added_on"`
}

// TorrentFile represents a file within a torrent.
// Fields match the JSON keys from GET /api/v2/torrents/files.
type TorrentFile struct {
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
	Priority int     `json:"priority"`
}

// AuthError indicates a 401/403 response from qBittorrent (expired session or bad credentials).
type AuthError struct {
	StatusCode int
	Body       string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("qbittorrent auth error (HTTP %d): %s", e.StatusCode, e.Body)
}

// Unreachable marks an auth failure as the downloader being unusable rather than
// a verdict on the torrent: bad credentials or a banned IP never mean the
// release is bad.
func (e *AuthError) Unreachable() bool { return true }

// APIError indicates a non-2xx response from qBittorrent that isn't an auth error.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("qbittorrent API error (HTTP %d): %s", e.StatusCode, e.Body)
}

// Unreachable reports a 5xx as qBittorrent failing, not rejecting the request.
// A 4xx is qBittorrent's verdict on the request itself.
func (e *APIError) Unreachable() bool { return e.StatusCode >= 500 }
