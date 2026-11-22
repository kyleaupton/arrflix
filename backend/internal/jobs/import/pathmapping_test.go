//go:build integration

package importw

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/downloader"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
)

// jobDownloaderID returns the downloader the fixture's download job was
// grabbed on.
func jobDownloaderID(t *testing.T, ctx context.Context, r *repo.Repository, task model.ImportTask) uuid.UUID {
	t.Helper()
	job, err := r.GetDownloadJob(ctx, task.DownloadJobID)
	if err != nil {
		t.Fatalf("get download job: %v", err)
	}
	return job.DownloaderID
}

func createVolume(t *testing.T, ctx context.Context, r *repo.Repository, name, arrflixBase string, dl uuid.UUID, downloaderBase string) model.Volume {
	t.Helper()
	v, err := r.CreateVolume(ctx, repo.SaveVolumeParams{
		Name:        name,
		ArrflixBase: arrflixBase,
		Aliases:     []repo.VolumeAliasParams{{DownloaderID: dl, Base: downloaderBase}},
	})
	if err != nil {
		t.Fatalf("create volume %q: %v", name, err)
	}
	return v
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

// importedContent returns the bytes of the file the completed task placed in
// the library.
func importedContent(t *testing.T, ctx context.Context, r *repo.Repository, fix importFixture) []byte {
	t.Helper()
	task, err := r.GetImportTask(ctx, fix.task.ID)
	if err != nil || task.DestPath == nil {
		t.Fatalf("import task has no destination (err %v)", err)
	}
	got, err := os.ReadFile(filepath.Join(fix.libraryRoot, *task.DestPath))
	if err != nil {
		t.Fatalf("read imported file: %v", err)
	}
	return got
}

func pathEvents(t *testing.T, ctx context.Context, r *repo.Repository, taskID uuid.UUID) []string {
	t.Helper()
	events, err := r.ListImportTaskEvents(ctx, taskID)
	if err != nil {
		t.Fatalf("list import task events: %v", err)
	}
	var out []string
	for _, e := range events {
		if e.EventType == "path_resolved" && e.Message != nil {
			out = append(out, *e.Message)
		}
	}
	return out
}

// REQ-PATH-001 — A file the downloader reports at its own path must be
// imported from the correct Arrflix path.
//
// The downloader reports /downloads/X/X.mkv, a path that exists only in its
// own frame. A volume aliases the downloader's /downloads to Arrflix's
// <tmp>/torrents, so the import must read <tmp>/torrents/X/X.mkv, record that
// translation on the task, and place exactly those bytes in the library. With
// a nested volume on /downloads/tv, a file under it resolves to the nested
// volume, not the outer one.
func TestReqPath001_ImportsDownloaderPathFromMappedArrflixPath(t *testing.T) {
	t.Parallel()

	t.Run("alias swaps the downloader's base for the volume's", func(t *testing.T) {
		t.Parallel()
		pool := dbtest.New(t)
		r := repo.New(pool)
		ctx := context.Background()
		fix := seedImportTask(t, ctx, r, true)
		dl := jobDownloaderID(t, ctx, r, fix.task)

		root := t.TempDir()
		arrflixBase := filepath.Join(root, "torrents")
		content := []byte("mapped movie bytes")
		writeFile(t, filepath.Join(arrflixBase, "X", "X.mkv"), content)
		createVolume(t, ctx, r, "media", arrflixBase, dl, "/downloads")

		if err := newTestWorker(t, r, "/downloads/X/X.mkv").processTask(ctx, fix.task); err != nil {
			t.Fatalf("processTask: %v", err)
		}
		if got := importedContent(t, ctx, r, fix); !bytes.Equal(got, content) {
			t.Errorf("imported content = %q, want the mapped file's %q", got, content)
		}
		task, _ := r.GetImportTask(ctx, fix.task.ID)
		if want := filepath.Join(arrflixBase, "X", "X.mkv"); task.SourcePath != want {
			t.Errorf("task source path = %q, want %q", task.SourcePath, want)
		}
		events := pathEvents(t, ctx, r, fix.task.ID)
		if len(events) != 1 || !strings.Contains(events[0], "/downloads/X/X.mkv") ||
			!strings.Contains(events[0], filepath.Join(arrflixBase, "X", "X.mkv")) || !strings.Contains(events[0], `volume "media"`) {
			t.Errorf("path_resolved events = %q, want one naming both paths and volume \"media\"", events)
		}
	})

	t.Run("nested volume resolves by longest prefix", func(t *testing.T) {
		t.Parallel()
		pool := dbtest.New(t)
		r := repo.New(pool)
		ctx := context.Background()
		fix := seedImportTask(t, ctx, r, true)
		dl := jobDownloaderID(t, ctx, r, fix.task)

		outer := filepath.Join(t.TempDir(), "outer")
		inner := filepath.Join(t.TempDir(), "inner")
		writeFile(t, filepath.Join(outer, "tv", "X.mkv"), []byte("outer volume's bytes"))
		content := []byte("inner volume's bytes")
		writeFile(t, filepath.Join(inner, "X.mkv"), content)
		createVolume(t, ctx, r, "outer", outer, dl, "/downloads")
		createVolume(t, ctx, r, "inner", inner, dl, "/downloads/tv")

		if err := newTestWorker(t, r, "/downloads/tv/X.mkv").processTask(ctx, fix.task); err != nil {
			t.Fatalf("processTask: %v", err)
		}
		if got := importedContent(t, ctx, r, fix); !bytes.Equal(got, content) {
			t.Errorf("imported content = %q, want the nested volume's %q", got, content)
		}
	})
}

// REQ-PATH-003 — An install where every actor sees the same paths must need
// no path configuration.
//
// With no volumes configured, the downloader reports a path that exists as-is
// in Arrflix. The import must succeed from exactly that path and record that
// no mapping applied.
func TestReqPath003_SharedPathsNeedNoConfiguration(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()
	fix := seedImportTask(t, ctx, r, true)
	content, err := os.ReadFile(fix.sourcePath)
	if err != nil {
		t.Fatal(err)
	}

	if err := newTestWorker(t, r, fix.sourcePath).processTask(ctx, fix.task); err != nil {
		t.Fatalf("processTask: %v", err)
	}
	if got := importedContent(t, ctx, r, fix); !bytes.Equal(got, content) {
		t.Errorf("imported content = %q, want %q", got, content)
	}
	task, _ := r.GetImportTask(ctx, fix.task.ID)
	if task.SourcePath != fix.sourcePath {
		t.Errorf("task source path = %q, want it unchanged %q", task.SourcePath, fix.sourcePath)
	}
	events := pathEvents(t, ctx, r, fix.task.ID)
	if len(events) != 1 || !strings.Contains(events[0], "no path mapping applied") {
		t.Errorf("path_resolved events = %q, want one saying no mapping applied", events)
	}
}

// REQ-PATH-005 — A path that can't be resolved must fail visibly at import,
// never silently misplace or skip a file.
//
// Each case leaves a real file at the task's stored source path, which is what
// a silent fallback would import. A resolved path that doesn't exist must fail
// the attempt with a message naming both frames, stay retryable, and succeed
// on retry once the mapping is fixed. A path that stays relative must fail
// without retrying. An unreachable downloader must fail retryably rather than
// fall back. In no failing case may any file be imported.
func TestReqPath005_UnresolvablePathFailsVisibly(t *testing.T) {
	t.Parallel()

	assertNothingImported := func(t *testing.T, ctx context.Context, r *repo.Repository, fix importFixture) {
		t.Helper()
		files, err := r.ListFilesForItem(ctx, fix.task.MediaItemID)
		if err != nil {
			t.Fatalf("list files: %v", err)
		}
		if len(files) != 0 {
			t.Errorf("file rows after a failed resolution = %d, want 0", len(files))
		}
		entries, err := os.ReadDir(fix.libraryRoot)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("library holds %d entries after a failed resolution, want none", len(entries))
		}
	}

	t.Run("missing mapped path fails visibly and a retry succeeds once fixed", func(t *testing.T) {
		t.Parallel()
		pool := dbtest.New(t)
		r := repo.New(pool)
		ctx := context.Background()
		fix := seedImportTask(t, ctx, r, true)
		dl := jobDownloaderID(t, ctx, r, fix.task)

		wrongBase := t.TempDir()
		vol := createVolume(t, ctx, r, "media", wrongBase, dl, "/downloads")
		w := newTestWorker(t, r, "/downloads/X/X.mkv")

		err := w.processTask(ctx, fix.task)
		if err == nil {
			t.Fatalf("processTask succeeded with an unresolvable path")
		}
		resolved := filepath.Join(wrongBase, "X", "X.mkv")
		want := `qbit reports "/downloads/X/X.mkv", resolved to "` + resolved +
			`", which doesn't exist in Arrflix. Add or fix a path mapping.`
		if err.Error() != want {
			t.Errorf("error = %q\nwant    %q", err.Error(), want)
		}
		if !apperrors.IsRetryable(err) {
			t.Errorf("missing resolved path is not retryable")
		}
		assertNothingImported(t, ctx, r, fix)

		w.handleError(ctx, fix.task, err)
		task, gerr := r.GetImportTask(ctx, fix.task.ID)
		if gerr != nil {
			t.Fatal(gerr)
		}
		if task.Status == "failed" {
			t.Errorf("task failed terminally, want it scheduled for retry")
		}
		if task.LastError == nil || *task.LastError != want {
			t.Errorf("task last error = %v, want %q", task.LastError, want)
		}

		fixedBase := t.TempDir()
		content := []byte("bytes behind the fixed mapping")
		writeFile(t, filepath.Join(fixedBase, "X", "X.mkv"), content)
		if _, err := r.UpdateVolume(ctx, vol.ID, repo.SaveVolumeParams{
			Name:        "media",
			ArrflixBase: fixedBase,
			Aliases:     []repo.VolumeAliasParams{{DownloaderID: dl, Base: "/downloads"}},
		}); err != nil {
			t.Fatalf("fix volume: %v", err)
		}
		if err := w.processTask(ctx, task); err != nil {
			t.Fatalf("retry after fixing the mapping: %v", err)
		}
		if got := importedContent(t, ctx, r, fix); !bytes.Equal(got, content) {
			t.Errorf("imported content = %q, want %q", got, content)
		}
	})

	t.Run("unmapped path that doesn't exist fails without importing the stored file", func(t *testing.T) {
		t.Parallel()
		pool := dbtest.New(t)
		r := repo.New(pool)
		ctx := context.Background()
		fix := seedImportTask(t, ctx, r, true)

		missing := filepath.Join(t.TempDir(), "nowhere", "X.mkv")
		err := newTestWorker(t, r, missing).processTask(ctx, fix.task)
		if err == nil || !strings.Contains(err.Error(), "Add or fix a path mapping") {
			t.Fatalf("processTask error = %v, want the visible path-mapping failure", err)
		}
		assertNothingImported(t, ctx, r, fix)
	})

	t.Run("relative path is rejected and not retried", func(t *testing.T) {
		t.Parallel()
		pool := dbtest.New(t)
		r := repo.New(pool)
		ctx := context.Background()
		fix := seedImportTask(t, ctx, r, true)

		client := &fakeClient{files: []downloader.File{{Path: "X/X.mkv", Size: 16}}}
		err := newTestWorkerWithClient(r, client).processTask(ctx, fix.task)
		if err == nil {
			t.Fatalf("processTask succeeded with a relative path")
		}
		if apperrors.KindOf(err) != apperrors.KindInternal || apperrors.IsRetryable(err) {
			t.Errorf("relative path error kind = %s retryable = %v, want internal and not retryable",
				apperrors.KindOf(err), apperrors.IsRetryable(err))
		}
		assertNothingImported(t, ctx, r, fix)
	})

	t.Run("unreachable downloader is retryable, not a fallback", func(t *testing.T) {
		t.Parallel()
		pool := dbtest.New(t)
		r := repo.New(pool)
		ctx := context.Background()
		fix := seedImportTask(t, ctx, r, true)

		client := &fakeClient{err: apperrors.BadGatewayf("connection refused")}
		err := newTestWorkerWithClient(r, client).processTask(ctx, fix.task)
		if err == nil {
			t.Fatalf("processTask succeeded with an unreachable downloader")
		}
		if apperrors.KindOf(err) != apperrors.KindBadGateway || !apperrors.IsRetryable(err) {
			t.Errorf("unreachable error kind = %s retryable = %v, want bad gateway and retryable",
				apperrors.KindOf(err), apperrors.IsRetryable(err))
		}
		assertNothingImported(t, ctx, r, fix)
	})
}
