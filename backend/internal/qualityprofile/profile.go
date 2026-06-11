// Package qualityprofile is the pure selection engine for quality profiles. It
// decides which release to grab, whether an imported file passes verification,
// and whether a candidate is a worthwhile upgrade over what is already held.
//
// The package is pure in the domain-module sense: it holds no state, touches no
// repo/DB/HTTP, and evaluates plain in-memory values. A Profile carries two
// kinds of constraint: structured bin identity (ranked allowed bins + cutoff,
// keyed on parsing.BinKey because you cannot order a string enum) and condition
// trees (hard gates and scored custom formats), which it delegates to the rules
// substrate via reg.Eval. Persistence, the admin API, and the UI sit on top of
// the surface defined here.
package qualityprofile

import (
	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/rules"
)

// Profile is a resolved quality profile: the in-memory shape the engine
// evaluates against, already joined from its persisted parts (bins, sizes,
// gates, and custom-format references resolved to trees + weights).
type Profile struct {
	ID     uuid.UUID
	Name   string
	Domain parsing.Domain

	// Bins is the allowed bin identities, ranked best-first (index 0 is best).
	// A release whose bin is absent from this list is gated out.
	Bins []parsing.BinKey
	// Cutoff is the bin at which upgrading stops; it must be one of Bins.
	Cutoff parsing.BinKey
	// Sizes is the per-bin runtime-scaled size band. Dormant until media.runtime
	// is a registered field (Phase 3 wires size gating); the type and field ship
	// now so the persisted shape and the engine seam are stable.
	Sizes map[parsing.BinKey]SizeBand

	// MinSeeders gates torrents with too few seeders. Checked structurally.
	MinSeeders int
	// Indexers scopes which indexers a search fans out to. It is carried config,
	// applied by the service at search time — NOT an engine gate, because the
	// arrflix indexer UUID cannot be matched against a release's numeric
	// candidate.indexer_id without the repo.
	Indexers []uuid.UUID

	// Gates are hard gates: a tree that evaluates True removes the release.
	Gates []Gate
	// Formats are scored custom formats: a tree that evaluates True adds Weight.
	Formats []ScoredFormat
}

// SizeBand is a per-bin size constraint in runtime-scaled bytes-per-minute. The
// engine scales these by a release's runtime to a byte range at evaluation time.
type SizeBand struct {
	MinBytesPerMin       int64
	PreferredBytesPerMin int64
	MaxBytesPerMin       int64
}

// scaled projects the per-minute band onto an absolute byte range for a release
// of the given runtime (minutes). A zero bound stays zero — the engine reads
// zero as "unbounded" for that side.
func (b SizeBand) scaled(runtimeMin int) (min, max int64) {
	return b.MinBytesPerMin * int64(runtimeMin), b.MaxBytesPerMin * int64(runtimeMin)
}

// runtimeOf reads a subject's matched runtime in minutes. A nil runtime yields
// false, and the caller skips size enforcement — a safe no-op that preserves
// correctness while runtime population rolls out.
func runtimeOf(s model.Subject) (int, bool) {
	if s.Media.Runtime == nil {
		return 0, false
	}
	return *s.Media.Runtime, true
}

// Gate is a hard gate: when Tree evaluates True the release is rejected. Tree is
// a pointer because rules.Eval takes *rules.Condition and JSONB unmarshals to
// one; a nil Tree is an inert gate. Note Gate the type and Profile.Gate the
// method coexist — Go scopes them separately.
type Gate struct {
	Name string
	Tree *rules.Condition
}

// ScoredFormat is a custom format: when Tree evaluates True its Weight is added
// to the release's score. Tree is a pointer for the same reason as Gate.Tree.
type ScoredFormat struct {
	Name   string
	Tree   *rules.Condition
	Weight int
}

// binOf projects a subject's parsed quality core into this domain's bin
// identity. The model advertises "NONE" for an absent modifier (a display-layer
// enum value) while parsing's ModNone is the empty string; normalize so the cast
// lands on ModNone. BinFor folds an unrecognized modifier into the plain-source
// bin anyway, so this only affects clarity, not behavior.
func binOf(s model.Subject, d parsing.Domain) parsing.BinKey {
	mod := s.Release.Quality.Modifier.Value
	if mod == "NONE" {
		mod = string(parsing.ModNone)
	}
	return parsing.BinFor(
		parsing.Source(s.Release.Quality.Source.Value),
		parsing.Resolution(s.Release.Quality.Resolution.Value),
		parsing.Modifier(mod),
		d,
	).Key()
}

// rank returns a bin's position in the ranked allowed list (0 = best). A bin not
// in the list ranks worse than every allowed bin (len(Bins)), so unknown/unlisted
// quality always loses an ordering comparison.
func (p Profile) rank(b parsing.BinKey) int {
	for i, allowed := range p.Bins {
		if allowed == b {
			return i
		}
	}
	return len(p.Bins)
}

// BelowCutoff reports whether a held file's bin ranks strictly below the
// profile's cutoff (or is unlisted). The cutoff is met when this is false;
// acquisition uses it to decide whether to look for upgrades at all. Upgrade
// worthiness of a found candidate is StrictlyBetter's concern.
func (p Profile) BelowCutoff(current parsing.BinKey) bool {
	return p.rank(current) > p.rank(p.Cutoff)
}

// ResolveTier maps a (tier, domain) pair to its bound profile. The binding table
// is seeded with HD/4K per media type; "Best available" is a reserved future
// tier, not a flag.
func ResolveTier(t Tier, d parsing.Domain, bindings []TierBinding) (uuid.UUID, bool) {
	for _, b := range bindings {
		if b.Tier == t && b.Domain == d {
			return b.ProfileID, true
		}
	}
	return uuid.Nil, false
}

// Tier is a coarse quality preset a requester selects; it resolves to a concrete
// profile per media type via a TierBinding.
type Tier string

const (
	TierHD Tier = "HD"
	Tier4K Tier = "4K"
)

// TierBinding binds a (Tier, Domain) to a concrete profile.
type TierBinding struct {
	Tier      Tier
	Domain    parsing.Domain
	ProfileID uuid.UUID
}
