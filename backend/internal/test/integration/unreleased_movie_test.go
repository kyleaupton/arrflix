//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	tmdb "github.com/cyruzin/golang-tmdb"

	"github.com/kyleaupton/arrflix/internal/indexer"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
	"github.com/kyleaupton/arrflix/internal/test/tmdbtest"
)

const unreleasedTmdbID = int64(693134) // Dune: Part Two

// day returns the date-only form of now shifted by n days, the shape TMDB and
// the DATE columns use.
func day(now time.Time, n int) string {
	return now.UTC().AddDate(0, 0, n).Format("2006-01-02")
}

// midnight parses a date-only string as the midnight-UTC instant a DATE column
// reads back as.
func midnight(t *testing.T, date string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		t.Fatalf("parse date %q: %v", date, err)
	}
	return d
}

// releaseDate is one entry of TMDB's appended release_dates for a region.
type releaseDate struct {
	region string
	typ    int // 3 theatrical, 4 digital, 5 physical
	date   string
}

// movieWithReleases builds the enrichment payload TMDB returns for a movie:
// the primary release_date plus the appended per-region release_dates.
func movieWithReleases(t *testing.T, theatrical string, releases ...releaseDate) tmdb.MovieDetails {
	t.Helper()
	byRegion := map[string][]map[string]any{}
	var order []string
	for _, r := range releases {
		if _, ok := byRegion[r.region]; !ok {
			order = append(order, r.region)
		}
		byRegion[r.region] = append(byRegion[r.region], map[string]any{
			"type":         r.typ,
			"release_date": r.date + "T00:00:00.000Z",
		})
	}
	results := make([]map[string]any, 0, len(order))
	for _, region := range order {
		results = append(results, map[string]any{"iso_3166_1": region, "release_dates": byRegion[region]})
	}
	raw, err := json.Marshal(map[string]any{
		"id":            unreleasedTmdbID,
		"title":         "Dune: Part Two",
		"release_date":  theatrical,
		"release_dates": map[string]any{"results": results},
	})
	if err != nil {
		t.Fatalf("marshal movie details: %v", err)
	}
	var details tmdb.MovieDetails
	if err := json.Unmarshal(raw, &details); err != nil {
		t.Fatalf("unmarshal movie details: %v", err)
	}
	return details
}

// requestUnreleased boots an app whose TMDB serves details for the test movie,
// then requests it as the admin (auto-approved, so it spawns). It returns the
// app, the fake TMDB (to move dates later), and the spawned media item.
func requestUnreleased(t *testing.T, details tmdb.MovieDetails) (*testapp.App, *tmdbtest.Server, model.MediaItem) {
	t.Helper()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)
	tmdbSrv.OnMovieDetails(unreleasedTmdbID, details)
	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))
	ctx := context.Background()
	if err := app.Services.QualityProfiles.SeedDefaults(ctx); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}
	admin := adminUser(t, app, ctx)
	if _, err := app.Services.Requests.Create(ctx, service.CreateRequestInput{
		RequestedBy: admin.ID,
		TmdbID:      unreleasedTmdbID,
		Type:        "movie",
		Tier:        "HD",
	}); err != nil {
		t.Fatalf("request movie: %v", err)
	}
	item, err := app.Repo.GetMediaItemByTmdbID(ctx, unreleasedTmdbID)
	if err != nil {
		t.Fatalf("get spawned movie: %v", err)
	}
	return app, tmdbSrv, item
}

// onlyWant returns the movie's single want.
func onlyWant(t *testing.T, app *testapp.App, item model.MediaItem) model.Want {
	t.Helper()
	ctx := context.Background()
	tracking, err := app.Repo.FindTrackingByMediaItem(ctx, item.ID)
	if err != nil {
		t.Fatalf("find tracking: %v", err)
	}
	wants, err := app.Repo.ListWantsByTracking(ctx, tracking.ID)
	if err != nil {
		t.Fatalf("list wants: %v", err)
	}
	if len(wants) != 1 {
		t.Fatalf("wants = %d, want 1", len(wants))
	}
	return wants[0]
}

// countingSource is an indexer that finds nothing and counts how often it is
// asked.
func countingSource(searches *atomic.Int32) stubIndexerSource {
	return stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			searches.Add(1)
			return nil, nil
		},
	}
}

// TestReqUnrel003_HomeReleaseDateIsStored — REQ-UNREL-003 — The obtainable date
// must be durable, not recomputed per page view: requesting a movie stores its
// home-release date on the media item, and a metadata refresh updates it.
func TestReqUnrel003_HomeReleaseDateIsStored(t *testing.T) {
	t.Parallel()
	now := time.Now()
	digital := day(now, 40)
	app, tmdbSrv, item := requestUnreleased(t, movieWithReleases(t, day(now, -10),
		releaseDate{"US", 3, day(now, -10)},
		releaseDate{"US", 4, digital},
	))
	ctx := context.Background()

	if item.HomeReleaseDate == nil || !item.HomeReleaseDate.Equal(midnight(t, digital)) {
		t.Fatalf("home_release_date after request = %v, want %s", item.HomeReleaseDate, digital)
	}

	physical := day(now, 25)
	tmdbSrv.OnMovieDetails(unreleasedTmdbID, movieWithReleases(t, day(now, -10),
		releaseDate{"US", 3, day(now, -10)},
		releaseDate{"US", 4, digital},
		releaseDate{"GB", 5, physical},
	))
	if err := app.Services.Enrichment.EnrichMediaItem(ctx, item); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	refreshed, err := app.Repo.GetMediaItem(ctx, item.ID)
	if err != nil {
		t.Fatalf("get refreshed movie: %v", err)
	}
	if refreshed.HomeReleaseDate == nil || !refreshed.HomeReleaseDate.Equal(midnight(t, physical)) {
		t.Fatalf("home_release_date after refresh = %v, want %s", refreshed.HomeReleaseDate, physical)
	}
}

// TestReqUnrel004_NoSearchBeforeObtainable — REQ-UNREL-004 — No search may be
// issued for a title before it could plausibly be obtainable: for a movie, the
// day before its home release, or 30 days after it reached cinemas when no home
// release is known. A new want is born dormant until then, and a want that was
// already being searched goes dormant after its next pass.
func TestReqUnrel004_NoSearchBeforeObtainable(t *testing.T) {
	t.Parallel()

	// dormantOnCreate requests the movie, checks the want waits for searchFrom,
	// and runs the real acquisition worker to prove nothing is searched.
	dormantOnCreate := func(t *testing.T, details tmdb.MovieDetails, searchFrom time.Time) {
		t.Helper()
		app, _, item := requestUnreleased(t, details)
		want := onlyWant(t, app, item)
		if want.Status != string(model.WantPending) || !want.NextRunAt.Equal(searchFrom) {
			t.Fatalf("new want = %s due %v, want pending due %v", want.Status, want.NextRunAt, searchFrom)
		}

		var searches atomic.Int32
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go newAcquisitionWorker(app.Repo, countingSource(&searches)).Run(ctx)
		time.Sleep(300 * time.Millisecond)
		cancel()

		if n := searches.Load(); n != 0 {
			t.Fatalf("searches issued = %d, want 0 before the movie is obtainable", n)
		}
	}

	t.Run("home release known: dormant until the day before", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		digital := day(now, 40)
		dormantOnCreate(t, movieWithReleases(t, day(now, -10),
			releaseDate{"US", 3, day(now, -10)},
			releaseDate{"US", 4, digital},
		), midnight(t, digital).Add(-24*time.Hour))
	})

	t.Run("no home release: dormant until theatrical plus 30 days", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		theatrical := day(now, -10)
		dormantOnCreate(t, movieWithReleases(t, theatrical,
			releaseDate{"US", 3, theatrical},
		), midnight(t, theatrical).Add(30*24*time.Hour))
	})

	t.Run("already searching: goes dormant after its next pass", func(t *testing.T) {
		t.Parallel()
		pool := dbtest.New(t)
		r := repo.New(pool)
		ctx := context.Background()

		// A want that came due before the movie had a home-release date, then
		// the date arrived with the backfill refresh.
		want := seedPendingWant(t, ctx, r)
		digital := day(time.Now(), 40)
		home := midnight(t, digital)
		if _, err := r.UpdateMediaItemMetadata(ctx, repo.UpdateMediaItemMetadataParams{
			ID:              want.MediaItemID,
			HomeReleaseDate: &home,
		}); err != nil {
			t.Fatalf("set home release date: %v", err)
		}

		var searches atomic.Int32
		workerCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go newAcquisitionWorker(r, countingSource(&searches)).Run(workerCtx)

		deadline := time.Now().Add(10 * time.Second)
		var got model.Want
		for {
			var err error
			got, err = r.GetWant(ctx, want.ID)
			if err != nil {
				t.Fatalf("get want: %v", err)
			}
			if searches.Load() >= 1 && got.Status == string(model.WantPending) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("searches=%d status=%q, want the due search handled within deadline", searches.Load(), got.Status)
			}
			time.Sleep(10 * time.Millisecond)
		}

		// More polls go by; the want is dormant, so none of them search.
		time.Sleep(300 * time.Millisecond)
		cancel()
		if n := searches.Load(); n != 1 {
			t.Fatalf("searches issued = %d, want 1 (the pass that was already due)", n)
		}
		got, err := r.GetWant(ctx, want.ID)
		if err != nil {
			t.Fatalf("get want: %v", err)
		}
		if want := home.Add(-24 * time.Hour); !got.NextRunAt.Equal(want) {
			t.Fatalf("next_run_at after the pass = %v, want %v (the day before the home release)", got.NextRunAt, want)
		}
	})
}

// TestUnreleasedMovie_RefreshReschedulesDormantWant proves a moved home-release
// date moves the wait with it: a refresh that pushes the date later, or pulls
// it earlier, reschedules the dormant want to the new search date.
func TestUnreleasedMovie_RefreshReschedulesDormantWant(t *testing.T) {
	t.Parallel()
	now := time.Now()
	theatrical := day(now, -10)
	app, tmdbSrv, item := requestUnreleased(t, movieWithReleases(t, theatrical,
		releaseDate{"US", 4, day(now, 40)},
	))
	ctx := context.Background()

	for _, tc := range []struct {
		name    string
		digital string
	}{
		{"slips later", day(now, 90)},
		{"pulled earlier", day(now, 20)},
	} {
		tmdbSrv.OnMovieDetails(unreleasedTmdbID, movieWithReleases(t, theatrical,
			releaseDate{"US", 4, tc.digital},
		))
		current, err := app.Repo.GetMediaItem(ctx, item.ID)
		if err != nil {
			t.Fatalf("%s: get movie: %v", tc.name, err)
		}
		if err := app.Services.Enrichment.EnrichMediaItem(ctx, current); err != nil {
			t.Fatalf("%s: refresh: %v", tc.name, err)
		}
		want := onlyWant(t, app, item)
		if expect := midnight(t, tc.digital).Add(-24 * time.Hour); !want.NextRunAt.Equal(expect) {
			t.Errorf("%s: next_run_at = %v, want %v", tc.name, want.NextRunAt, expect)
		}
	}
}

// TestReqUnrel001_RequestedMovieNotOutReadsUnreleased — REQ-UNREL-001 — A title
// that cannot yet be obtained must be presented as waiting for a date, never as
// searching: a requested movie whose search date is ahead projects as
// unreleased, carrying its home-release date when one is known and never the
// internal theatrical estimate.
func TestReqUnrel001_RequestedMovieNotOutReadsUnreleased(t *testing.T) {
	t.Parallel()
	statusPath := fmt.Sprintf("/api/v1/titles/movie/%d/status", unreleasedTmdbID)

	t.Run("home release known", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		digital := day(now, 40)
		app, _, _ := requestUnreleased(t, movieWithReleases(t, day(now, -10),
			releaseDate{"US", 4, digital},
		))
		var st model.TitleStatus
		app.GET(t, statusPath, &st, http.StatusOK)
		if st.State != "unreleased" {
			t.Errorf("state = %q, want unreleased", st.State)
		}
		if st.HomeReleaseDate == nil || !st.HomeReleaseDate.Equal(midnight(t, digital)) {
			t.Errorf("homeReleaseDate = %v, want %s", st.HomeReleaseDate, digital)
		}
	})

	t.Run("in cinemas, home release unknown", func(t *testing.T) {
		t.Parallel()
		app, _, _ := requestUnreleased(t, movieWithReleases(t, day(time.Now(), -10)))
		var st model.TitleStatus
		app.GET(t, statusPath, &st, http.StatusOK)
		if st.State != "unreleased" {
			t.Errorf("state = %q, want unreleased", st.State)
		}
		if st.HomeReleaseDate != nil {
			t.Errorf("homeReleaseDate = %v, want none (the estimate is never sent)", st.HomeReleaseDate)
		}
	})

	t.Run("out at home: searching", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		app, _, _ := requestUnreleased(t, movieWithReleases(t, day(now, -90),
			releaseDate{"US", 4, day(now, -30)},
		))
		var st model.TitleStatus
		app.GET(t, statusPath, &st, http.StatusOK)
		if st.State != "searching" {
			t.Errorf("state = %q, want searching", st.State)
		}
	})
}
