package model

import (
	"time"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

// DownloadJob is the domain shape for a download_job row. It mirrors the
// persistence-layer dbgen.DownloadJob but uses idiomatic Go types
// (uuid.UUID, time.Time, *apperrors.Kind for the nullable enum) and lives
// outside the persistence boundary.
//
// ErrorKind is a *apperrors.Kind rather than a pgtype-shaped wrapper:
// dbgen.NullErrorKind is replaced with the idiomatic JSON-nullable enum
// (the wire surface becomes either "internal" or null instead of
// { "ErrorKind": "internal", "Valid": true }).
type DownloadJob struct {
	ID                   uuid.UUID       `json:"id"`
	Status               string          `json:"status"`
	Protocol             string          `json:"protocol"`
	IndexerID            int64           `json:"indexerId"`
	Guid                 string          `json:"guid"`
	CandidateTitle       string          `json:"candidateTitle"`
	CandidateLink        string          `json:"candidateLink"`
	MediaType            string          `json:"mediaType"`
	MediaItemID          uuid.UUID       `json:"mediaItemId"`
	SeasonID             uuid.UUID       `json:"seasonId"`
	EpisodeID            uuid.UUID       `json:"episodeId"`
	LibraryID            uuid.UUID       `json:"libraryId"`
	NameTemplateID       uuid.UUID       `json:"nameTemplateId"`
	DownloaderID         uuid.UUID       `json:"downloaderId"`
	DownloaderExternalID *string         `json:"downloaderExternalId"`
	DownloaderStatus     *string         `json:"downloaderStatus"`
	Progress             *float64        `json:"progress"`
	SavePath             *string         `json:"savePath"`
	ContentPath          *string         `json:"contentPath"`
	AttemptCount         int32           `json:"attemptCount"`
	NextRunAt            time.Time       `json:"nextRunAt"`
	LastError            *string         `json:"lastError"`
	CreatedAt            time.Time       `json:"createdAt"`
	UpdatedAt            time.Time       `json:"updatedAt"`
	PreviousJobID        uuid.UUID       `json:"previousJobId"`
	DownloadSpeed        *int64          `json:"downloadSpeed"`
	EtaSeconds           *int64          `json:"etaSeconds"`
	TotalSize            *int64          `json:"totalSize"`
	ErrorKind            *apperrors.Kind `json:"errorKind" swaggertype:"string" extensions:"x-nullable"`
}

// DownloadJobWithSummary is a DownloadJob joined with the matched media item
// (poster, title, year, certification), the optional season/episode numbers,
// counts of related import tasks per status, and a computed import_status
// rollup. Mirrors dbgen.GetDownloadJobWithImportSummaryRow and
// dbgen.ListDownloadJobsWithImportSummaryRow (both have identical shapes).
type DownloadJobWithSummary struct {
	DownloadJob
	TmdbID             *int64  `json:"tmdbId"`
	MediaPosterPath    *string `json:"mediaPosterPath"`
	MediaTitle         *string `json:"mediaTitle"`
	MediaYear          *int32  `json:"mediaYear"`
	MediaCertification *string `json:"mediaCertification"`
	SeasonNumber       *int32  `json:"seasonNumber"`
	EpisodeNumber      *int32  `json:"episodeNumber"`
	TotalImportTasks   int32   `json:"totalImportTasks"`
	PendingImports     int32   `json:"pendingImports"`
	ActiveImports      int32   `json:"activeImports"`
	CompletedImports   int32   `json:"completedImports"`
	FailedImports      int32   `json:"failedImports"`
	CancelledImports   int32   `json:"cancelledImports"`
	ImportStatus       string  `json:"importStatus"`
}

// DownloadJobBySeriesEntry is a DownloadJob enriched with the season and
// episode numbers from joined media tables. Mirrors
// dbgen.ListDownloadJobsByTmdbSeriesIDRow.
type DownloadJobBySeriesEntry struct {
	DownloadJob
	SeasonNumber  *int32 `json:"seasonNumber"`
	EpisodeNumber *int32 `json:"episodeNumber"`
}

// DownloadJobHistoryEntry is a row in the retry chain. Mirrors the
// persistence shape dbgen.GetDownloadJobHistoryRow — a DownloadJob plus the
// chain depth from the recursive CTE.
type DownloadJobHistoryEntry struct {
	DownloadJob
	ChainDepth int32 `json:"chainDepth"`
}

// DownloadJobTimelineEvent is one entry in the combined download/import
// timeline for a download job. Mirrors dbgen.GetDownloadJobTimelineRow.
// Source is "download" or "import" depending on which table the row came
// from; ImportTaskID is set only for the import-sourced rows.
type DownloadJobTimelineEvent struct {
	Source       string    `json:"source"`
	ID           uuid.UUID `json:"id"`
	EventType    string    `json:"eventType"`
	OldStatus    *string   `json:"oldStatus"`
	NewStatus    *string   `json:"newStatus"`
	Message      *string   `json:"message"`
	Metadata     []byte    `json:"metadata" swaggertype:"object"`
	CreatedAt    time.Time `json:"createdAt"`
	ImportTaskID uuid.UUID `json:"importTaskId"`
}

// DownloadJobEvent is the domain shape for a row in the download_job_event
// log. Mirrors dbgen.DownloadJobEvent.
type DownloadJobEvent struct {
	ID            uuid.UUID `json:"id"`
	DownloadJobID uuid.UUID `json:"downloadJobId"`
	EventType     string    `json:"eventType"`
	OldStatus     *string   `json:"oldStatus"`
	NewStatus     *string   `json:"newStatus"`
	Message       *string   `json:"message"`
	Metadata      []byte    `json:"metadata" swaggertype:"object"`
	CreatedAt     time.Time `json:"createdAt"`
}
