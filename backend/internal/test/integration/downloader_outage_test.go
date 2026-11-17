//go:build integration

package integration

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/downloader"
	"github.com/kyleaupton/arrflix/internal/downloader/qbittorrent"
	"github.com/kyleaupton/arrflix/internal/indexer"
	downloadworker "github.com/kyleaupton/arrflix/internal/jobs/download"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/qbittest"
)

// scriptedDownloaderClient is a fake downloader whose connection test, adds and
// polls can be made to fail. Failures are scripted as "the first N calls fail";
// the counters let a test wait until the worker has hit them.
type scriptedDownloaderClient struct {
	fakeDownloaderClient

	mu       sync.Mutex
	testOK   bool
	addErr   error
	addFails int
	getErr   error
	getFails int
	tests    int
	adds     int
	gets     int
}

func (f *scriptedDownloaderClient) Test(ctx context.Context) (downloader.TestResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tests++
	if !f.testOK {
		return downloader.TestResult{Success: false, Error: "Unable to connect to qBittorrent."}, nil
	}
	return downloader.TestResult{Success: true}, nil
}

func (f *scriptedDownloaderClient) Add(ctx context.Context, req downloader.AddRequest) (downloader.AddResult, error) {
	f.mu.Lock()
	f.adds++
	fail := f.addErr != nil && (f.addFails < 0 || f.adds <= f.addFails)
	f.mu.Unlock()
	if fail {
		return downloader.AddResult{}, f.addErr
	}
	return f.fakeDownloaderClient.Add(ctx, req)
}

func (f *scriptedDownloaderClient) Get(ctx context.Context, externalID string) (downloader.Item, error) {
	f.mu.Lock()
	f.gets++
	fail := f.getErr != nil && f.gets <= f.getFails
	f.mu.Unlock()
	if fail {
		return downloader.Item{}, f.getErr
	}
	return f.fakeDownloaderClient.Get(ctx, externalID)
}

func (f *scriptedDownloaderClient) setTestOK(ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.testOK = ok
}

func (f *scriptedDownloaderClient) counts() (tests, adds, gets int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tests, f.adds, f.gets
}

// connRefused is the error a dial to a stopped qBittorrent produces, marked
// unreachable the way the qBittorrent client marks transport failures on its own
// API.
func connRefused(op string) error {
	return fmt.Errorf("%s: %w", op, downloader.MarkUnreachable(&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}))
}

// newOutageWorker wires a download worker around client on a fast cadence with a
// tiny outage backoff cap, so a long run of outage retries takes milliseconds.
func newOutageWorker(t *testing.T, ctx context.Context, r *repo.Repository, client downloader.Client) *downloadworker.Worker {
	t.Helper()
	reg := downloader.NewRegistry()
	reg.Register(downloader.TypeQbittorrent, func(rec downloader.ConfigRecord) (downloader.Client, error) {
		return client, nil
	})
	dm := downloader.NewManager(reg, r, logger.New(false))
	if err := dm.Initialize(ctx); err != nil {
		t.Fatalf("initialize downloader manager: %v", err)
	}
	return downloadworker.NewWithConfig(r, dm, logger.New(false), nil, nil, downloadworker.Config{
		PollInterval:     10 * time.Millisecond,
		ClaimLimit:       20,
		MaxAttempts:      3,
		OutageBackoffMax: 10 * time.Millisecond,
	})
}

// waitForJobStatus polls until the download job reaches status. A job that
// fails while waiting for anything else fails the test at once.
func waitForJobStatus(t *testing.T, ctx context.Context, r *repo.Repository, jobID uuid.UUID, status string, timeout time.Duration) model.DownloadJob {
	t.Helper()
	var job model.DownloadJob
	waitFor(t, timeout, "download job status "+status, func() bool {
		var err error
		job, err = r.GetDownloadJob(ctx, jobID)
		if err != nil {
			t.Fatalf("get download job: %v", err)
		}
		if job.Status == "failed" && status != "failed" {
			t.Fatalf("download job failed (%s), want %s", deref(job.LastError), status)
		}
		return job.Status == status
	})
	return job
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// assertNotExcluded fails if the want has any release exclusion.
func assertNotExcluded(t *testing.T, ctx context.Context, r *repo.Repository, wantID uuid.UUID) {
	t.Helper()
	excl, err := r.ListWantReleaseExclusionsForWants(ctx, []uuid.UUID{wantID})
	if err != nil {
		t.Fatalf("list exclusions: %v", err)
	}
	if len(excl) != 0 {
		t.Errorf("exclusions for want %s = %d, want 0 (a downloader outage must not blocklist the release)", wantID, len(excl))
	}
}

// TestReqHard003_MissingClientStaysRetryable:
// REQ-HARD-003 — A downloader that is unreachable must leave its jobs retryable.
// It must never fail them as non-retryable, which blocklists good releases. A
// downloader that failed its connection test at startup has no client; its job
// keeps retrying without spending attempts or excluding the release, and the
// downloader is connected lazily once it answers.
func TestReqHard003_MissingClientStaysRetryable(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)
	job := grabMovieWant(t, ctx, r, want, []indexer.SearchResult{movieResult("guid-a")})

	client := &scriptedDownloaderClient{testOK: false}
	w := newOutageWorker(t, ctx, r, client)
	cancel := runWorker(t, ctx, w)
	defer cancel()

	// Let the worker retry the client-less job well past the attempt limit.
	waitFor(t, 10*time.Second, "outage retries", func() bool {
		events, err := r.ListDownloadJobEvents(ctx, job.ID)
		if err != nil {
			t.Fatalf("list events: %v", err)
		}
		retries := 0
		for _, e := range events {
			if e.EventType == "retry_scheduled" {
				retries++
			}
		}
		return retries > 5
	})

	got, err := r.GetDownloadJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get download job: %v", err)
	}
	if got.Status != "created" {
		t.Errorf("job status = %q, want created (still retryable)", got.Status)
	}
	if got.AttemptCount != 0 {
		t.Errorf("job attemptCount = %d, want 0 (an outage spends no attempts)", got.AttemptCount)
	}
	assertNotExcluded(t, ctx, r, want.ID)

	// The downloader comes back: the next job that needs it connects it, and the
	// job runs to completion.
	client.setTestOK(true)
	waitForJobStatus(t, ctx, r, job.ID, "completed", 20*time.Second)
	assertNotExcluded(t, ctx, r, want.ID)
}

// TestReqHard003_ConnectionErrorsRetryUntilDownloaderAnswers:
// REQ-HARD-003 — A downloader that is unreachable must leave its jobs retryable.
// It must never fail them as non-retryable, which blocklists good releases. Adds
// and polls that hit a refused connection more than 3 times in a row neither fail
// the job nor exclude its release, and the job completes once the downloader
// answers.
func TestReqHard003_ConnectionErrorsRetryUntilDownloaderAnswers(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)
	job := grabMovieWant(t, ctx, r, want, []indexer.SearchResult{movieResult("guid-a")})

	client := &scriptedDownloaderClient{
		testOK:   true,
		addErr:   connRefused("add magnet"),
		addFails: 5,
		getErr:   connRefused("downloader get"),
		getFails: 5,
	}
	w := newOutageWorker(t, ctx, r, client)
	cancel := runWorker(t, ctx, w)
	defer cancel()

	done := waitForJobStatus(t, ctx, r, job.ID, "completed", 20*time.Second)

	if _, adds, gets := client.counts(); adds <= 5 || gets <= 5 {
		t.Errorf("downloader calls = %d adds, %d gets; want each > 5 (5 refused, then answered)", adds, gets)
	}
	// The one successful add is the only attempt the job spent.
	if done.AttemptCount != 1 {
		t.Errorf("job attemptCount = %d, want 1 (refused connections spend no attempts)", done.AttemptCount)
	}
	assertNotExcluded(t, ctx, r, want.ID)
}

// TestReqHard003_RejectedTorrentStillExcluded:
// REQ-HARD-003 — A downloader that is unreachable must leave its jobs retryable;
// a downloader that answers and rejects the torrent itself (a 4xx) is a verdict
// on the release. That still spends the job's attempts, fails it, and excludes
// the release.
func TestReqHard003_RejectedTorrentStillExcluded(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	want := seedPendingWant(t, ctx, r)
	job := grabMovieWant(t, ctx, r, want, []indexer.SearchResult{movieResult("guid-a")})

	client := &scriptedDownloaderClient{
		testOK:   true,
		addErr:   fmt.Errorf("upload torrent: %w", &qbittorrent.APIError{StatusCode: 415, Body: "Torrent file is not valid"}),
		addFails: -1,
	}
	w := newOutageWorker(t, ctx, r, client)
	cancel := runWorker(t, ctx, w)
	defer cancel()

	// MaxAttempts=3 with 2^attempt-second backoff → ~6s before the terminal fail.
	waitForJobStatus(t, ctx, r, job.ID, "failed", 30*time.Second)
	waitForWantStatus(t, ctx, r, want.ID, model.WantPending, 10*time.Second)
	assertExcluded(t, ctx, r, want.ID, job.IndexerID, job.Guid)
}

// TestReqHard003_UnreachableTorrentLinkStillExcluded:
// REQ-HARD-003 — Only the downloader being unreachable leaves a job retrying
// indefinitely. qBittorrent answering while the release's torrent link (the
// indexer or Prowlarr proxy) is dead is a failure of the release: it spends the
// job's attempts, fails it, and excludes the release.
func TestReqHard003_UnreachableTorrentLinkStillExcluded(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()

	release := movieResult("guid-dead-link")
	release.DownloadURL = dead.URL + "/guid-dead-link.torrent"
	want := seedPendingWant(t, ctx, r)
	job := grabMovieWant(t, ctx, r, want, []indexer.SearchResult{release})

	qb := qbittest.New(t)
	reg := downloader.NewRegistry()
	reg.Register(downloader.TypeQbittorrent, func(rec downloader.ConfigRecord) (downloader.Client, error) {
		return qbittorrent.NewQBittorrentClient(rec.ID, qb.URL, "admin", "admin"), nil
	})
	dm := downloader.NewManager(reg, r, logger.New(false))
	if err := dm.Initialize(ctx); err != nil {
		t.Fatalf("initialize downloader manager: %v", err)
	}
	w := downloadworker.NewWithConfig(r, dm, logger.New(false), nil, nil, downloadworker.Config{
		PollInterval:     10 * time.Millisecond,
		ClaimLimit:       20,
		MaxAttempts:      3,
		OutageBackoffMax: 10 * time.Millisecond,
	})
	cancel := runWorker(t, ctx, w)
	defer cancel()

	// MaxAttempts=3 with 2^attempt-second backoff → ~6s before the terminal fail.
	waitForJobStatus(t, ctx, r, job.ID, "failed", 30*time.Second)
	waitForWantStatus(t, ctx, r, want.ID, model.WantPending, 10*time.Second)
	assertExcluded(t, ctx, r, want.ID, job.IndexerID, job.Guid)
}
