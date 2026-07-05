package repo

import (
	"context"
	"time"

	"github.com/google/uuid"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

type ImportTasksRepo interface {
	CreateImportTask(ctx context.Context, params CreateImportTaskParams) (model.ImportTask, error)
	GetImportTask(ctx context.Context, id uuid.UUID) (model.ImportTask, error)
	GetImportTaskWithDetails(ctx context.Context, id uuid.UUID) (model.ImportTaskWithDetails, error)
	GetImportTaskHistory(ctx context.Context, id uuid.UUID) ([]model.ImportTaskHistoryEntry, error)
	ListImportTasks(ctx context.Context, limit, offset int32) ([]model.ImportTask, error)
	ListImportTasksByDownloadJob(ctx context.Context, downloadJobID uuid.UUID) ([]model.ImportTask, error)
	ListImportTasksByMediaItem(ctx context.Context, mediaItemID uuid.UUID) ([]model.ImportTask, error)
	ListImportTasksByEpisode(ctx context.Context, episodeID uuid.UUID) ([]model.ImportTask, error)
	ListImportTasksByStatus(ctx context.Context, status string, limit, offset int32) ([]model.ImportTask, error)
	CountImportTasksByStatus(ctx context.Context) (model.ImportTaskCounts, error)

	ClaimRunnableImportTasks(ctx context.Context, limit int32) ([]model.ImportTask, error)

	SetImportTaskInProgress(ctx context.Context, id uuid.UUID) (model.ImportTask, error)
	SetImportTaskCompleted(ctx context.Context, params SetImportTaskCompletedParams) (model.ImportTask, error)
	SetImportTaskFailed(ctx context.Context, id uuid.UUID, lastError string, kind apperrors.Kind) (model.ImportTask, error)
	CancelImportTask(ctx context.Context, id uuid.UUID) (model.ImportTask, error)
	CancelPendingImportTasksForJob(ctx context.Context, downloadJobID uuid.UUID) error
	ScheduleImportTaskRetry(ctx context.Context, params ScheduleImportTaskRetryParams) (model.ImportTask, error)
	UpdateImportTaskSourcePath(ctx context.Context, id uuid.UUID, sourcePath string) error

	// Event logging
	CreateImportTaskEvent(ctx context.Context, params CreateImportTaskEventParams) (model.ImportTaskEvent, error)
	ListImportTaskEvents(ctx context.Context, importTaskID uuid.UUID) ([]model.ImportTaskEvent, error)
	GetImportTaskTimeline(ctx context.Context, importTaskID uuid.UUID) ([]model.ImportTaskEvent, error)
}

// CreateImportTaskParams is the domain-shaped input for CreateImportTask.
// Mirrors the writeable subset of model.ImportTask (omits server-managed
// ID/CreatedAt/UpdatedAt and runtime-managed status/progress fields).
type CreateImportTaskParams struct {
	DownloadJobID  uuid.UUID
	SourcePath     string
	PreviousTaskID uuid.UUID
	WantID         uuid.UUID
	MediaType      string
	MediaItemID    uuid.UUID
	EpisodeID      uuid.UUID
	LibraryID      uuid.UUID
	NameTemplateID uuid.UUID
}

// SetImportTaskCompletedParams is the domain-shaped input for
// SetImportTaskCompleted. Mirrors the columns the worker writes when an
// import succeeds.
type SetImportTaskCompletedParams struct {
	ID           uuid.UUID
	DestPath     string
	ImportMethod string
	FileID       uuid.UUID
}

// ScheduleImportTaskRetryParams is the domain-shaped input for
// ScheduleImportTaskRetry. Mirrors the columns the worker writes when
// scheduling a backoff retry.
type ScheduleImportTaskRetryParams struct {
	ID        uuid.UUID
	LastError string
	Kind      apperrors.Kind
	NextRunAt time.Time
}

// CreateImportTaskEventParams is the domain-shaped input for
// CreateImportTaskEvent. Mirrors the writeable subset of
// model.ImportTaskEvent (omits server-managed ID/CreatedAt).
type CreateImportTaskEventParams struct {
	ImportTaskID uuid.UUID
	EventType    string
	OldStatus    *string
	NewStatus    *string
	Message      *string
	Metadata     []byte
}

// kindFromNullable converts the dbgen.NullErrorKind persistence shape into
// a domain *apperrors.Kind. Invalid (NULL) becomes nil; valid values are
// wrapped in a pointer to the canonical apperrors.Kind enum.
func kindFromNullable(n dbgen.NullErrorKind) *apperrors.Kind {
	if !n.Valid {
		return nil
	}
	k := apperrors.Kind(n.ErrorKind)
	return &k
}

// toModelImportTask translates a persistence-shaped dbgen.ImportTask into
// the domain-shaped model.ImportTask.
func toModelImportTask(row dbgen.ImportTask) model.ImportTask {
	return model.ImportTask{
		ID:             uuidFromPgtype(row.ID),
		Status:         row.Status,
		DownloadJobID:  uuidFromPgtype(row.DownloadJobID),
		SourcePath:     row.SourcePath,
		PreviousTaskID: uuidFromPgtype(row.PreviousTaskID),
		WantID:         uuidFromPgtype(row.WantID),
		MediaType:      row.MediaType,
		MediaItemID:    uuidFromPgtype(row.MediaItemID),
		EpisodeID:      uuidFromPgtype(row.EpisodeID),
		LibraryID:      uuidFromPgtype(row.LibraryID),
		NameTemplateID: uuidFromPgtype(row.NameTemplateID),
		DestPath:       row.DestPath,
		ImportMethod:   row.ImportMethod,
		FileID:         uuidFromPgtype(row.FileID),
		AttemptCount:   row.AttemptCount,
		MaxAttempts:    row.MaxAttempts,
		NextRunAt:      row.NextRunAt,
		LastError:      row.LastError,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
		ErrorKind:      kindFromNullable(row.ErrorKind),
	}
}

// toModelImportTaskWithDetails composes the joined-row dbgen
// GetImportTaskWithDetailsRow into a model.ImportTaskWithDetails. The base
// fields go through the same translator as model.ImportTask; the joined
// fields are flat.
func toModelImportTaskWithDetails(row dbgen.GetImportTaskWithDetailsRow) model.ImportTaskWithDetails {
	return model.ImportTaskWithDetails{
		ImportTask: model.ImportTask{
			ID:             uuidFromPgtype(row.ID),
			Status:         row.Status,
			DownloadJobID:  uuidFromPgtype(row.DownloadJobID),
			SourcePath:     row.SourcePath,
			PreviousTaskID: uuidFromPgtype(row.PreviousTaskID),
			WantID:         uuidFromPgtype(row.WantID),
			MediaType:      row.MediaType,
			MediaItemID:    uuidFromPgtype(row.MediaItemID),
			EpisodeID:      uuidFromPgtype(row.EpisodeID),
			LibraryID:      uuidFromPgtype(row.LibraryID),
			NameTemplateID: uuidFromPgtype(row.NameTemplateID),
			DestPath:       row.DestPath,
			ImportMethod:   row.ImportMethod,
			FileID:         uuidFromPgtype(row.FileID),
			AttemptCount:   row.AttemptCount,
			MaxAttempts:    row.MaxAttempts,
			NextRunAt:      row.NextRunAt,
			LastError:      row.LastError,
			CreatedAt:      row.CreatedAt,
			UpdatedAt:      row.UpdatedAt,
			ErrorKind:      kindFromNullable(row.ErrorKind),
		},
		MediaTitle:           row.MediaTitle,
		MediaYear:            row.MediaYear,
		MediaTmdbID:          row.MediaTmdbID,
		MediaItemType:        row.MediaItemType,
		EpisodeNumber:        row.EpisodeNumber,
		EpisodeTitle:         row.EpisodeTitle,
		SeasonNumber:         row.SeasonNumber,
		LibraryName:          row.LibraryName,
		LibraryRootPath:      row.LibraryRootPath,
		NameTemplate:         row.NameTemplate,
		MovieDirTemplate:     row.MovieDirTemplate,
		SeriesShowTemplate:   row.SeriesShowTemplate,
		SeriesSeasonTemplate: row.SeriesSeasonTemplate,
		CandidateTitle:       row.CandidateTitle,
		DownloadIndexerID:    row.DownloadIndexerID,
		DownloadGuid:         row.DownloadGuid,
	}
}

// toModelImportTaskHistoryEntry composes a dbgen.GetImportTaskHistoryRow
// (an import_task row plus a chain_depth) into the domain shape.
func toModelImportTaskHistoryEntry(row dbgen.GetImportTaskHistoryRow) model.ImportTaskHistoryEntry {
	return model.ImportTaskHistoryEntry{
		ImportTask: model.ImportTask{
			ID:             uuidFromPgtype(row.ID),
			Status:         row.Status,
			DownloadJobID:  uuidFromPgtype(row.DownloadJobID),
			SourcePath:     row.SourcePath,
			PreviousTaskID: uuidFromPgtype(row.PreviousTaskID),
			WantID:         uuidFromPgtype(row.WantID),
			MediaType:      row.MediaType,
			MediaItemID:    uuidFromPgtype(row.MediaItemID),
			EpisodeID:      uuidFromPgtype(row.EpisodeID),
			LibraryID:      uuidFromPgtype(row.LibraryID),
			NameTemplateID: uuidFromPgtype(row.NameTemplateID),
			DestPath:       row.DestPath,
			ImportMethod:   row.ImportMethod,
			FileID:         uuidFromPgtype(row.FileID),
			AttemptCount:   row.AttemptCount,
			MaxAttempts:    row.MaxAttempts,
			NextRunAt:      row.NextRunAt,
			LastError:      row.LastError,
			CreatedAt:      row.CreatedAt,
			UpdatedAt:      row.UpdatedAt,
			ErrorKind:      kindFromNullable(row.ErrorKind),
		},
		ChainDepth: row.ChainDepth,
	}
}

// toModelImportTaskEvent translates a dbgen.ImportTaskEvent into the
// domain shape.
func toModelImportTaskEvent(row dbgen.ImportTaskEvent) model.ImportTaskEvent {
	return model.ImportTaskEvent{
		ID:           uuidFromPgtype(row.ID),
		ImportTaskID: uuidFromPgtype(row.ImportTaskID),
		EventType:    row.EventType,
		OldStatus:    row.OldStatus,
		NewStatus:    row.NewStatus,
		Message:      row.Message,
		Metadata:     row.Metadata,
		CreatedAt:    row.CreatedAt,
	}
}

// toModelImportTaskCounts translates the aggregate dbgen
// CountImportTasksByStatusRow into the domain shape.
func toModelImportTaskCounts(row dbgen.CountImportTasksByStatusRow) model.ImportTaskCounts {
	return model.ImportTaskCounts{
		Pending:    row.Pending,
		InProgress: row.InProgress,
		Completed:  row.Completed,
		Failed:     row.Failed,
		Cancelled:  row.Cancelled,
	}
}

func (r *Repository) CreateImportTask(ctx context.Context, params CreateImportTaskParams) (model.ImportTask, error) {
	row, err := r.Q.CreateImportTask(ctx, dbgen.CreateImportTaskParams{
		DownloadJobID:  pgtypeFromUUID(params.DownloadJobID),
		SourcePath:     params.SourcePath,
		PreviousTaskID: pgtypeFromUUIDOrNull(params.PreviousTaskID),
		WantID:         pgtypeFromUUIDOrNull(params.WantID),
		MediaType:      params.MediaType,
		MediaItemID:    pgtypeFromUUID(params.MediaItemID),
		EpisodeID:      pgtypeFromUUIDOrNull(params.EpisodeID),
		LibraryID:      pgtypeFromUUID(params.LibraryID),
		NameTemplateID: pgtypeFromUUID(params.NameTemplateID),
	})
	if err != nil {
		return model.ImportTask{}, apperrors.FromPg(err, "create import task for download job %s", params.DownloadJobID)
	}
	return toModelImportTask(row), nil
}

func (r *Repository) GetImportTask(ctx context.Context, id uuid.UUID) (model.ImportTask, error) {
	row, err := r.Q.GetImportTask(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.ImportTask{}, apperrors.FromPg(err, "import task %s not found", id)
	}
	return toModelImportTask(row), nil
}

func (r *Repository) GetImportTaskWithDetails(ctx context.Context, id uuid.UUID) (model.ImportTaskWithDetails, error) {
	row, err := r.Q.GetImportTaskWithDetails(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.ImportTaskWithDetails{}, apperrors.FromPg(err, "import task %s not found", id)
	}
	return toModelImportTaskWithDetails(row), nil
}

func (r *Repository) GetImportTaskHistory(ctx context.Context, id uuid.UUID) ([]model.ImportTaskHistoryEntry, error) {
	rows, err := r.Q.GetImportTaskHistory(ctx, pgtypeFromUUID(id))
	if err != nil {
		return nil, apperrors.FromPg(err, "import task %s history", id)
	}
	out := make([]model.ImportTaskHistoryEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelImportTaskHistoryEntry(row))
	}
	return out, nil
}

func (r *Repository) ListImportTasks(ctx context.Context, limit, offset int32) ([]model.ImportTask, error) {
	rows, err := r.Q.ListImportTasks(ctx, dbgen.ListImportTasksParams{
		LimitVal:  limit,
		OffsetVal: offset,
	})
	if err != nil {
		return nil, apperrors.FromPg(err, "list import tasks")
	}
	out := make([]model.ImportTask, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelImportTask(row))
	}
	return out, nil
}

func (r *Repository) ListImportTasksByDownloadJob(ctx context.Context, downloadJobID uuid.UUID) ([]model.ImportTask, error) {
	rows, err := r.Q.ListImportTasksByDownloadJob(ctx, pgtypeFromUUID(downloadJobID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list import tasks for download job %s", downloadJobID)
	}
	out := make([]model.ImportTask, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelImportTask(row))
	}
	return out, nil
}

func (r *Repository) ListImportTasksByMediaItem(ctx context.Context, mediaItemID uuid.UUID) ([]model.ImportTask, error) {
	rows, err := r.Q.ListImportTasksByMediaItem(ctx, pgtypeFromUUID(mediaItemID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list import tasks for media item %s", mediaItemID)
	}
	out := make([]model.ImportTask, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelImportTask(row))
	}
	return out, nil
}

func (r *Repository) ListImportTasksByEpisode(ctx context.Context, episodeID uuid.UUID) ([]model.ImportTask, error) {
	rows, err := r.Q.ListImportTasksByEpisode(ctx, pgtypeFromUUID(episodeID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list import tasks for episode %s", episodeID)
	}
	out := make([]model.ImportTask, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelImportTask(row))
	}
	return out, nil
}

func (r *Repository) ListImportTasksByStatus(ctx context.Context, status string, limit, offset int32) ([]model.ImportTask, error) {
	rows, err := r.Q.ListImportTasksByStatus(ctx, dbgen.ListImportTasksByStatusParams{
		Status:    status,
		LimitVal:  limit,
		OffsetVal: offset,
	})
	if err != nil {
		return nil, apperrors.FromPg(err, "list import tasks with status %s", status)
	}
	out := make([]model.ImportTask, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelImportTask(row))
	}
	return out, nil
}

func (r *Repository) CountImportTasksByStatus(ctx context.Context) (model.ImportTaskCounts, error) {
	row, err := r.Q.CountImportTasksByStatus(ctx)
	if err != nil {
		return model.ImportTaskCounts{}, apperrors.FromPg(err, "count import tasks by status")
	}
	return toModelImportTaskCounts(row), nil
}

func (r *Repository) ClaimRunnableImportTasks(ctx context.Context, limit int32) ([]model.ImportTask, error) {
	rows, err := r.Q.ClaimRunnableImportTasks(ctx, limit)
	if err != nil {
		return nil, apperrors.FromPg(err, "claim runnable import tasks")
	}
	out := make([]model.ImportTask, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelImportTask(row))
	}
	return out, nil
}

func (r *Repository) SetImportTaskInProgress(ctx context.Context, id uuid.UUID) (model.ImportTask, error) {
	row, err := r.Q.SetImportTaskInProgress(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.ImportTask{}, apperrors.FromPg(err, "mark import task %s in progress", id)
	}
	return toModelImportTask(row), nil
}

func (r *Repository) SetImportTaskCompleted(ctx context.Context, params SetImportTaskCompletedParams) (model.ImportTask, error) {
	row, err := r.Q.SetImportTaskCompleted(ctx, dbgen.SetImportTaskCompletedParams{
		ID:           pgtypeFromUUID(params.ID),
		DestPath:     &params.DestPath,
		ImportMethod: &params.ImportMethod,
		FileID:       pgtypeFromUUIDOrNull(params.FileID),
	})
	if err != nil {
		return model.ImportTask{}, apperrors.FromPg(err, "mark import task %s completed", params.ID)
	}
	return toModelImportTask(row), nil
}

func (r *Repository) SetImportTaskFailed(ctx context.Context, id uuid.UUID, lastError string, kind apperrors.Kind) (model.ImportTask, error) {
	row, err := r.Q.SetImportTaskFailed(ctx, dbgen.SetImportTaskFailedParams{
		ID:        pgtypeFromUUID(id),
		LastError: &lastError,
		ErrorKind: nullableKind(kind),
	})
	if err != nil {
		return model.ImportTask{}, apperrors.FromPg(err, "mark import task %s failed", id)
	}
	return toModelImportTask(row), nil
}

func (r *Repository) CancelImportTask(ctx context.Context, id uuid.UUID) (model.ImportTask, error) {
	row, err := r.Q.CancelImportTask(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.ImportTask{}, apperrors.FromPg(err, "cancel import task %s", id)
	}
	return toModelImportTask(row), nil
}

func (r *Repository) CancelPendingImportTasksForJob(ctx context.Context, downloadJobID uuid.UUID) error {
	return apperrors.FromPg(r.Q.CancelPendingImportTasksForJob(ctx, pgtypeFromUUID(downloadJobID)),
		"cancel pending import tasks for download job %s", downloadJobID)
}

func (r *Repository) ScheduleImportTaskRetry(ctx context.Context, params ScheduleImportTaskRetryParams) (model.ImportTask, error) {
	row, err := r.Q.ScheduleImportTaskRetry(ctx, dbgen.ScheduleImportTaskRetryParams{
		ID:        pgtypeFromUUID(params.ID),
		LastError: &params.LastError,
		ErrorKind: nullableKind(params.Kind),
		NextRunAt: params.NextRunAt,
	})
	if err != nil {
		return model.ImportTask{}, apperrors.FromPg(err, "schedule retry for import task %s", params.ID)
	}
	return toModelImportTask(row), nil
}

func (r *Repository) UpdateImportTaskSourcePath(ctx context.Context, id uuid.UUID, sourcePath string) error {
	return apperrors.FromPg(r.Q.UpdateImportTaskSourcePath(ctx, dbgen.UpdateImportTaskSourcePathParams{
		ID:         pgtypeFromUUID(id),
		SourcePath: sourcePath,
	}), "update source path for import task %s", id)
}

func (r *Repository) CreateImportTaskEvent(ctx context.Context, params CreateImportTaskEventParams) (model.ImportTaskEvent, error) {
	row, err := r.Q.CreateImportTaskEvent(ctx, dbgen.CreateImportTaskEventParams{
		ImportTaskID: pgtypeFromUUID(params.ImportTaskID),
		EventType:    params.EventType,
		OldStatus:    params.OldStatus,
		NewStatus:    params.NewStatus,
		Message:      params.Message,
		Metadata:     params.Metadata,
	})
	if err != nil {
		return model.ImportTaskEvent{}, apperrors.FromPg(err, "create import task %s event", params.ImportTaskID)
	}
	return toModelImportTaskEvent(row), nil
}

func (r *Repository) ListImportTaskEvents(ctx context.Context, importTaskID uuid.UUID) ([]model.ImportTaskEvent, error) {
	rows, err := r.Q.ListImportTaskEvents(ctx, pgtypeFromUUID(importTaskID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list events for import task %s", importTaskID)
	}
	out := make([]model.ImportTaskEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelImportTaskEvent(row))
	}
	return out, nil
}

func (r *Repository) GetImportTaskTimeline(ctx context.Context, importTaskID uuid.UUID) ([]model.ImportTaskEvent, error) {
	rows, err := r.Q.GetImportTaskTimeline(ctx, pgtypeFromUUID(importTaskID))
	if err != nil {
		return nil, apperrors.FromPg(err, "import task %s timeline", importTaskID)
	}
	out := make([]model.ImportTaskEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelImportTaskEvent(row))
	}
	return out, nil
}
