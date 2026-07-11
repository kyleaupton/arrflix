//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
)

// TestNotification_NotifyWantAvailable proves the first producer's fan-out: a
// want reaching 'available' enqueues one want.available notification per tracking
// requester, carrying the media title/year the template renders. The want is
// in-memory — NotifyWantAvailable reads only its TrackingID/MediaItemID — so the
// test seeds just the media item, tracking, and requesters it resolves against.
func TestNotification_NotifyWantAvailable(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	svc := service.NewNotificationService(r)
	ctx := context.Background()

	year := int32(1999)
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

	bin := parsing.BinKey{Source: parsing.SourceBluRay, Resolution: parsing.Res1080p, Modifier: parsing.ModNone}
	profile, err := r.CreateQualityProfile(ctx, repo.CreateQualityProfileParams{
		Name:   "HD",
		Domain: "movie",
		Bins:   []parsing.BinKey{bin},
		Cutoff: bin,
	})
	if err != nil {
		t.Fatalf("create quality profile: %v", err)
	}

	tracking, err := r.CreateTracking(ctx, repo.CreateTrackingParams{
		MediaItemID:      media.ID,
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

	alice := newNotifUser(t, ctx, r, "notify-alice@test.local")
	bob := newNotifUser(t, ctx, r, "notify-bob@test.local")
	for _, u := range []model.User{alice, bob} {
		if _, err := r.AddRequester(ctx, repo.AddRequesterParams{
			TrackingID: tracking.ID,
			UserID:     u.ID,
			Tier:       "HD",
			ScopeRule:  "all",
		}); err != nil {
			t.Fatalf("add requester %s: %v", u.ID, err)
		}
	}

	// The want is the work item that just went available; only its tracking/media
	// linkage matters to the producer.
	want := model.Want{TrackingID: tracking.ID, MediaItemID: media.ID}
	if err := svc.NotifyWantAvailable(ctx, want); err != nil {
		t.Fatalf("notify want available: %v", err)
	}

	due, err := r.ListDueOutbox(ctx, 100)
	if err != nil {
		t.Fatalf("list due: %v", err)
	}
	if len(due) != 2 {
		t.Fatalf("enqueued rows = %d, want 2 (one per requester)", len(due))
	}
	recipients := map[string]bool{}
	for _, row := range due {
		if row.EventType != "want.available" || row.Channel != string(model.ChannelInApp) {
			t.Fatalf("row = %q/%s, want want.available/in_app", row.EventType, row.Channel)
		}
		if row.RecipientUserID != nil {
			recipients[row.RecipientUserID.String()] = true
		}
		var payload struct {
			Media struct {
				Title string `json:"title"`
				Year  int    `json:"year"`
			} `json:"media"`
		}
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			t.Fatalf("payload: %v", err)
		}
		if payload.Media.Title != "The Matrix" || payload.Media.Year != 1999 {
			t.Fatalf("payload media = %+v, want The Matrix (1999)", payload.Media)
		}
	}
	if !recipients[alice.ID.String()] || !recipients[bob.ID.String()] {
		t.Fatalf("recipients = %v, want both alice and bob", recipients)
	}
}
