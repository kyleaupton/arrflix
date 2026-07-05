package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/jobs/state"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

type DownloadJobsService struct {
	repo *repo.Repository
	sm   *state.DownloadJobMachine
}

func NewDownloadJobsService(r *repo.Repository) *DownloadJobsService {
	return &DownloadJobsService{repo: r, sm: state.NewDownloadJobMachine()}
}

func (s *DownloadJobsService) Create(ctx context.Context, params repo.CreateDownloadJobParams) (model.DownloadJob, error) {
	return s.repo.CreateDownloadJob(ctx, params)
}

func (s *DownloadJobsService) Get(ctx context.Context, id uuid.UUID) (model.DownloadJob, error) {
	return s.repo.GetDownloadJob(ctx, id)
}

// GetWithImportSummary returns a download job with computed import status summary.
func (s *DownloadJobsService) GetWithImportSummary(ctx context.Context, id uuid.UUID) (model.DownloadJobWithSummary, error) {
	return s.repo.GetDownloadJobWithImportSummary(ctx, id)
}

// GetTimeline returns the combined event log for a download job (download events + import events).
func (s *DownloadJobsService) GetTimeline(ctx context.Context, id uuid.UUID) ([]model.DownloadJobTimelineEvent, error) {
	return s.repo.GetDownloadJobTimeline(ctx, id)
}

func (s *DownloadJobsService) List(ctx context.Context) ([]model.DownloadJob, error) {
	return s.repo.ListDownloadJobs(ctx)
}

// ListWithImportSummary returns all download jobs with computed import status summary.
func (s *DownloadJobsService) ListWithImportSummary(ctx context.Context) ([]model.DownloadJobWithSummary, error) {
	return s.repo.ListDownloadJobsWithImportSummary(ctx)
}

func (s *DownloadJobsService) ListByMovie(ctx context.Context, tmdbMovieID int64) ([]model.DownloadJob, error) {
	return s.repo.ListDownloadJobsByTmdbMovieID(ctx, tmdbMovieID)
}

func (s *DownloadJobsService) ListBySeries(ctx context.Context, tmdbSeriesID int64) ([]model.DownloadJobBySeriesEntry, error) {
	return s.repo.ListDownloadJobsByTmdbSeriesID(ctx, tmdbSeriesID)
}

// Cancel cancels a download job and all its pending import tasks.
func (s *DownloadJobsService) Cancel(ctx context.Context, id uuid.UUID) error {
	if _, err := s.repo.CancelDownloadJob(ctx, id); err != nil {
		return err
	}

	// Also cancel all pending import tasks for this job
	if err := s.repo.CancelPendingImportTasksForJob(ctx, id); err != nil {
		// Log but don't fail the cancel operation
		_ = err
	}

	return nil
}

// ListImportTasks returns all import tasks for a download job.
func (s *DownloadJobsService) ListImportTasks(ctx context.Context, jobID uuid.UUID) ([]model.ImportTask, error) {
	return s.repo.ListImportTasksByDownloadJob(ctx, jobID)
}

// RetryDownload creates a new download job from a failed one, linking via previous_job_id.
//
// Post-A2 a failed job's wants are already recovering on their own (the download
// worker excluded the release and returned them to 'pending' to re-search). A user
// retry copies the want links onto the successor job, so a want can briefly race
// two acquisitions — the successor's grab and the autonomous re-search. The grab
// CAS and alreadyCoveredByPack keep that from double-filing, bounding the blast
// radius, but it's a known rough edge; the real fix belongs with a retry/rearm
// rework that reconciles the two paths.
func (s *DownloadJobsService) RetryDownload(ctx context.Context, id uuid.UUID) (model.DownloadJobWithSummary, error) {
	// Fetch existing job to validate state
	job, err := s.repo.GetDownloadJob(ctx, id)
	if err != nil {
		return model.DownloadJobWithSummary{}, err
	}

	if !s.sm.CanRetryStr(job.Status) {
		return model.DownloadJobWithSummary{}, apperrors.Conflictf("download job %s cannot be retried from status %q", id, job.Status).
			Op("DownloadJobsService.RetryDownload")
	}

	newJob, err := s.repo.RetryDownloadJob(ctx, id)
	if err != nil {
		return model.DownloadJobWithSummary{}, err
	}

	// Log retry_requested event on the new job
	msg := fmt.Sprintf("retry of job %s", id.String())
	_, _ = s.repo.CreateDownloadJobEvent(ctx, repo.CreateDownloadJobEventParams{
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
func (s *DownloadJobsService) GetHistory(ctx context.Context, id uuid.UUID) ([]model.DownloadJobHistoryEntry, error) {
	return s.repo.GetDownloadJobHistory(ctx, id)
}

// ReimportResult contains the result of a reimport operation.
type ReimportResult struct {
	CreatedTasks []model.ImportTask `json:"created_tasks"`
	SkippedCount int                `json:"skipped_count"`
}

// ReimportFailed creates new import tasks for failed (or all terminal) tasks of a download job.
// If all is false, only failed tasks are reimported. If all is true, all terminal tasks (completed, failed, cancelled) are reimported.
func (s *DownloadJobsService) ReimportFailed(ctx context.Context, jobID uuid.UUID, all bool) (ReimportResult, error) {
	tasks, err := s.repo.ListImportTasksByDownloadJob(ctx, jobID)
	if err != nil {
		return ReimportResult{}, err
	}

	// Filter to root tasks only (no previous_task_id) and terminal states
	var toReimport []model.ImportTask
	skippedCount := 0
	for _, task := range tasks {
		// Only reimport root tasks
		if task.PreviousTaskID != uuid.Nil {
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

	var createdTasks []model.ImportTask
	for _, task := range toReimport {
		newTask, err := s.repo.CreateImportTask(ctx, repo.CreateImportTaskParams{
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
			return ReimportResult{}, err
		}

		// Log the reimport event
		msg := fmt.Sprintf("reimport of task %s", task.ID.String())
		_, _ = s.repo.CreateImportTaskEvent(ctx, repo.CreateImportTaskEventParams{
			ImportTaskID: newTask.ID,
			EventType:    "reimport_requested",
			Message:      &msg,
		})

		createdTasks = append(createdTasks, newTask)
	}

	return ReimportResult{
		CreatedTasks: createdTasks,
		SkippedCount: skippedCount,
	}, nil
}
