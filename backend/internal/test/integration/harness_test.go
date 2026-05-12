//go:build integration

package integration

import (
	"testing"

	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/tmdbtest"
)

// TestHarness_Smoke exercises both test harnesses end-to-end:
//   - dbtest spins a Postgres container, runs migrations, and clones a fresh
//     per-test database. We verify the schema is live by querying the
//     `library` table from migration 0004.
//   - tmdbtest serves a fake TMDB and we round-trip a search through the
//     real golang-tmdb client.
func TestHarness_Smoke(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)

	var count int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM library`).Scan(&count); err != nil {
		t.Fatalf("query library: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected empty library, got count=%d", count)
	}

	srv, client := tmdbtest.New(t)
	srv.OnSearchMulti("test", tmdbtest.Movie{
		ID:          1,
		Title:       "Test",
		ReleaseDate: "2020-01-01",
	})

	res, err := client.GetSearchMulti("test", nil)
	if err != nil {
		t.Fatalf("GetSearchMulti: %v", err)
	}
	if got := len(res.Results); got != 1 {
		t.Fatalf("expected 1 result, got %d", got)
	}
	if got := res.Results[0].ID; got != 1 {
		t.Fatalf("expected ID=1, got %d", got)
	}
}
