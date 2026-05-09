package model

import (
	"time"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

// ImportTask is the domain shape for an import_task row. It mirrors the
// persistence-layer dbgen.ImportTask but uses idiomatic Go types
// (uuid.UUID, time.Time, *apperrors.Kind for the nullable enum) and lives
// outside the persistence boundary.
//
// ErrorKind is a *apperrors.Kind rather than a pgtype-shaped wrapper:
// dbgen.NullErrorKind is replaced with the idiomatic JSON-nullable enum
// (the wire surface becomes either "internal" or null instead of
// { "ErrorKind": "internal", "Valid": true }).
type ImportTask struct {
	ID             uuid.UUID       `json:"id"`
	Status         string          `json:"status"`
	DownloadJobID  uuid.UUID       `json:"downloadJobId"`
	SourcePath     string          `json:"sourcePath"`
	PreviousTaskID uuid.UUID       `json:"previousTaskId"`
	MediaType      string          `json:"mediaType"`
	MediaItemID    uuid.UUID       `json:"mediaItemId"`
	EpisodeID      uuid.UUID       `json:"episodeId"`
	LibraryID      uuid.UUID       `json:"libraryId"`
	NameTemplateID uuid.UUID       `json:"nameTemplateId"`
	DestPath       *string         `json:"destPath"`
	ImportMethod   *string         `json:"importMethod"`
	MediaFileID    uuid.UUID       `json:"mediaFileId"`
	AttemptCount   int32           `json:"attemptCount"`
	MaxAttempts    int32           `json:"maxAttempts"`
	NextRunAt      time.Time       `json:"nextRunAt"`
	LastError      *string         `json:"lastError"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	ErrorKind      *apperrors.Kind `json:"errorKind" swaggertype:"string" extensions:"x-nullable"`
}

// ImportTaskWithDetails is an ImportTask joined with related media item,
// library, name template, and download job context. Mirrors the persistence
// shape dbgen.GetImportTaskWithDetailsRow.
type ImportTaskWithDetails struct {
	ImportTask
	MediaTitle           string  `json:"mediaTitle"`
	MediaYear            *int32  `json:"mediaYear"`
	MediaTmdbID          *int64  `json:"mediaTmdbId"`
	MediaItemType        string  `json:"mediaItemType"`
	EpisodeNumber        *int32  `json:"episodeNumber"`
	EpisodeTitle         *string `json:"episodeTitle"`
	SeasonNumber         *int32  `json:"seasonNumber"`
	LibraryName          string  `json:"libraryName"`
	LibraryRootPath      string  `json:"libraryRootPath"`
	NameTemplate         string  `json:"nameTemplate"`
	MovieDirTemplate     *string `json:"movieDirTemplate"`
	SeriesShowTemplate   *string `json:"seriesShowTemplate"`
	SeriesSeasonTemplate *string `json:"seriesSeasonTemplate"`
	CandidateTitle       *string `json:"candidateTitle"`
}

// ImportTaskHistoryEntry is a row in the reimport chain. Mirrors the
// persistence shape dbgen.GetImportTaskHistoryRow — an ImportTask plus the
// chain depth.
type ImportTaskHistoryEntry struct {
	ImportTask
	ChainDepth int32 `json:"chainDepth"`
}

// ImportTaskEvent is the domain shape for a row in the import_task_event
// log. Mirrors dbgen.ImportTaskEvent.
type ImportTaskEvent struct {
	ID           uuid.UUID `json:"id"`
	ImportTaskID uuid.UUID `json:"importTaskId"`
	EventType    string    `json:"eventType"`
	OldStatus    *string   `json:"oldStatus"`
	NewStatus    *string   `json:"newStatus"`
	Message      *string   `json:"message"`
	Metadata     []byte    `json:"metadata" swaggertype:"object"`
	CreatedAt    time.Time `json:"createdAt"`
}

// ImportTaskCounts is the aggregate count of import tasks per status.
// Mirrors dbgen.CountImportTasksByStatusRow.
type ImportTaskCounts struct {
	Pending    int32 `json:"pending"`
	InProgress int32 `json:"inProgress"`
	Completed  int32 `json:"completed"`
	Failed     int32 `json:"failed"`
	Cancelled  int32 `json:"cancelled"`
}
