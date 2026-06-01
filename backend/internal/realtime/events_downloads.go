package realtime

import "github.com/kyleaupton/arrflix/internal/model"

// Download event names. Snake_case on the wire; the frontend realtime bindings
// listen for these literals.
const (
	NameDownloadJobUpdated = "download_job_updated"
)

// DownloadJobUpdated builds a per-change delta for one download job.
func DownloadJobUpdated(job model.DownloadJobWithSummary) Event {
	return Event{Name: NameDownloadJobUpdated, Recipient: Broadcast, Data: mustMarshal(job)}
}
