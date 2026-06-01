package realtime

import "github.com/google/uuid"

// Import event names. Snake_case on the wire.
const (
	NameImportTaskUpdated = "import_task_updated"
)

// ImportTaskUpdatedPayload identifies which import task changed. The task id
// travels in the payload, not the SSE `id:` line — that line carries a UUIDv7
// for Last-Event-ID resume, not a domain id — so a consumer can refetch by id.
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
