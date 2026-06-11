package qualityprofile

import (
	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/rules"
)

// Fixed preset IDs so seeding is idempotent and tests are stable.
var (
	presetHDMovie  = uuid.MustParse("a0000000-0000-0000-0000-000000000001")
	preset4KMovie  = uuid.MustParse("a0000000-0000-0000-0000-000000000002")
	presetHDSeries = uuid.MustParse("a0000000-0000-0000-0000-000000000003")
	preset4KSeries = uuid.MustParse("a0000000-0000-0000-0000-000000000004")
)

// Fixed custom-format IDs, one per domain, so seeding the repack/proper scorer
// is idempotent and the join references are stable across restarts.
var (
	repackFormatMovie  = uuid.MustParse("a0000000-0000-0000-0001-000000000001")
	repackFormatSeries = uuid.MustParse("a0000000-0000-0000-0001-000000000002")
)

// SeedCustomFormatID maps a seeded custom format (domain + name) to its fixed
// UUID, so the service writes stable custom_format rows and join references when
// seeding the presets. The bool is false for a name that isn't a seeded format.
func SeedCustomFormatID(domain parsing.Domain, name string) (uuid.UUID, bool) {
	if name != repackProper().Name {
		return uuid.Nil, false
	}
	switch domain {
	case parsing.DomainMovie:
		return repackFormatMovie, true
	case parsing.DomainSeries:
		return repackFormatSeries, true
	default:
		return uuid.Nil, false
	}
}

// DefaultTierBindings returns the seeded (tier, domain) → preset bindings: HD
// and 4K for each media domain. The service seeds these idempotently alongside
// the preset profiles.
func DefaultTierBindings() []TierBinding {
	return []TierBinding{
		{Tier: TierHD, Domain: parsing.DomainMovie, ProfileID: presetHDMovie},
		{Tier: Tier4K, Domain: parsing.DomainMovie, ProfileID: preset4KMovie},
		{Tier: TierHD, Domain: parsing.DomainSeries, ProfileID: presetHDSeries},
		{Tier: Tier4K, Domain: parsing.DomainSeries, ProfileID: preset4KSeries},
	}
}

func bin(s parsing.Source, r parsing.Resolution, m parsing.Modifier) parsing.BinKey {
	return parsing.BinKey{Source: s, Resolution: r, Modifier: m}
}

// repackProper is the one seeded search-phase scorer: a repack/proper edges out
// an otherwise-equal original. Codec/HDR/audio formats are deferred to the
// custom-format fast-follow once parsing registers those as search-phase fields.
func repackProper() ScoredFormat {
	return ScoredFormat{
		Name:   "Repack/Proper",
		Weight: 5,
		Tree: &rules.Condition{
			Op:    rules.OpEq,
			Left:  &rules.Operand{Kind: rules.OperandField, Path: "quality.is_repack"},
			Right: &rules.Operand{Kind: rules.OperandLiteral, Literal: &rules.Literal{Type: rules.LitBool, Bool: true}},
		},
	}
}

// DefaultProfiles returns the seeded HD/4K presets for movie and series. Bin
// lists are an editorial best-first ranking drawn from the upstream vocabulary;
// every Cutoff is a member of its profile's Bins. Sizes are left nil — global
// per-domain size defaults are merged by the service in Phase 3 once a runtime
// field exists to scale them.
func DefaultProfiles() []Profile {
	return []Profile{
		{
			ID:     presetHDMovie,
			Name:   "HD",
			Domain: parsing.DomainMovie,
			Bins: []parsing.BinKey{
				bin(parsing.SourceBluRay, parsing.Res1080p, parsing.ModRemux),
				bin(parsing.SourceBluRay, parsing.Res1080p, parsing.ModNone),
				bin(parsing.SourceWEBDL, parsing.Res1080p, parsing.ModNone),
				bin(parsing.SourceWEBRip, parsing.Res1080p, parsing.ModNone),
				bin(parsing.SourceTV, parsing.Res1080p, parsing.ModNone),
				bin(parsing.SourceBluRay, parsing.Res720p, parsing.ModNone),
				bin(parsing.SourceWEBDL, parsing.Res720p, parsing.ModNone),
				bin(parsing.SourceWEBRip, parsing.Res720p, parsing.ModNone),
				bin(parsing.SourceTV, parsing.Res720p, parsing.ModNone),
			},
			Cutoff:  bin(parsing.SourceBluRay, parsing.Res1080p, parsing.ModNone),
			Formats: []ScoredFormat{repackProper()},
		},
		{
			ID:     preset4KMovie,
			Name:   "4K",
			Domain: parsing.DomainMovie,
			Bins: []parsing.BinKey{
				bin(parsing.SourceBluRay, parsing.Res2160p, parsing.ModRemux),
				bin(parsing.SourceBluRay, parsing.Res2160p, parsing.ModNone),
				bin(parsing.SourceWEBDL, parsing.Res2160p, parsing.ModNone),
				bin(parsing.SourceWEBRip, parsing.Res2160p, parsing.ModNone),
				bin(parsing.SourceTV, parsing.Res2160p, parsing.ModNone),
			},
			Cutoff:  bin(parsing.SourceBluRay, parsing.Res2160p, parsing.ModNone),
			Formats: []ScoredFormat{repackProper()},
		},
		{
			ID:     presetHDSeries,
			Name:   "HD",
			Domain: parsing.DomainSeries,
			Bins: []parsing.BinKey{
				bin(parsing.SourceBluRay, parsing.Res1080p, parsing.ModRemux),
				bin(parsing.SourceBluRay, parsing.Res1080p, parsing.ModNone),
				bin(parsing.SourceWEBDL, parsing.Res1080p, parsing.ModNone),
				bin(parsing.SourceWEBRip, parsing.Res1080p, parsing.ModNone),
				bin(parsing.SourceTV, parsing.Res1080p, parsing.ModNone),
				bin(parsing.SourceBluRay, parsing.Res720p, parsing.ModNone),
				bin(parsing.SourceWEBDL, parsing.Res720p, parsing.ModNone),
				bin(parsing.SourceWEBRip, parsing.Res720p, parsing.ModNone),
				bin(parsing.SourceTV, parsing.Res720p, parsing.ModNone),
			},
			Cutoff:  bin(parsing.SourceBluRay, parsing.Res1080p, parsing.ModNone),
			Formats: []ScoredFormat{repackProper()},
		},
		{
			ID:     preset4KSeries,
			Name:   "4K",
			Domain: parsing.DomainSeries,
			Bins: []parsing.BinKey{
				bin(parsing.SourceBluRay, parsing.Res2160p, parsing.ModRemux),
				bin(parsing.SourceBluRay, parsing.Res2160p, parsing.ModNone),
				bin(parsing.SourceWEBDL, parsing.Res2160p, parsing.ModNone),
				bin(parsing.SourceWEBRip, parsing.Res2160p, parsing.ModNone),
				bin(parsing.SourceTV, parsing.Res2160p, parsing.ModNone),
			},
			Cutoff:  bin(parsing.SourceBluRay, parsing.Res2160p, parsing.ModNone),
			Formats: []ScoredFormat{repackProper()},
		},
	}
}
