//go:build integration

package integration

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tmdb "github.com/cyruzin/golang-tmdb"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/indexer"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/routing"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
	"github.com/kyleaupton/arrflix/internal/test/tmdbtest"
)

const uhdHash = "fedcba9876543210fedcba9876543210fedcba98"

// uhdRelease is a 2160p magnet release the seeded 4K movie profile accepts.
func uhdRelease() indexer.SearchResult {
	r := movieResult("guid-uhd")
	r.Title = "The Matrix 1999 2160p UHD BluRay x265-GRP"
	r.DownloadURL = "magnet:?xt=urn:btih:" + uhdHash + "&dn=The.Matrix.1999.2160p.UHD.BluRay.x265-GRP"
	return r
}

// mkdir creates dir (and parents) and returns it.
func mkdir(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	return dir
}

// createSaveToRule creates a rule routing 2160p releases to saveTo.
func createSaveToRule(t *testing.T, app *testapp.App, saveTo string) model.RoutingRule {
	t.Helper()
	body := makeRoutingRuleBody("4K save location", resolutionCondition("2160p"))
	body["saveTo"] = saveTo
	var rule model.RoutingRule
	app.POST(t, "/api/v1/routing/rules", body, &rule, http.StatusCreated)
	return rule
}

// evaluateTitle dry-runs routing for a movie release title.
func evaluateTitle(t *testing.T, app *testapp.App, title string) routing.Evaluation {
	t.Helper()
	var eval routing.Evaluation
	app.POST(t, "/api/v1/routing/evaluate", map[string]any{
		"mediaType": "movie",
		"candidate": map[string]any{
			"title": title, "filename": "", "link": "", "guid": "g", "indexer": "i",
			"indexerId": 1, "indexerFlags": []string{}, "categories": []string{"Movies"},
			"protocol": "torrent", "publishDate": "2024-01-01T00:00:00Z",
			"size": 1, "seeders": 1, "peers": 0, "age": 0, "ageHours": 0, "grabs": 0,
		},
	}, &eval, http.StatusOK)
	return eval
}

// REQ-PATH-002 — When routing sets a save location for a grab, the grab tells
// the downloader where to save, in the downloader's own frame.
//
// A rule matching 2160p sets saveTo=<root>/torrents/4k. With a volume whose
// Arrflix base is <root>/torrents and whose alias for the downloader is
// /downloads, the download job records and the downloader receives
// /downloads/4k. With no volume the Arrflix path is sent unchanged. With no
// rule and an unset default, no save path is sent at all.
func TestReqPath002_GrabSendsRoutedSavePathInDownloaderFrame(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		rule   bool
		volume bool
		want   func(root string) string
	}{
		{"volume alias translates the path", true, true, func(string) string { return "/downloads/4k" }},
		{"no volume sends the path unchanged", true, false, func(root string) string { return filepath.Join(root, "torrents", "4k") }},
		{"no rule and unset default sends nothing", false, false, func(string) string { return "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			app, ctx, qb := qbitApp(t)
			dl, err := app.Repo.GetDefaultDownloader(ctx, "torrent")
			if err != nil {
				t.Fatalf("default downloader: %v", err)
			}

			root := t.TempDir()
			saveTo := mkdir(t, filepath.Join(root, "torrents", "4k"))
			if tc.rule {
				createSaveToRule(t, app, saveTo)
			}
			if tc.volume {
				app.POST(t, "/api/v1/volumes", volumeBody("torrents", filepath.Join(root, "torrents"),
					aliasBody(dl.ID, "/downloads")), nil, http.StatusCreated)
			}

			req := requestMovieAt(t, app, app.Token, "4K", http.StatusCreated)
			job := grabTrackedMovieRelease(t, ctx, app, *req.SpawnedTrackingID, uhdRelease())

			want := tc.want(root)
			got := ""
			if job.SavePath != nil {
				got = *job.SavePath
			}
			if want == "" && job.SavePath != nil {
				t.Errorf("job SavePath = %q, want none", got)
			} else if got != want {
				t.Errorf("job SavePath = %q, want %q", got, want)
			}

			handOffThroughWorker(t, ctx, app.Repo, job.ID)
			tor, ok := qb.Torrent(uhdHash)
			if !ok {
				t.Fatalf("torrent %s not added to the downloader", uhdHash)
			}
			if tor.SavePath != want {
				t.Errorf("downloader savepath = %q, want %q", tor.SavePath, want)
			}
		})
	}
}

// TestRouting_SaveToDefault asserts the save-to default fills the slot when no
// rule sets it, a rule overrides it, dry-run reports which one applied plus
// the downloader-frame path, and clearing the default unsets the slot.
func TestRouting_SaveToDefault(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	dl, _, _ := seedDefaults(t, app)

	root := t.TempDir()
	defaultDir := mkdir(t, filepath.Join(root, "torrents", "default"))
	ruleDir := mkdir(t, filepath.Join(root, "torrents", "4k"))

	// Out of the box the default is unset and nothing is sent.
	var defaults map[string]any
	app.GET(t, "/api/v1/routing/defaults", &defaults, http.StatusOK)
	if _, ok := defaults["saveTo"]; ok {
		t.Errorf("fresh defaults = %v, want no saveTo", defaults)
	}
	eval := evaluateTitle(t, app, "Dune.2021.1080p.BluRay.x264-GRP")
	if eval.Final.SaveTo != nil || !eval.FellBack.SaveTo {
		t.Errorf("unset default: Final.SaveTo = %v, FellBack.SaveTo = %v; want nil, true", eval.Final.SaveTo, eval.FellBack.SaveTo)
	}

	app.PUT(t, "/api/v1/routing/defaults", map[string]any{"saveTo": defaultDir + "/"}, &defaults, http.StatusOK)
	if defaults["saveTo"] != defaultDir {
		t.Errorf("saved default = %v, want cleaned %q", defaults["saveTo"], defaultDir)
	}
	app.GET(t, "/api/v1/routing/defaults", &defaults, http.StatusOK)
	if defaults["saveTo"] != defaultDir {
		t.Errorf("read-back default = %v, want %q", defaults["saveTo"], defaultDir)
	}

	createSaveToRule(t, app, ruleDir)

	// No rule matches 1080p: the default fills the slot.
	eval = evaluateTitle(t, app, "Dune.2021.1080p.BluRay.x264-GRP")
	if eval.Final.SaveTo == nil || *eval.Final.SaveTo != defaultDir || !eval.FellBack.SaveTo {
		t.Errorf("1080p: Final.SaveTo = %v, FellBack.SaveTo = %v; want %q from the default", eval.Final.SaveTo, eval.FellBack.SaveTo, defaultDir)
	}
	if eval.DownloaderSavePath != "" {
		t.Errorf("no volume: DownloaderSavePath = %q, want empty", eval.DownloaderSavePath)
	}

	// The 2160p rule overrides the default; a volume shows the translated path.
	app.POST(t, "/api/v1/volumes", volumeBody("torrents", filepath.Join(root, "torrents"),
		aliasBody(dl.ID, "/downloads")), nil, http.StatusCreated)
	eval = evaluateTitle(t, app, "Dune.2021.2160p.BluRay.REMUX-FraMeSToR")
	if eval.Final.SaveTo == nil || *eval.Final.SaveTo != ruleDir || eval.FellBack.SaveTo {
		t.Errorf("2160p: Final.SaveTo = %v, FellBack.SaveTo = %v; want %q from the rule", eval.Final.SaveTo, eval.FellBack.SaveTo, ruleDir)
	}
	if eval.DownloaderSavePath != "/downloads/4k" {
		t.Errorf("DownloaderSavePath = %q, want /downloads/4k", eval.DownloaderSavePath)
	}

	// Clearing the default unsets it again.
	app.PUT(t, "/api/v1/routing/defaults", map[string]any{"saveTo": ""}, &defaults, http.StatusOK)
	eval = evaluateTitle(t, app, "Dune.2021.1080p.BluRay.x264-GRP")
	if eval.Final.SaveTo != nil {
		t.Errorf("cleared default: Final.SaveTo = %q, want nil", *eval.Final.SaveTo)
	}
}

// TestRouting_SaveToValidation asserts a relative or missing saveTo is a 422
// on that field, for a rule (create and update) and for the default.
func TestRouting_SaveToValidation(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	missing := filepath.Join(t.TempDir(), "nope")
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{"data/torrents", missing, file} {
		body := makeRoutingRuleBody("bad save", resolutionCondition("2160p"))
		body["saveTo"] = bad
		var pd apperrors.ProblemDetails
		app.POST(t, "/api/v1/routing/rules", body, &pd, http.StatusUnprocessableEntity)
		findFieldError(t, pd, "body.saveTo")

		pd = apperrors.ProblemDetails{}
		app.PUT(t, "/api/v1/routing/defaults", map[string]any{"saveTo": bad}, &pd, http.StatusUnprocessableEntity)
		findFieldError(t, pd, "body.saveTo")
	}

	rule := createSaveToRule(t, app, t.TempDir())
	body := makeRoutingRuleBody(rule.Name, resolutionCondition("2160p"))
	body["saveTo"] = "relative"
	var pd apperrors.ProblemDetails
	app.PUT(t, "/api/v1/routing/rules/"+rule.ID.String(), body, &pd, http.StatusUnprocessableEntity)
	findFieldError(t, pd, "body.saveTo")
}

// TestRouting_SaveToDeletedFailsGrab asserts a save location removed after the
// rule was saved fails the grab with a retryable error naming the path, rather
// than grabbing without a save path.
func TestRouting_SaveToDeletedFailsGrab(t *testing.T) {
	t.Parallel()
	app, ctx, _ := qbitApp(t)

	saveTo := mkdir(t, filepath.Join(t.TempDir(), "4k"))
	createSaveToRule(t, app, saveTo)
	if err := os.Remove(saveTo); err != nil {
		t.Fatal(err)
	}

	req := requestMovieAt(t, app, app.Token, "4K", http.StatusCreated)
	wants, err := app.Repo.ListWantsByTracking(ctx, *req.SpawnedTrackingID)
	if err != nil || len(wants) != 1 {
		t.Fatalf("wants = %v, %v; want one", wants, err)
	}
	claimed := claimWant(t, ctx, app.Repo, wants[0].ID)

	r := app.Repo
	source := stubIndexerSource{
		SearchFn: func(context.Context, indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return []indexer.SearchResult{uhdRelease()}, nil
		},
	}
	quality := service.NewQualityProfileService(r)
	svc := service.NewAcquisitionService(r, logger.New(true), source, service.NewRoutingService(r), quality,
		service.NewProposalService(r, quality, nil, nil, logger.New(true)))

	_, outcome, err := svc.ProcessWant(ctx, claimed)
	if err == nil {
		t.Fatalf("ProcessWant succeeded (outcome %v), want a grab failure", outcome)
	}
	if !strings.Contains(err.Error(), saveTo) {
		t.Errorf("error %q does not name the save location %q", err, saveTo)
	}
	if !apperrors.IsRetryable(err) {
		t.Errorf("error %q is not retryable; the want must stay retryable", err)
	}

	jobs, err := r.ListActiveDownloadJobsByWant(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("active download jobs = %d, want none", len(jobs))
	}
}

// TestRouting_SaveToDeletedFailsManualGrabVisibly asserts a manual grab whose
// routed save location was removed after saving fails with a wire-visible
// Conflict naming the path, not an opaque 500.
func TestRouting_SaveToDeletedFailsManualGrabVisibly(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	tmdbSrv, tmdbClient := tmdbtest.New(t)
	tmdbSrv.OnMovieDetails(603, tmdb.MovieDetails{ID: 603, Title: "The Matrix", ReleaseDate: "1999-03-31"})
	seedRoutingDefaults(t, ctx, r)

	routingSvc := service.NewRoutingService(r)
	saveTo := mkdir(t, filepath.Join(t.TempDir(), "movies"))
	if _, err := routingSvc.SetDefaults(ctx, service.RoutingDefaults{SaveTo: &saveTo}); err != nil {
		t.Fatalf("set routing defaults: %v", err)
	}
	if err := os.Remove(saveTo); err != nil {
		t.Fatal(err)
	}

	log := logger.New(true)
	tmdbSvc := service.NewTmdbServiceWithClient(r, log, tmdbClient)
	media := service.NewMediaService(r, log, tmdbSvc, service.NewSettingsService(r))
	source := stubIndexerSource{
		SearchFn: func(context.Context, indexer.SearchQuery) ([]indexer.SearchResult, error) {
			return []indexer.SearchResult{cannedResult()}, nil
		},
	}
	svc := service.NewDownloadCandidatesService(r, log, source, media, routingSvc)
	if _, err := svc.SearchDownloadCandidates(ctx, 603); err != nil {
		t.Fatalf("SearchDownloadCandidates: %v", err)
	}

	canned := cannedResult()
	_, _, err := svc.EnqueueCandidate(ctx, 603, canned.IndexerID, canned.GUID)
	if err == nil {
		t.Fatal("EnqueueCandidate succeeded, want a grab failure")
	}
	if !apperrors.IsConflict(err) {
		t.Errorf("error kind = %v, want Conflict", apperrors.KindOf(err))
	}
	if pd := apperrors.ToProblem(err); !strings.Contains(pd.Detail, saveTo) {
		t.Errorf("wire detail %q does not name the save location %q", pd.Detail, saveTo)
	}
}
