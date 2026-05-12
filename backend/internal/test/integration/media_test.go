//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"testing"

	tmdb "github.com/cyruzin/golang-tmdb"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
	"github.com/kyleaupton/arrflix/internal/test/tmdbtest"
)

// TestMedia_Search exercises GET /api/v1/search end-to-end against a fake
// TMDB. This is the first test that drives a TMDB call through MediaService
// (the smoke test only round-trips through a raw *tmdb.Client), so a green
// result here is the structural proof that service.WithTmdbClient reaches
// the embedded reference inside MediaService rather than just Services.Tmdb.
func TestMedia_Search(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)
	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))

	tmdbSrv.OnSearchMulti("inception",
		tmdbtest.Movie{ID: 27205, Title: "Inception", ReleaseDate: "2010-07-15"},
	)

	var resp model.SearchResponse
	app.GET(t, "/api/v1/search?q=inception", &resp, http.StatusOK)

	if resp.Query != "inception" {
		t.Errorf("Query = %q, want %q", resp.Query, "inception")
	}
	if resp.TotalResults != 1 {
		t.Errorf("TotalResults = %d, want 1", resp.TotalResults)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("len(Results) = %d, want 1: %+v", len(resp.Results), resp.Results)
	}

	got := resp.Results[0]
	if got.ID != 27205 {
		t.Errorf("Results[0].ID = %d, want 27205", got.ID)
	}
	if got.Title != "Inception" {
		t.Errorf("Results[0].Title = %q, want %q", got.Title, "Inception")
	}
	if got.MediaType != "movie" {
		t.Errorf("Results[0].MediaType = %q, want %q", got.MediaType, "movie")
	}
	if got.IsInLibrary {
		t.Errorf("Results[0].IsInLibrary = true, want false (empty library)")
	}
}

// TestMedia_SearchValidation_EmptyQuery sends /api/v1/search without the
// required `q` parameter. Huma's tag-level required:"true" + minLength:"1"
// catches this before MediaService runs, so no TMDB wiring is needed.
func TestMedia_SearchValidation_EmptyQuery(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	var pd apperrors.ProblemDetails
	app.GET(t, "/api/v1/search", &pd, http.StatusUnprocessableEntity)

	fe := findFieldError(t, pd, "query.q")
	if fe.Message == "" {
		t.Errorf("expected non-empty message for query.q field error: %+v", fe)
	}
}

// TestMedia_Search_NoResults proves the "TMDB returned no hits for this
// query" path round-trips end-to-end: empty results array (not null), zero
// total, query echoed back. Passing zero Hits to OnSearchMulti is the
// explicit way to express this shape — see tmdbtest.OnSearchMulti's docs.
func TestMedia_Search_NoResults(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)
	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))

	tmdbSrv.OnSearchMulti("nothingmatches")

	var resp model.SearchResponse
	app.GET(t, "/api/v1/search?q=nothingmatches", &resp, http.StatusOK)

	if resp.Query != "nothingmatches" {
		t.Errorf("Query = %q, want %q", resp.Query, "nothingmatches")
	}
	if resp.TotalResults != 0 {
		t.Errorf("TotalResults = %d, want 0", resp.TotalResults)
	}
	if resp.Results == nil {
		t.Errorf("Results = nil, want empty slice (wire shape must be [], not null)")
	}
	if len(resp.Results) != 0 {
		t.Errorf("len(Results) = %d, want 0: %+v", len(resp.Results), resp.Results)
	}
}

// In-library enrichment (the IsInLibrary=true case in MediaService.Search)
// is deliberately NOT covered here. That branch is a one-line map lookup
// whose precondition — a media_item row — is produced in production by the
// import pipeline, not by any public HTTP endpoint. Seeding the row via
// app.Repo or raw SQL would bypass the wire contract this suite is meant
// to exercise; faithfully driving the import flow would be enormous setup
// for one map lookup. The branch belongs in a unit test of MediaService
// with a faked repo. See CLAUDE.md "Scope: integration vs unit tests".

// TestMedia_Search_MixedTypes registers one movie and one series hit and
// asserts the service correctly switches on media_type to populate per-type
// fields: Title from TMDB's `title` for movies vs `name` for tv, Year from
// `release_date` vs `first_air_date`. A single test catches the whole class
// of "I refactored the switch and broke the tv branch" regression.
func TestMedia_Search_MixedTypes(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)
	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))

	tmdbSrv.OnSearchMulti("inception",
		tmdbtest.Movie{ID: 27205, Title: "Inception", ReleaseDate: "2010-07-15"},
		tmdbtest.Series{ID: 1399, Name: "Game of Thrones", FirstAirDate: "2011-04-17"},
	)

	var resp model.SearchResponse
	app.GET(t, "/api/v1/search?q=inception", &resp, http.StatusOK)

	if resp.TotalResults != 2 {
		t.Errorf("TotalResults = %d, want 2", resp.TotalResults)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("len(Results) = %d, want 2: %+v", len(resp.Results), resp.Results)
	}

	byID := map[int64]model.SearchResult{}
	for _, r := range resp.Results {
		byID[r.ID] = r
	}

	movie, ok := byID[27205]
	if !ok {
		t.Fatalf("missing movie result (id 27205): %+v", resp.Results)
	}
	if movie.MediaType != "movie" {
		t.Errorf("movie.MediaType = %q, want %q", movie.MediaType, "movie")
	}
	if movie.Title != "Inception" {
		t.Errorf("movie.Title = %q, want %q", movie.Title, "Inception")
	}
	if movie.Year == nil || *movie.Year != 2010 {
		t.Errorf("movie.Year = %v, want 2010", movie.Year)
	}

	series, ok := byID[1399]
	if !ok {
		t.Fatalf("missing series result (id 1399): %+v", resp.Results)
	}
	if series.MediaType != "tv" {
		t.Errorf("series.MediaType = %q, want %q", series.MediaType, "tv")
	}
	if series.Title != "Game of Thrones" {
		t.Errorf("series.Title = %q, want %q", series.Title, "Game of Thrones")
	}
	if series.Year == nil || *series.Year != 2011 {
		t.Errorf("series.Year = %v, want 2011", series.Year)
	}
}

// TestMedia_GetMovie_Happy proves the /api/v1/movie/{id} route end-to-end:
// path-param binding (TMDB id), TMDB-client wiring through the injected
// fake, and the model.MovieDetail wire shape on a success response.
//
// GetMovieDetail calls four TMDB endpoints — main details plus credits,
// videos, and recommendations. The latter three are "graceful" in the
// service (errors are logged and swallowed), but tmdbtest's dispatch fails
// loudly on any unregistered route, so the test registers all four. The
// graceful endpoints get zero-value responses; this test is about the wire
// for the main payload, not about credits/videos/recs transformation
// (those are pure mapping logic and belong in a unit test).
func TestMedia_GetMovie_Happy(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)
	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))

	const tmdbID = int64(27205)
	tmdbSrv.OnMovieDetails(tmdbID, tmdb.MovieDetails{
		ID:          tmdbID,
		Title:       "Inception",
		Overview:    "A thief who steals corporate secrets...",
		Status:      "Released",
		ReleaseDate: "2010-07-15",
	})
	tmdbSrv.OnMovieCredits(tmdbID, tmdb.MovieCredits{})
	tmdbSrv.OnMovieVideos(tmdbID, tmdb.VideoResults{})
	// MovieRecommendations embeds *MovieRecommendationsResults — passing a
	// zero-value struct here would round-trip through JSON as a nil embedded
	// pointer, then transformMovieRecommendations would panic dereferencing
	// the promoted .Results field. Materialize the pointer explicitly.
	tmdbSrv.OnMovieRecommendations(tmdbID, tmdb.MovieRecommendations{
		MovieRecommendationsResults: &tmdb.MovieRecommendationsResults{},
	})

	var resp model.MovieDetail
	app.GET(t, fmt.Sprintf("/api/v1/movie/%d", tmdbID), &resp, http.StatusOK)

	if resp.TmdbID != tmdbID {
		t.Errorf("TmdbID = %d, want %d", resp.TmdbID, tmdbID)
	}
	if resp.Title != "Inception" {
		t.Errorf("Title = %q, want %q", resp.Title, "Inception")
	}
	if resp.Status != "Released" {
		t.Errorf("Status = %q, want %q", resp.Status, "Released")
	}
	if resp.Year == nil || *resp.Year != 2010 {
		t.Errorf("Year = %v, want 2010", resp.Year)
	}
	if resp.Files == nil {
		t.Errorf("Files = nil, want empty slice (wire shape must be [], not null)")
	}
}

// TestMedia_GetSeries_Happy is the series analogue of GetMovie_Happy. The
// main details return zero seasons on purpose — the per-season /tv/{id}/
// season/{n} fetches loop over tmdbDetails.Seasons, so an empty Seasons
// slice keeps this test from needing OnTVSeasonDetails registrations.
// Season-detail wire coverage can come later if/when there's a reason to
// exercise it end-to-end (vs. unit-testing the season mapping in service).
func TestMedia_GetSeries_Happy(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)
	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))

	const tmdbID = int64(1399)
	tmdbSrv.OnTVDetails(tmdbID, tmdb.TVDetails{
		ID:           tmdbID,
		Name:         "Game of Thrones",
		Overview:     "Seven noble families fight...",
		Status:       "Ended",
		FirstAirDate: "2011-04-17",
	})
	tmdbSrv.OnTVCredits(tmdbID, tmdb.TVCredits{})
	tmdbSrv.OnTVVideos(tmdbID, tmdb.VideoResults{})

	var resp model.SeriesDetail
	app.GET(t, fmt.Sprintf("/api/v1/series/%d", tmdbID), &resp, http.StatusOK)

	if resp.TmdbID != tmdbID {
		t.Errorf("TmdbID = %d, want %d", resp.TmdbID, tmdbID)
	}
	if resp.Title != "Game of Thrones" {
		t.Errorf("Title = %q, want %q (note: wire field is Title, sourced from TMDB's Name)", resp.Title, "Game of Thrones")
	}
	if resp.Status != "Ended" {
		t.Errorf("Status = %q, want %q", resp.Status, "Ended")
	}
	if resp.Year == nil || *resp.Year != 2011 {
		t.Errorf("Year = %v, want 2011", resp.Year)
	}
	if resp.Availability.IsInLibrary {
		t.Errorf("Availability.IsInLibrary = true, want false (empty library)")
	}
	if resp.Seasons == nil {
		t.Errorf("Seasons = nil, want empty slice (wire shape must be [], not null)")
	}
}

// TestMedia_GetPerson_Happy proves /api/v1/person/{id} end-to-end. Simplest
// of the detail endpoints — single TMDB call, no graceful sub-fetches, no
// DB cross-reference.
func TestMedia_GetPerson_Happy(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)
	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))

	const tmdbID = int64(287)
	tmdbSrv.OnPersonDetails(tmdbID, tmdb.PersonDetails{
		ID:                 tmdbID,
		Name:               "Brad Pitt",
		Biography:          "American actor and film producer.",
		KnownForDepartment: "Acting",
	})

	var resp model.PersonDetail
	app.GET(t, fmt.Sprintf("/api/v1/person/%d", tmdbID), &resp, http.StatusOK)

	if resp.TmdbID != tmdbID {
		t.Errorf("TmdbID = %d, want %d", resp.TmdbID, tmdbID)
	}
	if resp.Name != "Brad Pitt" {
		t.Errorf("Name = %q, want %q", resp.Name, "Brad Pitt")
	}
	if resp.Biography == "" {
		t.Errorf("Biography is empty, want non-empty")
	}
	if resp.KnownForDepartment != "Acting" {
		t.Errorf("KnownForDepartment = %q, want %q", resp.KnownForDepartment, "Acting")
	}
}

// TestMedia_ListLibrary_Empty hits /api/v1/library against a fresh DB with
// no media_item rows. Asserts the pagination envelope wire shape: empty
// Data array (not null), Pagination metadata reflecting the params we sent
// and a zero total. The populated case (media_item rows in the DB) is
// deliberately not covered here — see CLAUDE.md "Scope: integration vs
// unit tests" for why state preconditions produced by internal pipelines
// belong in unit tests, not integration tests.
func TestMedia_ListLibrary_Empty(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	var resp model.PaginatedLibraryResponse
	app.GET(t, "/api/v1/library?page=1&pageSize=20", &resp, http.StatusOK)

	if resp.Data == nil {
		t.Errorf("Data = nil, want empty slice (wire shape must be [], not null)")
	}
	if len(resp.Data) != 0 {
		t.Errorf("len(Data) = %d, want 0: %+v", len(resp.Data), resp.Data)
	}
	if resp.Pagination.Total != 0 {
		t.Errorf("Pagination.Total = %d, want 0", resp.Pagination.Total)
	}
	if resp.Pagination.Page != 1 {
		t.Errorf("Pagination.Page = %d, want 1", resp.Pagination.Page)
	}
	if resp.Pagination.PageSize != 20 {
		t.Errorf("Pagination.PageSize = %d, want 20", resp.Pagination.PageSize)
	}
	if resp.Pagination.TotalPages != 0 {
		t.Errorf("Pagination.TotalPages = %d, want 0", resp.Pagination.TotalPages)
	}
}
