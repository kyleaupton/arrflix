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
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

// TestTracking_Cancel proves stop-tracking: every non-terminal want (and its
// in-flight download job) is canceled, the tracking is normalized to 'canceled',
// and an already-'available' want plus its file are left intact.
func TestTracking_Cancel(t *testing.T) {
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

	// Series media item → one season → three aired episodes.
	tmdbID := int64(1399)
	item, err := app.Repo.UpsertMediaItem(ctx, repo.UpsertMediaItemParams{
		Type:   string(parsing.DomainSeries),
		Title:  "Cancel Show",
		TmdbID: &tmdbID,
	})
	if err != nil {
		t.Fatalf("upsert media item: %v", err)
	}
	season, err := app.Repo.UpsertSeason(ctx, repo.UpsertSeasonParams{MediaItemID: item.ID, SeasonNumber: 1})
	if err != nil {
		t.Fatalf("upsert season: %v", err)
	}
	past := time.Now().Add(-48 * time.Hour)
	mkEpisode := func(num int32, tmdb int64) model.MediaEpisode {
		t.Helper()
		ep, err := app.Repo.UpsertEpisode(ctx, repo.UpsertEpisodeParams{
			SeasonID:      season.ID,
			EpisodeNumber: num,
			AirDate:       &past,
			TmdbID:        &tmdb,
		})
		if err != nil {
			t.Fatalf("upsert episode e%d: %v", num, err)
		}
		return ep
	}
	eAvail := mkEpisode(1, 101) // already acquired
	ePending := mkEpisode(2, 102)
	eDownloading := mkEpisode(3, 103)

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

	mkWant := func(episodeID uuid.UUID, status model.WantStatus) model.Want {
		t.Helper()
		w, err := app.Repo.CreateWant(ctx, repo.CreateWantParams{
			TrackingID:       tracking.ID,
			MediaItemID:      item.ID,
			EpisodeID:        &episodeID,
			QualityProfileID: profile.ID,
			Status:           string(status),
			Segment:          string(model.WantSegmentOngoing),
		})
		if err != nil {
			t.Fatalf("create want (%s): %v", status, err)
		}
		return w
	}
	wAvail := mkWant(eAvail.ID, model.WantAvailable)
	wPending := mkWant(ePending.ID, model.WantPending)
	wDownloading := mkWant(eDownloading.ID, model.WantDownloading)

	// Routing prerequisites + an in-flight download job for the downloading want.
	library, err := app.Repo.CreateLibrary(ctx, repo.CreateLibraryParams{
		Name: "Series", Type: "series", RootPath: "/series", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	downloader, err := app.Repo.CreateDownloader(ctx, repo.CreateDownloaderParams{
		Name:           "qbit",
		DownloaderType: "qbittorrent",
		Protocol:       "torrent",
		URL:            "http://localhost:8080",
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("create downloader: %v", err)
	}
	nameTemplate, err := app.Repo.CreateNameTemplate(ctx, repo.CreateNameTemplateParams{
		Name: "default-series", Type: "series", Template: "{title}",
	})
	if err != nil {
		t.Fatalf("create name template: %v", err)
	}

	// A file backs the available want — it must survive the cancel.
	availFile, err := app.Repo.CreateFile(ctx, repo.CreateFileParams{
		ID:          uuid.New(),
		LibraryID:   library.ID,
		Path:        "show.s01e01.mkv",
		MediaItemID: &item.ID,
		EpisodeID:   &eAvail.ID,
	})
	if err != nil {
		t.Fatalf("create file for available episode: %v", err)
	}

	job, err := app.Repo.CreateDownloadJob(ctx, repo.CreateDownloadJobParams{
		Protocol:       "torrent",
		MediaType:      "series",
		MediaItemID:    item.ID,
		IndexerID:      1,
		Guid:           "guid-downloading",
		CandidateTitle: "Cancel Show S01E03 1080p WEB",
		CandidateLink:  "http://localhost/torrent",
		DownloaderID:   downloader.ID,
		LibraryID:      library.ID,
		NameTemplateID: nameTemplate.ID,
	})
	if err != nil {
		t.Fatalf("create download job: %v", err)
	}
	if err := app.Repo.LinkDownloadJobWant(ctx, job.ID, wDownloading.ID); err != nil {
		t.Fatalf("link download job to want: %v", err)
	}
	if _, err := app.Repo.SetDownloadJobDownloadSnapshot(ctx, repo.SetDownloadJobDownloadSnapshotParams{
		ID:     job.ID,
		Status: "downloading",
	}); err != nil {
		t.Fatalf("set download job downloading: %v", err)
	}

	// Stop tracking. Admin holds tracking.cancel.any, so the own/any gate passes
	// even though this repo-built tracking has no requesters.
	admin := adminUser(t, app, ctx)
	canceled, err := app.Services.Tracking.Cancel(ctx, admin.ID, tracking.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if canceled.State != string(model.TrackingCanceled) {
		t.Errorf("returned tracking state = %q, want %q", canceled.State, model.TrackingCanceled)
	}

	assertWantStatus := func(label string, id uuid.UUID, wantStatus model.WantStatus) {
		t.Helper()
		got, err := app.Repo.GetWant(ctx, id)
		if err != nil {
			t.Fatalf("get %s want: %v", label, err)
		}
		if got.Status != string(wantStatus) {
			t.Errorf("%s want status = %q, want %q", label, got.Status, wantStatus)
		}
	}
	// Non-terminal wants flipped canceled; the available want is untouched.
	assertWantStatus("pending", wPending.ID, model.WantCanceled)
	assertWantStatus("downloading", wDownloading.ID, model.WantCanceled)
	assertWantStatus("available", wAvail.ID, model.WantAvailable)

	// The in-flight job for the downloading want is cancelled.
	gotJob, err := app.Repo.GetDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get download job: %v", err)
	}
	if gotJob.Status != "cancelled" {
		t.Errorf("download job status = %q, want %q", gotJob.Status, "cancelled")
	}

	// The available want's file is left intact.
	if _, err := app.Repo.GetFile(ctx, availFile.ID); err != nil {
		t.Errorf("available file should survive cancel, got: %v", err)
	}
}

// TestTracking_GetByTmdbID_Series proves the read resolves a series by
// (tmdbId, type=series): a series media item, its tracking, and its wants come
// back, and the type discriminates the lookup from any movie with the same id.
func TestTracking_GetByTmdbID_Series(t *testing.T) {
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

	tmdbID := int64(1396)
	item, err := app.Repo.UpsertMediaItem(ctx, repo.UpsertMediaItemParams{
		Type:   string(parsing.DomainSeries),
		Title:  "Resolve Show",
		TmdbID: &tmdbID,
	})
	if err != nil {
		t.Fatalf("upsert media item: %v", err)
	}
	season, err := app.Repo.UpsertSeason(ctx, repo.UpsertSeasonParams{MediaItemID: item.ID, SeasonNumber: 1})
	if err != nil {
		t.Fatalf("upsert season: %v", err)
	}
	air := time.Now().Add(-24 * time.Hour)
	episodeTmdb := int64(2001)
	episode, err := app.Repo.UpsertEpisode(ctx, repo.UpsertEpisodeParams{
		SeasonID:      season.ID,
		EpisodeNumber: 1,
		AirDate:       &air,
		TmdbID:        &episodeTmdb,
	})
	if err != nil {
		t.Fatalf("upsert episode: %v", err)
	}
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
	want, err := app.Repo.CreateWant(ctx, repo.CreateWantParams{
		TrackingID:       tracking.ID,
		MediaItemID:      item.ID,
		EpisodeID:        &episode.ID,
		QualityProfileID: profile.ID,
		Status:           string(model.WantPending),
		Segment:          string(model.WantSegmentOngoing),
	})
	if err != nil {
		t.Fatalf("create want: %v", err)
	}

	admin := adminUser(t, app, ctx)
	gotTracking, gotWants, _, err := app.Services.Tracking.GetByTmdbID(ctx, admin.ID, tmdbID, string(parsing.DomainSeries))
	if err != nil {
		t.Fatalf("GetByTmdbID(series): %v", err)
	}
	if gotTracking == nil {
		t.Fatalf("resolved tracking = nil, want %s", tracking.ID)
	}
	if gotTracking.ID != tracking.ID {
		t.Errorf("resolved tracking = %s, want %s", gotTracking.ID, tracking.ID)
	}
	if len(gotWants) != 1 || gotWants[0].ID != want.ID {
		t.Fatalf("resolved wants = %+v, want the single seeded want %s", gotWants, want.ID)
	}
	if gotWants[0].EpisodeID == nil || *gotWants[0].EpisodeID != episode.ID {
		t.Errorf("resolved want episodeId = %v, want %s", gotWants[0].EpisodeID, episode.ID)
	}

	// The type discriminates: no movie with this id exists, so a movie lookup
	// resolves as untracked — nil tracking, no error (not a 404).
	if gotMovie, _, _, err := app.Services.Tracking.GetByTmdbID(ctx, admin.ID, tmdbID, string(parsing.DomainMovie)); err != nil || gotMovie != nil {
		t.Errorf("GetByTmdbID(movie) for series-only id = (%v, %v), want (nil, nil)", gotMovie, err)
	}
}
