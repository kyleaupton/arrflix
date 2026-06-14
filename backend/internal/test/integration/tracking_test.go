//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/scope"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
)

// TestTracking_RoundTrip proves the Phase 1 persistence floor: each entity
// round-trips through the repo with its fields intact, the dedup lookup and
// claim queue behave as the future spawn/worker logic relies on, and the
// one-tracking-per-media-item constraint surfaces as a Conflict.
func TestTracking_RoundTrip(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	// Seed prerequisites.
	user, err := r.CreateUser(ctx, repo.CreateUserParams{
		Email:        "requester@example.com",
		Username:     "requester",
		PasswordHash: "x",
		IsActive:     true,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	year := int32(2020)
	tmdbID := int64(603)
	media, err := r.CreateMediaItem(ctx, repo.CreateMediaItemParams{
		Type:   "movie",
		Title:  "The Matrix",
		Year:   &year,
		TmdbID: &tmdbID,
	})
	if err != nil {
		t.Fatalf("create media item: %v", err)
	}

	bluray1080 := parsing.BinKey{Source: parsing.SourceBluRay, Resolution: parsing.Res1080p, Modifier: parsing.ModNone}
	profile, err := r.CreateQualityProfile(ctx, repo.CreateQualityProfileParams{
		Name:       "HD",
		Domain:     "movie",
		Bins:       []parsing.BinKey{bluray1080},
		Cutoff:     bluray1080,
		MinSeeders: 0,
	})
	if err != nil {
		t.Fatalf("create quality profile: %v", err)
	}

	// request round-trip.
	req, err := r.CreateRequest(ctx, repo.CreateRequestParams{
		RequestedBy: user.ID,
		TmdbID:      tmdbID,
		Type:        "movie",
		Tier:        "HD",
		Status:      string(model.RequestPending),
	})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	gotReq, err := r.GetRequest(ctx, req.ID)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if gotReq.RequestedBy != user.ID || gotReq.TmdbID != tmdbID || gotReq.Tier != "HD" || gotReq.Status != string(model.RequestPending) {
		t.Errorf("request round-trip = %+v, want requestedBy=%s tmdbId=%d tier=HD status=pending", gotReq, user.ID, tmdbID)
	}
	if gotReq.SpawnedTrackingID != nil {
		t.Errorf("fresh request spawnedTrackingId = %v, want nil", gotReq.SpawnedTrackingID)
	}

	// tracking round-trip.
	tracking, err := r.CreateTracking(ctx, repo.CreateTrackingParams{
		MediaItemID:      media.ID,
		QualityProfileID: profile.ID,
		State:            string(model.TrackingActive),
		Scope:            "self",
		UpgradeBehavior:  "none",
		ScheduleStrategy: "smart",
	})
	if err != nil {
		t.Fatalf("create tracking: %v", err)
	}
	gotTracking, err := r.GetTracking(ctx, tracking.ID)
	if err != nil {
		t.Fatalf("get tracking: %v", err)
	}
	if gotTracking.MediaItemID != media.ID || gotTracking.QualityProfileID != profile.ID || gotTracking.State != string(model.TrackingActive) {
		t.Errorf("tracking round-trip = %+v, want mediaItem=%s profile=%s state=active", gotTracking, media.ID, profile.ID)
	}

	// The request now points at its tracking.
	spawned, err := r.SetRequestSpawned(ctx, req.ID, tracking.ID)
	if err != nil {
		t.Fatalf("set request spawned: %v", err)
	}
	if spawned.Status != string(model.RequestSpawned) || spawned.SpawnedTrackingID == nil || *spawned.SpawnedTrackingID != tracking.ID {
		t.Errorf("spawned request = %+v, want status=spawned spawnedTrackingId=%s", spawned, tracking.ID)
	}

	// tracking_requester round-trip.
	requester, err := r.AddRequester(ctx, repo.AddRequesterParams{
		TrackingID: tracking.ID,
		UserID:     user.ID,
		Tier:       "HD",
		ScopeRule:  string(scope.RuleAll),
	})
	if err != nil {
		t.Fatalf("add requester: %v", err)
	}
	if requester.TrackingID != tracking.ID || requester.UserID != user.ID || requester.Tier != "HD" {
		t.Errorf("requester round-trip = %+v, want tracking=%s user=%s tier=HD", requester, tracking.ID, user.ID)
	}
	gotRequester, err := r.FindRequester(ctx, tracking.ID, user.ID)
	if err != nil {
		t.Fatalf("find requester: %v", err)
	}
	if gotRequester.UserID != user.ID {
		t.Errorf("find requester = %+v, want user %s", gotRequester, user.ID)
	}
	// The movie requester carries the inert 'all' scope default, no season, no
	// overrides.
	if requester.ScopeRule != string(scope.RuleAll) || requester.ScopeSeason != nil || requester.ScopeOverrides != nil {
		t.Errorf("default scope = {rule:%q season:%v overrides:%v}, want {all nil nil}",
			requester.ScopeRule, requester.ScopeSeason, requester.ScopeOverrides)
	}

	// Scope round-trip: a second user joins with a real series scope —
	// scope_rule='season', scope_season=2, plus an include/exclude overrides
	// blob — and reads back with every field intact.
	scopedUser, err := r.CreateUser(ctx, repo.CreateUserParams{
		Email:        "scoped@example.com",
		Username:     "scoped",
		PasswordHash: "x",
		IsActive:     true,
	})
	if err != nil {
		t.Fatalf("create scoped user: %v", err)
	}
	season := int32(2)
	wantIncl, wantExcl := uuid.New(), uuid.New()
	overrides := json.RawMessage(`{"include":["` + wantIncl.String() + `"],"exclude":["` + wantExcl.String() + `"]}`)
	scopedRequester, err := r.AddRequester(ctx, repo.AddRequesterParams{
		TrackingID:     tracking.ID,
		UserID:         scopedUser.ID,
		Tier:           "HD",
		ScopeRule:      string(scope.RuleSeason),
		ScopeSeason:    &season,
		ScopeOverrides: overrides,
	})
	if err != nil {
		t.Fatalf("add scoped requester: %v", err)
	}
	assertScope := func(label string, got model.TrackingRequester) {
		if got.ScopeRule != string(scope.RuleSeason) || got.ScopeSeason == nil || *got.ScopeSeason != season {
			t.Errorf("%s scope = {rule:%q season:%v}, want {season 2}", label, got.ScopeRule, got.ScopeSeason)
		}
		// JSONB normalizes key order and whitespace, so compare semantically.
		var ov scope.Overrides
		if err := json.Unmarshal(got.ScopeOverrides, &ov); err != nil {
			t.Fatalf("%s overrides unmarshal: %v", label, err)
		}
		if len(ov.Include) != 1 || ov.Include[0] != wantIncl || len(ov.Exclude) != 1 || ov.Exclude[0] != wantExcl {
			t.Errorf("%s overrides = %+v, want include=[%s] exclude=[%s]", label, ov, wantIncl, wantExcl)
		}
	}
	assertScope("added", scopedRequester)

	requesters, err := r.ListRequestersByTracking(ctx, tracking.ID)
	if err != nil {
		t.Fatalf("list requesters: %v", err)
	}
	var roundTripped *model.TrackingRequester
	for i := range requesters {
		if requesters[i].UserID == scopedUser.ID {
			roundTripped = &requesters[i]
		}
	}
	if roundTripped == nil {
		t.Fatalf("scoped requester missing from ListRequestersByTracking")
	}
	assertScope("listed", *roundTripped)

	// The (scope_rule='season') = (scope_season IS NOT NULL) CHECK rejects a
	// 'season' rule with no season number, surfacing as a Validation error.
	badUser, err := r.CreateUser(ctx, repo.CreateUserParams{
		Email:        "badscope@example.com",
		Username:     "badscope",
		PasswordHash: "x",
		IsActive:     true,
	})
	if err != nil {
		t.Fatalf("create bad-scope user: %v", err)
	}
	if _, err := r.AddRequester(ctx, repo.AddRequesterParams{
		TrackingID: tracking.ID,
		UserID:     badUser.ID,
		Tier:       "HD",
		ScopeRule:  string(scope.RuleSeason), // season rule, but ScopeSeason nil
	}); !apperrors.IsValidation(err) {
		t.Errorf("season rule without season number = %v, want Validation (CHECK violation)", err)
	}

	// want round-trip.
	want, err := r.CreateWant(ctx, repo.CreateWantParams{
		TrackingID:       tracking.ID,
		MediaItemID:      media.ID,
		QualityProfileID: profile.ID,
		Status:           string(model.WantPending),
	})
	if err != nil {
		t.Fatalf("create want: %v", err)
	}
	gotWant, err := r.GetWant(ctx, want.ID)
	if err != nil {
		t.Fatalf("get want: %v", err)
	}
	if gotWant.TrackingID != tracking.ID || gotWant.MediaItemID != media.ID || gotWant.QualityProfileID != profile.ID || gotWant.Status != string(model.WantPending) {
		t.Errorf("want round-trip = %+v, want tracking=%s media=%s profile=%s status=pending", gotWant, tracking.ID, media.ID, profile.ID)
	}

	// Dedup lookup: the seeded media item resolves to its tracking; a second
	// media item is "not yet tracked" (NotFound).
	found, err := r.FindTrackingByMediaItem(ctx, media.ID)
	if err != nil {
		t.Fatalf("find tracking by media item: %v", err)
	}
	if found.ID != tracking.ID {
		t.Errorf("FindTrackingByMediaItem = %s, want %s", found.ID, tracking.ID)
	}
	otherMedia, err := r.CreateMediaItem(ctx, repo.CreateMediaItemParams{Type: "movie", Title: "Untracked"})
	if err != nil {
		t.Fatalf("create second media item: %v", err)
	}
	if _, err := r.FindTrackingByMediaItem(ctx, otherMedia.ID); !apperrors.IsNotFound(err) {
		t.Errorf("FindTrackingByMediaItem for untracked media = %v, want NotFound", err)
	}

	// Claim-shape: the pending want (next_run_at defaults to now()) is claimed
	// and flipped to 'searching'; a second claim returns nothing, proving
	// FOR UPDATE SKIP LOCKED + the status flip.
	claimed, err := r.ClaimRunnableWants(ctx, 1)
	if err != nil {
		t.Fatalf("claim runnable wants: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != want.ID {
		t.Fatalf("first claim = %+v, want the seeded want %s", claimed, want.ID)
	}
	if claimed[0].Status != string(model.WantSearching) {
		t.Errorf("claimed want status = %q, want searching", claimed[0].Status)
	}
	again, err := r.ClaimRunnableWants(ctx, 1)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("second claim = %+v, want empty (status already flipped)", again)
	}

	// Unique constraint: a second tracking for the same media item collides.
	if _, err := r.CreateTracking(ctx, repo.CreateTrackingParams{
		MediaItemID:      media.ID,
		QualityProfileID: profile.ID,
		State:            string(model.TrackingActive),
		Scope:            "self",
		UpgradeBehavior:  "none",
		ScheduleStrategy: "smart",
	}); !apperrors.IsConflict(err) {
		t.Errorf("duplicate tracking for media item = %v, want Conflict", err)
	}
}
