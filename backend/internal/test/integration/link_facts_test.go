//go:build integration

package integration

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/kyleaupton/arrflix/internal/downloader"
	"github.com/kyleaupton/arrflix/internal/indexer"
	importw "github.com/kyleaupton/arrflix/internal/jobs/import"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

// importedMovie runs a movie request through acquisition, download and import
// with the download payload on the same filesystem as the library, so the
// import places the file by hardlink. It returns the library, the imported
// file's state and the absolute path of the library copy.
func importedMovie(t *testing.T) (*testapp.App, context.Context, model.Library, model.FileState, string) {
	t.Helper()
	app, ctx, library := cancelApp(t)
	_, token := friendToken(t, ctx, app)

	req := createMovieRequest(t, app, token)
	if req.SpawnedTrackingID == nil {
		t.Fatalf("auto-approved request spawned no tracking")
	}
	var wants []model.Want
	app.GET(t, "/api/v1/tracking/"+req.SpawnedTrackingID.String()+"/wants", &wants, http.StatusOK)
	if len(wants) != 1 {
		t.Fatalf("wants on spawned tracking = %d, want 1", len(wants))
	}

	content := []byte("hardlinked movie bytes")
	payload := filepath.Join(t.TempDir(), "The.Matrix.1999.1080p.BluRay.x264.mkv")
	if err := os.WriteFile(payload, content, 0o644); err != nil {
		t.Fatalf("write download payload: %v", err)
	}
	client := &fakeDownloaderClient{files: []downloader.File{{Path: payload, Size: int64(len(content))}}}
	source := stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return []indexer.SearchResult{movieResult("guid-link")}, nil
		},
	}
	reg := downloader.NewRegistry()
	reg.Register(downloader.TypeQbittorrent, func(downloader.ConfigRecord) (downloader.Client, error) {
		return client, nil
	})
	dm := downloader.NewManager(reg, app.Repo, logger.New(false))
	if err := dm.Initialize(ctx); err != nil {
		t.Fatalf("initialize downloader manager: %v", err)
	}

	stopAcq := runWorker(t, ctx, newAcquisitionWorker(app.Repo, source))
	stopDl := runWorker(t, ctx, newDownloadWorkerWithClient(t, ctx, app.Repo, client))
	stopImp := runWorker(t, ctx, importw.New(app.Repo, dm, logger.New(false), nil, nil, nil))
	waitForWantStatus(t, ctx, app.Repo, wants[0].ID, model.WantAvailable, 30*time.Second)
	stopImp()
	stopDl()
	stopAcq()

	files, err := app.Repo.ListFilesForItem(ctx, wants[0].MediaItemID)
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("library files for title = %d, want 1", len(files))
	}
	state, err := app.Repo.GetFileState(ctx, files[0].ID)
	if err != nil {
		t.Fatalf("get file state: %v", err)
	}
	return app, ctx, library, state, filepath.Join(library.RootPath, files[0].Path)
}

// statLinkFacts stats path and returns its size and link facts as file_state
// stores them.
func statLinkFacts(t *testing.T, path string) (size int64, facts model.LinkFacts) {
	t.Helper()
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return st.Size, model.LinkFacts{
		Device:    int64(st.Dev),   //nolint:gosec,unconvert // stored bit-for-bit
		Inode:     int64(st.Ino),   //nolint:gosec // stored bit-for-bit
		LinkCount: int64(st.Nlink), //nolint:gosec,unconvert // width varies by platform
	}
}

// assertStateMatchesStat fails unless state holds exactly the size and link
// facts a stat of path reports now.
func assertStateMatchesStat(t *testing.T, state model.FileState, path string) model.LinkFacts {
	t.Helper()
	size, want := statLinkFacts(t, path)
	if state.SizeBytes == nil || *state.SizeBytes != size {
		t.Errorf("file_state.size_bytes = %v, want %d", deref(state.SizeBytes), size)
	}
	if state.Device == nil || *state.Device != want.Device {
		t.Errorf("file_state.st_dev = %v, want %d", deref(state.Device), want.Device)
	}
	if state.Inode == nil || *state.Inode != want.Inode {
		t.Errorf("file_state.st_ino = %v, want %d", deref(state.Inode), want.Inode)
	}
	if state.LinkCount == nil || *state.LinkCount != want.LinkCount {
		t.Errorf("file_state.st_nlink = %v, want %d", deref(state.LinkCount), want.LinkCount)
	}
	return want
}

func deref(p *int64) any {
	if p == nil {
		return "NULL"
	}
	return *p
}

// REQ-LINK-001 — Every file Arrflix imports has its device, inode and link
// count captured from the post-link stat().
//
// A hardlink import leaves the bytes with two names, the download and the
// library copy. The file's file_state must match a stat of the library copy
// (size, device, inode, link count) and its link count must be 2.
func TestReqLink001_ImportRecordsPostLinkStat(t *testing.T) {
	t.Parallel()
	_, _, _, state, dest := importedMovie(t)

	facts := assertStateMatchesStat(t, state, dest)
	if facts.LinkCount != 2 {
		t.Fatalf("stat of %s reports %d links, want 2 (import should hardlink)", dest, facts.LinkCount)
	}
	if state.LinkCount == nil || *state.LinkCount != 2 {
		t.Errorf("file_state.st_nlink = %v, want 2", deref(state.LinkCount))
	}
}

// REQ-LINK-002 — Every file a scan or verify re-stats has its device, inode
// and link count refreshed.
//
// After an import, a third name for the bytes is linked outside Arrflix and
// the library is rescanned. The already-known file's file_state must take the
// new link count (3), still match a stat of the library copy, and its
// last-verified time must move.
func TestReqLink002_RescanRefreshesKnownFileState(t *testing.T) {
	t.Parallel()
	app, ctx, library, before, dest := importedMovie(t)

	if err := os.Link(dest, filepath.Join(t.TempDir(), "elsewhere.mkv")); err != nil {
		t.Fatalf("add outside hardlink: %v", err)
	}

	var scanOut map[string]any
	app.POST(t, "/api/v1/libraries/"+library.ID.String()+"/scan", nil, &scanOut, http.StatusAccepted)

	deadline := time.Now().Add(30 * time.Second)
	var after model.FileState
	for {
		var err error
		after, err = app.Repo.GetFileState(ctx, before.FileID)
		if err != nil {
			t.Fatalf("get file state: %v", err)
		}
		if after.LastVerifiedAt.After(before.LastVerifiedAt) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("last_verified_at did not move past %s after rescan", before.LastVerifiedAt)
		}
		time.Sleep(100 * time.Millisecond)
	}

	if after.LinkCount == nil || *after.LinkCount != 3 {
		t.Errorf("file_state.st_nlink after rescan = %v, want 3", deref(after.LinkCount))
	}
	assertStateMatchesStat(t, after, dest)
}
