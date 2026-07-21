package realtime

import (
	"github.com/kyleaupton/arrflix/internal/authz"
	"github.com/kyleaupton/arrflix/internal/model"
)

// Download event names. Snake_case on the wire; the frontend realtime bindings
// listen for these literals.
const (
	NameDownloadJobUpdated = "download_job_updated"
)

// DownloadJobUpdated builds a per-change delta for one download job. It targets
// jobs.read holders: the payload carries the candidate release title, indexer,
// and client state — operator data, and the same grant that gates the REST
// download-jobs list.
func DownloadJobUpdated(job model.DownloadJobWithSummary) Event {
	return Event{
		Name:      NameDownloadJobUpdated,
		Recipient: Capability(authz.JobsRead),
		Data:      mustMarshal(job),
	}
}
