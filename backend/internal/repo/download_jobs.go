package repo

import (
	"context"
	"time"

	"github.com/google/uuid"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

type DownloadJobsRepo interface {
	CreateDownloadJob(ctx context.Context, params CreateDownloadJobParams) (model.DownloadJob, error)
	GetDownloadJob(ctx context.Context, id uuid.UUID) (model.DownloadJob, error)
	GetDownloadJobByCandidate(ctx context.Context, indexerID int64, guid string) (model.DownloadJob, error)
	GetDownloadJobWithImportSummary(ctx context.Context, id uuid.UUID) (model.DownloadJobWithSummary, error)
	GetDownloadJobTimeline(ctx context.Context, downloadJobID uuid.UUID) ([]model.DownloadJobTimelineEvent, error)
	ListDownloadJobsByMediaItem(ctx context.Context, mediaItemID uuid.UUID) ([]model.DownloadJob, error)
	ListDownloadJobsByTmdbMovieID(ctx context.Context, tmdbMovieID int64) ([]model.DownloadJob, error)
	ListDownloadJobsByTmdbSeriesID(ctx context.Context, tmdbSeriesID int64) ([]model.DownloadJobBySeriesEntry, error)
	ListDownloadJobs(ctx context.Context) ([]model.DownloadJob, error)
	ListDownloadJobsWithImportSummary(ctx context.Context) ([]model.DownloadJobWithSummary, error)
	CancelDownloadJob(ctx context.Context, id uuid.UUID) (model.DownloadJob, error)

	ClaimRunnableDownloadJobs(ctx context.Context, limit int32) ([]model.DownloadJob, error)

	SetDownloadJobEnqueued(ctx context.Context, id uuid.UUID, downloaderExternalID string) (model.DownloadJob, error)
	SetDownloadJobDownloadSnapshot(ctx context.Context, params SetDownloadJobDownloadSnapshotParams) (model.DownloadJob, error)
	SetDownloadJobCompleted(ctx context.Context, id uuid.UUID, savePath, contentPath string) (model.DownloadJob, error)

	ScheduleDownloadJobRetry(ctx context.Context, params ScheduleDownloadJobRetryParams) (model.DownloadJob, error)
	MarkDownloadJobFailed(ctx context.Context, id uuid.UUID, lastError string, kind apperrors.Kind) (model.DownloadJob, error)

	RetryDownloadJob(ctx context.Context, id uuid.UUID) (model.DownloadJob, error)
	GetDownloadJobHistory(ctx context.Context, id uuid.UUID) ([]model.DownloadJobHistoryEntry, error)

	// Event logging
	CreateDownloadJobEvent(ctx context.Context, params CreateDownloadJobEventParams) (model.DownloadJobEvent, error)
	ListDownloadJobEvents(ctx context.Context, downloadJobID uuid.UUID) ([]model.DownloadJobEvent, error)
}

// CreateDownloadJobParams is the domain-shaped input for CreateDownloadJob.
// Mirrors the writeable subset of model.DownloadJob (omits server-managed
// ID/CreatedAt/UpdatedAt and runtime-managed status/progress fields).
type CreateDownloadJobParams struct {
	Protocol       string
	MediaType      string
	MediaItemID    uuid.UUID
	SeasonID       uuid.UUID
	EpisodeID      uuid.UUID
	IndexerID      int64
	Guid           string
	CandidateTitle string
	CandidateLink  string
	DownloaderID   uuid.UUID
	LibraryID      uuid.UUID
	NameTemplateID uuid.UUID
}

// SetDownloadJobDownloadSnapshotParams is the domain-shaped input for
// SetDownloadJobDownloadSnapshot. Mirrors the columns the worker writes
// while polling downloader state.
type SetDownloadJobDownloadSnapshotParams struct {
	ID               uuid.UUID
	Status           string
	DownloaderStatus *string
	Progress         *float64
	SavePath         *string
	ContentPath      *string
	DownloadSpeed    *int64
	EtaSeconds       *int64
	TotalSize        *int64
}

// ScheduleDownloadJobRetryParams is the domain-shaped input for
// ScheduleDownloadJobRetry. Mirrors the columns the worker writes when
// scheduling a backoff retry.
type ScheduleDownloadJobRetryParams struct {
	ID        uuid.UUID
	LastError string
	Kind      apperrors.Kind
	NextRunAt time.Time
}

// CreateDownloadJobEventParams is the domain-shaped input for
// CreateDownloadJobEvent. Mirrors the writeable subset of
// model.DownloadJobEvent (omits server-managed ID/CreatedAt).
type CreateDownloadJobEventParams struct {
	DownloadJobID uuid.UUID
	EventType     string
	OldStatus     *string
	NewStatus     *string
	Message       *string
	Metadata      []byte
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

// toModelDownloadJob translates a persistence-shaped dbgen.DownloadJob into
// the domain-shaped model.DownloadJob.
func toModelDownloadJob(row dbgen.DownloadJob) model.DownloadJob {
	return model.DownloadJob{
		ID:                   uuidFromPgtype(row.ID),
		Status:               row.Status,
		Protocol:             row.Protocol,
		IndexerID:            row.IndexerID,
		Guid:                 row.Guid,
		CandidateTitle:       row.CandidateTitle,
		CandidateLink:        row.CandidateLink,
		MediaType:            row.MediaType,
		MediaItemID:          uuidFromPgtype(row.MediaItemID),
		SeasonID:             uuidFromPgtype(row.SeasonID),
		EpisodeID:            uuidFromPgtype(row.EpisodeID),
		LibraryID:            uuidFromPgtype(row.LibraryID),
		NameTemplateID:       uuidFromPgtype(row.NameTemplateID),
		DownloaderID:         uuidFromPgtype(row.DownloaderID),
		DownloaderExternalID: row.DownloaderExternalID,
		DownloaderStatus:     row.DownloaderStatus,
		Progress:             row.Progress,
		SavePath:             row.SavePath,
		ContentPath:          row.ContentPath,
		AttemptCount:         row.AttemptCount,
		NextRunAt:            row.NextRunAt,
		LastError:            row.LastError,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
		PreviousJobID:        uuidFromPgtype(row.PreviousJobID),
		DownloadSpeed:        row.DownloadSpeed,
		EtaSeconds:           row.EtaSeconds,
		TotalSize:            row.TotalSize,
		ErrorKind:            kindFromNullable(row.ErrorKind),
	}
}

// toModelDownloadJobWithSummary composes the joined-row dbgen
// GetDownloadJobWithImportSummaryRow into a model.DownloadJobWithSummary.
// The base fields go through the same pattern as model.DownloadJob; the
// joined fields are flat.
func toModelDownloadJobWithSummary(row dbgen.GetDownloadJobWithImportSummaryRow) model.DownloadJobWithSummary {
	return model.DownloadJobWithSummary{
		DownloadJob: model.DownloadJob{
			ID:                   uuidFromPgtype(row.ID),
			Status:               row.Status,
			Protocol:             row.Protocol,
			IndexerID:            row.IndexerID,
			Guid:                 row.Guid,
			CandidateTitle:       row.CandidateTitle,
			CandidateLink:        row.CandidateLink,
			MediaType:            row.MediaType,
			MediaItemID:          uuidFromPgtype(row.MediaItemID),
			SeasonID:             uuidFromPgtype(row.SeasonID),
			EpisodeID:            uuidFromPgtype(row.EpisodeID),
			LibraryID:            uuidFromPgtype(row.LibraryID),
			NameTemplateID:       uuidFromPgtype(row.NameTemplateID),
			DownloaderID:         uuidFromPgtype(row.DownloaderID),
			DownloaderExternalID: row.DownloaderExternalID,
			DownloaderStatus:     row.DownloaderStatus,
			Progress:             row.Progress,
			SavePath:             row.SavePath,
			ContentPath:          row.ContentPath,
			AttemptCount:         row.AttemptCount,
			NextRunAt:            row.NextRunAt,
			LastError:            row.LastError,
			CreatedAt:            row.CreatedAt,
			UpdatedAt:            row.UpdatedAt,
			PreviousJobID:        uuidFromPgtype(row.PreviousJobID),
			DownloadSpeed:        row.DownloadSpeed,
			EtaSeconds:           row.EtaSeconds,
			TotalSize:            row.TotalSize,
			ErrorKind:            kindFromNullable(row.ErrorKind),
		},
		TmdbID:             row.TmdbID,
		MediaPosterPath:    row.MediaPosterPath,
		MediaTitle:         row.MediaTitle,
		MediaYear:          row.MediaYear,
		MediaCertification: row.MediaCertification,
		SeasonNumber:       row.SeasonNumber,
		EpisodeNumber:      row.EpisodeNumber,
		TotalImportTasks:   row.TotalImportTasks,
		PendingImports:     row.PendingImports,
		ActiveImports:      row.ActiveImports,
		CompletedImports:   row.CompletedImports,
		FailedImports:      row.FailedImports,
		CancelledImports:   row.CancelledImports,
		ImportStatus:       row.ImportStatus,
	}
}

// toModelDownloadJobWithSummaryFromList is the same translation as above but
// from the list-flavored row type. The two dbgen rows have identical fields
// so the body matches; we keep them split because Go can't generic-over the
// row type without erasing the field names.
func toModelDownloadJobWithSummaryFromList(row dbgen.ListDownloadJobsWithImportSummaryRow) model.DownloadJobWithSummary {
	return model.DownloadJobWithSummary{
		DownloadJob: model.DownloadJob{
			ID:                   uuidFromPgtype(row.ID),
			Status:               row.Status,
			Protocol:             row.Protocol,
			IndexerID:            row.IndexerID,
			Guid:                 row.Guid,
			CandidateTitle:       row.CandidateTitle,
			CandidateLink:        row.CandidateLink,
			MediaType:            row.MediaType,
			MediaItemID:          uuidFromPgtype(row.MediaItemID),
			SeasonID:             uuidFromPgtype(row.SeasonID),
			EpisodeID:            uuidFromPgtype(row.EpisodeID),
			LibraryID:            uuidFromPgtype(row.LibraryID),
			NameTemplateID:       uuidFromPgtype(row.NameTemplateID),
			DownloaderID:         uuidFromPgtype(row.DownloaderID),
			DownloaderExternalID: row.DownloaderExternalID,
			DownloaderStatus:     row.DownloaderStatus,
			Progress:             row.Progress,
			SavePath:             row.SavePath,
			ContentPath:          row.ContentPath,
			AttemptCount:         row.AttemptCount,
			NextRunAt:            row.NextRunAt,
			LastError:            row.LastError,
			CreatedAt:            row.CreatedAt,
			UpdatedAt:            row.UpdatedAt,
			PreviousJobID:        uuidFromPgtype(row.PreviousJobID),
			DownloadSpeed:        row.DownloadSpeed,
			EtaSeconds:           row.EtaSeconds,
			TotalSize:            row.TotalSize,
			ErrorKind:            kindFromNullable(row.ErrorKind),
		},
		TmdbID:             row.TmdbID,
		MediaPosterPath:    row.MediaPosterPath,
		MediaTitle:         row.MediaTitle,
		MediaYear:          row.MediaYear,
		MediaCertification: row.MediaCertification,
		SeasonNumber:       row.SeasonNumber,
		EpisodeNumber:      row.EpisodeNumber,
		TotalImportTasks:   row.TotalImportTasks,
		PendingImports:     row.PendingImports,
		ActiveImports:      row.ActiveImports,
		CompletedImports:   row.CompletedImports,
		FailedImports:      row.FailedImports,
		CancelledImports:   row.CancelledImports,
		ImportStatus:       row.ImportStatus,
	}
}

// toModelDownloadJobBySeriesEntry composes a
// dbgen.ListDownloadJobsByTmdbSeriesIDRow (a download_job row plus the
// season and episode numbers from joined media tables) into the domain
// shape.
func toModelDownloadJobBySeriesEntry(row dbgen.ListDownloadJobsByTmdbSeriesIDRow) model.DownloadJobBySeriesEntry {
	return model.DownloadJobBySeriesEntry{
		DownloadJob: model.DownloadJob{
			ID:                   uuidFromPgtype(row.ID),
			Status:               row.Status,
			Protocol:             row.Protocol,
			IndexerID:            row.IndexerID,
			Guid:                 row.Guid,
			CandidateTitle:       row.CandidateTitle,
			CandidateLink:        row.CandidateLink,
			MediaType:            row.MediaType,
			MediaItemID:          uuidFromPgtype(row.MediaItemID),
			SeasonID:             uuidFromPgtype(row.SeasonID),
			EpisodeID:            uuidFromPgtype(row.EpisodeID),
			LibraryID:            uuidFromPgtype(row.LibraryID),
			NameTemplateID:       uuidFromPgtype(row.NameTemplateID),
			DownloaderID:         uuidFromPgtype(row.DownloaderID),
			DownloaderExternalID: row.DownloaderExternalID,
			DownloaderStatus:     row.DownloaderStatus,
			Progress:             row.Progress,
			SavePath:             row.SavePath,
			ContentPath:          row.ContentPath,
			AttemptCount:         row.AttemptCount,
			NextRunAt:            row.NextRunAt,
			LastError:            row.LastError,
			CreatedAt:            row.CreatedAt,
			UpdatedAt:            row.UpdatedAt,
			PreviousJobID:        uuidFromPgtype(row.PreviousJobID),
			DownloadSpeed:        row.DownloadSpeed,
			EtaSeconds:           row.EtaSeconds,
			TotalSize:            row.TotalSize,
			ErrorKind:            kindFromNullable(row.ErrorKind),
		},
		SeasonNumber:  row.SeasonNumber,
		EpisodeNumber: row.EpisodeNumber,
	}
}

// toModelDownloadJobHistoryEntry composes a dbgen.GetDownloadJobHistoryRow
// (a download_job row plus a chain_depth) into the domain shape.
func toModelDownloadJobHistoryEntry(row dbgen.GetDownloadJobHistoryRow) model.DownloadJobHistoryEntry {
	return model.DownloadJobHistoryEntry{
		DownloadJob: model.DownloadJob{
			ID:                   uuidFromPgtype(row.ID),
			Status:               row.Status,
			Protocol:             row.Protocol,
			IndexerID:            row.IndexerID,
			Guid:                 row.Guid,
			CandidateTitle:       row.CandidateTitle,
			CandidateLink:        row.CandidateLink,
			MediaType:            row.MediaType,
			MediaItemID:          uuidFromPgtype(row.MediaItemID),
			SeasonID:             uuidFromPgtype(row.SeasonID),
			EpisodeID:            uuidFromPgtype(row.EpisodeID),
			LibraryID:            uuidFromPgtype(row.LibraryID),
			NameTemplateID:       uuidFromPgtype(row.NameTemplateID),
			DownloaderID:         uuidFromPgtype(row.DownloaderID),
			DownloaderExternalID: row.DownloaderExternalID,
			DownloaderStatus:     row.DownloaderStatus,
			Progress:             row.Progress,
			SavePath:             row.SavePath,
			ContentPath:          row.ContentPath,
			AttemptCount:         row.AttemptCount,
			NextRunAt:            row.NextRunAt,
			LastError:            row.LastError,
			CreatedAt:            row.CreatedAt,
			UpdatedAt:            row.UpdatedAt,
			PreviousJobID:        uuidFromPgtype(row.PreviousJobID),
			DownloadSpeed:        row.DownloadSpeed,
			EtaSeconds:           row.EtaSeconds,
			TotalSize:            row.TotalSize,
			ErrorKind:            kindFromNullable(row.ErrorKind),
		},
		ChainDepth: row.ChainDepth,
	}
}

// toModelDownloadJobTimelineEvent translates a dbgen.GetDownloadJobTimelineRow
// into the domain shape.
func toModelDownloadJobTimelineEvent(row dbgen.GetDownloadJobTimelineRow) model.DownloadJobTimelineEvent {
	return model.DownloadJobTimelineEvent{
		Source:       row.Source,
		ID:           uuidFromPgtype(row.ID),
		EventType:    row.EventType,
		OldStatus:    row.OldStatus,
		NewStatus:    row.NewStatus,
		Message:      row.Message,
		Metadata:     row.Metadata,
		CreatedAt:    row.CreatedAt,
		ImportTaskID: uuidFromPgtype(row.ImportTaskID),
	}
}

// toModelDownloadJobEvent translates a dbgen.DownloadJobEvent into the
// domain shape.
func toModelDownloadJobEvent(row dbgen.DownloadJobEvent) model.DownloadJobEvent {
	return model.DownloadJobEvent{
		ID:            uuidFromPgtype(row.ID),
		DownloadJobID: uuidFromPgtype(row.DownloadJobID),
		EventType:     row.EventType,
		OldStatus:     row.OldStatus,
		NewStatus:     row.NewStatus,
		Message:       row.Message,
		Metadata:      row.Metadata,
		CreatedAt:     row.CreatedAt,
	}
}

func (r *Repository) CreateDownloadJob(ctx context.Context, params CreateDownloadJobParams) (model.DownloadJob, error) {
	row, err := r.Q.CreateDownloadJob(ctx, dbgen.CreateDownloadJobParams{
		Protocol:       params.Protocol,
		MediaType:      params.MediaType,
		MediaItemID:    pgtypeFromUUID(params.MediaItemID),
		SeasonID:       pgtypeFromUUIDOrNull(params.SeasonID),
		EpisodeID:      pgtypeFromUUIDOrNull(params.EpisodeID),
		IndexerID:      params.IndexerID,
		Guid:           params.Guid,
		CandidateTitle: params.CandidateTitle,
		CandidateLink:  params.CandidateLink,
		DownloaderID:   pgtypeFromUUID(params.DownloaderID),
		LibraryID:      pgtypeFromUUID(params.LibraryID),
		NameTemplateID: pgtypeFromUUID(params.NameTemplateID),
	})
	if err != nil {
		return model.DownloadJob{}, apperrors.FromPg(err, "create download job for media %s", params.MediaItemID)
	}
	return toModelDownloadJob(row), nil
}

func (r *Repository) GetDownloadJob(ctx context.Context, id uuid.UUID) (model.DownloadJob, error) {
	row, err := r.Q.GetDownloadJob(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.DownloadJob{}, apperrors.FromPg(err, "download job %s not found", id)
	}
	return toModelDownloadJob(row), nil
}

func (r *Repository) GetDownloadJobByCandidate(ctx context.Context, indexerID int64, guid string) (model.DownloadJob, error) {
	row, err := r.Q.GetDownloadJobByCandidate(ctx, dbgen.GetDownloadJobByCandidateParams{
		IndexerID: indexerID,
		Guid:      guid,
	})
	if err != nil {
		return model.DownloadJob{}, apperrors.FromPg(err, "download job for candidate %d/%q not found", indexerID, guid)
	}
	return toModelDownloadJob(row), nil
}

func (r *Repository) GetDownloadJobWithImportSummary(ctx context.Context, id uuid.UUID) (model.DownloadJobWithSummary, error) {
	row, err := r.Q.GetDownloadJobWithImportSummary(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.DownloadJobWithSummary{}, apperrors.FromPg(err, "download job %s not found", id)
	}
	return toModelDownloadJobWithSummary(row), nil
}

func (r *Repository) GetDownloadJobTimeline(ctx context.Context, downloadJobID uuid.UUID) ([]model.DownloadJobTimelineEvent, error) {
	rows, err := r.Q.GetDownloadJobTimeline(ctx, pgtypeFromUUID(downloadJobID))
	if err != nil {
		return nil, apperrors.FromPg(err, "download job %s timeline", downloadJobID)
	}
	out := make([]model.DownloadJobTimelineEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelDownloadJobTimelineEvent(row))
	}
	return out, nil
}

func (r *Repository) ListDownloadJobsByMediaItem(ctx context.Context, mediaItemID uuid.UUID) ([]model.DownloadJob, error) {
	rows, err := r.Q.ListDownloadJobsByMediaItem(ctx, pgtypeFromUUID(mediaItemID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list download jobs for media item %s", mediaItemID)
	}
	out := make([]model.DownloadJob, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelDownloadJob(row))
	}
	return out, nil
}

func (r *Repository) ListDownloadJobsByTmdbMovieID(ctx context.Context, tmdbMovieID int64) ([]model.DownloadJob, error) {
	rows, err := r.Q.ListDownloadJobsByTmdbMovieID(ctx, &tmdbMovieID)
	if err != nil {
		return nil, apperrors.FromPg(err, "list download jobs for tmdb movie %d", tmdbMovieID)
	}
	out := make([]model.DownloadJob, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelDownloadJob(row))
	}
	return out, nil
}

func (r *Repository) ListDownloadJobsByTmdbSeriesID(ctx context.Context, tmdbSeriesID int64) ([]model.DownloadJobBySeriesEntry, error) {
	rows, err := r.Q.ListDownloadJobsByTmdbSeriesID(ctx, &tmdbSeriesID)
	if err != nil {
		return nil, apperrors.FromPg(err, "list download jobs for tmdb series %d", tmdbSeriesID)
	}
	out := make([]model.DownloadJobBySeriesEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelDownloadJobBySeriesEntry(row))
	}
	return out, nil
}

func (r *Repository) ListDownloadJobs(ctx context.Context) ([]model.DownloadJob, error) {
	rows, err := r.Q.ListDownloadJobs(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list download jobs")
	}
	out := make([]model.DownloadJob, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelDownloadJob(row))
	}
	return out, nil
}

func (r *Repository) ListDownloadJobsWithImportSummary(ctx context.Context) ([]model.DownloadJobWithSummary, error) {
	rows, err := r.Q.ListDownloadJobsWithImportSummary(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list download jobs with import summary")
	}
	out := make([]model.DownloadJobWithSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelDownloadJobWithSummaryFromList(row))
	}
	return out, nil
}

func (r *Repository) CancelDownloadJob(ctx context.Context, id uuid.UUID) (model.DownloadJob, error) {
	row, err := r.Q.CancelDownloadJob(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.DownloadJob{}, apperrors.FromPg(err, "cancel download job %s", id)
	}
	return toModelDownloadJob(row), nil
}

func (r *Repository) ClaimRunnableDownloadJobs(ctx context.Context, limit int32) ([]model.DownloadJob, error) {
	rows, err := r.Q.ClaimRunnableDownloadJobs(ctx, limit)
	if err != nil {
		return nil, apperrors.FromPg(err, "claim runnable download jobs")
	}
	out := make([]model.DownloadJob, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelDownloadJob(row))
	}
	return out, nil
}

func (r *Repository) SetDownloadJobEnqueued(ctx context.Context, id uuid.UUID, downloaderExternalID string) (model.DownloadJob, error) {
	row, err := r.Q.SetDownloadJobEnqueued(ctx, dbgen.SetDownloadJobEnqueuedParams{
		ID:                   pgtypeFromUUID(id),
		DownloaderExternalID: &downloaderExternalID,
	})
	if err != nil {
		return model.DownloadJob{}, apperrors.FromPg(err, "mark download job %s enqueued", id)
	}
	return toModelDownloadJob(row), nil
}

func (r *Repository) SetDownloadJobDownloadSnapshot(ctx context.Context, params SetDownloadJobDownloadSnapshotParams) (model.DownloadJob, error) {
	row, err := r.Q.SetDownloadJobDownloadSnapshot(ctx, dbgen.SetDownloadJobDownloadSnapshotParams{
		ID:               pgtypeFromUUID(params.ID),
		Status:           params.Status,
		DownloaderStatus: params.DownloaderStatus,
		Progress:         params.Progress,
		SavePath:         params.SavePath,
		ContentPath:      params.ContentPath,
		DownloadSpeed:    params.DownloadSpeed,
		EtaSeconds:       params.EtaSeconds,
		TotalSize:        params.TotalSize,
	})
	if err != nil {
		return model.DownloadJob{}, apperrors.FromPg(err, "update download job %s snapshot", params.ID)
	}
	return toModelDownloadJob(row), nil
}

func (r *Repository) SetDownloadJobCompleted(ctx context.Context, id uuid.UUID, savePath, contentPath string) (model.DownloadJob, error) {
	row, err := r.Q.SetDownloadJobCompleted(ctx, dbgen.SetDownloadJobCompletedParams{
		ID:          pgtypeFromUUID(id),
		SavePath:    &savePath,
		ContentPath: &contentPath,
	})
	if err != nil {
		return model.DownloadJob{}, apperrors.FromPg(err, "mark download job %s completed", id)
	}
	return toModelDownloadJob(row), nil
}

func (r *Repository) ScheduleDownloadJobRetry(ctx context.Context, params ScheduleDownloadJobRetryParams) (model.DownloadJob, error) {
	row, err := r.Q.ScheduleDownloadJobRetry(ctx, dbgen.ScheduleDownloadJobRetryParams{
		ID:        pgtypeFromUUID(params.ID),
		LastError: &params.LastError,
		ErrorKind: nullableKind(params.Kind),
		NextRunAt: params.NextRunAt,
	})
	if err != nil {
		return model.DownloadJob{}, apperrors.FromPg(err, "schedule retry for download job %s", params.ID)
	}
	return toModelDownloadJob(row), nil
}

func (r *Repository) MarkDownloadJobFailed(ctx context.Context, id uuid.UUID, lastError string, kind apperrors.Kind) (model.DownloadJob, error) {
	row, err := r.Q.MarkDownloadJobFailed(ctx, dbgen.MarkDownloadJobFailedParams{
		ID:        pgtypeFromUUID(id),
		LastError: &lastError,
		ErrorKind: nullableKind(kind),
	})
	if err != nil {
		return model.DownloadJob{}, apperrors.FromPg(err, "mark download job %s failed", id)
	}
	return toModelDownloadJob(row), nil
}

func (r *Repository) RetryDownloadJob(ctx context.Context, id uuid.UUID) (model.DownloadJob, error) {
	row, err := r.Q.RetryDownloadJob(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.DownloadJob{}, apperrors.FromPg(err, "retry download job %s", id)
	}
	return toModelDownloadJob(row), nil
}

func (r *Repository) GetDownloadJobHistory(ctx context.Context, id uuid.UUID) ([]model.DownloadJobHistoryEntry, error) {
	rows, err := r.Q.GetDownloadJobHistory(ctx, pgtypeFromUUID(id))
	if err != nil {
		return nil, apperrors.FromPg(err, "download job %s history", id)
	}
	out := make([]model.DownloadJobHistoryEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelDownloadJobHistoryEntry(row))
	}
	return out, nil
}

func (r *Repository) CreateDownloadJobEvent(ctx context.Context, params CreateDownloadJobEventParams) (model.DownloadJobEvent, error) {
	row, err := r.Q.CreateDownloadJobEvent(ctx, dbgen.CreateDownloadJobEventParams{
		DownloadJobID: pgtypeFromUUID(params.DownloadJobID),
		EventType:     params.EventType,
		OldStatus:     params.OldStatus,
		NewStatus:     params.NewStatus,
		Message:       params.Message,
		Metadata:      params.Metadata,
	})
	if err != nil {
		return model.DownloadJobEvent{}, apperrors.FromPg(err, "create download job %s event", params.DownloadJobID)
	}
	return toModelDownloadJobEvent(row), nil
}

func (r *Repository) ListDownloadJobEvents(ctx context.Context, downloadJobID uuid.UUID) ([]model.DownloadJobEvent, error) {
	rows, err := r.Q.ListDownloadJobEvents(ctx, pgtypeFromUUID(downloadJobID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list events for download job %s", downloadJobID)
	}
	out := make([]model.DownloadJobEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelDownloadJobEvent(row))
	}
	return out, nil
}
