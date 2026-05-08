package repo

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

type ImportTasksRepo interface {
	CreateImportTask(ctx context.Context, arg dbgen.CreateImportTaskParams) (dbgen.ImportTask, error)
	GetImportTask(ctx context.Context, id pgtype.UUID) (dbgen.ImportTask, error)
	GetImportTaskWithDetails(ctx context.Context, id pgtype.UUID) (dbgen.GetImportTaskWithDetailsRow, error)
	GetImportTaskHistory(ctx context.Context, id pgtype.UUID) ([]dbgen.GetImportTaskHistoryRow, error)
	ListImportTasks(ctx context.Context, limit, offset int32) ([]dbgen.ImportTask, error)
	ListImportTasksByDownloadJob(ctx context.Context, downloadJobID pgtype.UUID) ([]dbgen.ImportTask, error)
	ListImportTasksByMediaItem(ctx context.Context, mediaItemID pgtype.UUID) ([]dbgen.ImportTask, error)
	ListImportTasksByEpisode(ctx context.Context, episodeID pgtype.UUID) ([]dbgen.ImportTask, error)
	ListImportTasksByStatus(ctx context.Context, status string, limit, offset int32) ([]dbgen.ImportTask, error)
	CountImportTasksByStatus(ctx context.Context) (dbgen.CountImportTasksByStatusRow, error)

	ClaimRunnableImportTasks(ctx context.Context, limit int32) ([]dbgen.ImportTask, error)

	SetImportTaskInProgress(ctx context.Context, id pgtype.UUID) (dbgen.ImportTask, error)
	SetImportTaskCompleted(ctx context.Context, id pgtype.UUID, destPath, importMethod string, mediaFileID pgtype.UUID) (dbgen.ImportTask, error)
	SetImportTaskFailed(ctx context.Context, id pgtype.UUID, lastError string, kind apperrors.Kind) (dbgen.ImportTask, error)
	CancelImportTask(ctx context.Context, id pgtype.UUID) (dbgen.ImportTask, error)
	CancelPendingImportTasksForJob(ctx context.Context, downloadJobID pgtype.UUID) error
	ScheduleImportTaskRetry(ctx context.Context, id pgtype.UUID, lastError string, kind apperrors.Kind, nextRunAt time.Time) (dbgen.ImportTask, error)
	UpdateImportTaskSourcePath(ctx context.Context, id pgtype.UUID, sourcePath string) error

	// Event logging
	CreateImportTaskEvent(ctx context.Context, arg dbgen.CreateImportTaskEventParams) (dbgen.ImportTaskEvent, error)
	ListImportTaskEvents(ctx context.Context, importTaskID pgtype.UUID) ([]dbgen.ImportTaskEvent, error)
	GetImportTaskTimeline(ctx context.Context, importTaskID pgtype.UUID) ([]dbgen.ImportTaskEvent, error)
}

func (r *Repository) CreateImportTask(ctx context.Context, arg dbgen.CreateImportTaskParams) (dbgen.ImportTask, error) {
	task, err := r.Q.CreateImportTask(ctx, arg)
	return task, apperrors.FromPg(err, "create import task for download job %s", arg.DownloadJobID)
}

func (r *Repository) GetImportTask(ctx context.Context, id pgtype.UUID) (dbgen.ImportTask, error) {
	task, err := r.Q.GetImportTask(ctx, id)
	return task, apperrors.FromPg(err, "import task %s not found", id)
}

func (r *Repository) GetImportTaskWithDetails(ctx context.Context, id pgtype.UUID) (dbgen.GetImportTaskWithDetailsRow, error) {
	task, err := r.Q.GetImportTaskWithDetails(ctx, id)
	return task, apperrors.FromPg(err, "import task %s not found", id)
}

func (r *Repository) GetImportTaskHistory(ctx context.Context, id pgtype.UUID) ([]dbgen.GetImportTaskHistoryRow, error) {
	rows, err := r.Q.GetImportTaskHistory(ctx, id)
	return rows, apperrors.FromPg(err, "import task %s history", id)
}

func (r *Repository) ListImportTasks(ctx context.Context, limit, offset int32) ([]dbgen.ImportTask, error) {
	tasks, err := r.Q.ListImportTasks(ctx, dbgen.ListImportTasksParams{
		LimitVal:  limit,
		OffsetVal: offset,
	})
	return tasks, apperrors.FromPg(err, "list import tasks")
}

func (r *Repository) ListImportTasksByDownloadJob(ctx context.Context, downloadJobID pgtype.UUID) ([]dbgen.ImportTask, error) {
	tasks, err := r.Q.ListImportTasksByDownloadJob(ctx, downloadJobID)
	return tasks, apperrors.FromPg(err, "list import tasks for download job %s", downloadJobID)
}

func (r *Repository) ListImportTasksByMediaItem(ctx context.Context, mediaItemID pgtype.UUID) ([]dbgen.ImportTask, error) {
	tasks, err := r.Q.ListImportTasksByMediaItem(ctx, mediaItemID)
	return tasks, apperrors.FromPg(err, "list import tasks for media item %s", mediaItemID)
}

func (r *Repository) ListImportTasksByEpisode(ctx context.Context, episodeID pgtype.UUID) ([]dbgen.ImportTask, error) {
	tasks, err := r.Q.ListImportTasksByEpisode(ctx, episodeID)
	return tasks, apperrors.FromPg(err, "list import tasks for episode %s", episodeID)
}

func (r *Repository) ListImportTasksByStatus(ctx context.Context, status string, limit, offset int32) ([]dbgen.ImportTask, error) {
	tasks, err := r.Q.ListImportTasksByStatus(ctx, dbgen.ListImportTasksByStatusParams{
		Status:    status,
		LimitVal:  limit,
		OffsetVal: offset,
	})
	return tasks, apperrors.FromPg(err, "list import tasks with status %s", status)
}

func (r *Repository) CountImportTasksByStatus(ctx context.Context) (dbgen.CountImportTasksByStatusRow, error) {
	row, err := r.Q.CountImportTasksByStatus(ctx)
	return row, apperrors.FromPg(err, "count import tasks by status")
}

func (r *Repository) ClaimRunnableImportTasks(ctx context.Context, limit int32) ([]dbgen.ImportTask, error) {
	tasks, err := r.Q.ClaimRunnableImportTasks(ctx, limit)
	return tasks, apperrors.FromPg(err, "claim runnable import tasks")
}

func (r *Repository) SetImportTaskInProgress(ctx context.Context, id pgtype.UUID) (dbgen.ImportTask, error) {
	task, err := r.Q.SetImportTaskInProgress(ctx, id)
	return task, apperrors.FromPg(err, "mark import task %s in progress", id)
}

func (r *Repository) SetImportTaskCompleted(ctx context.Context, id pgtype.UUID, destPath, importMethod string, mediaFileID pgtype.UUID) (dbgen.ImportTask, error) {
	task, err := r.Q.SetImportTaskCompleted(ctx, dbgen.SetImportTaskCompletedParams{
		ID:           id,
		DestPath:     &destPath,
		ImportMethod: &importMethod,
		MediaFileID:  mediaFileID,
	})
	return task, apperrors.FromPg(err, "mark import task %s completed", id)
}

func (r *Repository) SetImportTaskFailed(ctx context.Context, id pgtype.UUID, lastError string, kind apperrors.Kind) (dbgen.ImportTask, error) {
	task, err := r.Q.SetImportTaskFailed(ctx, dbgen.SetImportTaskFailedParams{
		ID:        id,
		LastError: &lastError,
		ErrorKind: nullableKind(kind),
	})
	return task, apperrors.FromPg(err, "mark import task %s failed", id)
}

func (r *Repository) CancelImportTask(ctx context.Context, id pgtype.UUID) (dbgen.ImportTask, error) {
	task, err := r.Q.CancelImportTask(ctx, id)
	return task, apperrors.FromPg(err, "cancel import task %s", id)
}

func (r *Repository) CancelPendingImportTasksForJob(ctx context.Context, downloadJobID pgtype.UUID) error {
	return apperrors.FromPg(r.Q.CancelPendingImportTasksForJob(ctx, downloadJobID), "cancel pending import tasks for download job %s", downloadJobID)
}

func (r *Repository) ScheduleImportTaskRetry(ctx context.Context, id pgtype.UUID, lastError string, kind apperrors.Kind, nextRunAt time.Time) (dbgen.ImportTask, error) {
	task, err := r.Q.ScheduleImportTaskRetry(ctx, dbgen.ScheduleImportTaskRetryParams{
		ID:        id,
		LastError: &lastError,
		ErrorKind: nullableKind(kind),
		NextRunAt: nextRunAt,
	})
	return task, apperrors.FromPg(err, "schedule retry for import task %s", id)
}

func (r *Repository) UpdateImportTaskSourcePath(ctx context.Context, id pgtype.UUID, sourcePath string) error {
	return apperrors.FromPg(r.Q.UpdateImportTaskSourcePath(ctx, dbgen.UpdateImportTaskSourcePathParams{
		ID:         id,
		SourcePath: sourcePath,
	}), "update source path for import task %s", id)
}

func (r *Repository) CreateImportTaskEvent(ctx context.Context, arg dbgen.CreateImportTaskEventParams) (dbgen.ImportTaskEvent, error) {
	ev, err := r.Q.CreateImportTaskEvent(ctx, arg)
	return ev, apperrors.FromPg(err, "create import task %s event", arg.ImportTaskID)
}

func (r *Repository) ListImportTaskEvents(ctx context.Context, importTaskID pgtype.UUID) ([]dbgen.ImportTaskEvent, error) {
	events, err := r.Q.ListImportTaskEvents(ctx, importTaskID)
	return events, apperrors.FromPg(err, "list events for import task %s", importTaskID)
}

func (r *Repository) GetImportTaskTimeline(ctx context.Context, importTaskID pgtype.UUID) ([]dbgen.ImportTaskEvent, error) {
	events, err := r.Q.GetImportTaskTimeline(ctx, importTaskID)
	return events, apperrors.FromPg(err, "import task %s timeline", importTaskID)
}
