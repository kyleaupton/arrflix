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

// Person builds a media_type="person" search hit.
type Person struct {
	ID          int64
	Name        string
	ProfilePath string
}

func (p Person) asSearchResult() map[string]any {
	return map[string]any{
		"id":           p.ID,
		"media_type":   "person",
		"name":         p.Name,
		"profile_path": p.ProfilePath,
	}
}

// Server is a fake TMDB HTTP server. Routes are registered through the On*
// methods; any unregistered call fails the parent test via t.Errorf and
// returns HTTP 501. Service code that treats a sub-fetch as "graceful"
// (logs and continues on error) still needs its route registered here —
// a zero-value response is fine for those cases.
type Server struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	routes map[string]http.HandlerFunc
}

// New starts a fake TMDB server and returns it along with a real
// *tmdb.Client whose outbound requests are routed to that server via a
// per-client http.Transport. Cleanup is automatic via t.Cleanup.
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

// URL returns the base URL of the fake server.
func (s *Server) URL() string {
	return s.srv.URL
}

// OnSearchMulti registers a handler for GET /search/multi that responds with
// the provided hits when the `query` URL parameter matches expectedQuery.
// Passing zero hits expresses "TMDB returned no results"; a mismatched query
// fails the test. Only one expectedQuery is supported per call.
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
		got := r.URL.Query().Get("query")
		if got != expectedQuery {
			s.t.Errorf("tmdbtest: GET /search/multi: expected query %q, got %q", expectedQuery, got)
			http.Error(w, "tmdbtest: query mismatch", http.StatusNotImplemented)
			return
		}
		writeJSON(w, body)
	})
}

func (s *Server) OnMovieDetails(id int64, details tmdb.MovieDetails) {
	s.register(http.MethodGet, fmt.Sprintf("/movie/%d", id), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, details)
	})
}

// OnMovieDetailsNotFound registers a handler for GET /movie/{id} that
// returns TMDB's standard 404 body. The matcher's metadata provider
// reads the status code + status_code 34 to surface ErrNotFound rather
// than ErrUpstreamUnavailable; tests use this to exercise the "user
// supplied a bad ID" code path on the match-by-ID handler.
func (s *Server) OnMovieDetailsNotFound(id int64) {
	s.register(http.MethodGet, fmt.Sprintf("/movie/%d", id), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":        false,
			"status_code":    34,
			"status_message": "The resource you requested could not be found.",
		})
	})
}

// OnTVDetailsNotFound is the series counterpart of OnMovieDetailsNotFound.
func (s *Server) OnTVDetailsNotFound(id int64) {
	s.register(http.MethodGet, fmt.Sprintf("/tv/%d", id), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":        false,
			"status_code":    34,
			"status_message": "The resource you requested could not be found.",
		})
	})
}

func (s *Server) OnMovieCredits(id int64, credits tmdb.MovieCredits) {
	s.register(http.MethodGet, fmt.Sprintf("/movie/%d/credits", id), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, credits)
	})
}

func (s *Server) OnMovieVideos(id int64, videos tmdb.VideoResults) {
	s.register(http.MethodGet, fmt.Sprintf("/movie/%d/videos", id), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, videos)
	})
}

func (s *Server) OnMovieRecommendations(id int64, recs tmdb.MovieRecommendations) {
	s.register(http.MethodGet, fmt.Sprintf("/movie/%d/recommendations", id), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, recs)
	})
}

func (s *Server) OnTVDetails(id int64, details tmdb.TVDetails) {
	s.register(http.MethodGet, fmt.Sprintf("/tv/%d", id), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, details)
	})
}

func (s *Server) OnTVCredits(id int64, credits tmdb.TVCredits) {
	s.register(http.MethodGet, fmt.Sprintf("/tv/%d/credits", id), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, credits)
	})
}

func (s *Server) OnTVVideos(id int64, videos tmdb.VideoResults) {
	s.register(http.MethodGet, fmt.Sprintf("/tv/%d/videos", id), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, videos)
	})
}

func (s *Server) OnPersonDetails(id int64, details tmdb.PersonDetails) {
	s.register(http.MethodGet, fmt.Sprintf("/person/%d", id), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, details)
	})
}

// SeasonEpisode builds one episode inside a TV season-details response. Tests
// supply these so they don't have to construct golang-tmdb's anonymous episode
// struct by hand.
type SeasonEpisode struct {
	ID            int64
	EpisodeNumber int
	Name          string
	AirDate       string // "2024-01-15"; empty for unaired/unknown
}

// OnTVSeasonDetails handles GET /tv/{seriesID}/season/{season} — what
// SyncSeriesStructure calls per season to enumerate episodes for the tree sync.
func (s *Server) OnTVSeasonDetails(seriesID int64, season int, episodes ...SeasonEpisode) {
	eps := make([]map[string]any, 0, len(episodes))
	for _, e := range episodes {
		eps = append(eps, map[string]any{
			"id":             e.ID,
			"episode_number": e.EpisodeNumber,
			"name":           e.Name,
			"air_date":       e.AirDate,
			"season_number":  season,
		})
	}
	body := map[string]any{
		"_id":           fmt.Sprintf("season-%d", season),
		"season_number": season,
		"episodes":      eps,
	}
	path := fmt.Sprintf("/tv/%d/season/%d", seriesID, season)
	s.register(http.MethodGet, path, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, body)
	})
}

// OnTVEpisodeDetails handles GET /tv/{seriesID}/season/{season}/episode/{episode} —
// what the scan / persistConfident path calls when it needs an episode
// title for a matched series file.
func (s *Server) OnTVEpisodeDetails(seriesID int64, season, episode int, details tmdb.TVEpisodeDetails) {
	path := fmt.Sprintf("/tv/%d/season/%d/episode/%d", seriesID, season, episode)
	s.register(http.MethodGet, path, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, details)
	})
}

// OnFindByID handles GET /find/{externalID} — TMDB's cross-provider
// resolver. The matcher's TmdbProvider calls this to convert IMDb/TVDB
// IDs to TMDB. movies, series, and tvEpisodes are split arrays matching
// TMDB's response shape; passing nil for an unused bucket emits an
// empty array.
func (s *Server) OnFindByID(externalID, expectedSource string, movies []Movie, series []Series) {
	movieResults := make([]map[string]any, 0, len(movies))
	for _, m := range movies {
		movieResults = append(movieResults, m.asSearchResult())
	}
	tvResults := make([]map[string]any, 0, len(series))
	for _, sh := range series {
		tvResults = append(tvResults, sh.asSearchResult())
	}
	body := map[string]any{
		"movie_results":      movieResults,
		"tv_results":         tvResults,
		"tv_episode_results": []any{},
		"tv_season_results":  []any{},
		"person_results":     []any{},
	}
	s.register(http.MethodGet, "/find/"+externalID, func(w http.ResponseWriter, r *http.Request) {
		got := r.URL.Query().Get("external_source")
		if got != expectedSource {
			s.t.Errorf("tmdbtest: GET /find/%s: expected external_source=%q, got %q", externalID, expectedSource, got)
			http.Error(w, "tmdbtest: external_source mismatch", http.StatusNotImplemented)
			return
		}
		writeJSON(w, body)
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
