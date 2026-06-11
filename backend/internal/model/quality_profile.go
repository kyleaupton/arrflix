package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/parsing"
)

// QualityProfile is the domain shape for one persisted quality profile row. It
// carries structured bin identity (the ranked Bins and the Cutoff, typed as
// parsing.BinKey) and the opaque condition trees the engine evaluates.
//
// Gates stays raw bytes because model cannot name *rules.Condition (rules
// imports model for the Subject); the quality service decodes/encodes the
// [{name, tree}] array at its boundary, exactly as RoutingService does for
// RoutingRule.Conditions. Bins/Cutoff/SizeOverrides are typed here because
// model may import parsing — parsing.BinKey is a plain value triple.
//
// Indexers scopes which indexers a search fans out to; it is carried config the
// service applies, never an engine gate. SizeOverrides layers per-bin size
// bands on top of the global quality_size_default rows at resolve time.
type QualityProfile struct {
	ID            uuid.UUID        `json:"id"`
	Name          string           `json:"name"`
	Domain        string           `json:"domain"`
	Bins          []parsing.BinKey `json:"bins"`
	Cutoff        parsing.BinKey   `json:"cutoff"`
	MinSeeders    int              `json:"minSeeders"`
	Indexers      []uuid.UUID      `json:"indexers"`
	Gates         json.RawMessage  `json:"gates"`
	SizeOverrides []QualityBinSize `json:"sizeOverrides"`
	CreatedAt     time.Time        `json:"createdAt"`
	UpdatedAt     time.Time        `json:"updatedAt"`
}

// QualityBinSize is a per-bin size band override in bytes-per-minute. Min,
// Preferred, and Max each treat 0 as "unbounded" for that side, matching the
// engine and the quality_size_default seed.
type QualityBinSize struct {
	Bin       parsing.BinKey `json:"bin"`
	Min       int64          `json:"min"`
	Preferred int64          `json:"preferred"`
	Max       int64          `json:"max"`
}

// CustomFormat is a reusable named scorer (the *arr-style entity). Conditions is
// the canonical rules.Condition tree carried as raw bytes — the service decodes
// it when assembling a profile's scored formats.
type CustomFormat struct {
	ID         uuid.UUID       `json:"id"`
	Name       string          `json:"name"`
	Domain     string          `json:"domain"`
	Conditions json.RawMessage `json:"conditions"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

// ProfileFormat is one quality_profile_format join row: a custom format
// assigned to a profile with a scoring weight.
type ProfileFormat struct {
	CustomFormatID uuid.UUID `json:"customFormatId"`
	Weight         int       `json:"weight"`
}

// TierBinding binds a coarse (Tier, Domain) selection to a concrete profile.
type TierBinding struct {
	Tier      string    `json:"tier"`
	Domain    string    `json:"domain"`
	ProfileID uuid.UUID `json:"profileId"`
}

// QualitySizeDefault is one global per-domain per-bin size default in
// bytes-per-minute. 0 means "unbounded" for that side.
type QualitySizeDefault struct {
	Domain    string         `json:"domain"`
	Bin       parsing.BinKey `json:"bin"`
	Min       int64          `json:"min"`
	Preferred int64          `json:"preferred"`
	Max       int64          `json:"max"`
}
