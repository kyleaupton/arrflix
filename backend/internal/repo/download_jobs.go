package repo

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

type DownloadJobsRepo interface {
	CreateDownloadJob(ctx context.Context, arg dbgen.CreateDownloadJobParams) (dbgen.DownloadJob, error)
	GetDownloadJob(ctx context.Context, id pgtype.UUID) (dbgen.DownloadJob, error)
	GetDownloadJobByCandidate(ctx context.Context, indexerID int64, guid string) (dbgen.DownloadJob, error)
	GetDownloadJobWithImportSummary(ctx context.Context, id pgtype.UUID) (dbgen.GetDownloadJobWithImportSummaryRow, error)
	GetDownloadJobTimeline(ctx context.Context, downloadJobID pgtype.UUID) ([]dbgen.GetDownloadJobTimelineRow, error)
	ListDownloadJobsByMediaItem(ctx context.Context, mediaItemID pgtype.UUID) ([]dbgen.DownloadJob, error)
	ListDownloadJobsByTmdbMovieID(ctx context.Context, tmdbMovieID int64) ([]dbgen.DownloadJob, error)
	ListDownloadJobsByTmdbSeriesID(ctx context.Context, tmdbSeriesID int64) ([]dbgen.ListDownloadJobsByTmdbSeriesIDRow, error)
	ListDownloadJobs(ctx context.Context) ([]dbgen.DownloadJob, error)
	ListDownloadJobsWithImportSummary(ctx context.Context) ([]dbgen.ListDownloadJobsWithImportSummaryRow, error)
	CancelDownloadJob(ctx context.Context, id pgtype.UUID) (dbgen.DownloadJob, error)

	ClaimRunnableDownloadJobs(ctx context.Context, limit int32) ([]dbgen.DownloadJob, error)

	SetDownloadJobEnqueued(ctx context.Context, id pgtype.UUID, downloaderExternalID string) (dbgen.DownloadJob, error)
	SetDownloadJobDownloadSnapshot(ctx context.Context, arg dbgen.SetDownloadJobDownloadSnapshotParams) (dbgen.DownloadJob, error)
	SetDownloadJobCompleted(ctx context.Context, id pgtype.UUID, savePath, contentPath string) (dbgen.DownloadJob, error)

	ScheduleDownloadJobRetry(ctx context.Context, id pgtype.UUID, lastError string, kind apperrors.Kind, nextRunAt time.Time) (dbgen.DownloadJob, error)
	MarkDownloadJobFailed(ctx context.Context, id pgtype.UUID, lastError string, kind apperrors.Kind) (dbgen.DownloadJob, error)

	RetryDownloadJob(ctx context.Context, id pgtype.UUID) (dbgen.DownloadJob, error)
	GetDownloadJobHistory(ctx context.Context, id pgtype.UUID) ([]dbgen.GetDownloadJobHistoryRow, error)

	// Event logging
	CreateDownloadJobEvent(ctx context.Context, arg dbgen.CreateDownloadJobEventParams) (dbgen.DownloadJobEvent, error)
	ListDownloadJobEvents(ctx context.Context, downloadJobID pgtype.UUID) ([]dbgen.DownloadJobEvent, error)
}

func (r *Repository) CreateDownloadJob(ctx context.Context, arg dbgen.CreateDownloadJobParams) (dbgen.DownloadJob, error) {
	return r.Q.CreateDownloadJob(ctx, arg)
}

func (r *Repository) GetDownloadJob(ctx context.Context, id pgtype.UUID) (dbgen.DownloadJob, error) {
	return r.Q.GetDownloadJob(ctx, id)
}

func (r *Repository) GetDownloadJobByCandidate(ctx context.Context, indexerID int64, guid string) (dbgen.DownloadJob, error) {
	return r.Q.GetDownloadJobByCandidate(ctx, dbgen.GetDownloadJobByCandidateParams{
		IndexerID: indexerID,
		Guid:      guid,
	})
}

func (r *Repository) GetDownloadJobWithImportSummary(ctx context.Context, id pgtype.UUID) (dbgen.GetDownloadJobWithImportSummaryRow, error) {
	return r.Q.GetDownloadJobWithImportSummary(ctx, id)
}

func (r *Repository) GetDownloadJobTimeline(ctx context.Context, downloadJobID pgtype.UUID) ([]dbgen.GetDownloadJobTimelineRow, error) {
	return r.Q.GetDownloadJobTimeline(ctx, downloadJobID)
}

func (r *Repository) ListDownloadJobsByMediaItem(ctx context.Context, mediaItemID pgtype.UUID) ([]dbgen.DownloadJob, error) {
	return r.Q.ListDownloadJobsByMediaItem(ctx, mediaItemID)
}

func (r *Repository) ListDownloadJobsByTmdbMovieID(ctx context.Context, tmdbMovieID int64) ([]dbgen.DownloadJob, error) {
	return r.Q.ListDownloadJobsByTmdbMovieID(ctx, &tmdbMovieID)
}

func (r *Repository) ListDownloadJobsByTmdbSeriesID(ctx context.Context, tmdbSeriesID int64) ([]dbgen.ListDownloadJobsByTmdbSeriesIDRow, error) {
	return r.Q.ListDownloadJobsByTmdbSeriesID(ctx, &tmdbSeriesID)
}

func (r *Repository) ListDownloadJobs(ctx context.Context) ([]dbgen.DownloadJob, error) {
	return r.Q.ListDownloadJobs(ctx)
}

func (r *Repository) ListDownloadJobsWithImportSummary(ctx context.Context) ([]dbgen.ListDownloadJobsWithImportSummaryRow, error) {
	return r.Q.ListDownloadJobsWithImportSummary(ctx)
}

func (r *Repository) CancelDownloadJob(ctx context.Context, id pgtype.UUID) (dbgen.DownloadJob, error) {
	return r.Q.CancelDownloadJob(ctx, id)
}

func (r *Repository) ClaimRunnableDownloadJobs(ctx context.Context, limit int32) ([]dbgen.DownloadJob, error) {
	return r.Q.ClaimRunnableDownloadJobs(ctx, limit)
}

func (r *Repository) SetDownloadJobEnqueued(ctx context.Context, id pgtype.UUID, downloaderExternalID string) (dbgen.DownloadJob, error) {
	return r.Q.SetDownloadJobEnqueued(ctx, dbgen.SetDownloadJobEnqueuedParams{
		ID:                   id,
		DownloaderExternalID: &downloaderExternalID,
	})
}

func (r *Repository) SetDownloadJobDownloadSnapshot(ctx context.Context, arg dbgen.SetDownloadJobDownloadSnapshotParams) (dbgen.DownloadJob, error) {
	return r.Q.SetDownloadJobDownloadSnapshot(ctx, arg)
}

func (r *Repository) SetDownloadJobCompleted(ctx context.Context, id pgtype.UUID, savePath, contentPath string) (dbgen.DownloadJob, error) {
	return r.Q.SetDownloadJobCompleted(ctx, dbgen.SetDownloadJobCompletedParams{
		ID:          id,
		SavePath:    &savePath,
		ContentPath: &contentPath,
	})
}

func (r *Repository) ScheduleDownloadJobRetry(ctx context.Context, id pgtype.UUID, lastError string, kind apperrors.Kind, nextRunAt time.Time) (dbgen.DownloadJob, error) {
	return r.Q.ScheduleDownloadJobRetry(ctx, dbgen.ScheduleDownloadJobRetryParams{
		ID:        id,
		LastError: &lastError,
		ErrorKind: nullableKind(kind),
		NextRunAt: nextRunAt,
	})
}

func (r *Repository) MarkDownloadJobFailed(ctx context.Context, id pgtype.UUID, lastError string, kind apperrors.Kind) (dbgen.DownloadJob, error) {
	return r.Q.MarkDownloadJobFailed(ctx, dbgen.MarkDownloadJobFailedParams{
		ID:        id,
		LastError: &lastError,
		ErrorKind: nullableKind(kind),
	})
}

// nullableKind converts an in-memory apperrors.Kind into the dbgen.NullErrorKind
// shape SQLC expects for the nullable error_kind column. KindUnspecified — the
// naked-error fallback — becomes NULL rather than a nonexistent enum value.
//
// We deliberately don't use a SQLC type override mapping error_kind directly to
// apperrors.Kind because the swaggo doc generator can't resolve the aliased
// import. The values are identical between dbgen.ErrorKind and apperrors.Kind,
// so the cast is free.
func nullableKind(k apperrors.Kind) dbgen.NullErrorKind {
	if k == apperrors.KindUnspecified {
		return dbgen.NullErrorKind{Valid: false}
	}
	return dbgen.NullErrorKind{
		ErrorKind: dbgen.ErrorKind(k),
		Valid:     true,
	}
}

func (r *Repository) RetryDownloadJob(ctx context.Context, id pgtype.UUID) (dbgen.DownloadJob, error) {
	return r.Q.RetryDownloadJob(ctx, id)
}

func (r *Repository) GetDownloadJobHistory(ctx context.Context, id pgtype.UUID) ([]dbgen.GetDownloadJobHistoryRow, error) {
	return r.Q.GetDownloadJobHistory(ctx, id)
}

func (r *Repository) CreateDownloadJobEvent(ctx context.Context, arg dbgen.CreateDownloadJobEventParams) (dbgen.DownloadJobEvent, error) {
	return r.Q.CreateDownloadJobEvent(ctx, arg)
}

func (r *Repository) ListDownloadJobEvents(ctx context.Context, downloadJobID pgtype.UUID) ([]dbgen.DownloadJobEvent, error) {
	return r.Q.ListDownloadJobEvents(ctx, downloadJobID)
}
