// Package state provides state machine definitions and validation for job workflows.
package state

import (
	"fmt"
	"slices"
)

// DownloadJobStatus represents the status of a download job.
type DownloadJobStatus string

const (
	DownloadCreated     DownloadJobStatus = "created"
	DownloadEnqueued    DownloadJobStatus = "enqueued"
	DownloadDownloading DownloadJobStatus = "downloading"
	DownloadCompleted   DownloadJobStatus = "completed"
	DownloadFailed      DownloadJobStatus = "failed"
	DownloadCancelled   DownloadJobStatus = "cancelled"
)

// ImportTaskStatus represents the status of an import task.
type ImportTaskStatus string

const (
	ImportPending    ImportTaskStatus = "pending"
	ImportInProgress ImportTaskStatus = "in_progress"
	ImportCompleted  ImportTaskStatus = "completed"
	ImportFailed     ImportTaskStatus = "failed"
	ImportCancelled  ImportTaskStatus = "cancelled"
)

// DownloadJobTransitions defines valid state transitions for download jobs.
var DownloadJobTransitions = map[DownloadJobStatus][]DownloadJobStatus{
	DownloadCreated:     {DownloadEnqueued, DownloadFailed, DownloadCancelled},
	DownloadEnqueued:    {DownloadDownloading, DownloadCompleted, DownloadFailed, DownloadCancelled}, // completed: torrent may already exist in client
	DownloadDownloading: {DownloadCompleted, DownloadFailed, DownloadCancelled},
	DownloadCompleted:   {}, // Terminal
	DownloadFailed:      {}, // Terminal
	DownloadCancelled:   {}, // Terminal
}

// ImportTaskTransitions defines valid state transitions for import tasks.
var ImportTaskTransitions = map[ImportTaskStatus][]ImportTaskStatus{
	ImportPending:    {ImportInProgress, ImportCancelled},
	ImportInProgress: {ImportCompleted, ImportFailed},
	ImportCompleted:  {}, // Terminal - reimport creates new task
	ImportFailed:     {}, // Terminal - retry creates new task
	ImportCancelled:  {}, // Terminal
}

// DownloadJobMachine validates download job state transitions.
type DownloadJobMachine struct{}

// NewDownloadJobMachine creates a new download job state machine.
func NewDownloadJobMachine() *DownloadJobMachine {
	return &DownloadJobMachine{}
}

// CanTransition checks if transitioning from 'from' to 'to' is valid.
func (m *DownloadJobMachine) CanTransition(from, to DownloadJobStatus) bool {
	allowed, ok := DownloadJobTransitions[from]
	if !ok {
		return false
	}
	return slices.Contains(allowed, to)
}

// CanTransitionStr checks if transitioning from 'from' to 'to' is valid using string statuses.
func (m *DownloadJobMachine) CanTransitionStr(from, to string) bool {
	return m.CanTransition(DownloadJobStatus(from), DownloadJobStatus(to))
}

// MustTransition validates a transition and returns an error if invalid.
func (m *DownloadJobMachine) MustTransition(from, to DownloadJobStatus) error {
	if !m.CanTransition(from, to) {
		return fmt.Errorf("invalid download job transition: %s -> %s", from, to)
	}
	return nil
}

// MustTransitionStr validates a transition using string statuses.
func (m *DownloadJobMachine) MustTransitionStr(from, to string) error {
	return m.MustTransition(DownloadJobStatus(from), DownloadJobStatus(to))
}

// IsTerminal returns true if the status is a terminal state.
func (m *DownloadJobMachine) IsTerminal(status DownloadJobStatus) bool {
	allowed, ok := DownloadJobTransitions[status]
	return ok && len(allowed) == 0
}

// IsTerminalStr returns true if the string status is a terminal state.
func (m *DownloadJobMachine) IsTerminalStr(status string) bool {
	return m.IsTerminal(DownloadJobStatus(status))
}

// CanRetry returns true if the status allows retrying (only failed).
func (m *DownloadJobMachine) CanRetry(status DownloadJobStatus) bool {
	return status == DownloadFailed
}

// CanRetryStr returns true if the string status allows retrying.
func (m *DownloadJobMachine) CanRetryStr(status string) bool {
	return m.CanRetry(DownloadJobStatus(status))
}

// ImportTaskMachine validates import task state transitions.
type ImportTaskMachine struct{}

// NewImportTaskMachine creates a new import task state machine.
func NewImportTaskMachine() *ImportTaskMachine {
	return &ImportTaskMachine{}
}

// CanTransition checks if transitioning from 'from' to 'to' is valid.
func (m *ImportTaskMachine) CanTransition(from, to ImportTaskStatus) bool {
	allowed, ok := ImportTaskTransitions[from]
	if !ok {
		return false
	}
	return slices.Contains(allowed, to)
}

// CanTransitionStr checks if transitioning from 'from' to 'to' is valid using string statuses.
func (m *ImportTaskMachine) CanTransitionStr(from, to string) bool {
	return m.CanTransition(ImportTaskStatus(from), ImportTaskStatus(to))
}

// MustTransition validates a transition and returns an error if invalid.
func (m *ImportTaskMachine) MustTransition(from, to ImportTaskStatus) error {
	if !m.CanTransition(from, to) {
		return fmt.Errorf("invalid import task transition: %s -> %s", from, to)
	}
	return nil
}

// MustTransitionStr validates a transition using string statuses.
func (m *ImportTaskMachine) MustTransitionStr(from, to string) error {
	return m.MustTransition(ImportTaskStatus(from), ImportTaskStatus(to))
}

// IsTerminal returns true if the status is a terminal state.
func (m *ImportTaskMachine) IsTerminal(status ImportTaskStatus) bool {
	allowed, ok := ImportTaskTransitions[status]
	return ok && len(allowed) == 0
}

// IsTerminalStr returns true if the string status is a terminal state.
func (m *ImportTaskMachine) IsTerminalStr(status string) bool {
	return m.IsTerminal(ImportTaskStatus(status))
}

// CanReimport returns true if the status allows reimporting (only terminal states).
func (m *ImportTaskMachine) CanReimport(status ImportTaskStatus) bool {
	return status == ImportCompleted || status == ImportFailed
}

// CanReimportStr returns true if the string status allows reimporting.
func (m *ImportTaskMachine) CanReimportStr(status string) bool {
	return m.CanReimport(ImportTaskStatus(status))
}
