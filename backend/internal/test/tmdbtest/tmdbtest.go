// Package tmdbtest provides a fake TMDB HTTP server with declarative
// builders for integration tests.
//
// Typical usage:
//
//	srv, client := tmdbtest.New(t)
//	srv.OnSearchMulti("inception", tmdbtest.Movie{ID: 27205, Title: "Inception", ReleaseDate: "2010-07-15"})
//	res, err := client.GetSearchMulti("inception", nil)
//
// Any HTTP call that has not been registered with one of the On* methods will
// fail the test loudly with t.Errorf and return HTTP 501. This makes
// unexpected TMDB calls (from code under test) obvious rather than silent.
package tmdbtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	tmdb "github.com/cyruzin/golang-tmdb"
)

// rewriteTransport redirects every outbound request to the target host and
// strips the "/3" API-version path prefix. It exists because cyruzin/golang-tmdb's
// SetCustomBaseURL mutates a package-level global — fine for a single live
// client, unsafe for parallel tests that each want their own fake server.
// Installing this via Client.SetClientConfig keeps the rewrite per-client.
type rewriteTransport struct {
	target *url.URL
	rt     http.RoundTripper
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = t.target.Scheme
	req.URL.Host = t.target.Host
	req.URL.Path = strings.TrimPrefix(req.URL.Path, "/3")
	req.Host = t.target.Host
	return t.rt.RoundTrip(req)
}

// Hit is a search-result builder. Implementations produce the per-result JSON
// map that TMDB returns inside `results`.
type Hit interface {
	asSearchResult() map[string]any
}

// Movie builds a media_type="movie" search hit.
type Movie struct {
	ID          int64
	Title       string
	ReleaseDate string // "1999-03-31"
}

func (m Movie) asSearchResult() map[string]any {
	return map[string]any{
		"id":             m.ID,
		"media_type":     "movie",
		"title":          m.Title,
		"original_title": m.Title,
		"release_date":   m.ReleaseDate,
	}
}

// Series builds a media_type="tv" search hit.
type Series struct {
	ID           int64
	Name         string
	FirstAirDate string
}

func (s Series) asSearchResult() map[string]any {
	return map[string]any{
		"id":             s.ID,
		"media_type":     "tv",
		"name":           s.Name,
		"original_name":  s.Name,
		"first_air_date": s.FirstAirDate,
	}
}

// Server is a fake TMDB HTTP server. Routes are registered through the On*
// methods; unregistered calls fail the parent test.
type Server struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	routes map[string]http.HandlerFunc
}

// New starts a fake TMDB server and returns it along with a real
// *tmdb.Client whose outbound requests are routed to that server via a
// per-client http.Transport. The package-level tmdb.baseURL global is left
// untouched, so multiple New calls from parallel tests stay isolated.
//
// A t.Cleanup is registered to shut the server down when the test ends.
func New(t *testing.T) (*Server, *tmdb.Client) {
	t.Helper()
	s := &Server{
		t:      t,
		routes: make(map[string]http.HandlerFunc),
	}
	s.srv = httptest.NewServer(http.HandlerFunc(s.dispatch))
	t.Cleanup(s.srv.Close)

	client, err := tmdb.Init("test-key")
	if err != nil {
		t.Fatalf("tmdbtest: init tmdb client: %v", err)
	}

	target, err := url.Parse(s.srv.URL)
	if err != nil {
		t.Fatalf("tmdbtest: parse server url: %v", err)
	}
	client.SetClientConfig(http.Client{
		Timeout:   10 * time.Second,
		Transport: &rewriteTransport{target: target, rt: http.DefaultTransport},
	})

	return s, client
}

// URL returns the base URL of the fake server for callers that want to wire
// up a different TMDB client (e.g. a service constructed directly with
// SetCustomBaseURL).
func (s *Server) URL() string {
	return s.srv.URL
}

// OnSearchMulti registers a handler for GET /search/multi that responds with
// the provided hits when the `query` URL parameter matches expectedQuery.
// A mismatched query returns 404 (and does NOT fail the test, so callers can
// register multiple distinct queries).
func (s *Server) OnSearchMulti(expectedQuery string, hits ...Hit) {
	results := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		results = append(results, h.asSearchResult())
	}
	body := map[string]any{
		"page":          1,
		"results":       results,
		"total_pages":   1,
		"total_results": len(results),
	}
	s.register(http.MethodGet, "/search/multi", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") != expectedQuery {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, body)
	})
}

// OnMovieDetails registers a handler for GET /movie/<id>.
func (s *Server) OnMovieDetails(id int64, details tmdb.MovieDetails) {
	s.register(http.MethodGet, fmt.Sprintf("/movie/%d", id), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, details)
	})
}

// OnTVDetails registers a handler for GET /tv/<id>.
func (s *Server) OnTVDetails(id int64, details tmdb.TVDetails) {
	s.register(http.MethodGet, fmt.Sprintf("/tv/%d", id), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, details)
	})
}

func (s *Server) register(method, path string, h http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes[method+" "+path] = h
}

func (s *Server) dispatch(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	h, ok := s.routes[r.Method+" "+r.URL.Path]
	s.mu.Unlock()
	if !ok {
		s.t.Errorf("tmdbtest: unexpected call %s %s", r.Method, r.URL.Path)
		http.Error(w, "tmdbtest: unregistered route", http.StatusNotImplemented)
		return
	}
	h(w, r)
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
