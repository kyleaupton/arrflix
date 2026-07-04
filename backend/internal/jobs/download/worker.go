// Package download implements the download worker that polls downloaders
// and manages download job state transitions.
package download

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/downloader"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/importer"
	"github.com/kyleaupton/arrflix/internal/jobs/jobutil"
	"github.com/kyleaupton/arrflix/internal/jobs/state"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/realtime"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/sse"
)

// Worker polls download clients and manages download job lifecycle.
type Worker struct {
	repo   *repo.Repository
	dlm    *downloader.Manager
	log    *logger.Logger
	broker *sse.Broker
	sm     *state.DownloadJobMachine

	pollInterval time.Duration
	claimLimit   int32
	maxAttempts  int
}

// Config holds worker configuration.
type Config struct {
	PollInterval time.Duration
	ClaimLimit   int32
	MaxAttempts  int
}

// DefaultConfig returns default worker configuration.
func DefaultConfig() Config {
	return Config{
		PollInterval: 3 * time.Second,
		ClaimLimit:   20,
		MaxAttempts:  3,
	}
}

// New creates a new download worker with the default configuration.
func New(r *repo.Repository, dlm *downloader.Manager, log *logger.Logger, broker *sse.Broker) *Worker {
	return NewWithConfig(r, dlm, log, broker, DefaultConfig())
}

// NewWithConfig creates a new download worker with explicit configuration — the
// seam tests use to drive the loop on a fast cadence.
func NewWithConfig(r *repo.Repository, dlm *downloader.Manager, log *logger.Logger, broker *sse.Broker, cfg Config) *Worker {
	return &Worker{
		repo:         r,
		dlm:          dlm,
		log:          log,
		broker:       broker,
		sm:           state.NewDownloadJobMachine(),
		pollInterval: cfg.PollInterval,
		claimLimit:   cfg.ClaimLimit,
		maxAttempts:  cfg.MaxAttempts,
	}
}

// Run starts the worker loop.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	w.log.Info().Msg("download worker started")

	for {
		select {
		case <-ctx.Done():
			w.log.Info().Msg("download worker stopped")
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	jobs, err := w.repo.ClaimRunnableDownloadJobs(ctx, w.claimLimit)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to claim download jobs")
		return
	}

	for _, job := range jobs {
		if err := w.processJob(ctx, job); err != nil {
			w.handleError(ctx, job, err)
		}
	}
}

func (w *Worker) processJob(ctx context.Context, job model.DownloadJob) error {
	// Skip terminal states
	if w.sm.IsTerminalStr(job.Status) {
		return nil
	}

	client, err := w.dlm.GetClientByID(ctx, job.DownloaderID.String())
	if err != nil {
		return apperrors.Internalf("get downloader client: %v", err).
			Op("DownloadWorker.processJob").
			NotRetryable()
	}

	switch job.Status {
	case "created":
		return w.enqueueDownload(ctx, client, job)
	case "enqueued", "downloading":
		return w.pollDownload(ctx, client, job)
	default:
		return nil
	}
}

func (w *Worker) enqueueDownload(ctx context.Context, client downloader.Client, job model.DownloadJob) error {
	addReq := downloader.AddRequest{}
	switch job.Protocol {
	case "torrent":
		addReq.MagnetURL = job.CandidateLink
	case "usenet":
		addReq.NZBURL = job.CandidateLink
	default:
		return apperrors.Internalf("unknown protocol: %s", job.Protocol).
			Op("DownloadWorker.enqueueDownload").
			NotRetryable()
	}

	w.log.Info().
		Str("job_id", job.ID.String()).
		Str("protocol", job.Protocol).
		Str("link", job.CandidateLink).
		Msg("adding download to client")

	res, err := client.Add(ctx, addReq)
	if err != nil {
		w.log.Error().Err(err).Str("job_id", job.ID.String()).Msg("[DEBUG] Add() failed")
		return apperrors.BadGatewayf("downloader add: %v", err).
			Op("DownloadWorker.enqueueDownload")
	}

	w.log.Info().
		Str("job_id", job.ID.String()).
		Str("external_id", res.ExternalID).
		Str("name", res.Name).
		Msg("[DEBUG] Add() succeeded")

	updated, err := w.repo.SetDownloadJobEnqueued(ctx, job.ID, res.ExternalID)
	if err != nil {
		return err
	}

	w.logEvent(ctx, job.ID, "status_changed", "", map[string]any{
		"old_status": job.Status,
		"new_status": "enqueued",
	})

	w.publishJobUpdated(ctx, updated.ID)
	return nil
}

func (w *Worker) pollDownload(ctx context.Context, client downloader.Client, job model.DownloadJob) error {
	if job.DownloaderExternalID == nil || *job.DownloaderExternalID == "" {
		return apperrors.Internalf("job missing downloader_external_id").
			Op("DownloadWorker.pollDownload").
			NotRetryable()
	}

	externalID := *job.DownloaderExternalID
	item, err := client.Get(ctx, externalID)
	if err != nil {
		return apperrors.BadGatewayf("downloader get: %v", err).
			Op("DownloadWorker.pollDownload")
	}

	newStatus := mapItemStatus(item.Status)

	// Validate state transition
	if job.Status != newStatus && !w.sm.CanTransitionStr(job.Status, newStatus) {
		w.log.Warn().
			Str("job_id", job.ID.String()).
			Str("from", job.Status).
			Str("to", newStatus).
			Msg("invalid state transition, ignoring")
		return nil
	}

	// Update snapshot
	updated, err := w.repo.SetDownloadJobDownloadSnapshot(ctx, repo.SetDownloadJobDownloadSnapshotParams{
		ID:               job.ID,
		Status:           newStatus,
		DownloaderStatus: jobutil.Ptr(string(item.Status)),
		Progress:         jobutil.Ptr(item.Progress),
		SavePath:         jobutil.Ptr(item.SavePath),
		ContentPath:      jobutil.Ptr(item.ContentPath),
		DownloadSpeed:    jobutil.Ptr(item.DownloadSpeed),
		EtaSeconds:       jobutil.Ptr(item.ETA),
		TotalSize:        jobutil.Ptr(item.TotalSize),
		// Rotate to the back of the claim queue so polling this in-flight
		// download doesn't pin a claim slot and starve newly-created jobs.
		NextRunAt: time.Now().Add(w.pollInterval),
	})
	if err != nil {
		return err
	}

	// Mirror the wants to 'downloading' on the transition edge only, so they
	// aren't re-set on every poll. A job that races straight to completed without
	// an observed 'downloading' skips it — the import worker advances the want
	// from there.
	if job.Status != newStatus && newStatus == "downloading" {
		w.mirrorWants(ctx, job.ID, model.WantDownloading)
	}

	if job.Status != newStatus {
		w.logEvent(ctx, job.ID, "status_changed", "", map[string]any{
			"old_status": job.Status,
			"new_status": newStatus,
			"progress":   item.Progress,
		})
	}

	w.publishJobUpdated(ctx, updated.ID)

	// Handle terminal downloader error
	if newStatus == "failed" {
		w.logEvent(ctx, job.ID, "error", "downloader reported failed status", nil)
		w.failJobAndRecoverWants(ctx, job, "downloader reported failed status", apperrors.KindBadGateway)
		return nil
	}

	// Spawn import tasks when download completes
	if newStatus == "completed" && job.Status != "completed" {
		return w.spawnImportTasks(ctx, client, job, item)
	}

	return nil
}

func (w *Worker) spawnImportTasks(ctx context.Context, client downloader.Client, job model.DownloadJob, item downloader.Item) error {
	if job.MediaType == "movie" {
		return w.spawnMovieImportTask(ctx, client, job, item)
	}
	return w.spawnSeriesImportTasks(ctx, client, job, item)
}

func (w *Worker) spawnMovieImportTask(ctx context.Context, client downloader.Client, job model.DownloadJob, item downloader.Item) error {
	externalID := *job.DownloaderExternalID

	files, err := client.ListFiles(ctx, externalID)
	if err != nil && err != downloader.ErrUnsupported {
		return apperrors.BadGatewayf("list files: %v", err).
			Op("DownloadWorker.spawnMovieImportTask")
	}

	sourcePath := ""
	if len(files) > 0 {
		mainFile, ok := importer.PickMainMovieFile(files)
		if !ok {
			return apperrors.Internalf("no suitable video files found for import").
				Op("DownloadWorker.spawnMovieImportTask").
				NotRetryable()
		}
		if filepath.IsAbs(mainFile.Path) {
			sourcePath = mainFile.Path
		} else if item.SavePath != "" {
			sourcePath = filepath.Join(item.SavePath, mainFile.Path)
		} else if item.ContentPath != "" {
			sourcePath = filepath.Join(item.ContentPath, mainFile.Path)
		}
	} else {
		// If file listing unsupported, use content path directly
		sourcePath = item.ContentPath
	}

	if sourcePath == "" {
		return apperrors.Internalf("unable to determine source path for import").
			Op("DownloadWorker.spawnMovieImportTask").
			NotRetryable()
	}

	wantID, err := w.linkedWantID(ctx, job.ID)
	if err != nil {
		return err
	}

	task, err := w.repo.CreateImportTask(ctx, repo.CreateImportTaskParams{
		DownloadJobID:  job.ID,
		SourcePath:     sourcePath,
		PreviousTaskID: uuid.Nil,
		WantID:         wantID,
		MediaType:      "movie",
		MediaItemID:    job.MediaItemID,
		EpisodeID:      uuid.Nil,
		LibraryID:      job.LibraryID,
		NameTemplateID: job.NameTemplateID,
	})
	if err != nil {
		return err
	}

	w.log.Info().
		Str("job_id", job.ID.String()).
		Str("task_id", task.ID.String()).
		Str("source_path", sourcePath).
		Msg("spawned movie import task")

	return nil
}

func (w *Worker) spawnSeriesImportTasks(ctx context.Context, client downloader.Client, job model.DownloadJob, item downloader.Item) error {
	externalID := *job.DownloaderExternalID

	files, err := client.ListFiles(ctx, externalID)
	if err != nil {
		return apperrors.BadGatewayf("list files: %v", err).
			Op("DownloadWorker.spawnSeriesImportTasks")
	}

	// Derive target season and episode from job
	// For season packs: job.SeasonID is set, job.EpisodeID is null
	// For single episodes: both job.SeasonID and job.EpisodeID are set
	var targetSeason *int
	var targetEpisode *int

	// First check SeasonID (always set for series downloads after schema update)
	if job.SeasonID != uuid.Nil {
		season, err := w.repo.GetSeason(ctx, job.SeasonID)
		if err == nil {
			sNum := int(season.SeasonNumber)
			targetSeason = &sNum
		}
	}

	// Then check EpisodeID for single-episode downloads
	if job.EpisodeID != uuid.Nil {
		episode, err := w.repo.GetEpisode(ctx, job.EpisodeID)
		if err == nil {
			eNum := int(episode.EpisodeNumber)
			targetEpisode = &eNum
			// If we didn't get season from SeasonID, derive from episode
			if targetSeason == nil {
				season, err := w.repo.GetSeason(ctx, episode.SeasonID)
				if err == nil {
					sNum := int(season.SeasonNumber)
					targetSeason = &sNum
				}
			}
		}
	}

	matchedFiles := importer.MatchFilesToEpisodes(files, targetSeason, targetEpisode)
	if len(matchedFiles) == 0 {
		return apperrors.Internalf("no files matched target episodes").
			Op("DownloadWorker.spawnSeriesImportTasks").
			NotRetryable()
	}

	// Map each linked want to its episode number, so a pack's files fan out onto
	// the right per-episode want. A job with no linked wants is the manual-grab
	// path (no want); its tasks file onto a NULL want_id, as before.
	wantByEpisode, err := w.wantsByEpisode(ctx, job.ID)
	if err != nil {
		return err
	}
	hasWants := len(wantByEpisode) > 0

	tasksCreated := 0
	for epNum, f := range matchedFiles {
		// Fan-out: file each matched episode onto its covering want. An overflow
		// file (an episode no linked want covers) is skipped — left for scan/
		// matching, never filed onto an unrelated want. The manual no-want path
		// (hasWants false) files every match onto a NULL want_id.
		wantID := uuid.Nil
		if hasWants {
			wid, ok := wantByEpisode[epNum]
			if !ok {
				w.log.Info().
					Str("job_id", job.ID.String()).
					Int("episode", epNum).
					Msg("pack file has no covering want, skipping (overflow)")
				continue
			}
			wantID = wid
		}

		sourcePath := f.Path
		if !filepath.IsAbs(sourcePath) && item.SavePath != "" {
			sourcePath = filepath.Join(item.SavePath, f.Path)
		}

		// Resolve episode ID for this file.
		episodeID, err := w.resolveEpisodeID(ctx, job.MediaItemID, targetSeason, epNum)
		if err != nil {
			w.log.Warn().Err(err).
				Int("episode", epNum).
				Msg("failed to resolve episode ID, skipping")
			continue
		}

		task, err := w.repo.CreateImportTask(ctx, repo.CreateImportTaskParams{
			DownloadJobID:  job.ID,
			SourcePath:     sourcePath,
			PreviousTaskID: uuid.Nil,
			WantID:         wantID,
			MediaType:      "series",
			MediaItemID:    job.MediaItemID,
			EpisodeID:      episodeID,
			LibraryID:      job.LibraryID,
			NameTemplateID: job.NameTemplateID,
		})
		if err != nil {
			w.log.Warn().Err(err).
				Int("episode", epNum).
				Msg("failed to create import task")
			continue
		}

		w.log.Info().
			Str("job_id", job.ID.String()).
			Str("task_id", task.ID.String()).
			Int("episode", epNum).
			Str("source_path", sourcePath).
			Msg("spawned series import task")

		tasksCreated++
	}

	// Under-coverage: a covered want with no matching file (a COMPLETE pack missing
	// a later-aired episode) would otherwise wedge in grabbed/downloading. Release
	// it back to 'pending' so it re-searches, and detach it from the pack job so
	// mirroring and the cancel cascade stay scoped to what the pack actually carries.
	if hasWants {
		w.releaseUncoveredWants(ctx, job.ID, wantByEpisode, matchedFiles)
	}

	if tasksCreated == 0 {
		return apperrors.Internalf("failed to create any import tasks").
			Op("DownloadWorker.spawnSeriesImportTasks").
			NotRetryable()
	}

	return nil
}

// wantsByEpisode maps each of a job's linked wants to its episode number, the
// lookup the import fan-out files matched files through. A want with no episode_id
// (defensive — series wants always carry one) is skipped.
func (w *Worker) wantsByEpisode(ctx context.Context, jobID uuid.UUID) (map[int]uuid.UUID, error) {
	wants, err := w.repo.ListWantsByDownloadJob(ctx, jobID)
	if err != nil {
		return nil, err
	}
	out := make(map[int]uuid.UUID, len(wants))
	for _, want := range wants {
		if want.EpisodeID == nil {
			continue
		}
		ep, err := w.repo.GetEpisode(ctx, *want.EpisodeID)
		if err != nil {
			return nil, err
		}
		out[int(ep.EpisodeNumber)] = want.ID
	}
	return out, nil
}

// releaseUncoveredWants returns every linked want the pack didn't carry a file for
// to 'pending' and unlinks it from the job. Best-effort: a release/unlink failure
// is logged, not propagated — it must not break the import of the files that did
// land.
func (w *Worker) releaseUncoveredWants(ctx context.Context, jobID uuid.UUID, wantByEpisode map[int]uuid.UUID, matched map[int]downloader.File) {
	for epNum, wantID := range wantByEpisode {
		if _, ok := matched[epNum]; ok {
			continue
		}
		if _, _, err := w.repo.ReleaseWantFromGrab(ctx, wantID, "season pack did not contain this episode; re-searching"); err != nil {
			w.log.Warn().Err(err).Str("want_id", wantID.String()).Int("episode", epNum).Msg("failed to release under-covered want")
			continue
		}
		if err := w.repo.UnlinkDownloadJobWant(ctx, jobID, wantID); err != nil {
			w.log.Warn().Err(err).Str("want_id", wantID.String()).Str("job_id", jobID.String()).Msg("failed to unlink under-covered want from job")
			continue
		}
		w.log.Info().
			Str("job_id", jobID.String()).
			Str("want_id", wantID.String()).
			Int("episode", epNum).
			Msg("released under-covered want back to pending")
	}
}

// failJobAndRecoverWants terminally fails a download job and recovers its linked
// wants instead of dead-ending them. For each want it excludes the failed release
// (so the re-search can't re-pick it) and returns the want to 'pending' to be
// searched again — replacing the pre-A2 "mark the want failed", which required a
// manual re-request. The recovery loop is bounded precisely because the exclusion
// bars re-picking the same release: ProcessWant re-searches once and, on no other
// winner, falls back to the scheduler's recheck cadence.
//
// Best-effort per want (log-and-continue), the same contract as
// releaseUncoveredWants: one want's recovery failure must not block the others or
// the job-fail. It shares the precedent's exclusion-before-release ordering — see
// the inline note.
func (w *Worker) failJobAndRecoverWants(ctx context.Context, job model.DownloadJob, lastError string, kind apperrors.Kind) {
	if _, err := w.repo.MarkDownloadJobFailed(ctx, job.ID, lastError, kind); err != nil {
		w.log.Warn().Err(err).Str("job_id", job.ID.String()).Msg("failed to mark download job failed")
	}
	w.publishJobUpdated(ctx, job.ID)

	wants, err := w.repo.ListWantsByDownloadJob(ctx, job.ID)
	if err != nil {
		w.log.Warn().Err(err).Str("job_id", job.ID.String()).Msg("failed to list wants for recovery")
		return
	}

	detail := fmt.Sprintf("%s: %s", job.CandidateTitle, lastError)
	for _, want := range wants {
		// Exclude the failed release BEFORE releasing the want: the release makes
		// the want claimable immediately, so if the exclusion isn't already in
		// place the very next search could re-pick the release that just failed.
		if err := w.repo.AddWantReleaseExclusion(ctx, repo.AddWantReleaseExclusionParams{
			WantID:    want.ID,
			IndexerID: job.IndexerID,
			GUID:      job.Guid,
			Reason:    model.ExclusionDownloadFailed,
			Detail:    &detail,
		}); err != nil {
			// Releasing without the exclusion would loop on the same release. Fall
			// back to the pre-A2 terminal-fail so the want doesn't wedge.
			w.log.Warn().Err(err).Str("want_id", want.ID.String()).Msg("failed to exclude release; failing want")
			jobutil.MirrorWant(ctx, w.repo, w.broker, w.log, want.ID, model.WantFailed)
			continue
		}

		released, ok, err := w.repo.ReleaseWantFromGrab(ctx, want.ID,
			fmt.Sprintf("download failed (%s); release excluded, re-searching", lastError))
		if err != nil {
			w.log.Warn().Err(err).Str("want_id", want.ID.String()).Msg("failed to release want after download failure")
			continue
		}
		if !ok {
			// Canceled or already released — benign, nothing to re-arm.
			continue
		}
		realtime.Emit(ctx, w.broker, realtime.WantUpdated(released))
		w.restampManualHold(ctx, released)
	}
}

// restampManualHold re-stamps the 'needs_pick' hold on a just-released want whose
// tracking segment is manual. The grab cleared the hold, and ReleaseWantFromGrab
// leaves it cleared; without this a want owned by a manual segment would fall into
// the auto-search pool rather than waiting for the user to pick again. Mirrors
// rearmMovieWant's re-stamp. One extra query per recovered want — failures are
// rare.
func (w *Worker) restampManualHold(ctx context.Context, released model.Want) {
	tracking, err := w.repo.GetTracking(ctx, released.TrackingID)
	if err != nil {
		w.log.Warn().Err(err).Str("want_id", released.ID.String()).Msg("failed to load tracking for manual-hold restamp")
		return
	}
	hold := model.HoldForNewWant(tracking, model.WantSegment(released.Segment))
	if hold == nil {
		return
	}
	if _, err := w.repo.SetWantHold(ctx, released.ID, hold); err != nil {
		w.log.Warn().Err(err).Str("want_id", released.ID.String()).Msg("failed to restamp manual hold")
	}
}

func (w *Worker) resolveEpisodeID(ctx context.Context, mediaItemID uuid.UUID, targetSeason *int, epNum int) (uuid.UUID, error) {
	seasonNum := 1
	if targetSeason != nil {
		seasonNum = *targetSeason
	}

	// Get or create season
	season, err := w.repo.UpsertSeason(ctx, repo.UpsertSeasonParams{
		MediaItemID:  mediaItemID,
		SeasonNumber: int32(seasonNum),
	})
	if err != nil {
		return uuid.Nil, err
	}

	// Get-then-create: the worker has no tmdb_id to upsert on, and the
	// season+number index isn't unique, so a blind upsert would duplicate the
	// row structure-sync already created. Look it up; only create as fallback.
	episode, err := w.repo.GetEpisodeByNumber(ctx, season.ID, int32(epNum))
	if err == nil {
		return episode.ID, nil
	}
	if !apperrors.IsNotFound(err) {
		return uuid.Nil, err
	}

	created, err := w.repo.UpsertEpisode(ctx, repo.UpsertEpisodeParams{
		SeasonID:      season.ID,
		EpisodeNumber: int32(epNum),
	})
	if err != nil {
		return uuid.Nil, err
	}

	return created.ID, nil
}

// mirrorWants reflects a lifecycle transition onto every want the job advances,
// joined through download_job_want. In P1 a job links exactly one want; the loop
// is the M:N shape a season pack (covering N wants) fans out over. A list failure
// is logged but never propagated — mirroring must not break the pipeline, the
// same contract as jobutil.MirrorWant.
func (w *Worker) mirrorWants(ctx context.Context, jobID uuid.UUID, status model.WantStatus) {
	wants, err := w.repo.ListWantsByDownloadJob(ctx, jobID)
	if err != nil {
		w.log.Warn().Err(err).Str("job_id", jobID.String()).Msg("failed to list wants for mirror")
		return
	}
	for _, want := range wants {
		jobutil.MirrorWant(ctx, w.repo, w.broker, w.log, want.ID, status)
	}
}

// linkedWantID returns the single want a movie job's import task files onto. A
// movie tracking is single-atom — one want per job — so the first (only) link is
// it. uuid.Nil when there is no linked want (the manual-grab path), which
// CreateImportTask stores as a NULL want_id. The series path instead fans out over
// a per-episode → want map (see wantsByEpisode).
func (w *Worker) linkedWantID(ctx context.Context, jobID uuid.UUID) (uuid.UUID, error) {
	wants, err := w.repo.ListWantsByDownloadJob(ctx, jobID)
	if err != nil {
		return uuid.Nil, err
	}
	if len(wants) == 0 {
		return uuid.Nil, nil
	}
	return wants[0].ID, nil
}

func (w *Worker) handleError(ctx context.Context, job model.DownloadJob, err error) {
	msg := err.Error()
	kind := apperrors.KindOf(err)

	w.log.Error().
		Err(err).
		Str("job_id", job.ID.String()).
		Str("kind", string(kind)).
		Msg("download job error")

	w.logEvent(ctx, job.ID, "error", msg, map[string]any{
		"kind":          kind,
		"attempt_count": job.AttemptCount + 1,
	})

	// Non-retryable errors fail immediately.
	if !apperrors.IsRetryable(err) {
		w.failJobAndRecoverWants(ctx, job, msg, kind)
		return
	}

	// Retryable errors: respect the max-attempts ceiling. A retryable failure that
	// exhausts its attempts still recovers its wants — the release is excluded and
	// re-searched. Indexer-wide outages (which would burn attempts across many
	// jobs) are indexer-health-gating's concern, deferred.
	attempt := int(job.AttemptCount) + 1
	if attempt >= w.maxAttempts {
		w.failJobAndRecoverWants(ctx, job,
			fmt.Sprintf("max attempts (%d) exceeded: %s", w.maxAttempts, msg), kind)
		return
	}

	// Schedule retry with exponential backoff
	backoff := jobutil.Backoff(attempt)
	nextRun := time.Now().Add(backoff)

	w.logEvent(ctx, job.ID, "retry_scheduled", msg, map[string]any{
		"next_run_at": nextRun,
		"backoff":     backoff.String(),
	})

	_, _ = w.repo.ScheduleDownloadJobRetry(ctx, repo.ScheduleDownloadJobRetryParams{
		ID:        job.ID,
		LastError: msg,
		Kind:      kind,
		NextRunAt: nextRun,
	})
}

func (w *Worker) logEvent(ctx context.Context, jobID uuid.UUID, eventType, message string, metadata map[string]any) {
	var metaBytes []byte
	if metadata != nil {
		metaBytes, _ = json.Marshal(metadata)
	}

	_, err := w.repo.CreateDownloadJobEvent(ctx, repo.CreateDownloadJobEventParams{
		DownloadJobID: jobID,
		EventType:     eventType,
		OldStatus:     nil,
		NewStatus:     nil,
		Message:       jobutil.StrPtr(message),
		Metadata:      metaBytes,
	})
	if err != nil {
		w.log.Warn().Err(err).Msg("failed to log download job event")
	}
}

func (w *Worker) publishJobUpdated(ctx context.Context, jobID uuid.UUID) {
	if w.broker == nil {
		return
	}
	// Fetch job with computed import_status for consistent frontend display
	enriched, err := w.repo.GetDownloadJobWithImportSummary(ctx, jobID)
	if err != nil {
		w.log.Warn().Err(err).Str("job_id", jobID.String()).Msg("failed to fetch enriched job for SSE")
		return
	}
	realtime.Emit(ctx, w.broker, realtime.DownloadJobUpdated(enriched))
}

func mapItemStatus(st downloader.JobStatus) string {
	switch st {
	case downloader.StatusCompleted, downloader.StatusSeeding:
		return "completed"
	case downloader.StatusQueued, downloader.StatusDownloading, downloader.StatusPaused:
		return "downloading"
	case downloader.StatusErrored:
		return "failed"
	default:
		return "downloading"
	}
}
