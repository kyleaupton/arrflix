package qbittorrent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kyleaupton/arrflix/internal/downloader"
)

const testMagnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=x"

// fakeQbit answers login with a SID and every other request with addStatus,
// standing in for a qBittorrent that is up.
func fakeQbit(t *testing.T, addStatus int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v2/auth/login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "sid"})
		_, _ = w.Write([]byte("Ok."))
	})
	mux.HandleFunc("GET /api/v2/torrents/info", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("[]"))
	})
	mux.HandleFunc("POST /api/v2/torrents/add", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", addStatus)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// deadURL returns the URL of a server that has already shut down, so a request
// to it fails with a refused connection.
func deadURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	return srv.URL
}

// torrentHost serves a .torrent file body at any path.
func torrentHost(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("d4:infod4:name1:xee"))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestReqHard003_UnreachableClassification:
// REQ-HARD-003 — A downloader that is unreachable must leave its jobs retryable;
// a downloader that rejects the torrent itself is a verdict on the release. The
// real client's errors are classified by where they came from: failing to reach
// qBittorrent's own API (refused, 5xx) is an outage, while a 4xx from qBittorrent
// or a failure to fetch the release's torrent file from the indexer is not.
func TestReqHard003_UnreachableClassification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		qbitURL    func(t *testing.T) string
		torrentURL func(t *testing.T) string
		want       bool
	}{
		{
			name:       "qBittorrent connection refused",
			qbitURL:    deadURL,
			torrentURL: func(*testing.T) string { return testMagnet },
			want:       true,
		},
		{
			name:       "qBittorrent 5xx",
			qbitURL:    func(t *testing.T) string { return fakeQbit(t, http.StatusServiceUnavailable).URL },
			torrentURL: func(*testing.T) string { return testMagnet },
			want:       true,
		},
		{
			name:       "qBittorrent rejects torrent (4xx)",
			qbitURL:    func(t *testing.T) string { return fakeQbit(t, http.StatusUnsupportedMediaType).URL },
			torrentURL: func(t *testing.T) string { return torrentHost(t) + "/release.torrent" },
			want:       false,
		},
		{
			name:       "torrent link host unreachable",
			qbitURL:    func(t *testing.T) string { return fakeQbit(t, http.StatusOK).URL },
			torrentURL: func(t *testing.T) string { return deadURL(t) + "/release.torrent" },
			want:       false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			client := NewQBittorrentClient("dl", c.qbitURL(t), "admin", "admin")
			_, err := client.Add(context.Background(), downloader.AddRequest{MagnetURL: c.torrentURL(t)})
			if err == nil {
				t.Fatal("Add succeeded, want an error")
			}
			if got := downloader.IsUnreachable(err); got != c.want {
				t.Errorf("IsUnreachable(%v) = %v, want %v", err, got, c.want)
			}
		})
	}
}

// TestIsUnreachable pins the error types that classify themselves.
func TestIsUnreachable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"marked", downloader.MarkUnreachable(errors.New("dial tcp: connection refused")), true},
		{"5xx", &APIError{StatusCode: http.StatusServiceUnavailable}, true},
		{"auth", &AuthError{StatusCode: http.StatusForbidden}, true},
		{"4xx rejection", &APIError{StatusCode: http.StatusUnsupportedMediaType}, false},
		{"unclassified", errors.New("torrent not found"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := downloader.IsUnreachable(c.err); got != c.want {
				t.Errorf("IsUnreachable(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}
