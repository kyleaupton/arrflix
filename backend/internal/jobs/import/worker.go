// Package importw implements the import worker that processes import tasks
// (hardlinks/copies files from downloads to library).
package importw

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/downloader"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/importer"
	"github.com/kyleaupton/arrflix/internal/jobs/state"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/mediainfo"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/pathmapping"
	"github.com/kyleaupton/arrflix/internal/release"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/sse"
	"github.com/kyleaupton/arrflix/internal/template"
)

// Worker processes import tasks: hardlinks/copies files from downloads to library.
type Worker struct {
	repo       *repo.Repository
	dlm        *downloader.Manager
	pathMapper *pathmapping.Mapper
	log        *logger.Logger
	broker     *sse.Broker
	sm         *state.ImportTaskMachine
	mediaInfo  *mediainfo.Analyzer

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
		PollInterval: 2 * time.Second,
		ClaimLimit:   10,
		MaxAttempts:  5,
	}
}

// New creates a new import worker.
func New(r *repo.Repository, dlm *downloader.Manager, log *logger.Logger, broker *sse.Broker) *Worker {
	cfg := DefaultConfig()
	return &Worker{
		repo:         r,
		dlm:          dlm,
		pathMapper:   pathmapping.New(),
		log:          log,
		broker:       broker,
		sm:           state.NewImportTaskMachine(),
		mediaInfo:    mediainfo.NewAnalyzer(*log),
		pollInterval: cfg.PollInterval,
		claimLimit:   cfg.ClaimLimit,
		maxAttempts:  cfg.MaxAttempts,
	}
}

// Run starts the worker loop.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	w.log.Info().Msg("import worker started")

	for {
		select {
		case <-ctx.Done():
			w.log.Info().Msg("import worker stopped")
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	// ClaimRunnableImportTasks atomically sets status to in_progress
	tasks, err := w.repo.ClaimRunnableImportTasks(ctx, w.claimLimit)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to claim import tasks")
		return
	}

	for _, task := range tasks {
		if err := w.processTask(ctx, task); err != nil {
			w.handleError(ctx, task, err)
		}
	}
}

func (w *Worker) processTask(ctx context.Context, task model.ImportTask) error {
	w.log.Info().
		Str("task_id", task.ID.String()).
		Str("source_path", task.SourcePath).
		Msg("processing import task")

	// Log status change to in_progress
	w.logEvent(ctx, task.ID, "status_changed", "", map[string]any{
		"old_status": "pending",
		"new_status": "in_progress",
	})

	// Try to re-derive source path from download job (self-healing)
	sourcePath, err := w.deriveSourcePath(ctx, task)
	if err != nil {
		return err // Already categorized appropriately
	}

	// Update task if path changed
	if sourcePath != task.SourcePath {
		w.log.Info().
			Str("task_id", task.ID.String()).
			Str("old_path", task.SourcePath).
			Str("new_path", sourcePath).
			Msg("self-healing: updated source path")
		if err := w.repo.UpdateImportTaskSourcePath(ctx, task.ID, sourcePath); err != nil {
			w.log.Warn().Err(err).Msg("failed to update source path in DB")
		}
		task.SourcePath = sourcePath
	}

	// Validate source exists
	srcInfo, err := os.Stat(task.SourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return apperrors.Internalf("source file not found: %s", task.SourcePath).
				Op("ImportWorker.processTask").
				NotRetryable()
		}
		return apperrors.Internalf("stat source %s: %v", task.SourcePath, err).
			Op("ImportWorker.processTask")
	}
	if srcInfo.IsDir() {
		return apperrors.Internalf("source is a directory, expected file: %s", task.SourcePath).
			Op("ImportWorker.processTask").
			NotRetryable()
	}

	// Extract mediainfo from source file for template rendering
	mi := w.mediaInfo.Analyze(task.SourcePath)
	if mi == nil {
		w.log.Warn().Str("path", task.SourcePath).Msg("failed to extract mediainfo, continuing without it")
	}

	// Get required data
	taskDetails, err := w.repo.GetImportTaskWithDetails(ctx, task.ID)
	if err != nil {
		return err
	}

	// Compute destination path using name template
	destPath, err := w.computeDestPath(task, taskDetails, mi)
	if err != nil {
		return err
	}

	// Full absolute destination
	fullDest := filepath.Join(taskDetails.LibraryRootPath, destPath)

	// Check for existing destination
	if _, err := os.Stat(fullDest); err == nil {
		// Handle reimport: if this is a reimport, remove old file first
		if task.PreviousTaskID != uuid.Nil {
			w.log.Info().
				Str("task_id", task.ID.String()).
				Str("dest", fullDest).
				Msg("reimport: removing existing destination")
			if err := os.Remove(fullDest); err != nil && !os.IsNotExist(err) {
				return apperrors.Internalf("remove existing dest %s: %v", fullDest, err).
					Op("ImportWorker.processTask")
			}
		} else {
			// Not a reimport, fail
			return apperrors.Conflictf("destination already exists: %s", fullDest).
				Op("ImportWorker.processTask")
		}
	}

	// Perform import (hardlink or copy)
	method, err := importer.HardlinkOrCopy(task.SourcePath, fullDest)
	if err != nil {
		return apperrors.Internalf("import file %s: %v", task.SourcePath, err).
			Op("ImportWorker.processTask")
	}

	w.log.Info().
		Str("task_id", task.ID.String()).
		Str("method", method).
		Str("dest", fullDest).
		Msg("file imported successfully")

	// Create media file record.
	var episodeIDPtr *uuid.UUID
	if task.EpisodeID != uuid.Nil {
		ep := task.EpisodeID
		episodeIDPtr = &ep
	}

	mediaFile, err := w.repo.CreateMediaFile(ctx, repo.CreateMediaFileParams{
		LibraryID:   task.LibraryID,
		MediaItemID: task.MediaItemID,
		EpisodeID:   episodeIDPtr,
		Path:        destPath,
	})
	if err != nil {
		// File was created but record failed - log but don't fail the task
		w.log.Error().Err(err).
			Str("task_id", task.ID.String()).
			Str("dest", destPath).
			Msg("failed to create media file record after successful import")
	}

	// Create file state
	if mediaFile.ID != uuid.Nil {
		fileSize := srcInfo.Size()
		_, _ = w.repo.UpsertMediaFileState(ctx, repo.UpsertMediaFileStateParams{
			MediaFileID: mediaFile.ID,
			FileExists:  true,
			FileSize:    &fileSize,
		})
	}

	// Record import in media_file_import table.
	_, _ = w.repo.CreateMediaFileImport(ctx, repo.CreateMediaFileImportParams{
		MediaFileID:  mediaFile.ID,
		ImportTaskID: task.ID,
		Method:       method,
		SourcePath:   &task.SourcePath,
		DestPath:     destPath,
		Success:      true,
		ErrorMessage: nil,
	})

	// Mark task completed
	_, err = w.repo.SetImportTaskCompleted(ctx, repo.SetImportTaskCompletedParams{
		ID:           task.ID,
		DestPath:     destPath,
		ImportMethod: method,
		MediaFileID:  mediaFile.ID,
	})
	if err != nil {
		return err
	}

	w.logEvent(ctx, task.ID, "status_changed", "", map[string]any{
		"old_status":    "in_progress",
		"new_status":    "completed",
		"dest_path":     destPath,
		"import_method": method,
	})

	w.publishTaskUpdated(ctx, task)
	return nil
}

func (w *Worker) computeDestPath(task model.ImportTask, details model.ImportTaskWithDetails, mi *model.MediaInfoFields) (string, error) {
	srcExt := filepath.Ext(task.SourcePath)

	// Build evaluation context for template rendering
	candidateTitle := ""
	if details.CandidateTitle != nil {
		candidateTitle = *details.CandidateTitle
	}

	q := release.Parse(candidateTitle)
	candidate := model.DownloadCandidate{
		Title: candidateTitle,
	}
	evalCtx := model.NewEvaluationContext(candidate, q)

	// Add media metadata
	year := 0
	if details.MediaYear != nil {
		year = int(*details.MediaYear)
	}
	tmdbID := int64(0)
	if details.MediaTmdbID != nil {
		tmdbID = *details.MediaTmdbID
	}

	if task.MediaType == "movie" {
		evalCtx = evalCtx.WithMedia(model.MediaTypeMovie, details.MediaTitle, year, tmdbID)
	} else {
		evalCtx = evalCtx.WithMedia(model.MediaTypeSeries, details.MediaTitle, year, tmdbID)
		var seasonNum, epNum *int
		if details.SeasonNumber != nil {
			sn := int(*details.SeasonNumber)
			seasonNum = &sn
		}
		if details.EpisodeNumber != nil {
			en := int(*details.EpisodeNumber)
			epNum = &en
		}
		evalCtx = evalCtx.WithSeriesInfo(seasonNum, epNum, details.EpisodeTitle)
	}

	// Add mediainfo if available
	if mi != nil {
		evalCtx = evalCtx.WithMediaInfo(mi)
	}

	templateData := evalCtx.ToTemplateData()

	// Render template parts
	var rel string
	if task.MediaType == "series" {
		showPart, err := template.Render(coalesce(details.SeriesShowTemplate), templateData)
		if err != nil {
			return "", apperrors.Internalf("render show template: %v", err).
				Op("ImportWorker.computeDestPath").
				NotRetryable()
		}
		seasonPart, err := template.Render(coalesce(details.SeriesSeasonTemplate), templateData)
		if err != nil {
			return "", apperrors.Internalf("render season template: %v", err).
				Op("ImportWorker.computeDestPath").
				NotRetryable()
		}
		filePart, err := template.Render(details.NameTemplate, templateData)
		if err != nil {
			return "", apperrors.Internalf("render file template: %v", err).
				Op("ImportWorker.computeDestPath").
				NotRetryable()
		}
		rel = filepath.Join(showPart, seasonPart, filePart)
	} else {
		// Movie type
		var dirPart string
		if details.MovieDirTemplate != nil && *details.MovieDirTemplate != "" {
			var err error
			dirPart, err = template.Render(*details.MovieDirTemplate, templateData)
			if err != nil {
				return "", apperrors.Internalf("render movie dir template: %v", err).
					Op("ImportWorker.computeDestPath").
					NotRetryable()
			}
		}
		filePart, err := template.Render(details.NameTemplate, templateData)
		if err != nil {
			return "", apperrors.Internalf("render file template: %v", err).
				Op("ImportWorker.computeDestPath").
				NotRetryable()
		}
		if dirPart != "" {
			rel = filepath.Join(dirPart, filePart)
		} else {
			rel = filePart
		}
	}

	rel = importer.EnsureExt(rel, srcExt)
	return rel, nil
}

func (w *Worker) handleError(ctx context.Context, task model.ImportTask, err error) {
	msg := err.Error()
	kind := apperrors.KindOf(err)

	w.log.Error().
		Err(err).
		Str("task_id", task.ID.String()).
		Str("kind", string(kind)).
		Msg("import task error")

	w.logEvent(ctx, task.ID, "error", msg, map[string]any{
		"kind":          kind,
		"attempt_count": task.AttemptCount + 1,
	})

	// Non-retryable errors fail immediately.
	if !apperrors.IsRetryable(err) {
		_, _ = w.repo.SetImportTaskFailed(ctx, task.ID, msg, kind)
		w.publishTaskUpdated(ctx, task)
		return
	}

	// Retryable errors: respect the per-task or worker-level ceiling.
	attempt := int(task.AttemptCount) + 1
	maxAttempts := int(task.MaxAttempts)
	if maxAttempts == 0 {
		maxAttempts = w.maxAttempts
	}

	if attempt >= maxAttempts {
		_, _ = w.repo.SetImportTaskFailed(ctx, task.ID,
			fmt.Sprintf("max attempts (%d) exceeded: %s", maxAttempts, msg),
			kind)
		w.publishTaskUpdated(ctx, task)
		return
	}

	// Schedule retry with exponential backoff
	backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
	nextRun := time.Now().Add(backoff)

	w.logEvent(ctx, task.ID, "retry_scheduled", msg, map[string]any{
		"next_run_at": nextRun,
		"backoff":     backoff.String(),
	})

	_, _ = w.repo.ScheduleImportTaskRetry(ctx, repo.ScheduleImportTaskRetryParams{
		ID:        task.ID,
		LastError: msg,
		Kind:      kind,
		NextRunAt: nextRun,
	})
	w.publishTaskUpdated(ctx, task)
}

func (w *Worker) logEvent(ctx context.Context, taskID uuid.UUID, eventType, message string, metadata map[string]any) {
	var metaBytes []byte
	if metadata != nil {
		metaBytes, _ = json.Marshal(metadata)
	}

	_, err := w.repo.CreateImportTaskEvent(ctx, repo.CreateImportTaskEventParams{
		ImportTaskID: taskID,
		EventType:    eventType,
		OldStatus:    nil,
		NewStatus:    nil,
		Message:      strPtr(message),
		Metadata:     metaBytes,
	})
	if err != nil {
		w.log.Warn().Err(err).Msg("failed to log import task event")
	}
}

func (w *Worker) publishTaskUpdated(ctx context.Context, task model.ImportTask) {
	if w.broker == nil {
		return
	}
	// Notify about import task update
	w.broker.Publish(sse.Event{
		Type: "import_task_updated",
		ID:   task.ID.String(),
		Data: nil,
	})
	// Also notify about parent download job since import_status may have changed
	if task.DownloadJobID != uuid.Nil {
		w.publishDownloadJobUpdated(ctx, task.DownloadJobID)
	}
}

func (w *Worker) publishDownloadJobUpdated(ctx context.Context, jobID uuid.UUID) {
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

// deriveSourcePath attempts to re-derive the source path from the download job.
// This enables self-healing when path mappings change or volume mounts are fixed.
func (w *Worker) deriveSourcePath(ctx context.Context, task model.ImportTask) (string, error) {
	// No download job - use stored path (manual import case)
	if task.DownloadJobID == uuid.Nil {
		return task.SourcePath, nil
	}

	// Get download job
	job, err := w.repo.GetDownloadJob(ctx, task.DownloadJobID)
	if err != nil {
		w.log.Debug().Err(err).Msg("failed to get download job, using stored path")
		return task.SourcePath, nil
	}

	// Need external ID to query downloader
	if job.DownloaderExternalID == nil || *job.DownloaderExternalID == "" {
		return task.SourcePath, nil
	}

	// Get downloader client
	client, err := w.dlm.GetClientByID(ctx, job.DownloaderID.String())
	if err != nil {
		w.log.Debug().Err(err).Msg("failed to get downloader client, using stored path")
		return task.SourcePath, nil
	}

	// Query downloader for files
	files, err := client.ListFiles(ctx, *job.DownloaderExternalID)
	if err != nil {
		w.log.Debug().Err(err).Msg("failed to list files from downloader, using stored path")
		return task.SourcePath, nil
	}

	if len(files) == 0 {
		w.log.Debug().Msg("no files returned from downloader, using stored path")
		return task.SourcePath, nil
	}

	// Identify correct file based on media type
	var rawPath string
	if task.MediaType == "movie" {
		mainFile, ok := importer.PickMainMovieFile(files)
		if !ok {
			return "", apperrors.Internalf("no video files found in download").
				Op("ImportWorker.deriveSourcePath").
				NotRetryable()
		}
		rawPath = mainFile.Path
	} else {
		// Series - match to episode
		if task.EpisodeID == uuid.Nil {
			return task.SourcePath, nil
		}

		episode, err := w.repo.GetEpisode(ctx, task.EpisodeID)
		if err != nil {
			w.log.Debug().Err(err).Msg("failed to get episode, using stored path")
			return task.SourcePath, nil
		}

		season, err := w.repo.GetSeason(ctx, episode.SeasonID)
		if err != nil {
			w.log.Debug().Err(err).Msg("failed to get season, using stored path")
			return task.SourcePath, nil
		}

		seasonNum := int(season.SeasonNumber)
		epNum := int(episode.EpisodeNumber)
		matched := importer.MatchFilesToEpisodes(files, &seasonNum, &epNum)

		if f, ok := matched[epNum]; ok {
			rawPath = f.Path
		} else {
			return "", apperrors.Internalf("no file matched episode S%02dE%02d", seasonNum, epNum).
				Op("ImportWorker.deriveSourcePath").
				NotRetryable()
		}
	}

	// Build absolute path if relative
	if !filepath.IsAbs(rawPath) {
		item, err := client.Get(ctx, *job.DownloaderExternalID)
		if err == nil && item.SavePath != "" {
			rawPath = filepath.Join(item.SavePath, rawPath)
		}
	}

	// Apply path mapping (stub - returns unchanged for now)
	return w.pathMapper.Apply(ctx, job.DownloaderID, rawPath), nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func coalesce(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
