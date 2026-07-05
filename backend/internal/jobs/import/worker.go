// Package importw implements the import worker that processes import tasks
// (hardlinks/copies files from downloads to library).
package importw

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/downloader"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/importer"
	"github.com/kyleaupton/arrflix/internal/jobs/jobutil"
	"github.com/kyleaupton/arrflix/internal/jobs/state"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/mediainfo"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/osdb"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/pathmapping"
	"github.com/kyleaupton/arrflix/internal/realtime"
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

	// Compute destination path using name template. The returned parse is the
	// release the naming derived from — stamped onto file_origin below so the
	// provenance survives download_job pruning without a re-parse.
	destPath, parsedRelease, err := w.computeDestPath(task, taskDetails, mi)
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

	// Verify the placed file is present and non-empty BEFORE recording it.
	// HardlinkOrCopy can report success while the file is missing or zero-length
	// (overlay/network FS hiccup, a disk that errored). Committing a file row +
	// 'available' want for a file that isn't really there is the divergence this
	// guards against: failing here means no file row is written, so the DB and
	// disk stay consistent — the import retries, and the want only advances once
	// the bytes are confirmed on disk. This is the VerifyStep, run as a precondition
	// of the commit rather than after it.
	if info, serr := os.Stat(fullDest); serr != nil || info.Size() == 0 {
		return apperrors.Internalf("verify failed: imported file missing or empty: %s", fullDest).
			Op("ImportWorker.processTask")
	}

	var episodeIDPtr *uuid.UUID
	if task.EpisodeID != uuid.Nil {
		ep := task.EpisodeID
		episodeIDPtr = &ep
	}

	// On tx failure the hardlink is left as an orphan; a future scan picks it up.
	err = w.repo.InTx(ctx, func(r *repo.Repository) error {
		// Closed-world create: the import knows the identity up front, so
		// the file row is born with media_item_id / episode_id set.
		mediaItemID := task.MediaItemID
		file, ferr := r.CreateFile(ctx, repo.CreateFileParams{
			ID:          uuid.New(),
			LibraryID:   task.LibraryID,
			Path:        destPath,
			MediaItemID: &mediaItemID,
			EpisodeID:   episodeIDPtr,
		})
		if ferr != nil {
			return ferr
		}

		fileSize := srcInfo.Size()

		// Best-effort: the import is byte-identical to the source, so hashing
		// the dest matches scan-discovered files. Nullable column; never fatal.
		var osdbHash *string
		if h, herr := osdb.Hash(fullDest); herr == nil {
			osdbHash = &h
		} else {
			w.log.Debug().Err(herr).Str("dest", fullDest).Msg("osdb hash skipped")
		}

		if _, serr := r.UpsertFileState(ctx, repo.UpsertFileStateParams{
			FileID:    file.ID,
			Exists:    true,
			SizeBytes: &fileSize,
			OsdbHash:  osdbHash,
		}); serr != nil {
			return serr
		}

		if _, ierr := r.CreateFileImport(ctx, repo.CreateFileImportParams{
			FileID:       file.ID,
			ImportTaskID: task.ID,
			Method:       method,
			SourcePath:   &task.SourcePath,
			DestPath:     destPath,
			Success:      true,
			ErrorMessage: nil,
		}); ierr != nil {
			return ierr
		}

		// Stamp durable provenance: origin='grab', with the release title the
		// pick+naming used and the grab's indexer/guid/download_job backref. This
		// is the only path that persists the parsed quality/release attributes the
		// decision otherwise discards.
		domain := parsing.DomainSeries
		if task.MediaType == "movie" {
			domain = parsing.DomainMovie
		}
		// nil on the interactive/legacy path (no download_job) so the nullable FK
		// stays NULL rather than referencing a zero UUID; indexer/guid are already
		// nil there (LEFT JOIN).
		var dlJobID *uuid.UUID
		if task.DownloadJobID != uuid.Nil {
			id := task.DownloadJobID
			dlJobID = &id
		}
		if oerr := r.CreateFileOriginIfAbsent(ctx, jobutil.FileOriginParams(
			file.ID, "grab", jobutil.Deref(taskDetails.CandidateTitle),
			parsedRelease, domain, dlJobID,
			taskDetails.DownloadIndexerID, taskDetails.DownloadGuid,
		)); oerr != nil {
			return oerr
		}

		if _, terr := r.SetImportTaskCompleted(ctx, repo.SetImportTaskCompletedParams{
			ID:           task.ID,
			DestPath:     destPath,
			ImportMethod: method,
			FileID:       file.ID,
		}); terr != nil {
			return terr
		}

		// want→imported commits atomically with task completion + the file row,
		// via the tx-bound r. Guarded so the interactive/legacy path (no want)
		// is untouched. The mirror routes through MirrorWantStatus's
		// terminal-sticky guard: if the want was canceled mid-flight, the file
		// import still commits but the want stays 'canceled' (ok==false) rather
		// than being resurrected to 'imported'.
		if task.WantID != uuid.Nil {
			if _, _, werr := r.MirrorWantStatus(ctx, task.WantID, string(model.WantImported)); werr != nil {
				return werr
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Presence/size was confirmed before the file row committed (see the verify
	// guard above), so the want advances 'imported' → 'available' — Arrflix's own
	// authority that the file is on disk, reachable with no media server. MirrorWant
	// no-ops on the interactive/legacy path (no want).
	jobutil.MirrorWant(ctx, w.repo, w.broker, w.log, task.WantID, model.WantAvailable)

	w.logEvent(ctx, task.ID, "status_changed", "", map[string]any{
		"old_status":    "in_progress",
		"new_status":    "completed",
		"dest_path":     destPath,
		"import_method": method,
	})

	w.publishTaskUpdated(ctx, task)
	return nil
}

// computeDestPath renders the destination relpath and returns the ParsedRelease
// it derived from the candidate title along the way. The parse is returned (not
// re-derived) so the caller can stamp file_origin from the exact release the
// naming used — re-parsing the same string would be redundant.
func (w *Worker) computeDestPath(task model.ImportTask, details model.ImportTaskWithDetails, mi *model.MediaInfoFields) (string, parsing.ParsedRelease, error) {
	srcExt := filepath.Ext(task.SourcePath)

	// Build evaluation context for template rendering
	candidateTitle := ""
	if details.CandidateTitle != nil {
		candidateTitle = *details.CandidateTitle
	}

	// Domain is known: the task is scoped to a single media type. Pass it to
	// Parse — it drives both the identity pattern set and the bin vocabulary
	// the rendered quality.name / quality.full use. This re-parses the same
	// candidate_title the acquisition pick parsed; Parse is pure, so the rendered
	// quality matches the selection by construction (no need to thread the picked
	// Subject through — re-parsing a string is cheaper than persisting it).
	domain := parsing.DomainSeries
	if task.MediaType == "movie" {
		domain = parsing.DomainMovie
	}
	q := parsing.Parse(candidateTitle, domain)
	candidate := model.DownloadCandidate{
		Title: candidateTitle,
	}
	evalCtx := model.NewSubject(candidate, q)

	// Add media metadata
	year := 0
	if details.MediaYear != nil {
		year = int(*details.MediaYear)
	}
	tmdbID := int64(0)
	if details.MediaTmdbID != nil {
		tmdbID = *details.MediaTmdbID
	}

	// Runtime is nil here: the import path renders the template from the parsed
	// title plus ffprobe mediainfo and runs no size-band gating, so the matched
	// media's runtime isn't threaded through the task. (The import-time re-gate
	// the acquisition spec describes is not wired in yet — when it lands, it will
	// supply asserted attributes here.)
	if task.MediaType == "movie" {
		evalCtx = evalCtx.WithMedia(model.MediaTypeMovie, details.MediaTitle, year, tmdbID, nil)
	} else {
		evalCtx = evalCtx.WithMedia(model.MediaTypeSeries, details.MediaTitle, year, tmdbID, nil)
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
		showPart, err := template.Render(jobutil.Deref(details.SeriesShowTemplate), templateData)
		if err != nil {
			return "", q, apperrors.Internalf("render show template: %v", err).
				Op("ImportWorker.computeDestPath").
				NotRetryable()
		}
		seasonPart, err := template.Render(jobutil.Deref(details.SeriesSeasonTemplate), templateData)
		if err != nil {
			return "", q, apperrors.Internalf("render season template: %v", err).
				Op("ImportWorker.computeDestPath").
				NotRetryable()
		}
		filePart, err := template.Render(details.NameTemplate, templateData)
		if err != nil {
			return "", q, apperrors.Internalf("render file template: %v", err).
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
				return "", q, apperrors.Internalf("render movie dir template: %v", err).
					Op("ImportWorker.computeDestPath").
					NotRetryable()
			}
		}
		filePart, err := template.Render(details.NameTemplate, templateData)
		if err != nil {
			return "", q, apperrors.Internalf("render file template: %v", err).
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
	return rel, q, nil
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

	// Import failures terminally fail the want (both sites below), deliberately
	// unlike download-worker recovery (failJobAndRecoverWants): a filesystem/import
	// error isn't fixed by re-searching a different release, so excluding-and-
	// re-searching here would just re-download into the same broken import. The
	// re-gate phase revisits these (its exclusion reason 'regate_failed' is already
	// reserved in the schema).
	//
	// Non-retryable errors fail immediately.
	if !apperrors.IsRetryable(err) {
		_, _ = w.repo.SetImportTaskFailed(ctx, task.ID, msg, kind)
		jobutil.MirrorWant(ctx, w.repo, w.broker, w.log, task.WantID, model.WantFailed)
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
		jobutil.MirrorWant(ctx, w.repo, w.broker, w.log, task.WantID, model.WantFailed)
		w.publishTaskUpdated(ctx, task)
		return
	}

	// Schedule retry with exponential backoff
	backoff := jobutil.Backoff(attempt)
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
		Message:      jobutil.StrPtr(message),
		Metadata:     metaBytes,
	})
	if err != nil {
		w.log.Warn().Err(err).Msg("failed to log import task event")
	}
}

func (w *Worker) publishTaskUpdated(ctx context.Context, task model.ImportTask) {
	realtime.Emit(ctx, w.broker, realtime.ImportTaskUpdated(task.ID))
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
	realtime.Emit(ctx, w.broker, realtime.DownloadJobUpdated(enriched))
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
