package model

import (
	"time"

	"github.com/google/uuid"
)

// TitleStatus is the acquisition read model for one title, as seen by one
// viewer — the single answer every surface renders from, rather than each
// deriving its own from wants, jobs, files, and requests.
//
// State is the headline the chip shows. It is deliberately not the whole
// truth: Library and Work carry facts that hold *simultaneously* with it, so a
// title can be available and still working (an upgrade in flight) without
// needing a state for every combination.
//
// See specs/modules/title-status/README.md.
type TitleStatus struct {
	MediaType string `json:"mediaType"`
	TmdbID    int64  `json:"tmdbId"`

	// State is the headline: one of not_requested, unreleased,
	// awaiting_approval, denied, searching, needs_pick, proposed, downloading,
	// importing, available, partially_available, unavailable, canceled.
	State string `json:"state"`
	// Phase is what the pipeline is actively doing: searching, downloading,
	// importing, or empty when nothing is in flight.
	Phase string `json:"phase,omitempty"`
	// Active is true while work is in flight, independent of State. An
	// available title with Active true is being upgraded.
	Active bool `json:"active"`

	Library TitleLibrary `json:"library"`
	Counts  TitleCounts  `json:"counts"`

	// Viewer and Actions are the only viewer-dependent parts of the projection.
	// Everything above holds for anyone looking at the title.
	Viewer  TitleViewer   `json:"viewer"`
	Actions []TitleAction `json:"actions"`

	// Episodes carries per-episode state for a series, in season/episode order.
	// Nil for movies.
	Episodes []TitleEpisodeStatus `json:"episodes,omitempty"`
}

// TitleLibrary is what the library holds for a title.
type TitleLibrary struct {
	HasFiles  bool `json:"hasFiles"`
	FileCount int  `json:"fileCount"`
}

// TitleCounts summarizes the acquirable atoms. Available and Working overlap:
// an atom with a file and an upgrade in flight counts in both.
type TitleCounts struct {
	Total     int `json:"total"`
	Available int `json:"available"`
	Working   int `json:"working"`
}

// TitleEpisodeStatus is one episode's cell in a season grid, carrying the same
// state vocabulary as the title headline so the two cannot disagree.
type TitleEpisodeStatus struct {
	EpisodeID     uuid.UUID  `json:"episodeId"`
	SeasonNumber  int32      `json:"seasonNumber"`
	EpisodeNumber int32      `json:"episodeNumber"`
	State         string     `json:"state"`
	AirDate       *time.Time `json:"airDate,omitempty"`
}

// TitleViewer is who the projection was computed for. It carries only what the
// UI needs to phrase itself in the second person ("your request"); the grant set
// itself never crosses the wire, only its consequences in Actions.
//
// RequestID is what the cancel action acts on. An action the server offers must
// come with whatever taking it requires, or the client is forced back into the
// cross-cache join this projection exists to remove. Nil when the viewer has no
// live request.
// Intent is what this viewer asked for, which is not the same as what the
// system is doing: tracking.quality_profile_id is single-valued, so two
// requesters wanting different tiers cannot both be acted on. Reporting the ask
// back is honest and useful ("your request: 4K, full series") without implying
// the system agreed to it.
type TitleViewer struct {
	IsRequester bool         `json:"isRequester"`
	RequestID   *uuid.UUID   `json:"requestId,omitempty"`
	Intent      *TitleIntent `json:"intent,omitempty"`
}

// TitleIntent is the shape of the viewer's own live request. ScopeRule is empty
// for a movie, which has no scope to choose.
type TitleIntent struct {
	Tier      string `json:"tier" enum:"HD,4K"`
	ScopeRule string `json:"scopeRule,omitempty" enum:",all,future_only"`
}

// TitleAction is an affordance plus its consequences. A disabled action is
// present only when it needs to explain itself; one that simply does not apply
// is omitted.
type TitleAction struct {
	Kind             string            `json:"kind"`
	Enabled          bool              `json:"enabled"`
	RequiresApproval bool              `json:"requiresApproval"`
	Tiers            []TitleActionTier `json:"tiers,omitempty"`
	DisabledReason   string            `json:"disabledReason,omitempty"`
}

// TitleActionTier is a tier the viewer may request at. Approval is per tier: a
// viewer can be trusted with HD on their own and still need a decision for 4K.
//
// Tier carries the same enum as the request body it is destined for, so a tier
// offered here is one the create endpoint will accept — the client cannot
// assemble a request the API would reject.
type TitleActionTier struct {
	Tier             string `json:"tier" enum:"HD,4K"`
	RequiresApproval bool   `json:"requiresApproval"`
}
