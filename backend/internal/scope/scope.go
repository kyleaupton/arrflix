// Package scope answers "which episodes does this tracking want?" It is the
// pure domain concept that sits between the synced episode tree and per-episode
// want generation. Scope is held per-requester on a tracking (one preset rule +
// per-episode overrides); the tracking's effective scope is the union across
// requesters.
//
// This package is deliberately pure: it imports only uuid + time + stdlib, has
// no repo/db/model dependencies, and defines its own minimal Episode shape
// (model.MediaEpisode lacks the season *number* the presets need). The service
// in internal/service/ builds Episodes from model.MediaEpisode + the season
// number and feeds them here.
package scope

import (
	"sort"
	"time"

	"github.com/google/uuid"
)

// RuleKind names a scope preset. The set is fixed and mirrors the CHECK
// constraint on tracking_requester.scope_rule.
type RuleKind string

const (
	RuleAll                    RuleKind = "all"
	RuleFutureOnly             RuleKind = "future_only"
	RuleSeason                 RuleKind = "season"
	RulePilot                  RuleKind = "pilot"
	RuleLatestSeasonPlusFuture RuleKind = "latest_season_plus_future"
)

// Rule is a preset plus its single parameter. Season is consulted only for
// RuleSeason and ignored otherwise.
type Rule struct {
	Kind   RuleKind
	Season int
}

// Overrides doubles as the JSONB storage DTO (tracking_requester.scope_overrides)
// and the per-requester resolver input. Episodes are keyed on the internal
// media_episode UUID, which is stable across TMDB renumbering.
type Overrides struct {
	Include []uuid.UUID `json:"include,omitempty"`
	Exclude []uuid.UUID `json:"exclude,omitempty"`
}

// Episode is the minimal shape the resolver needs. The service builds these
// from model.MediaEpisode + the season number. AirDate is nil for unaired or
// unknown episodes.
type Episode struct {
	ID      uuid.UUID
	Season  int
	Number  int
	AirDate *time.Time
}

// RequesterScope is one requester's intent. Anchor is the requester's
// follow-start time, which drives future_only.
type RequesterScope struct {
	Rule      Rule
	Overrides Overrides
	Anchor    time.Time
}

// InScope resolves one requester's scope into a per-episode decision map. now
// drives latest_season_plus_future. An episode is wanted iff
// (preset says yes AND not excluded) OR explicitly included — so an Include
// override can pull in a special (season 0) that no preset selects, and an
// Exclude override can drop an episode a preset would otherwise want.
func InScope(rs RequesterScope, eps []Episode, now time.Time) map[uuid.UUID]bool {
	preset := presetSelect(rs.Rule, eps, rs.Anchor, now)

	exclude := make(map[uuid.UUID]bool, len(rs.Overrides.Exclude))
	for _, id := range rs.Overrides.Exclude {
		exclude[id] = true
	}
	include := make(map[uuid.UUID]bool, len(rs.Overrides.Include))
	for _, id := range rs.Overrides.Include {
		include[id] = true
	}

	out := make(map[uuid.UUID]bool, len(eps))
	for _, ep := range eps {
		out[ep.ID] = (preset[ep.ID] && !exclude[ep.ID]) || include[ep.ID]
	}
	return out
}

// EffectiveInScope unions multiple requesters' scopes: an episode is in the
// effective set if any requester wants it. The returned slice is sorted by
// (season, number) for determinism.
func EffectiveInScope(scopes []RequesterScope, eps []Episode, now time.Time) []uuid.UUID {
	wanted := make(map[uuid.UUID]bool, len(eps))
	for _, rs := range scopes {
		for id, in := range InScope(rs, eps, now) {
			if in {
				wanted[id] = true
			}
		}
	}

	pos := make(map[uuid.UUID]Episode, len(eps))
	for _, ep := range eps {
		pos[ep.ID] = ep
	}

	out := make([]uuid.UUID, 0, len(wanted))
	for id := range wanted {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := pos[out[i]], pos[out[j]]
		if a.Season != b.Season {
			return a.Season < b.Season
		}
		return a.Number < b.Number
	})
	return out
}

// presetSelect applies a preset (no overrides) to the episode list. Specials
// (season 0) are excluded by every preset — they're includable only via an
// explicit Include override.
func presetSelect(rule Rule, eps []Episode, anchor, now time.Time) map[uuid.UUID]bool {
	out := make(map[uuid.UUID]bool, len(eps))
	switch rule.Kind {
	case RuleAll:
		for _, ep := range eps {
			out[ep.ID] = ep.Season >= 1
		}

	case RuleFutureOnly:
		// Unaired/unknown (nil air date) is treated as future.
		for _, ep := range eps {
			out[ep.ID] = ep.Season >= 1 && (ep.AirDate == nil || ep.AirDate.After(anchor))
		}

	case RuleSeason:
		for _, ep := range eps {
			out[ep.ID] = ep.Season == rule.Season
		}

	case RulePilot:
		if p, ok := pilot(eps); ok {
			out[p] = true
		}

	case RuleLatestSeasonPlusFuture:
		l := latestBegunSeason(eps, now)
		for _, ep := range eps {
			out[ep.ID] = ep.Season >= 1 && ep.Season >= l
		}
	}
	return out
}

// pilot returns the ID of the single lowest (season, number) episode among
// seasons >= 1.
func pilot(eps []Episode) (uuid.UUID, bool) {
	var best *Episode
	for i := range eps {
		ep := &eps[i]
		if ep.Season < 1 {
			continue
		}
		if best == nil || ep.Season < best.Season || (ep.Season == best.Season && ep.Number < best.Number) {
			best = ep
		}
	}
	if best == nil {
		return uuid.Nil, false
	}
	return best.ID, true
}

// latestBegunSeason returns L: the highest season (>= 1) that has begun airing
// — has at least one episode with AirDate <= now. Fallback when nothing has
// aired yet: the lowest season >= 1 (0 if there are no real seasons, which
// selects nothing since presetSelect also gates on Season >= 1).
func latestBegunSeason(eps []Episode, now time.Time) int {
	begun := 0
	lowest := 0
	for _, ep := range eps {
		if ep.Season < 1 {
			continue
		}
		if lowest == 0 || ep.Season < lowest {
			lowest = ep.Season
		}
		if ep.AirDate != nil && !ep.AirDate.After(now) && ep.Season > begun {
			begun = ep.Season
		}
	}
	if begun == 0 {
		return lowest
	}
	return begun
}
