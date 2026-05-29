package model

import (
	"time"

	"github.com/google/uuid"
)

// FileImport is the domain shape for a file_import row — the per-attempt
// import audit (hardlink/copy/scan/manual_match) anchored to a file.
type FileImport struct {
	ID           uuid.UUID `json:"id"`
	FileID       uuid.UUID `json:"fileId"`
	ImportTaskID uuid.UUID `json:"importTaskId"`
	Method       string    `json:"method"`
	SourcePath   *string   `json:"sourcePath,omitempty"`
	DestPath     string    `json:"destPath"`
	AttemptedAt  time.Time `json:"attemptedAt"`
	Success      bool      `json:"success"`
	ErrorMessage *string   `json:"errorMessage,omitempty"`
}
