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
	job, err := r.Q.CreateDownloadJob(ctx, arg)
	return job, apperrors.FromPg(err, "create download job for media %s", arg.MediaItemID)
}

func (r *Repository) GetDownloadJob(ctx context.Context, id pgtype.UUID) (dbgen.DownloadJob, error) {
	job, err := r.Q.GetDownloadJob(ctx, id)
	return job, apperrors.FromPg(err, "download job %s not found", id)
}

func (r *Repository) GetDownloadJobByCandidate(ctx context.Context, indexerID int64, guid string) (dbgen.DownloadJob, error) {
	job, err := r.Q.GetDownloadJobByCandidate(ctx, dbgen.GetDownloadJobByCandidateParams{
		IndexerID: indexerID,
		Guid:      guid,
	})
	return job, apperrors.FromPg(err, "download job for candidate %d/%q not found", indexerID, guid)
}

func (r *Repository) GetDownloadJobWithImportSummary(ctx context.Context, id pgtype.UUID) (dbgen.GetDownloadJobWithImportSummaryRow, error) {
	row, err := r.Q.GetDownloadJobWithImportSummary(ctx, id)
	return row, apperrors.FromPg(err, "download job %s not found", id)
}

func (r *Repository) GetDownloadJobTimeline(ctx context.Context, downloadJobID pgtype.UUID) ([]dbgen.GetDownloadJobTimelineRow, error) {
	rows, err := r.Q.GetDownloadJobTimeline(ctx, downloadJobID)
	return rows, apperrors.FromPg(err, "download job %s timeline", downloadJobID)
}

func (r *Repository) ListDownloadJobsByMediaItem(ctx context.Context, mediaItemID pgtype.UUID) ([]dbgen.DownloadJob, error) {
	jobs, err := r.Q.ListDownloadJobsByMediaItem(ctx, mediaItemID)
	return jobs, apperrors.FromPg(err, "list download jobs for media item %s", mediaItemID)
}

func (r *Repository) ListDownloadJobsByTmdbMovieID(ctx context.Context, tmdbMovieID int64) ([]dbgen.DownloadJob, error) {
	jobs, err := r.Q.ListDownloadJobsByTmdbMovieID(ctx, &tmdbMovieID)
	return jobs, apperrors.FromPg(err, "list download jobs for tmdb movie %d", tmdbMovieID)
}

func (r *Repository) ListDownloadJobsByTmdbSeriesID(ctx context.Context, tmdbSeriesID int64) ([]dbgen.ListDownloadJobsByTmdbSeriesIDRow, error) {
	rows, err := r.Q.ListDownloadJobsByTmdbSeriesID(ctx, &tmdbSeriesID)
	return rows, apperrors.FromPg(err, "list download jobs for tmdb series %d", tmdbSeriesID)
}

func (r *Repository) ListDownloadJobs(ctx context.Context) ([]dbgen.DownloadJob, error) {
	jobs, err := r.Q.ListDownloadJobs(ctx)
	return jobs, apperrors.FromPg(err, "list download jobs")
}

func (r *Repository) ListDownloadJobsWithImportSummary(ctx context.Context) ([]dbgen.ListDownloadJobsWithImportSummaryRow, error) {
	rows, err := r.Q.ListDownloadJobsWithImportSummary(ctx)
	return rows, apperrors.FromPg(err, "list download jobs with import summary")
}

func (r *Repository) CancelDownloadJob(ctx context.Context, id pgtype.UUID) (dbgen.DownloadJob, error) {
	job, err := r.Q.CancelDownloadJob(ctx, id)
	return job, apperrors.FromPg(err, "cancel download job %s", id)
}

func (r *Repository) ClaimRunnableDownloadJobs(ctx context.Context, limit int32) ([]dbgen.DownloadJob, error) {
	jobs, err := r.Q.ClaimRunnableDownloadJobs(ctx, limit)
	return jobs, apperrors.FromPg(err, "claim runnable download jobs")
}

func (r *Repository) SetDownloadJobEnqueued(ctx context.Context, id pgtype.UUID, downloaderExternalID string) (dbgen.DownloadJob, error) {
	job, err := r.Q.SetDownloadJobEnqueued(ctx, dbgen.SetDownloadJobEnqueuedParams{
		ID:                   id,
		DownloaderExternalID: &downloaderExternalID,
	})
	return job, apperrors.FromPg(err, "mark download job %s enqueued", id)
}

func (r *Repository) SetDownloadJobDownloadSnapshot(ctx context.Context, arg dbgen.SetDownloadJobDownloadSnapshotParams) (dbgen.DownloadJob, error) {
	job, err := r.Q.SetDownloadJobDownloadSnapshot(ctx, arg)
	return job, apperrors.FromPg(err, "update download job %s snapshot", arg.ID)
}

func (r *Repository) SetDownloadJobCompleted(ctx context.Context, id pgtype.UUID, savePath, contentPath string) (dbgen.DownloadJob, error) {
	job, err := r.Q.SetDownloadJobCompleted(ctx, dbgen.SetDownloadJobCompletedParams{
		ID:          id,
		SavePath:    &savePath,
		ContentPath: &contentPath,
	})
	return job, apperrors.FromPg(err, "mark download job %s completed", id)
}

func (r *Repository) ScheduleDownloadJobRetry(ctx context.Context, id pgtype.UUID, lastError string, kind apperrors.Kind, nextRunAt time.Time) (dbgen.DownloadJob, error) {
	job, err := r.Q.ScheduleDownloadJobRetry(ctx, dbgen.ScheduleDownloadJobRetryParams{
		ID:        id,
		LastError: &lastError,
		ErrorKind: nullableKind(kind),
		NextRunAt: nextRunAt,
	})
	return job, apperrors.FromPg(err, "schedule retry for download job %s", id)
}

func (r *Repository) MarkDownloadJobFailed(ctx context.Context, id pgtype.UUID, lastError string, kind apperrors.Kind) (dbgen.DownloadJob, error) {
	job, err := r.Q.MarkDownloadJobFailed(ctx, dbgen.MarkDownloadJobFailedParams{
		ID:        id,
		LastError: &lastError,
		ErrorKind: nullableKind(kind),
	})
	return job, apperrors.FromPg(err, "mark download job %s failed", id)
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
	job, err := r.Q.RetryDownloadJob(ctx, id)
	return job, apperrors.FromPg(err, "retry download job %s", id)
}

func (r *Repository) GetDownloadJobHistory(ctx context.Context, id pgtype.UUID) ([]dbgen.GetDownloadJobHistoryRow, error) {
	rows, err := r.Q.GetDownloadJobHistory(ctx, id)
	return rows, apperrors.FromPg(err, "download job %s history", id)
}

func (r *Repository) CreateDownloadJobEvent(ctx context.Context, arg dbgen.CreateDownloadJobEventParams) (dbgen.DownloadJobEvent, error) {
	ev, err := r.Q.CreateDownloadJobEvent(ctx, arg)
	return ev, apperrors.FromPg(err, "create download job %s event", arg.DownloadJobID)
}

func (r *Repository) ListDownloadJobEvents(ctx context.Context, downloadJobID pgtype.UUID) ([]dbgen.DownloadJobEvent, error) {
	events, err := r.Q.ListDownloadJobEvents(ctx, downloadJobID)
	return events, apperrors.FromPg(err, "list events for download job %s", downloadJobID)
}
