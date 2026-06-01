package realtime

import "github.com/google/uuid"

// Import event names. Snake_case on the wire.
const (
	NameImportTaskUpdated = "import_task_updated"
)

// ImportTaskUpdatedPayload identifies which import task changed. The prior
// wire payload was null and the task id rode the SSE `id:` line — but that
// line was dropped by the old huma adapter (its Message.ID is an int), so the
// id never reached any consumer. The `id:` line is now a UUIDv7 for resume,
// not a domain id, so the task id moves into the payload where a consumer
// that re-fetches by id can actually read it.
type ImportTaskUpdatedPayload struct {
	TaskID uuid.UUID `json:"taskId"`
}

// ImportTaskUpdated builds the import_task_updated event for the given task.
func ImportTaskUpdated(taskID uuid.UUID) Event {
	return Event{
		Name:      NameImportTaskUpdated,
		Recipient: Broadcast,
		Data:      mustMarshal(ImportTaskUpdatedPayload{TaskID: taskID}),
	}
}
