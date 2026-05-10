package model

import (
	"time"

	"github.com/google/uuid"
)

// MediaFileImport is the domain shape for a media_file_import row. It mirrors
// the persistence-layer dbgen.MediaFileImport but uses idiomatic Go types
// (uuid.UUID, time.Time, *T for nullable scalars) and lives outside the
// persistence boundary.
type MediaFileImport struct {
	ID           uuid.UUID `json:"id"`
	MediaFileID  uuid.UUID `json:"mediaFileId"`
	ImportTaskID uuid.UUID `json:"importTaskId"`
	Method       string    `json:"method"`
	SourcePath   *string   `json:"sourcePath,omitempty"`
	DestPath     string    `json:"destPath"`
	AttemptedAt  time.Time `json:"attemptedAt"`
	Success      bool      `json:"success"`
	ErrorMessage *string   `json:"errorMessage,omitempty"`
}
