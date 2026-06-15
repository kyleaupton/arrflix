//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/qualityprofile"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/scope"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

// wantsByEpisode indexes a tracking's wants by their episode id for assertion.
func wantsByEpisode(t *testing.T, app *testapp.App, ctx context.Context, trackingID uuid.UUID) map[uuid.UUID]model.Want {
	t.Helper()
	wants, err := app.Services.Tracking.ListWants(ctx, trackingID)
	if err != nil {
		t.Fatalf("list wants: %v", err)
	}
	out := make(map[uuid.UUID]model.Want, len(wants))
	for _, w := range wants {
		if w.EpisodeID == nil {
			t.Fatalf("series want %s has nil episodeId", w.ID)
		}
		out[*w.EpisodeID] = w
	}
	return out
}

// TestReconcile_ProducesAndCancelsWants seeds a series episode tree directly and
// drives the reconciler: wants appear only for aired, file-less, non-deprecated,
// in-scope episodes; the pass is idempotent; and narrowing scope cancels the
// now-out-of-scope want without disturbing the tracking state.
func TestReconcile_ProducesAndCancelsWants(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	if err := app.Services.QualityProfiles.SeedDefaults(ctx); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}
	profile, err := app.Services.QualityProfiles.ResolveByTier(ctx, qualityprofile.TierHD, parsing.DomainSeries)
	if err != nil {
		t.Fatalf("resolve HD series profile: %v", err)
	}

	past := time.Now().Add(-48 * time.Hour)
	future := time.Now().Add(48 * time.Hour)

	// Series media item with two seasons.
	tmdbID := int64(1399)
	item, err := app.Repo.UpsertMediaItem(ctx, repo.UpsertMediaItemParams{
		Type:   string(parsing.DomainSeries),
		Title:  "Test Show",
		TmdbID: &tmdbID,
	})
	if err != nil {
		t.Fatalf("upsert media item: %v", err)
	}
	s1, err := app.Repo.UpsertSeason(ctx, repo.UpsertSeasonParams{MediaItemID: item.ID, SeasonNumber: 1})
	if err != nil {
		t.Fatalf("upsert season 1: %v", err)
	}
	s2, err := app.Repo.UpsertSeason(ctx, repo.UpsertSeasonParams{MediaItemID: item.ID, SeasonNumber: 2})
	if err != nil {
		t.Fatalf("upsert season 2: %v", err)
	}

	mkEpisode := func(season uuid.UUID, num int32, air time.Time, tmdb int64) model.MediaEpisode {
		t.Helper()
		ep, err := app.Repo.UpsertEpisode(ctx, repo.UpsertEpisodeParams{
			SeasonID:      season,
			EpisodeNumber: num,
			AirDate:       &air,
			TmdbID:        &tmdb,
		})
		if err != nil {
			t.Fatalf("upsert episode s%v e%d: %v", season, num, err)
		}
		return ep
	}

	e1 := mkEpisode(s1.ID, 1, past, 101) // aired, file-less → want
	e2 := mkEpisode(s1.ID, 2, past, 102) // aired, has file  → no want
	_ = mkEpisode(s1.ID, 3, future, 103) // unaired          → no want
	e4 := mkEpisode(s1.ID, 4, past, 104) // aired, deprecated → no want
	e5 := mkEpisode(s2.ID, 1, past, 201) // aired, file-less → want (season 2)

	// Give e2 a file so it reads as already-acquired.
	if _, err := app.Repo.CreateLibrary(ctx, repo.CreateLibraryParams{
		Name: "lib", Type: "series", RootPath: "/tmp/lib", Enabled: true,
	}); err != nil {
		t.Fatalf("create library: %v", err)
	}
	libs, err := app.Repo.ListLibraries(ctx)
	if err != nil || len(libs) == 0 {
		t.Fatalf("list libraries: %v", err)
	}
	if _, err := app.Repo.CreateFile(ctx, repo.CreateFileParams{
		ID:          uuid.New(),
		LibraryID:   libs[0].ID,
		Path:        "show.s01e02.mkv",
		MediaItemID: &item.ID,
		EpisodeID:   &e2.ID,
	}); err != nil {
		t.Fatalf("create file for e2: %v", err)
	}

	// Deprecate e4 by omitting its tmdb id from the synced set.
	if err := app.Repo.DeprecateRemovedEpisodes(ctx, item.ID, []int64{101, 102, 103, 201}); err != nil {
		t.Fatalf("deprecate e4: %v", err)
	}

	// Tracking + requester at scope 'all'.
	tracking, err := app.Repo.CreateTracking(ctx, repo.CreateTrackingParams{
		MediaItemID:      item.ID,
		QualityProfileID: profile.ID,
		State:            string(model.TrackingActive),
		Scope:            "self",
		UpgradeBehavior:  "none",
		ScheduleStrategy: "smart",
	})
	if err != nil {
		t.Fatalf("create tracking: %v", err)
	}
	admin := adminUser(t, app, ctx)
	if _, err := app.Repo.AddRequester(ctx, repo.AddRequesterParams{
		TrackingID: tracking.ID,
		UserID:     admin.ID,
		Tier:       "HD",
		ScopeRule:  string(scope.RuleAll),
	}); err != nil {
		t.Fatalf("add requester: %v", err)
	}

	// First reconcile: wants only for e1 and e5.
	if err := app.Services.Reconcile.Reconcile(ctx, tracking.ID); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got := wantsByEpisode(t, app, ctx, tracking.ID)
	if len(got) != 2 {
		t.Fatalf("want count = %d, want 2 (e1, e5)", len(got))
	}
	for _, id := range []uuid.UUID{e1.ID, e5.ID} {
		w, ok := got[id]
		if !ok {
			t.Fatalf("missing want for episode %s", id)
		}
		if w.Status != string(model.WantPending) {
			t.Errorf("want for %s status = %q, want pending", id, w.Status)
		}
		if w.QualityProfileID != profile.ID {
			t.Errorf("want for %s profile = %s, want %s", id, w.QualityProfileID, profile.ID)
		}
	}
	for _, id := range []uuid.UUID{e2.ID, e4.ID} {
		if _, ok := got[id]; ok {
			t.Errorf("unexpected want for episode %s (has-file/deprecated)", id)
		}
	}

	// Idempotent: re-running produces no duplicates.
	if err := app.Services.Reconcile.Reconcile(ctx, tracking.ID); err != nil {
		t.Fatalf("reconcile (2): %v", err)
	}
	if got := wantsByEpisode(t, app, ctx, tracking.ID); len(got) != 2 {
		t.Fatalf("want count after re-run = %d, want 2 (no duplicates)", len(got))
	}

	// Narrow scope to season 1: e5's want (season 2) falls out of scope and is
	// canceled; e1 stays pending; the tracking stays active.
	seasonOne := int32(1)
	if _, err := app.Repo.AddRequester(ctx, repo.AddRequesterParams{
		TrackingID:  tracking.ID,
		UserID:      admin.ID,
		Tier:        "HD",
		ScopeRule:   string(scope.RuleSeason),
		ScopeSeason: &seasonOne,
	}); err != nil {
		t.Fatalf("narrow scope: %v", err)
	}
	if err := app.Services.Reconcile.Reconcile(ctx, tracking.ID); err != nil {
		t.Fatalf("reconcile (3): %v", err)
	}
	got = wantsByEpisode(t, app, ctx, tracking.ID)
	if w := got[e1.ID]; w.Status != string(model.WantPending) {
		t.Errorf("e1 want status = %q, want pending (still in scope)", w.Status)
	}
	if w := got[e5.ID]; w.Status != string(model.WantCanceled) {
		t.Errorf("e5 want status = %q, want canceled (out of scope)", w.Status)
	}
	if tr, err := app.Services.Tracking.Get(ctx, tracking.ID); err != nil {
		t.Fatalf("get tracking: %v", err)
	} else if tr.State != string(model.TrackingActive) {
		t.Errorf("tracking state = %q, want active (one episode leaving scope must not cancel the series)", tr.State)
	}
}
