//go:build integration

package integration

import (
	"net/http"
	"testing"

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
