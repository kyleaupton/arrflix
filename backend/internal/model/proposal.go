package model

import (
	"time"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/parsing"
)

// Proposal is the domain shape for a proposal row — a deferred grab held for
// one-tap approval. It carries the fully-resolved grab (the same fields as
// repo.CreateDownloadJobParams) so Approve replays it into the grab path with no
// re-parse or external I/O, plus display (Size/Seeders) and the supersede keys
// (Bin/Score) a strictly-better later pick is compared against. JSON-tagged: it
// is the API surface the series page renders. Mirrors dbgen.Proposal, with the
// three bin columns reconstructed into a parsing.BinKey by the repo translator.
type Proposal struct {
	ID          uuid.UUID `json:"id"`
	TrackingID  uuid.UUID `json:"trackingId"`
	MediaItemID uuid.UUID `json:"mediaItemId"`
	IsPack      bool      `json:"isPack"`

	// Resolved-grab fields (mirror repo.CreateDownloadJobParams).
	Protocol       string     `json:"protocol"`
	MediaType      string     `json:"mediaType"`
	SeasonID       *uuid.UUID `json:"seasonId,omitempty"`
	EpisodeID      *uuid.UUID `json:"episodeId,omitempty"`
	IndexerID      int64      `json:"indexerId"`
	GUID           string     `json:"guid"`
	CandidateTitle string     `json:"candidateTitle"`
	CandidateLink  string     `json:"candidateLink"`
	DownloaderID   uuid.UUID  `json:"downloaderId"`
	LibraryID      uuid.UUID  `json:"libraryId"`
	NameTemplateID uuid.UUID  `json:"nameTemplateId"`

	// Display.
	Size    int64 `json:"size"`
	Seeders int   `json:"seeders"`

	// Supersede comparison keys.
	Bin   parsing.BinKey `json:"bin"`
	Score int            `json:"score"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ProposalView is a Proposal plus the episode/want ids it covers — the read
// shape the series grid consumes to map an episode to its proposal. A pack
// proposal maps several CoveredEpisodeIDs to the one proposal object, so Approve/
// Decline from any covered episode act on the same proposal id.
type ProposalView struct {
	Proposal
	CoveredEpisodeIDs []uuid.UUID `json:"coveredEpisodeIds"`
	CoveredWantIDs    []uuid.UUID `json:"coveredWantIds"`
}
