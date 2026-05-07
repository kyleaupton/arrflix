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
	return r.Q.CreateImportTask(ctx, arg)
}

func (r *Repository) GetImportTask(ctx context.Context, id pgtype.UUID) (dbgen.ImportTask, error) {
	return r.Q.GetImportTask(ctx, id)
}

func (r *Repository) GetImportTaskWithDetails(ctx context.Context, id pgtype.UUID) (dbgen.GetImportTaskWithDetailsRow, error) {
	return r.Q.GetImportTaskWithDetails(ctx, id)
}

func (r *Repository) GetImportTaskHistory(ctx context.Context, id pgtype.UUID) ([]dbgen.GetImportTaskHistoryRow, error) {
	return r.Q.GetImportTaskHistory(ctx, id)
}

func (r *Repository) ListImportTasks(ctx context.Context, limit, offset int32) ([]dbgen.ImportTask, error) {
	return r.Q.ListImportTasks(ctx, dbgen.ListImportTasksParams{
		LimitVal:  limit,
		OffsetVal: offset,
	})
}

func (r *Repository) ListImportTasksByDownloadJob(ctx context.Context, downloadJobID pgtype.UUID) ([]dbgen.ImportTask, error) {
	return r.Q.ListImportTasksByDownloadJob(ctx, downloadJobID)
}

func (r *Repository) ListImportTasksByMediaItem(ctx context.Context, mediaItemID pgtype.UUID) ([]dbgen.ImportTask, error) {
	return r.Q.ListImportTasksByMediaItem(ctx, mediaItemID)
}

func (r *Repository) ListImportTasksByEpisode(ctx context.Context, episodeID pgtype.UUID) ([]dbgen.ImportTask, error) {
	return r.Q.ListImportTasksByEpisode(ctx, episodeID)
}

func (r *Repository) ListImportTasksByStatus(ctx context.Context, status string, limit, offset int32) ([]dbgen.ImportTask, error) {
	return r.Q.ListImportTasksByStatus(ctx, dbgen.ListImportTasksByStatusParams{
		Status:    status,
		LimitVal:  limit,
		OffsetVal: offset,
	})
}

func (r *Repository) CountImportTasksByStatus(ctx context.Context) (dbgen.CountImportTasksByStatusRow, error) {
	return r.Q.CountImportTasksByStatus(ctx)
}

func (r *Repository) ClaimRunnableImportTasks(ctx context.Context, limit int32) ([]dbgen.ImportTask, error) {
	return r.Q.ClaimRunnableImportTasks(ctx, limit)
}

func (r *Repository) SetImportTaskInProgress(ctx context.Context, id pgtype.UUID) (dbgen.ImportTask, error) {
	return r.Q.SetImportTaskInProgress(ctx, id)
}

func (r *Repository) SetImportTaskCompleted(ctx context.Context, id pgtype.UUID, destPath, importMethod string, mediaFileID pgtype.UUID) (dbgen.ImportTask, error) {
	return r.Q.SetImportTaskCompleted(ctx, dbgen.SetImportTaskCompletedParams{
		ID:           id,
		DestPath:     &destPath,
		ImportMethod: &importMethod,
		MediaFileID:  mediaFileID,
	})
}

func (r *Repository) SetImportTaskFailed(ctx context.Context, id pgtype.UUID, lastError string, kind apperrors.Kind) (dbgen.ImportTask, error) {
	return r.Q.SetImportTaskFailed(ctx, dbgen.SetImportTaskFailedParams{
		ID:        id,
		LastError: &lastError,
		ErrorKind: nullableKind(kind),
	})
}

func (r *Repository) CancelImportTask(ctx context.Context, id pgtype.UUID) (dbgen.ImportTask, error) {
	return r.Q.CancelImportTask(ctx, id)
}

func (r *Repository) CancelPendingImportTasksForJob(ctx context.Context, downloadJobID pgtype.UUID) error {
	return r.Q.CancelPendingImportTasksForJob(ctx, downloadJobID)
}

func (r *Repository) ScheduleImportTaskRetry(ctx context.Context, id pgtype.UUID, lastError string, kind apperrors.Kind, nextRunAt time.Time) (dbgen.ImportTask, error) {
	return r.Q.ScheduleImportTaskRetry(ctx, dbgen.ScheduleImportTaskRetryParams{
		ID:        id,
		LastError: &lastError,
		ErrorKind: nullableKind(kind),
		NextRunAt: nextRunAt,
	})
}

func (r *Repository) UpdateImportTaskSourcePath(ctx context.Context, id pgtype.UUID, sourcePath string) error {
	return r.Q.UpdateImportTaskSourcePath(ctx, dbgen.UpdateImportTaskSourcePathParams{
		ID:         id,
		SourcePath: sourcePath,
	})
}

func (r *Repository) CreateImportTaskEvent(ctx context.Context, arg dbgen.CreateImportTaskEventParams) (dbgen.ImportTaskEvent, error) {
	return r.Q.CreateImportTaskEvent(ctx, arg)
}

func (r *Repository) ListImportTaskEvents(ctx context.Context, importTaskID pgtype.UUID) ([]dbgen.ImportTaskEvent, error) {
	return r.Q.ListImportTaskEvents(ctx, importTaskID)
}

func (r *Repository) GetImportTaskTimeline(ctx context.Context, importTaskID pgtype.UUID) ([]dbgen.ImportTaskEvent, error) {
	return r.Q.GetImportTaskTimeline(ctx, importTaskID)
}
