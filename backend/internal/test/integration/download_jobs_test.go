//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
)

// TestDownloadJobs_ClaimRotation proves head-of-line blocking is gone: a wall of
// in-flight 'downloading' jobs with old next_run_at starves a newer 'created'
// job under a limited claim budget, but once each downloading job's next_run_at
// is advanced via the poll snapshot (the production rotation), the created job
// becomes claimable.
func TestDownloadJobs_ClaimRotation(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)

	downloader, err := r.CreateDownloader(ctx, repo.CreateDownloaderParams{
		Name:           "qbit-rotate",
		DownloaderType: "qbittorrent",
		Protocol:       "torrent",
		URL:            "http://localhost:8080",
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("create downloader: %v", err)
	}
	library, err := r.CreateLibrary(ctx, repo.CreateLibraryParams{
		Name: "Movies-rotate", Type: "movie", RootPath: "/movies-rotate",
	})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	nameTemplate, err := r.CreateNameTemplate(ctx, repo.CreateNameTemplateParams{
		Name: "default-rotate", Type: "movie", Template: "{title}",
	})
	if err != nil {
		t.Fatalf("create name template: %v", err)
	}

	createJob := func(guid string) uuid.UUID {
		t.Helper()
		job, err := r.CreateDownloadJob(ctx, repo.CreateDownloadJobParams{
			Protocol:       "torrent",
			MediaType:      "movie",
			MediaItemID:    want.MediaItemID,
			IndexerID:      1,
			Guid:           guid,
			CandidateTitle: "The Matrix 1999 1080p BluRay",
			CandidateLink:  "http://localhost/torrent",
			DownloaderID:   downloader.ID,
			LibraryID:      library.ID,
			NameTemplateID: nameTemplate.ID,
		})
		if err != nil {
			t.Fatalf("create download job %q: %v", guid, err)
		}
		return job.ID
	}

	// Two 'downloading' jobs pinned at an old next_run_at — the stalled wall.
	past := time.Now().Add(-time.Hour)
	downloadingIDs := map[uuid.UUID]bool{}
	for _, guid := range []string{"dl-1", "dl-2"} {
		id := createJob(guid)
		downloadingIDs[id] = true
		if _, err := r.SetDownloadJobDownloadSnapshot(ctx, repo.SetDownloadJobDownloadSnapshotParams{
			ID:        id,
			Status:    "downloading",
			NextRunAt: past,
		}); err != nil {
			t.Fatalf("snapshot %q downloading: %v", guid, err)
		}
	}

	// One freshly-'created' job (next_run_at ~ now, newer than the stalled wall).
	createdID := createJob("created-1")

	// Budget of 2: the two old 'downloading' jobs fill every slot, so the newer
	// 'created' job is starved — the head-of-line blocking bug.
	claimed, err := r.ClaimRunnableDownloadJobs(ctx, 2)
	if err != nil {
		t.Fatalf("claim (pre-rotation): %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("claimed %d, want 2 (the stalled wall)", len(claimed))
	}
	for _, j := range claimed {
		if !downloadingIDs[j.ID] {
			t.Fatalf("claimed %s (status %q), want only the stalled downloading jobs", j.ID, j.Status)
		}
	}

	// Poll-rotation: advancing each downloading job's next_run_at past now() (what
	// the worker's snapshot now does) takes them out of the runnable set.
	future := time.Now().Add(3 * time.Second)
	for id := range downloadingIDs {
		if _, err := r.SetDownloadJobDownloadSnapshot(ctx, repo.SetDownloadJobDownloadSnapshotParams{
			ID:        id,
			Status:    "downloading",
			NextRunAt: future,
		}); err != nil {
			t.Fatalf("rotate snapshot: %v", err)
		}
	}

	// Same budget now claims the previously-starved 'created' job.
	claimed, err = r.ClaimRunnableDownloadJobs(ctx, 2)
	if err != nil {
		t.Fatalf("claim (post-rotation): %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed %d, want 1 (only the created job is runnable)", len(claimed))
	}
	if claimed[0].ID != createdID {
		t.Errorf("claimed %s, want the created job %s", claimed[0].ID, createdID)
	}
}
