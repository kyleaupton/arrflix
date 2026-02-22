package service

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	"github.com/kyleaupton/arrflix/internal/jobs/state"
	"github.com/kyleaupton/arrflix/internal/repo"
)

type DownloadJobsService struct {
	repo *repo.Repository
	sm   *state.DownloadJobMachine
}

func NewDownloadJobsService(r *repo.Repository) *DownloadJobsService {
	return &DownloadJobsService{repo: r, sm: state.NewDownloadJobMachine()}
}

func (s *DownloadJobsService) Create(ctx context.Context, arg dbgen.CreateDownloadJobParams) (dbgen.DownloadJob, error) {
	return s.repo.CreateDownloadJob(ctx, arg)
}

func (s *DownloadJobsService) Get(ctx context.Context, id pgtype.UUID) (dbgen.DownloadJob, error) {
	return s.repo.GetDownloadJob(ctx, id)
}

// GetWithImportSummary returns a download job with computed import status summary.
func (s *DownloadJobsService) GetWithImportSummary(ctx context.Context, id pgtype.UUID) (dbgen.GetDownloadJobWithImportSummaryRow, error) {
	return s.repo.GetDownloadJobWithImportSummary(ctx, id)
}

// GetTimeline returns the combined event log for a download job (download events + import events).
func (s *DownloadJobsService) GetTimeline(ctx context.Context, id pgtype.UUID) ([]dbgen.GetDownloadJobTimelineRow, error) {
	return s.repo.GetDownloadJobTimeline(ctx, id)
}

func (s *DownloadJobsService) List(ctx context.Context) ([]dbgen.DownloadJob, error) {
	return s.repo.ListDownloadJobs(ctx)
}

// ListWithImportSummary returns all download jobs with computed import status summary.
func (s *DownloadJobsService) ListWithImportSummary(ctx context.Context) ([]dbgen.ListDownloadJobsWithImportSummaryRow, error) {
	return s.repo.ListDownloadJobsWithImportSummary(ctx)
}

func (s *DownloadJobsService) ListByMovie(ctx context.Context, tmdbMovieID int64) ([]dbgen.DownloadJob, error) {
	return s.repo.ListDownloadJobsByTmdbMovieID(ctx, tmdbMovieID)
}

func (s *DownloadJobsService) ListBySeries(ctx context.Context, tmdbSeriesID int64) ([]dbgen.ListDownloadJobsByTmdbSeriesIDRow, error) {
	return s.repo.ListDownloadJobsByTmdbSeriesID(ctx, tmdbSeriesID)
}

// Cancel cancels a download job and all its pending import tasks.
func (s *DownloadJobsService) Cancel(ctx context.Context, id pgtype.UUID) (dbgen.DownloadJob, error) {
	job, err := s.repo.CancelDownloadJob(ctx, id)
	if err != nil {
		return dbgen.DownloadJob{}, fmt.Errorf("cancel job: %w", err)
	}

	// Also cancel all pending import tasks for this job
	if err := s.repo.CancelPendingImportTasksForJob(ctx, id); err != nil {
		// Log but don't fail the cancel operation
		_ = err
	}

	return job, nil
}

// ListImportTasks returns all import tasks for a download job.
func (s *DownloadJobsService) ListImportTasks(ctx context.Context, jobID pgtype.UUID) ([]dbgen.ImportTask, error) {
	return s.repo.ListImportTasksByDownloadJob(ctx, jobID)
}

// RetryDownload creates a new download job from a failed one, linking via previous_job_id.
func (s *DownloadJobsService) RetryDownload(ctx context.Context, id pgtype.UUID) (dbgen.GetDownloadJobWithImportSummaryRow, error) {
	// Fetch existing job to validate state
	job, err := s.repo.GetDownloadJob(ctx, id)
	if err != nil {
		return dbgen.GetDownloadJobWithImportSummaryRow{}, fmt.Errorf("get job: %w", err)
	}

	if !s.sm.CanRetryStr(job.Status) {
		return dbgen.GetDownloadJobWithImportSummaryRow{}, fmt.Errorf("cannot retry job in status %q", job.Status)
	}

	newJob, err := s.repo.RetryDownloadJob(ctx, id)
	if err != nil {
		return dbgen.GetDownloadJobWithImportSummaryRow{}, fmt.Errorf("retry job: %w", err)
	}

	// Log retry_requested event on the new job
	msg := fmt.Sprintf("retry of job %s", id.String())
	_, _ = s.repo.CreateDownloadJobEvent(ctx, dbgen.CreateDownloadJobEventParams{
		DownloadJobID: newJob.ID,
		EventType:     "retry_requested",
		OldStatus:     nil,
		NewStatus:     nil,
		Message:       &msg,
		Metadata:      nil,
	})

	return s.repo.GetDownloadJobWithImportSummary(ctx, newJob.ID)
}

// GetHistory returns the retry chain for a download job.
func (s *DownloadJobsService) GetHistory(ctx context.Context, id pgtype.UUID) ([]dbgen.GetDownloadJobHistoryRow, error) {
	return s.repo.GetDownloadJobHistory(ctx, id)
}

// ReimportResult contains the result of a reimport operation.
type ReimportResult struct {
	CreatedTasks []dbgen.ImportTask `json:"created_tasks"`
	SkippedCount int                `json:"skipped_count"`
}

// ReimportFailed creates new import tasks for failed (or all terminal) tasks of a download job.
// If all is false, only failed tasks are reimported. If all is true, all terminal tasks (completed, failed, cancelled) are reimported.
func (s *DownloadJobsService) ReimportFailed(ctx context.Context, jobID pgtype.UUID, all bool) (ReimportResult, error) {
	tasks, err := s.repo.ListImportTasksByDownloadJob(ctx, jobID)
	if err != nil {
		return ReimportResult{}, fmt.Errorf("list import tasks: %w", err)
	}

	// Filter to root tasks only (no previous_task_id) and terminal states
	var toReimport []dbgen.ImportTask
	skippedCount := 0
	for _, task := range tasks {
		// Only reimport root tasks
		if task.PreviousTaskID.Valid {
			continue
		}

		// Check status
		isTerminal := task.Status == "completed" || task.Status == "failed" || task.Status == "cancelled"
		isFailed := task.Status == "failed"

		if !isTerminal {
			skippedCount++
			continue
		}

		if all || isFailed {
			toReimport = append(toReimport, task)
		} else {
			skippedCount++
		}
	}

	var createdTasks []dbgen.ImportTask
	for _, task := range toReimport {
		newTask, err := s.repo.CreateImportTask(ctx, dbgen.CreateImportTaskParams{
			DownloadJobID:  task.DownloadJobID,
			SourcePath:     task.SourcePath,
			PreviousTaskID: task.ID,
			MediaType:      task.MediaType,
			MediaItemID:    task.MediaItemID,
			EpisodeID:      task.EpisodeID,
			LibraryID:      task.LibraryID,
			NameTemplateID: task.NameTemplateID,
		})
		if err != nil {
			return ReimportResult{}, fmt.Errorf("create reimport task: %w", err)
		}

		// Log the reimport event
		msg := fmt.Sprintf("reimport of task %s", task.ID.String())
		_, _ = s.repo.CreateImportTaskEvent(ctx, dbgen.CreateImportTaskEventParams{
			ImportTaskID: newTask.ID,
			EventType:    "reimport_requested",
			OldStatus:    nil,
			NewStatus:    nil,
			Message:      &msg,
			Metadata:     nil,
		})

		createdTasks = append(createdTasks, newTask)
	}

	return ReimportResult{
		CreatedTasks: createdTasks,
		SkippedCount: skippedCount,
	}, nil
}
