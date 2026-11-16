// Package qbittest provides a fake qBittorrent Web API server for integration
// tests, so the real qBittorrent client runs against an in-process stand-in.
//
// Typical usage:
//
//	qb := qbittest.New(t)
//	// point a downloader record's URL at qb.URL, run the code under test, then:
//	torrent, ok := qb.Torrent(hash)
//
// The fake keeps torrents, categories and tags in memory. Adding a torrent
// records its category but never creates one: like a strict qBittorrent, a
// category exists only once createCategory has made it. Only magnet adds are
// supported. Any endpoint the fake doesn't implement fails the test loudly with
// t.Errorf and HTTP 501, so a call the code under test shouldn't make (pause,
// delete) is caught rather than silently answered.
package qbittest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Torrent is the fake's record of one added torrent.
type Torrent struct {
	Hash     string
	Name     string
	Category string
	Tags     []string
}

// Server is a fake qBittorrent Web API.
type Server struct {
	URL string

	t   *testing.T
	srv *httptest.Server

	mu         sync.Mutex
	torrents   map[string]*Torrent
	categories map[string]bool
	tagCalls   int
	failTags   bool
	onAdd      func(hash string)
}

// New starts a fake qBittorrent server, closed automatically when the test ends.
func New(t *testing.T) *Server {
	t.Helper()
	s := &Server{
		t:          t,
		torrents:   map[string]*Torrent{},
		categories: map[string]bool{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v2/auth/login", s.login)
	mux.HandleFunc("GET /api/v2/app/version", text("v4.6.7"))
	mux.HandleFunc("GET /api/v2/app/webapiVersion", text("2.9.3"))
	mux.HandleFunc("GET /api/v2/torrents/categories", s.listCategories)
	mux.HandleFunc("POST /api/v2/torrents/createCategory", s.createCategory)
	mux.HandleFunc("POST /api/v2/torrents/add", s.add)
	mux.HandleFunc("POST /api/v2/torrents/addTags", s.addTags)
	mux.HandleFunc("GET /api/v2/torrents/info", s.info)
	mux.HandleFunc("GET /api/v2/torrents/files", s.files)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("qbittest: unexpected request %s %s", r.Method, r.URL.Path)
		http.Error(w, "not implemented by qbittest", http.StatusNotImplemented)
	})
	s.srv = httptest.NewServer(mux)
	s.URL = s.srv.URL
	t.Cleanup(s.srv.Close)
	return s
}

// Close shuts the server down, making the downloader unreachable.
func (s *Server) Close() { s.srv.Close() }

// SeedCategory creates a category up front, as if the user had made it.
func (s *Server) SeedCategory(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.categories[name] = true
}

// OnAdd registers a hook run while an add request is in flight, after the
// torrent is recorded and before the response is sent. A hook that blocks holds
// the caller's add open.
func (s *Server) OnAdd(fn func(hash string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onAdd = fn
}

// FailTags makes every addTags call answer HTTP 500 (still counted).
func (s *Server) FailTags() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failTags = true
}

// Torrent returns a copy of the torrent with the given hash.
func (s *Server) Torrent(hash string) (Torrent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tor, ok := s.torrents[hash]
	if !ok {
		return Torrent{}, false
	}
	out := *tor
	out.Tags = slices.Clone(tor.Tags)
	return out, true
}

// HasCategory reports whether the category exists.
func (s *Server) HasCategory(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.categories[name]
}

// TagCalls counts addTags requests, failed ones included.
func (s *Server) TagCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tagCalls
}

func text(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }
}

func (s *Server) login(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "SID", Value: "qbittest-session"})
	_, _ = w.Write([]byte("Ok."))
}

func (s *Server) listCategories(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	out := map[string]any{}
	for name := range s.categories {
		out[name] = map[string]string{"name": name, "savePath": ""}
	}
	s.mu.Unlock()
	writeJSON(w, out)
}

func (s *Server) createCategory(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("category")
	s.mu.Lock()
	defer s.mu.Unlock()
	if name == "" || s.categories[name] {
		http.Error(w, "", http.StatusConflict)
		return
	}
	s.categories[name] = true
}

func (s *Server) add(w http.ResponseWriter, r *http.Request) {
	magnet := r.FormValue("urls")
	u, err := url.Parse(magnet)
	if err != nil || u.Scheme != "magnet" {
		s.t.Errorf("qbittest: add supports magnet links only, got %q", magnet)
		http.Error(w, "magnet only", http.StatusNotImplemented)
		return
	}
	var hash string
	for _, xt := range u.Query()["xt"] {
		if h, ok := strings.CutPrefix(xt, "urn:btih:"); ok {
			hash = strings.ToLower(h)
		}
	}
	tor := &Torrent{Hash: hash, Name: u.Query().Get("dn"), Category: r.FormValue("category")}
	if tags := r.FormValue("tags"); tags != "" {
		tor.Tags = strings.Split(tags, ",")
	}

	s.mu.Lock()
	s.torrents[hash] = tor
	hook := s.onAdd
	s.mu.Unlock()

	if hook != nil {
		hook(hash)
	}
	_, _ = w.Write([]byte("Ok."))
}

func (s *Server) addTags(w http.ResponseWriter, r *http.Request) {
	hashes := strings.Split(r.FormValue("hashes"), "|")
	tags := strings.Split(r.FormValue("tags"), ",")

	s.mu.Lock()
	defer s.mu.Unlock()
	s.tagCalls++
	if s.failTags {
		http.Error(w, "qbittest: tagging fails", http.StatusInternalServerError)
		return
	}
	for _, h := range hashes {
		tor, ok := s.torrents[h]
		if !ok {
			continue
		}
		for _, tag := range tags {
			if !slices.Contains(tor.Tags, tag) {
				tor.Tags = append(tor.Tags, tag)
			}
		}
	}
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	var filter []string
	if h := r.URL.Query().Get("hashes"); h != "" {
		filter = strings.Split(h, "|")
	}
	s.mu.Lock()
	out := []map[string]any{}
	for hash, tor := range s.torrents {
		if filter != nil && !slices.Contains(filter, hash) {
			continue
		}
		out = append(out, map[string]any{
			"hash":     hash,
			"name":     tor.Name,
			"state":    "downloading",
			"progress": 0.5,
			"category": tor.Category,
			"tags":     strings.Join(tor.Tags, ","),
		})
	}
	s.mu.Unlock()
	writeJSON(w, out)
}

func (s *Server) files(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, []any{})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
