// Package download implements the download worker that polls downloaders
// and manages download job state transitions.
package download

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	"github.com/kyleaupton/arrflix/internal/downloader"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/importer"
	"github.com/kyleaupton/arrflix/internal/jobs/state"
	"github.com/kyleaupton/arrflix/internal/logger"
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

// New creates a new download worker.
func New(r *repo.Repository, dlm *downloader.Manager, log *logger.Logger, broker *sse.Broker) *Worker {
	cfg := DefaultConfig()
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

func (w *Worker) processJob(ctx context.Context, job dbgen.DownloadJob) error {
	// Skip terminal states
	if w.sm.IsTerminalStr(job.Status) {
		return nil
	}

	client, err := w.dlm.GetClientByID(ctx, job.DownloaderID.String())
	if err != nil {
		return apperrors.AsPermanent(fmt.Errorf("get downloader client: %w", err))
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

func (w *Worker) enqueueDownload(ctx context.Context, client downloader.Client, job dbgen.DownloadJob) error {
	addReq := downloader.AddRequest{}
	switch job.Protocol {
	case "torrent":
		addReq.MagnetURL = job.CandidateLink
	case "usenet":
		addReq.NZBURL = job.CandidateLink
	default:
		return apperrors.AsPermanent(fmt.Errorf("unknown protocol: %s", job.Protocol))
	}

	w.log.Info().
		Str("job_id", job.ID.String()).
		Str("protocol", job.Protocol).
		Str("link", job.CandidateLink).
		Msg("adding download to client")

	res, err := client.Add(ctx, addReq)
	if err != nil {
		w.log.Error().Err(err).Str("job_id", job.ID.String()).Msg("[DEBUG] Add() failed")
		return fmt.Errorf("downloader add: %w", err)
	}

	w.log.Info().
		Str("job_id", job.ID.String()).
		Str("external_id", res.ExternalID).
		Str("name", res.Name).
		Msg("[DEBUG] Add() succeeded")

	updated, err := w.repo.SetDownloadJobEnqueued(ctx, job.ID, res.ExternalID)
	if err != nil {
		return fmt.Errorf("set enqueued: %w", err)
	}

	w.logEvent(ctx, job.ID, "status_changed", "", map[string]any{
		"old_status": job.Status,
		"new_status": "enqueued",
	})

	w.publishJobUpdated(ctx, updated.ID)
	return nil
}

func (w *Worker) pollDownload(ctx context.Context, client downloader.Client, job dbgen.DownloadJob) error {
	if job.DownloaderExternalID == nil || *job.DownloaderExternalID == "" {
		return apperrors.AsPermanent(fmt.Errorf("job missing downloader_external_id"))
	}

	externalID := *job.DownloaderExternalID
	item, err := client.Get(ctx, externalID)
	if err != nil {
		return fmt.Errorf("downloader get: %w", err)
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
	updated, err := w.repo.SetDownloadJobDownloadSnapshot(ctx, dbgen.SetDownloadJobDownloadSnapshotParams{
		ID:               job.ID,
		Status:           newStatus,
		DownloaderStatus: ptr(string(item.Status)),
		Progress:         ptr(item.Progress),
		SavePath:         ptr(item.SavePath),
		ContentPath:      ptr(item.ContentPath),
	})
	if err != nil {
		return fmt.Errorf("update snapshot: %w", err)
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
		_, _ = w.repo.MarkDownloadJobFailed(ctx, job.ID, "downloader reported failed status", apperrors.Permanent)
		return nil
	}

	// Spawn import tasks when download completes
	if newStatus == "completed" && job.Status != "completed" {
		return w.spawnImportTasks(ctx, client, job, item)
	}

	return nil
}

func (w *Worker) spawnImportTasks(ctx context.Context, client downloader.Client, job dbgen.DownloadJob, item downloader.Item) error {
	if job.MediaType == "movie" {
		return w.spawnMovieImportTask(ctx, client, job, item)
	}
	return w.spawnSeriesImportTasks(ctx, client, job, item)
}

func (w *Worker) spawnMovieImportTask(ctx context.Context, client downloader.Client, job dbgen.DownloadJob, item downloader.Item) error {
	externalID := *job.DownloaderExternalID

	files, err := client.ListFiles(ctx, externalID)
	if err != nil && err != downloader.ErrUnsupported {
		return fmt.Errorf("list files: %w", err)
	}

	sourcePath := ""
	if len(files) > 0 {
		mainFile, ok := importer.PickMainMovieFile(files)
		if !ok {
			return apperrors.AsPermanent(fmt.Errorf("no suitable video files found for import"))
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
		return apperrors.AsPermanent(fmt.Errorf("unable to determine source path for import"))
	}

	// Create import task
	task, err := w.repo.CreateImportTask(ctx, dbgen.CreateImportTaskParams{
		DownloadJobID:  job.ID,
		SourcePath:     sourcePath,
		PreviousTaskID: pgtype.UUID{Valid: false},
		MediaType:      "movie",
		MediaItemID:    job.MediaItemID,
		EpisodeID:      pgtype.UUID{Valid: false},
		LibraryID:      job.LibraryID,
		NameTemplateID: job.NameTemplateID,
	})
	if err != nil {
		return fmt.Errorf("create import task: %w", err)
	}

	w.log.Info().
		Str("job_id", job.ID.String()).
		Str("task_id", task.ID.String()).
		Str("source_path", sourcePath).
		Msg("spawned movie import task")

	return nil
}

func (w *Worker) spawnSeriesImportTasks(ctx context.Context, client downloader.Client, job dbgen.DownloadJob, item downloader.Item) error {
	externalID := *job.DownloaderExternalID

	files, err := client.ListFiles(ctx, externalID)
	if err != nil {
		return fmt.Errorf("list files: %w", err)
	}

	// Derive target season and episode from job
	// For season packs: job.SeasonID is set, job.EpisodeID is null
	// For single episodes: both job.SeasonID and job.EpisodeID are set
	var targetSeason *int
	var targetEpisode *int

	// First check SeasonID (always set for series downloads after schema update)
	if job.SeasonID.Valid {
		season, err := w.repo.GetSeason(ctx, job.SeasonID)
		if err == nil {
			sNum := int(season.SeasonNumber)
			targetSeason = &sNum
		}
	}

	// Then check EpisodeID for single-episode downloads
	if job.EpisodeID.Valid {
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
		return apperrors.AsPermanent(fmt.Errorf("no files matched target episodes"))
	}

	tasksCreated := 0
	for epNum, f := range matchedFiles {
		sourcePath := f.Path
		if !filepath.IsAbs(sourcePath) && item.SavePath != "" {
			sourcePath = filepath.Join(item.SavePath, f.Path)
		}

		// Resolve episode ID for this file
		episodeID, err := w.resolveEpisodeID(ctx, job.MediaItemID, targetSeason, epNum)
		if err != nil {
			w.log.Warn().Err(err).
				Int("episode", epNum).
				Msg("failed to resolve episode ID, skipping")
			continue
		}

		task, err := w.repo.CreateImportTask(ctx, dbgen.CreateImportTaskParams{
			DownloadJobID:  job.ID,
			SourcePath:     sourcePath,
			PreviousTaskID: pgtype.UUID{Valid: false},
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

	if tasksCreated == 0 {
		return apperrors.AsPermanent(fmt.Errorf("failed to create any import tasks"))
	}

	return nil
}

func (w *Worker) resolveEpisodeID(ctx context.Context, mediaItemID pgtype.UUID, targetSeason *int, epNum int) (pgtype.UUID, error) {
	seasonNum := 1
	if targetSeason != nil {
		seasonNum = *targetSeason
	}

	// Get or create season
	season, err := w.repo.UpsertSeason(ctx, mediaItemID, int32(seasonNum), pgtype.Date{Valid: false})
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("upsert season: %w", err)
	}

	// Get or create episode
	episode, err := w.repo.UpsertEpisode(ctx, season.ID, int32(epNum), nil, pgtype.Date{Valid: false}, nil, nil)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("upsert episode: %w", err)
	}

	return episode.ID, nil
}

func (w *Worker) handleError(ctx context.Context, job dbgen.DownloadJob, err error) {
	msg := err.Error()
	category := apperrors.CategoryOf(err)

	w.log.Error().
		Err(err).
		Str("job_id", job.ID.String()).
		Str("category", string(category)).
		Msg("download job error")

	w.logEvent(ctx, job.ID, "error", msg, map[string]any{
		"category":      category,
		"attempt_count": job.AttemptCount + 1,
	})

	// Permanent errors fail immediately
	if category == apperrors.Permanent {
		_, _ = w.repo.MarkDownloadJobFailed(ctx, job.ID, msg, category)
		return
	}

	// Check if we've exceeded max attempts
	attempt := int(job.AttemptCount) + 1
	if attempt >= w.maxAttempts {
		_, _ = w.repo.MarkDownloadJobFailed(ctx, job.ID,
			fmt.Sprintf("max attempts (%d) exceeded: %s", w.maxAttempts, msg),
			apperrors.Transient)
		return
	}

	// Schedule retry with exponential backoff
	backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
	nextRun := time.Now().Add(backoff)

	w.logEvent(ctx, job.ID, "retry_scheduled", msg, map[string]any{
		"next_run_at": nextRun,
		"backoff":     backoff.String(),
	})

	_, _ = w.repo.ScheduleDownloadJobRetry(ctx, job.ID, msg, category, nextRun)
}

func (w *Worker) logEvent(ctx context.Context, jobID pgtype.UUID, eventType, message string, metadata map[string]any) {
	var metaBytes []byte
	if metadata != nil {
		metaBytes, _ = json.Marshal(metadata)
	}

	_, err := w.repo.CreateDownloadJobEvent(ctx, dbgen.CreateDownloadJobEventParams{
		DownloadJobID: jobID,
		EventType:     eventType,
		OldStatus:     nil,
		NewStatus:     nil,
		Message:       strPtr(message),
		Metadata:      metaBytes,
	})
	if err != nil {
		w.log.Warn().Err(err).Msg("failed to log download job event")
	}
}

func (w *Worker) publishJobUpdated(ctx context.Context, jobID pgtype.UUID) {
	if w.broker == nil {
		return
	}
	// Fetch job with computed import_status for consistent frontend display
	enriched, err := w.repo.GetDownloadJobWithImportSummary(ctx, jobID)
	if err != nil {
		w.log.Warn().Err(err).Str("job_id", jobID.String()).Msg("failed to fetch enriched job for SSE")
		return
	}
	b, err := json.Marshal(enriched)
	if err != nil {
		return
	}
	w.broker.Publish(sse.Event{
		Type: "download_job_updated",
		ID:   jobID.String(),
		Data: b,
	})
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

func ptr[T any](v T) *T { return &v }

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
