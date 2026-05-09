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

type ImportTasksService struct {
	repo *repo.Repository
	sm   *state.ImportTaskMachine
}

func NewImportTasksService(r *repo.Repository) *ImportTasksService {
	return &ImportTasksService{
		repo: r,
		sm:   state.NewImportTaskMachine(),
	}
}

// Get returns an import task by ID.
func (s *ImportTasksService) Get(ctx context.Context, id uuid.UUID) (model.ImportTask, error) {
	return s.repo.GetImportTask(ctx, id)
}

// GetWithDetails returns an import task with related media info.
func (s *ImportTasksService) GetWithDetails(ctx context.Context, id uuid.UUID) (model.ImportTaskWithDetails, error) {
	return s.repo.GetImportTaskWithDetails(ctx, id)
}

// List returns paginated import tasks.
func (s *ImportTasksService) List(ctx context.Context, limit, offset int32) ([]model.ImportTask, error) {
	return s.repo.ListImportTasks(ctx, limit, offset)
}

// ListByStatus returns paginated import tasks filtered by status.
func (s *ImportTasksService) ListByStatus(ctx context.Context, status string, limit, offset int32) ([]model.ImportTask, error) {
	return s.repo.ListImportTasksByStatus(ctx, status, limit, offset)
}

// ListByDownloadJob returns all import tasks for a download job.
func (s *ImportTasksService) ListByDownloadJob(ctx context.Context, jobID uuid.UUID) ([]model.ImportTask, error) {
	return s.repo.ListImportTasksByDownloadJob(ctx, jobID)
}

// ListByMediaItem returns all import tasks for a media item.
func (s *ImportTasksService) ListByMediaItem(ctx context.Context, mediaItemID uuid.UUID) ([]model.ImportTask, error) {
	return s.repo.ListImportTasksByMediaItem(ctx, mediaItemID)
}

// ListByEpisode returns all import tasks for a specific episode.
func (s *ImportTasksService) ListByEpisode(ctx context.Context, episodeID uuid.UUID) ([]model.ImportTask, error) {
	return s.repo.ListImportTasksByEpisode(ctx, episodeID)
}

// CountByStatus returns counts of import tasks grouped by status.
func (s *ImportTasksService) CountByStatus(ctx context.Context) (model.ImportTaskCounts, error) {
	return s.repo.CountImportTasksByStatus(ctx)
}

// GetTimeline returns the event log for an import task.
func (s *ImportTasksService) GetTimeline(ctx context.Context, id uuid.UUID) ([]model.ImportTaskEvent, error) {
	return s.repo.GetImportTaskTimeline(ctx, id)
}

// GetHistory returns the reimport chain for an import task.
func (s *ImportTasksService) GetHistory(ctx context.Context, id uuid.UUID) ([]model.ImportTaskHistoryEntry, error) {
	return s.repo.GetImportTaskHistory(ctx, id)
}

// Cancel cancels a pending import task.
func (s *ImportTasksService) Cancel(ctx context.Context, id uuid.UUID) (model.ImportTask, error) {
	task, err := s.repo.GetImportTask(ctx, id)
	if err != nil {
		return model.ImportTask{}, err
	}

	if !s.sm.CanTransitionStr(task.Status, "cancelled") {
		return model.ImportTask{}, apperrors.Conflictf("import task %s cannot be cancelled from status %q", id, task.Status).
			Op("ImportTasksService.Cancel")
	}

	return s.repo.CancelImportTask(ctx, id)
}

// Reimport creates a new import task for an existing completed or failed task.
// This allows re-importing a file that was previously imported with potentially
// different settings or after fixing an issue.
func (s *ImportTasksService) Reimport(ctx context.Context, id uuid.UUID) (model.ImportTask, error) {
	task, err := s.repo.GetImportTask(ctx, id)
	if err != nil {
		return model.ImportTask{}, err
	}

	if !s.sm.CanReimportStr(task.Status) {
		return model.ImportTask{}, apperrors.Conflictf("import task %s must be completed or failed to reimport (status %q)", id, task.Status).
			Op("ImportTasksService.Reimport")
	}

	// Create new task with previous_task_id set to the existing task ID.
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
		return model.ImportTask{}, err
	}

	// Log the reimport event
	msg := fmt.Sprintf("reimport of task %s", task.ID.String())
	_, _ = s.repo.CreateImportTaskEvent(ctx, repo.CreateImportTaskEventParams{
		ImportTaskID: newTask.ID,
		EventType:    "reimport_requested",
		Message:      &msg,
	})

	return newTask, nil
}
