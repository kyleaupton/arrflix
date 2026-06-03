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
// TMDB with a single movie hit and an empty library (IsInLibrary=false).
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

// TestMedia_SearchValidation_EmptyQuery asserts huma's tag-level
// required:"true" + minLength:"1" rejects a missing `q` before the
// service runs (no TMDB wiring needed).
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

// TestMedia_Search_NoResults asserts the empty-results wire shape: empty
// array (not null), zero total, query echoed back.
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

// IsInLibrary=true is not covered here: its precondition (a media_item row)
// is produced by the import pipeline, not any public HTTP endpoint. Belongs
// in a unit test against MediaService. See CLAUDE.md "Scope".

// TestMedia_Search_MixedTypes asserts the service switches on media_type
// to populate per-type fields correctly:
//   - Title: TMDB's `title` (movie) vs `name` (tv, person)
//   - Year: parsed from `release_date` (movie) vs `first_air_date` (tv);
//     persons have no year on the wire.
//   - PosterPath: TMDB's `poster_path` (movie/tv) vs `profile_path` (person)
//
// Results are indexed by ID — ordering is TMDB's, not part of the contract.
func TestMedia_Search_MixedTypes(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)
	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))

	tmdbSrv.OnSearchMulti("inception",
		tmdbtest.Movie{ID: 27205, Title: "Inception", ReleaseDate: "2010-07-15"},
		tmdbtest.Series{ID: 1399, Name: "Game of Thrones", FirstAirDate: "2011-04-17"},
		tmdbtest.Person{ID: 287, Name: "Brad Pitt", ProfilePath: "/brad.jpg"},
	)

	var resp model.SearchResponse
	app.GET(t, "/api/v1/search?q=inception", &resp, http.StatusOK)

	if resp.TotalResults != 3 {
		t.Errorf("TotalResults = %d, want 3", resp.TotalResults)
	}
	if len(resp.Results) != 3 {
		t.Fatalf("len(Results) = %d, want 3: %+v", len(resp.Results), resp.Results)
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

	person, ok := byID[287]
	if !ok {
		t.Fatalf("missing person result (id 287): %+v", resp.Results)
	}
	if person.MediaType != "person" {
		t.Errorf("person.MediaType = %q, want %q", person.MediaType, "person")
	}
	if person.Title != "Brad Pitt" {
		t.Errorf("person.Title = %q, want %q (sourced from TMDB Name)", person.Title, "Brad Pitt")
	}
	if person.Year != nil {
		t.Errorf("person.Year = %v, want nil (persons have no year)", person.Year)
	}
	if person.PosterPath == nil || *person.PosterPath != "/brad.jpg" {
		t.Errorf("person.PosterPath = %v, want %q (sourced from TMDB profile_path)", person.PosterPath, "/brad.jpg")
	}
}

// TestMedia_GetMovie_Happy covers the wire shape of /api/v1/movie/{id}.
// All four upstream TMDB endpoints are registered — credits/videos/recs are
// "graceful" in the service but tmdbtest fails loudly on unregistered
// routes. Sub-payload transformation is a unit-test concern.
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
	// canonical token, mapped from the mock's "Released"
	if resp.Status != "released" {
		t.Errorf("Status = %q, want %q", resp.Status, "released")
	}
	if resp.Year == nil || *resp.Year != 2010 {
		t.Errorf("Year = %v, want 2010", resp.Year)
	}
	if resp.Files == nil {
		t.Errorf("Files = nil, want empty slice (wire shape must be [], not null)")
	}

	// Wire-shape asymmetry: transformMovieCredits always returns &Credits{},
	// so Credits survives encoding empty. Videos/Recommendations are slices
	// that omitempty elides to nil.
	if resp.Credits == nil {
		t.Errorf("Credits = nil, want non-nil (transformMovieCredits returns &Credits{} even for empty input)")
	} else if len(resp.Credits.Cast) != 0 {
		t.Errorf("Credits.Cast len = %d, want 0", len(resp.Credits.Cast))
	}
	if resp.Videos != nil {
		t.Errorf("Videos = %+v, want nil (omitempty elides empty slice)", resp.Videos)
	}
	if resp.Recommendations != nil {
		t.Errorf("Recommendations = %+v, want nil (omitempty elides empty slice)", resp.Recommendations)
	}
}

// TestMedia_GetSeries_Happy is the series analogue of GetMovie_Happy.
// Returns zero seasons on purpose so no OnTVSeasonDetails registrations
// are needed. Title is sourced from TMDB's `name` (mapped in the service).
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
		t.Errorf("Title = %q, want %q", resp.Title, "Game of Thrones")
	}
	// canonical token, mapped from the mock's "Ended"
	if resp.Status != "ended" {
		t.Errorf("Status = %q, want %q", resp.Status, "ended")
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

// TestMedia_ListLibrary_Empty asserts the pagination envelope wire shape
// against an empty library. The populated case belongs in a unit test —
// see CLAUDE.md "Scope".
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

// TestMedia_GetMovie_BadPathID asserts huma's tag-level minimum:"1"
// rejects id=0 at the path-param binding (one test covers the constraint
// across all detail endpoints).
func TestMedia_GetMovie_BadPathID(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	var pd apperrors.ProblemDetails
	app.GET(t, "/api/v1/movie/0", &pd, http.StatusUnprocessableEntity)

	fe := findFieldError(t, pd, "path.id")
	if fe.Message == "" {
		t.Errorf("expected non-empty message for path.id field error: %+v", pd)
	}
}
