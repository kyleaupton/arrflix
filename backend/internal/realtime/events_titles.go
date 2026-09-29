package realtime

import "github.com/kyleaupton/arrflix/internal/authz"

// Title event names. Snake_case on the wire; the frontend realtime bindings
// listen for these literals.
const (
	NameTitleStatus   = "title_status"
	NameTitleProgress = "title_progress"
)

// TitleStatusPayload names the title whose acquisition state changed. It
// deliberately carries no state.
//
// The projection is per-viewer: the requester's own request governs part of the
// headline, and the actions offered differ by grant. Computing that at emit time
// means one derivation per watching session, and a payload that can already be
// stale by the time it lands. Naming the title instead lets each client refetch
// through the endpoint that applies its own lens, which by construction cannot
// disagree with what a page reload would show. The cost is one round trip on an
// event that fires only on transitions.
type TitleStatusPayload struct {
	MediaType string `json:"mediaType"`
	TmdbID    int64  `json:"tmdbId"`
}

// TitleStatus signals that a title's acquisition state changed and any mounted
// view of it should refetch.
//
// It targets library.read, not jobs.read: the event says only that something
// moved, which is exactly what a requester watching their own request is
// entitled to know.
func TitleStatus(mediaType string, tmdbID int64) Event {
	return Event{
		Name:      NameTitleStatus,
		Recipient: Capability(authz.LibraryRead),
		Data:      mustMarshal(TitleStatusPayload{MediaType: mediaType, TmdbID: tmdbID}),
	}
}

// TitleProgressPayload is the per-tick decoration for a title with a transfer
// underway: how far along, how fast, how much longer.
//
// Unlike TitleStatusPayload this carries its numbers, because a refetch per tick
// would be absurd for a value that moves every few seconds. That is safe here
// precisely because progress is viewer-independent — a percentage is the same
// number for everyone — so there is no lens to apply and nothing to withhold.
// The operator-only detail (which release, which indexer, which client) stays on
// download_job_updated behind jobs.read.
//
// Progress is a 0..1 fraction, matching download_job.progress so the two
// surfaces cannot disagree about scale. A tick carries no state: it never
// changes what the UI believes is happening, only how far along it is, so a
// dropped or reordered tick is corrected by the next one.
type TitleProgressPayload struct {
	MediaType      string  `json:"mediaType"`
	TmdbID         int64   `json:"tmdbId"`
	Progress       float64 `json:"progress"`
	BytesPerSecond *int64  `json:"bytesPerSecond,omitempty"`
	EtaSeconds     *int64  `json:"etaSeconds,omitempty"`
}

// TitleProgress builds a tick for one title's in-flight transfer.
func TitleProgress(p TitleProgressPayload) Event {
	return Event{
		Name:      NameTitleProgress,
		Recipient: Capability(authz.LibraryRead),
		Data:      mustMarshal(p),
	}
}
