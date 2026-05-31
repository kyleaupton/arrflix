package realtime

import "github.com/kyleaupton/arrflix/internal/model"

// Download event names. Snake_case on the wire; frontend consumers
// (downloadJobs.ts) listen for these literals.
const (
	NameDownloadJobsSnapshot = "download_jobs_snapshot"
	NameDownloadJobUpdated   = "download_job_updated"
)

// DownloadJobsSnapshot builds the connect-time snapshot of every download job.
// The payload is the list as-is — byte-identical to the prior wire shape.
func DownloadJobsSnapshot(jobs []model.DownloadJobWithSummary) Event {
	return Event{Name: NameDownloadJobsSnapshot, Recipient: Broadcast, Data: mustMarshal(jobs)}
}

// DownloadJobUpdated builds a per-change delta for one download job.
func DownloadJobUpdated(job model.DownloadJobWithSummary) Event {
	return Event{Name: NameDownloadJobUpdated, Recipient: Broadcast, Data: mustMarshal(job)}
}
