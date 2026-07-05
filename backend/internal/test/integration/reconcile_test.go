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

// sameInstant asserts two times denote the same instant (timezone-insensitive) —
// air dates round-trip through Postgres, so compare with Equal, not ==.
func sameInstant(t *testing.T, label string, got, want time.Time) {
	t.Helper()
	if !got.Equal(want) {
		t.Errorf("%s next_run_at = %v, want %v", label, got, want)
	}
}

// TestReconcile_StampsSegmentAndHold proves the reconciler classifies each want's
// segment against the tracking's creation time and applies the segment's autonomy:
// on a backfill-manual / ongoing-auto tracking, a past-aired episode's want is
// born 'backfill' + held ('needs_pick'), while a future-dated episode's want is
// 'ongoing' + unheld.
func TestReconcile_StampsSegmentAndHold(t *testing.T) {
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

	tmdbID := int64(1401)
	item, err := app.Repo.UpsertMediaItem(ctx, repo.UpsertMediaItemParams{
		Type:   string(parsing.DomainSeries),
		Title:  "Segment Show",
		TmdbID: &tmdbID,
	})
	if err != nil {
		t.Fatalf("upsert media item: %v", err)
	}
	s1, err := app.Repo.UpsertSeason(ctx, repo.UpsertSeasonParams{MediaItemID: item.ID, SeasonNumber: 1})
	if err != nil {
		t.Fatalf("upsert season: %v", err)
	}
	pastEp, err := app.Repo.UpsertEpisode(ctx, repo.UpsertEpisodeParams{SeasonID: s1.ID, EpisodeNumber: 1, AirDate: &past, TmdbID: ptrInt64(301)})
	if err != nil {
		t.Fatalf("upsert past episode: %v", err)
	}
	futureEp, err := app.Repo.UpsertEpisode(ctx, repo.UpsertEpisodeParams{SeasonID: s1.ID, EpisodeNumber: 2, AirDate: &future, TmdbID: ptrInt64(302)})
	if err != nil {
		t.Fatalf("upsert future episode: %v", err)
	}

	// Tracking with backfill on manual, ongoing on auto.
	tracking, err := app.Repo.CreateTracking(ctx, repo.CreateTrackingParams{
		MediaItemID:      item.ID,
		QualityProfileID: profile.ID,
		State:            string(model.TrackingActive),
		Scope:            "self",
		UpgradeBehavior:  "none",
		ScheduleStrategy: "smart",
		AutonomyBackfill: string(model.AutonomyManual),
		AutonomyOngoing:  string(model.AutonomyAuto),
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

	if err := app.Services.Reconcile.Reconcile(ctx, tracking.ID); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	got := wantsByEpisode(t, app, ctx, tracking.ID)
	back, ok := got[pastEp.ID]
	if !ok {
		t.Fatalf("missing want for past episode")
	}
	if back.Segment != string(model.WantSegmentBackfill) {
		t.Errorf("past-aired want segment = %q, want backfill", back.Segment)
	}
	if back.Hold == nil || *back.Hold != model.WantHoldNeedsPick {
		t.Errorf("past-aired want hold = %v, want %q (backfill is manual)", back.Hold, model.WantHoldNeedsPick)
	}

	fwd, ok := got[futureEp.ID]
	if !ok {
		t.Fatalf("missing want for future episode")
	}
	if fwd.Segment != string(model.WantSegmentOngoing) {
		t.Errorf("future-dated want segment = %q, want ongoing", fwd.Segment)
	}
	if fwd.Hold != nil {
		t.Errorf("future-dated want hold = %q, want nil (ongoing is auto)", *fwd.Hold)
	}
}

// ptrInt64 returns a pointer to its argument — the episode tmdb-id seeds need an
// addressable value.
func ptrInt64(v int64) *int64 { return &v }

// TestReconcile_ProducesAndCancelsWants seeds a series episode tree directly and
// drives the reconciler: wants appear for every file-less, dated, non-deprecated,
// in-scope episode — aired or future — with next_run_at stamped to the air date;
// undated and file-backed episodes get none; the pass is idempotent; and
// narrowing scope cancels the now-out-of-scope (future) want without disturbing
// the tracking state.
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

	mkEpisode := func(season uuid.UUID, num int32, air *time.Time, tmdb int64) model.MediaEpisode {
		t.Helper()
		ep, err := app.Repo.UpsertEpisode(ctx, repo.UpsertEpisodeParams{
			SeasonID:      season,
			EpisodeNumber: num,
			AirDate:       air,
			TmdbID:        &tmdb,
		})
		if err != nil {
			t.Fatalf("upsert episode s%v e%d: %v", season, num, err)
		}
		return ep
	}

	e1 := mkEpisode(s1.ID, 1, &past, 101)   // aired, file-less   → want (due now)
	e2 := mkEpisode(s1.ID, 2, &past, 102)   // aired, has file    → no want
	e3 := mkEpisode(s1.ID, 3, &future, 103) // future, file-less  → want (deferred to air)
	e4 := mkEpisode(s1.ID, 4, &past, 104)   // aired, deprecated  → no want
	e6 := mkEpisode(s1.ID, 5, nil, 105)     // undated (TBA)      → no want
	e5 := mkEpisode(s2.ID, 1, &future, 201) // future, file-less  → want (season 2)

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
	if err := app.Repo.DeprecateRemovedEpisodes(ctx, item.ID, []int64{101, 102, 103, 105, 201}); err != nil {
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
		AutonomyBackfill: string(model.AutonomyAuto),
		AutonomyOngoing:  string(model.AutonomyAuto),
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

	// First reconcile: wants for the file-less, dated, in-scope episodes —
	// e1 (aired), e3 (future, season 1), e5 (future, season 2).
	if err := app.Services.Reconcile.Reconcile(ctx, tracking.ID); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	now := time.Now()
	got := wantsByEpisode(t, app, ctx, tracking.ID)
	if len(got) != 3 {
		t.Fatalf("want count = %d, want 3 (e1, e3, e5)", len(got))
	}
	for _, id := range []uuid.UUID{e1.ID, e3.ID, e5.ID} {
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

	// (a) Aired episode: next_run_at stamped to its past air date, so the want is
	// already due — ClaimRunnableWants (next_run_at <= now()) can pick it up.
	// air_date is DATE-granular (midnight UTC), so assert against the stored value.
	sameInstant(t, "e1 (aired)", got[e1.ID].NextRunAt, *e1.AirDate)
	if !got[e1.ID].NextRunAt.Before(now) {
		t.Errorf("e1 next_run_at = %v, want before now (immediately claimable)", got[e1.ID].NextRunAt)
	}
	// (b) Future episode: next_run_at = its future air date, so the want defers —
	// not yet claimable until it airs.
	sameInstant(t, "e3 (future)", got[e3.ID].NextRunAt, *e3.AirDate)
	if !got[e3.ID].NextRunAt.After(now) {
		t.Errorf("e3 next_run_at = %v, want after now (deferred to air)", got[e3.ID].NextRunAt)
	}

	// (c) undated and (d) file-backed / deprecated episodes get no want.
	for _, id := range []uuid.UUID{e2.ID, e4.ID, e6.ID} {
		if _, ok := got[id]; ok {
			t.Errorf("unexpected want for episode %s (has-file/deprecated/undated)", id)
		}
	}

	// Idempotent: re-running produces no duplicates.
	if err := app.Services.Reconcile.Reconcile(ctx, tracking.ID); err != nil {
		t.Fatalf("reconcile (2): %v", err)
	}
	if got := wantsByEpisode(t, app, ctx, tracking.ID); len(got) != 3 {
		t.Fatalf("want count after re-run = %d, want 3 (no duplicates)", len(got))
	}

	// Narrow scope to season 1: e5's pre-created future want (season 2) falls out
	// of scope and is canceled; e1 stays pending; the tracking stays active.
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
