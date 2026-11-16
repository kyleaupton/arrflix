//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/downloader"
	"github.com/kyleaupton/arrflix/internal/indexer"
	importw "github.com/kyleaupton/arrflix/internal/jobs/import"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
	"github.com/kyleaupton/arrflix/internal/test/tmdbtest"
)

const (
	seriesReqTmdbID = int64(424242)
	seriesReqTitle  = "Harbor Lights"
)

// seriesReqApp is a series-capable app: a fake TMDB the test scripts the episode
// tree on, the default quality profiles and name templates, and the default
// downloader and series library (rooted in a per-test temp dir) routing falls
// back to.
type seriesReqApp struct {
	app     *testapp.App
	ctx     context.Context
	tmdb    *tmdbtest.Server
	library model.Library
}

func newSeriesReqApp(t *testing.T) seriesReqApp {
	t.Helper()
	pool := dbtest.New(t)
	tmdbSrv, tmdbClient := tmdbtest.New(t)
	app := testapp.New(t, pool, testapp.WithTMDB(tmdbClient))
	ctx := context.Background()
	if err := app.Services.QualityProfiles.SeedDefaults(ctx); err != nil {
		t.Fatalf("seed quality profiles: %v", err)
	}
	if err := app.Services.NameTemplates.SeedDefaults(ctx); err != nil {
		t.Fatalf("seed name templates: %v", err)
	}
	app.POST(t, "/api/v1/downloaders", map[string]any{
		"name":     "Default qBit",
		"type":     "qbittorrent",
		"protocol": "torrent",
		"url":      "http://localhost:9999",
		"enabled":  true,
		"default":  true,
	}, nil, http.StatusCreated)
	var library model.Library
	app.POST(t, "/api/v1/libraries", map[string]any{
		"name":     "Series",
		"type":     "series",
		"rootPath": t.TempDir(),
		"enabled":  true,
		"default":  true,
	}, &library, http.StatusCreated)
	return seriesReqApp{app: app, ctx: ctx, tmdb: tmdbSrv, library: library}
}

// scriptSeries (re)scripts the fake TMDB with the series' details and the given
// seasons' episode lists. Calling it again replaces what TMDB reports, which is
// how a test makes new episodes appear upstream.
func (s seriesReqApp) scriptSeries(seasons map[int][]tmdbtest.SeasonEpisode) {
	today := time.Now().UTC().Format("2006-01-02")
	tmdbSeasons := make([]tmdb.Season, 0, len(seasons))
	var last tmdb.LastEpisodeToAir
	var next tmdb.NextEpisodeToAir
	for n := 1; n <= len(seasons); n++ {
		tmdbSeasons = append(tmdbSeasons, tmdb.Season{SeasonNumber: n})
		// As TMDB does, point last/next-episode-to-air at the latest aired and
		// earliest upcoming episode; structure sync uses them to pick seasons.
		for _, ep := range seasons[n] {
			if ep.AirDate <= today {
				last = tmdb.LastEpisodeToAir{ID: ep.ID, SeasonNumber: n, EpisodeNumber: ep.EpisodeNumber, AirDate: ep.AirDate}
			} else if next.ID == 0 {
				next = tmdb.NextEpisodeToAir{ID: ep.ID, SeasonNumber: n, EpisodeNumber: ep.EpisodeNumber, AirDate: ep.AirDate}
			}
		}
	}
	s.tmdb.OnTVDetails(seriesReqTmdbID, tmdb.TVDetails{
		ID:               seriesReqTmdbID,
		Name:             seriesReqTitle,
		FirstAirDate:     "2024-01-05",
		Status:           "Returning Series",
		InProduction:     true,
		Seasons:          tmdbSeasons,
		LastEpisodeToAir: last,
		NextEpisodeToAir: next,
	})
	for n, eps := range seasons {
		s.tmdb.OnTVSeasonDetails(seriesReqTmdbID, n, eps...)
	}
}

// requestSeries requests the whole series at HD as the (auto-approving) admin
// over the API and returns the spawned tracking id.
func (s seriesReqApp) requestSeries(t *testing.T) uuid.UUID {
	t.Helper()
	var req model.Request
	s.app.POST(t, "/api/v1/requests", map[string]any{
		"tmdbId": seriesReqTmdbID, "type": "series", "tier": "HD",
	}, &req, http.StatusCreated)
	if req.Status != string(model.RequestSpawned) || req.SpawnedTrackingID == nil {
		t.Fatalf("series request status = %q, spawned tracking = %v; want spawned with a tracking", req.Status, req.SpawnedTrackingID)
	}
	return *req.SpawnedTrackingID
}

// episodeWants returns the tracking's wants keyed by "SxxEyy".
func (s seriesReqApp) episodeWants(t *testing.T, trackingID uuid.UUID) map[string]model.Want {
	t.Helper()
	wants, err := s.app.Repo.ListWantsByTracking(s.ctx, trackingID)
	if err != nil {
		t.Fatalf("list wants: %v", err)
	}
	out := make(map[string]model.Want, len(wants))
	for _, w := range wants {
		if w.EpisodeID == nil {
			t.Fatalf("series want %s has no episode", w.ID)
		}
		key := s.episodeKey(t, *w.EpisodeID)
		if _, dup := out[key]; dup {
			t.Fatalf("two wants for %s on one tracking", key)
		}
		out[key] = w
	}
	return out
}

func (s seriesReqApp) episodeKey(t *testing.T, episodeID uuid.UUID) string {
	t.Helper()
	ep, err := s.app.Repo.GetEpisode(s.ctx, episodeID)
	if err != nil {
		t.Fatalf("get episode: %v", err)
	}
	season, err := s.app.Repo.GetSeason(s.ctx, ep.SeasonID)
	if err != nil {
		t.Fatalf("get season: %v", err)
	}
	return fmt.Sprintf("S%02dE%02d", season.SeasonNumber, ep.EpisodeNumber)
}

func (s seriesReqApp) trackingState(t *testing.T, trackingID uuid.UUID) string {
	t.Helper()
	tr, err := s.app.Services.Tracking.Get(s.ctx, trackingID)
	if err != nil {
		t.Fatalf("get tracking: %v", err)
	}
	return tr.State
}

// recordingIndexer answers every search with what respond returns for the n-th
// (1-based) call and records each query it was asked.
type recordingIndexer struct {
	mu      sync.Mutex
	queries []indexer.SearchQuery
	respond func(n int, q indexer.SearchQuery) []indexer.SearchResult
}

func (r *recordingIndexer) source() stubIndexerSource {
	return stubIndexerSource{
		SearchFn: func(ctx context.Context, q indexer.SearchQuery) ([]indexer.SearchResult, error) {
			r.mu.Lock()
			r.queries = append(r.queries, q)
			n := len(r.queries)
			r.mu.Unlock()
			return r.respond(n, q), nil
		},
	}
}

func (r *recordingIndexer) recorded() []indexer.SearchQuery {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]indexer.SearchQuery(nil), r.queries...)
}

// seasonOnePack is a full-season S01 release of the test series.
func seasonOnePack(guid string) indexer.SearchResult {
	return packResult(guid, seriesReqTitle+" S01 COMPLETE 1080p BluRay x264", 3)
}

// writeEpisodePayloads writes one distinct-content video file per S01 episode
// number into a fresh download dir, returning the downloader's view of them and
// each episode's content keyed by "SxxEyy".
func writeEpisodePayloads(t *testing.T, episodes ...int) ([]downloader.File, map[string][]byte) {
	t.Helper()
	dir := t.TempDir()
	files := make([]downloader.File, 0, len(episodes))
	content := make(map[string][]byte, len(episodes))
	for _, ep := range episodes {
		key := fmt.Sprintf("S01E%02d", ep)
		body := []byte("bytes of " + key)
		path := filepath.Join(dir, fmt.Sprintf("%s %s 1080p BluRay x264.mkv", seriesReqTitle, key))
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatalf("write payload %s: %v", key, err)
		}
		files = append(files, downloader.File{Path: path, Size: int64(len(body))})
		content[key] = body
	}
	return files, content
}

// runSeriesPipeline starts the acquisition, download and import workers over the
// given indexer and downloader client, returning the func that stops them all.
func (s seriesReqApp) runSeriesPipeline(t *testing.T, idx *recordingIndexer, client *fakeDownloaderClient) func() {
	t.Helper()
	reg := downloader.NewRegistry()
	reg.Register(downloader.TypeQbittorrent, func(downloader.ConfigRecord) (downloader.Client, error) {
		return client, nil
	})
	dm := downloader.NewManager(reg, s.app.Repo, logger.New(false))
	if err := dm.Initialize(s.ctx); err != nil {
		t.Fatalf("initialize downloader manager: %v", err)
	}
	stops := []func(){
		runWorker(t, s.ctx, newAcquisitionWorker(s.app.Repo, idx.source())),
		runWorker(t, s.ctx, newDownloadWorkerWithClient(t, s.ctx, s.app.Repo, client)),
		runWorker(t, s.ctx, importw.New(s.app.Repo, dm, logger.New(false), nil, nil, nil)),
	}
	return func() {
		for _, stop := range stops {
			stop()
		}
	}
}

func dateOffset(days int) string {
	return time.Now().UTC().AddDate(0, 0, days).Format("2006-01-02")
}

// REQ-SERIES-001 — A series request creates durable intent that keeps producing
// work as new episodes air, not a one-off delivery.
//
// A series is requested when only S01E01 exists upstream. That episode is
// delivered (file on disk, want available), so everything the request originally
// covered is done. TMDB then reports a newly-aired S01E02 and a new season whose
// premiere is still to come. A routine metadata refresh — no new request, no
// operator action — must turn both into fresh pending wants on the same, still
// active tracking: the aired one due now, the upcoming one scheduled for its air
// date. The delivered episode gets no second want.
func TestReqSeries001_RequestKeepsProducingWorkAsEpisodesAir(t *testing.T) {
	t.Parallel()
	s := newSeriesReqApp(t)
	ctx := s.ctx

	s.scriptSeries(map[int][]tmdbtest.SeasonEpisode{
		1: {{ID: 5001, EpisodeNumber: 1, Name: "Pilot", AirDate: "2024-01-05"}},
	})
	trackingID := s.requestSeries(t)

	initial := s.episodeWants(t, trackingID)
	if len(initial) != 1 {
		t.Fatalf("wants after request = %v, want just S01E01", initial)
	}
	pilot := initial["S01E01"]

	// Deliver the pilot: its file lands in the library and its want is satisfied.
	if _, err := s.app.Repo.CreateFile(ctx, repo.CreateFileParams{
		ID:          uuid.New(),
		LibraryID:   s.library.ID,
		Path:        "Harbor Lights/Season 01/Harbor Lights S01E01.mkv",
		MediaItemID: &pilot.MediaItemID,
		EpisodeID:   pilot.EpisodeID,
	}); err != nil {
		t.Fatalf("create pilot file: %v", err)
	}
	if _, _, err := s.app.Repo.MirrorWantStatus(ctx, pilot.ID, string(model.WantAvailable)); err != nil {
		t.Fatalf("mark pilot available: %v", err)
	}

	// Upstream moves on: S01E02 aired yesterday, S02 premieres next week.
	premiere := dateOffset(7)
	s.scriptSeries(map[int][]tmdbtest.SeasonEpisode{
		1: {
			{ID: 5001, EpisodeNumber: 1, Name: "Pilot", AirDate: "2024-01-05"},
			{ID: 5002, EpisodeNumber: 2, Name: "Second", AirDate: dateOffset(-1)},
		},
		2: {{ID: 5101, EpisodeNumber: 1, Name: "Premiere", AirDate: premiere}},
	})
	item, err := s.app.Repo.GetMediaItemByTmdbID(ctx, seriesReqTmdbID)
	if err != nil {
		t.Fatalf("get series: %v", err)
	}
	if err := s.app.Services.Enrichment.EnrichMediaItem(ctx, item); err != nil {
		t.Fatalf("refresh series: %v", err)
	}

	if state := s.trackingState(t, trackingID); state != string(model.TrackingActive) {
		t.Errorf("tracking state = %q, want active", state)
	}
	now := time.Now()
	got := s.episodeWants(t, trackingID)
	if len(got) != 3 {
		t.Fatalf("wants after refresh = %d (%v), want 3 (S01E01, S01E02, S02E01)", len(got), keys(got))
	}
	if got["S01E01"].ID != pilot.ID || got["S01E01"].Status != string(model.WantAvailable) {
		t.Errorf("S01E01 want = %s/%s, want the original want, still available", got["S01E01"].ID, got["S01E01"].Status)
	}

	aired, ok := got["S01E02"]
	if !ok {
		t.Fatalf("no want for newly-aired S01E02")
	}
	if aired.Status != string(model.WantPending) || aired.Hold != nil || aired.NextRunAt.After(now) {
		t.Errorf("S01E02 want = status %q hold %v next_run_at %v, want pending, unheld, due now", aired.Status, aired.Hold, aired.NextRunAt)
	}

	upcoming, ok := got["S02E01"]
	if !ok {
		t.Fatalf("no want for announced S02E01")
	}
	if upcoming.Status != string(model.WantPending) || upcoming.Hold != nil {
		t.Errorf("S02E01 want = status %q hold %v, want pending, unheld", upcoming.Status, upcoming.Hold)
	}
	if got := upcoming.NextRunAt.UTC().Format("2006-01-02"); got != premiere {
		t.Errorf("S02E01 next_run_at = %s, want its air date %s", got, premiere)
	}
}

// REQ-SERIES-005 — One acquisition can satisfy many wants, and each file it
// produces is matched to exactly one of them.
//
// Three aired episodes are requested; the indexer offers only a season pack. The
// full pipeline runs (acquisition, download, import). Exactly one download must
// carry all three wants, every want must end available, and the library must
// hold exactly one file per episode — each attached to its own episode and
// holding that episode's bytes, so no want is satisfied by another's file.
func TestReqSeries005_OnePackSatisfiesManyWantsOneFileEach(t *testing.T) {
	t.Parallel()
	s := newSeriesReqApp(t)
	ctx := s.ctx

	s.scriptSeries(map[int][]tmdbtest.SeasonEpisode{
		1: {
			{ID: 7001, EpisodeNumber: 1, Name: "One", AirDate: "2024-01-05"},
			{ID: 7002, EpisodeNumber: 2, Name: "Two", AirDate: "2024-01-12"},
			{ID: 7003, EpisodeNumber: 3, Name: "Three", AirDate: "2024-01-19"},
		},
	})
	trackingID := s.requestSeries(t)
	wants := s.episodeWants(t, trackingID)
	if len(wants) != 3 {
		t.Fatalf("wants after request = %v, want S01E01..S01E03", keys(wants))
	}

	files, content := writeEpisodePayloads(t, 1, 2, 3)
	idx := &recordingIndexer{respond: func(int, indexer.SearchQuery) []indexer.SearchResult {
		return []indexer.SearchResult{seasonOnePack("guid-s01-pack")}
	}}
	defer s.runSeriesPipeline(t, idx, &fakeDownloaderClient{files: files})()

	for key, w := range wants {
		got := waitForWantStatus(t, ctx, s.app.Repo, w.ID, model.WantAvailable, 30*time.Second)
		if got.EpisodeID == nil || *got.EpisodeID != *w.EpisodeID {
			t.Errorf("%s want episode changed to %v", key, got.EpisodeID)
		}
	}

	item, err := s.app.Repo.GetMediaItemByTmdbID(ctx, seriesReqTmdbID)
	if err != nil {
		t.Fatalf("get series: %v", err)
	}
	jobs, err := s.app.Repo.ListDownloadJobsByMediaItem(ctx, item.ID)
	if err != nil {
		t.Fatalf("list download jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("download jobs = %d, want 1 (one pack for all three wants)", len(jobs))
	}
	linked, err := s.app.Repo.ListWantsByDownloadJob(ctx, jobs[0].ID)
	if err != nil {
		t.Fatalf("list wants for the pack: %v", err)
	}
	if len(linked) != 3 {
		t.Errorf("wants linked to the pack = %d, want 3", len(linked))
	}

	libFiles, err := s.app.Repo.ListFilesForItem(ctx, item.ID)
	if err != nil {
		t.Fatalf("list library files: %v", err)
	}
	if len(libFiles) != 3 {
		t.Fatalf("library files = %d, want 3 (one per episode)", len(libFiles))
	}
	seen := make(map[string]bool, len(libFiles))
	for _, f := range libFiles {
		if f.EpisodeID == nil {
			t.Errorf("library file %s is not attached to an episode", f.Path)
			continue
		}
		key := s.episodeKey(t, *f.EpisodeID)
		if seen[key] {
			t.Errorf("%s has more than one file", key)
		}
		seen[key] = true
		body, err := os.ReadFile(filepath.Join(s.library.RootPath, f.Path))
		if err != nil {
			t.Errorf("read %s library file %s: %v", key, f.Path, err)
			continue
		}
		if !bytes.Equal(body, content[key]) {
			t.Errorf("%s library file holds %q, want %q (another episode's file)", key, body, content[key])
		}
	}
}

// REQ-SERIES-006 — A pack that doesn't cover every want it was grabbed for
// returns the uncovered wants to the queue instead of stranding them.
//
// A full S01 pack is grabbed for three aired episodes but arrives carrying only
// E01 and E02. Those two must become available; E03 must be detached from the
// pack and searched for again by the acquisition worker — not left grabbed or
// downloading on a download that will never deliver it.
func TestReqSeries006_UncoveredPackWantReturnsToQueue(t *testing.T) {
	t.Parallel()
	s := newSeriesReqApp(t)
	ctx := s.ctx

	s.scriptSeries(map[int][]tmdbtest.SeasonEpisode{
		1: {
			{ID: 8001, EpisodeNumber: 1, Name: "One", AirDate: "2024-01-05"},
			{ID: 8002, EpisodeNumber: 2, Name: "Two", AirDate: "2024-01-12"},
			{ID: 8003, EpisodeNumber: 3, Name: "Three", AirDate: "2024-01-19"},
		},
	})
	trackingID := s.requestSeries(t)
	wants := s.episodeWants(t, trackingID)
	if len(wants) != 3 {
		t.Fatalf("wants after request = %v, want S01E01..S01E03", keys(wants))
	}

	// The pack is offered once; every later search finds nothing, so a re-search
	// leaves the want pending instead of re-grabbing the same pack.
	files, _ := writeEpisodePayloads(t, 1, 2)
	idx := &recordingIndexer{respond: func(n int, _ indexer.SearchQuery) []indexer.SearchResult {
		if n == 1 {
			return []indexer.SearchResult{seasonOnePack("guid-s01-pack")}
		}
		return nil
	}}
	defer s.runSeriesPipeline(t, idx, &fakeDownloaderClient{files: files})()

	waitForWantStatus(t, ctx, s.app.Repo, wants["S01E01"].ID, model.WantAvailable, 30*time.Second)
	waitForWantStatus(t, ctx, s.app.Repo, wants["S01E02"].ID, model.WantAvailable, 30*time.Second)

	// E03 is searched for again once the pack proved not to carry it.
	deadline := time.Now().Add(15 * time.Second)
	for {
		var researched bool
		for _, q := range idx.recorded()[1:] {
			if q.Episode != nil && *q.Episode == 3 {
				researched = true
			}
		}
		if researched {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("S01E03 was not searched again after the pack missed it (searches: %+v)", idx.recorded())
		}
		time.Sleep(10 * time.Millisecond)
	}

	e3, err := s.app.Repo.GetWant(ctx, wants["S01E03"].ID)
	if err != nil {
		t.Fatalf("get S01E03 want: %v", err)
	}
	switch model.WantStatus(e3.Status) {
	case model.WantPending, model.WantSearching:
	default:
		t.Errorf("S01E03 want status = %q, want back in the queue (pending/searching)", e3.Status)
	}
	jobs, err := s.app.Repo.ListActiveDownloadJobsByWant(ctx, e3.ID)
	if err != nil {
		t.Fatalf("list S01E03 download jobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("S01E03 still linked to %d download job(s), want detached from the pack", len(jobs))
	}
}

// REQ-SERIES-003 — Work for an unaired episode must not be attempted before it airs.
//
// A series is requested with S01E01 and S01E02 aired and S01E03 airing in three
// days. The real acquisition worker runs against an indexer offering a full
// S01 pack. The aired episodes are searched and grabbed; the unaired episode's
// want must be left exactly as reconcile made it — pending, never searched for,
// never grabbed or linked to a download (not even as a sibling of the pack), and
// still scheduled for its air date.
func TestReqSeries003_UnairedEpisodeIsNotWorkedBeforeAirDate(t *testing.T) {
	t.Parallel()
	s := newSeriesReqApp(t)
	ctx := s.ctx

	airs := dateOffset(3)
	s.scriptSeries(map[int][]tmdbtest.SeasonEpisode{
		1: {
			{ID: 6001, EpisodeNumber: 1, Name: "One", AirDate: dateOffset(-14)},
			{ID: 6002, EpisodeNumber: 2, Name: "Two", AirDate: dateOffset(-7)},
			{ID: 6003, EpisodeNumber: 3, Name: "Three", AirDate: airs},
		},
	})
	trackingID := s.requestSeries(t)
	before := s.episodeWants(t, trackingID)
	if len(before) != 3 {
		t.Fatalf("wants after request = %v, want S01E01..S01E03", keys(before))
	}
	unaired := before["S01E03"]

	idx := &recordingIndexer{respond: func(int, indexer.SearchQuery) []indexer.SearchResult {
		return []indexer.SearchResult{seasonOnePack("guid-s01-pack")}
	}}
	stop := s.runSeriesPipeline(t, idx, &fakeDownloaderClient{status: downloader.StatusDownloading})
	defer stop()

	waitForWantStatus(t, ctx, s.app.Repo, before["S01E01"].ID, model.WantGrabbed, 15*time.Second)
	waitForWantStatus(t, ctx, s.app.Repo, before["S01E02"].ID, model.WantGrabbed, 15*time.Second)
	// Let the worker run more cycles: a premature claim would happen here.
	time.Sleep(300 * time.Millisecond)
	stop()

	after, err := s.app.Repo.GetWant(ctx, unaired.ID)
	if err != nil {
		t.Fatalf("get S01E03 want: %v", err)
	}
	if after.Status != string(model.WantPending) {
		t.Errorf("S01E03 want status = %q, want pending (it hasn't aired)", after.Status)
	}
	if !after.NextRunAt.Equal(unaired.NextRunAt) {
		t.Errorf("S01E03 next_run_at = %v, want unchanged %v (its air date)", after.NextRunAt, unaired.NextRunAt)
	}
	if after.AttemptCount != 0 {
		t.Errorf("S01E03 attempt_count = %d, want 0", after.AttemptCount)
	}
	jobs, err := s.app.Repo.ListActiveDownloadJobsByWant(ctx, unaired.ID)
	if err != nil {
		t.Fatalf("list S01E03 download jobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("S01E03 is linked to %d download job(s), want none before it airs", len(jobs))
	}
	for _, q := range idx.recorded() {
		if q.Episode != nil && *q.Episode == 3 {
			t.Errorf("indexer was searched for S01E03 before it aired: %+v", q)
		}
	}
}

// REQ-SERIES-003 — Work for an unaired episode must not be attempted before it airs.
//
// Unaired episodes don't count toward the two-or-more-siblings threshold that
// widens a want's search to the whole season. With S01E01 aired and S01E02–E03
// airing later, E01 has no aired sibling: its search stays scoped to E01 alone,
// and the unaired wants are left pending, unsearched and unlinked.
func TestReqSeries003_UnairedEpisodesDoNotCountAsPackSiblings(t *testing.T) {
	t.Parallel()
	s := newSeriesReqApp(t)
	ctx := s.ctx

	s.scriptSeries(map[int][]tmdbtest.SeasonEpisode{
		1: {
			{ID: 6101, EpisodeNumber: 1, Name: "One", AirDate: dateOffset(-7)},
			{ID: 6102, EpisodeNumber: 2, Name: "Two", AirDate: dateOffset(3)},
			{ID: 6103, EpisodeNumber: 3, Name: "Three", AirDate: dateOffset(10)},
		},
	})
	trackingID := s.requestSeries(t)
	before := s.episodeWants(t, trackingID)
	if len(before) != 3 {
		t.Fatalf("wants after request = %v, want S01E01..S01E03", keys(before))
	}

	idx := &recordingIndexer{respond: func(int, indexer.SearchQuery) []indexer.SearchResult {
		return []indexer.SearchResult{seasonOnePack("guid-s01-pack")}
	}}
	stop := s.runSeriesPipeline(t, idx, &fakeDownloaderClient{status: downloader.StatusDownloading})
	defer stop()

	deadline := time.Now().Add(15 * time.Second)
	for len(idx.recorded()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("S01E01 was never searched for")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Let the worker run more cycles: a premature claim would happen here.
	time.Sleep(300 * time.Millisecond)
	stop()

	for _, q := range idx.recorded() {
		if q.Episode == nil {
			t.Errorf("search widened to the whole season with only one aired episode: %+v", q)
		} else if *q.Episode != 1 {
			t.Errorf("indexer was searched for S01E%02d before it aired: %+v", *q.Episode, q)
		}
	}
	for _, key := range []string{"S01E02", "S01E03"} {
		after, err := s.app.Repo.GetWant(ctx, before[key].ID)
		if err != nil {
			t.Fatalf("get %s want: %v", key, err)
		}
		if after.Status != string(model.WantPending) {
			t.Errorf("%s want status = %q, want pending (it hasn't aired)", key, after.Status)
		}
		if !after.NextRunAt.Equal(before[key].NextRunAt) {
			t.Errorf("%s next_run_at = %v, want unchanged %v (its air date)", key, after.NextRunAt, before[key].NextRunAt)
		}
		jobs, err := s.app.Repo.ListActiveDownloadJobsByWant(ctx, before[key].ID)
		if err != nil {
			t.Fatalf("list %s download jobs: %v", key, err)
		}
		if len(jobs) != 0 {
			t.Errorf("%s is linked to %d download job(s), want none before it airs", key, len(jobs))
		}
	}
}

func keys(m map[string]model.Want) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
