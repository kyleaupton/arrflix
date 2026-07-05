package scope

import (
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Fixed reference times so tests never touch the wall clock.
var (
	anchor = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now    = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
)

func date(s string) *time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return &t
}

// ep builds an Episode with a deterministic-but-unique ID. Tests refer to
// episodes by season/number via the bySN helper rather than holding IDs.
func ep(season, number int, air *time.Time) Episode {
	return Episode{ID: uuid.New(), Season: season, Number: number, AirDate: air}
}

// bySN maps (season, number) → episode ID for assertions.
func bySN(eps []Episode) map[[2]int]uuid.UUID {
	m := make(map[[2]int]uuid.UUID, len(eps))
	for _, e := range eps {
		m[[2]int{e.Season, e.Number}] = e.ID
	}
	return m
}

// wantedSN returns the (season, number) pairs an episode-decision map marks
// true, sorted for stable comparison.
func wantedSN(eps []Episode, in map[uuid.UUID]bool) [][2]int {
	id2sn := make(map[uuid.UUID][2]int, len(eps))
	for _, e := range eps {
		id2sn[e.ID] = [2]int{e.Season, e.Number}
	}
	var out [][2]int
	for id, ok := range in {
		if ok {
			out = append(out, id2sn[id])
		}
	}
	sortSN(out)
	return out
}

func sortSN(s [][2]int) {
	sort.Slice(s, func(i, j int) bool {
		if s[i][0] != s[j][0] {
			return s[i][0] < s[j][0]
		}
		return s[i][1] < s[j][1]
	})
}

func equalSN(a, b [][2]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// twoSeasons: S1 fully aired (before now), S2 partly aired / partly future,
// plus a special (S0). Used by most preset tests.
func twoSeasons() []Episode {
	return []Episode{
		ep(0, 1, date("2025-12-25")), // special, aired
		ep(1, 1, date("2026-02-01")), // aired before now, after anchor
		ep(1, 2, date("2026-02-08")),
		ep(2, 1, date("2026-05-15")), // aired before now
		ep(2, 2, date("2026-09-01")), // future relative to now
		ep(2, 3, nil),                // unaired/unknown
	}
}

func TestInScope_PresetAll(t *testing.T) {
	t.Parallel()
	eps := twoSeasons()
	rs := RequesterScope{Rule: Rule{Kind: RuleAll}, Anchor: anchor}

	got := wantedSN(eps, InScope(rs, eps, now))
	want := [][2]int{{1, 1}, {1, 2}, {2, 1}, {2, 2}, {2, 3}} // every season >= 1, special excluded
	if !equalSN(got, want) {
		t.Errorf("all = %v, want %v", got, want)
	}
}

func TestInScope_PresetFutureOnly(t *testing.T) {
	t.Parallel()
	eps := twoSeasons()
	rs := RequesterScope{Rule: Rule{Kind: RuleFutureOnly}, Anchor: anchor}

	// AirDate > anchor (2026-01-01) OR nil; special excluded. Every real
	// episode here airs after the anchor, plus the nil-date one.
	got := wantedSN(eps, InScope(rs, eps, now))
	want := [][2]int{{1, 1}, {1, 2}, {2, 1}, {2, 2}, {2, 3}}
	if !equalSN(got, want) {
		t.Errorf("future_only = %v, want %v", got, want)
	}
}

func TestInScope_FutureOnlyAnchorCutoff(t *testing.T) {
	t.Parallel()
	// Anchor mid-stream: only episodes after it (and the nil-date one) qualify.
	eps := twoSeasons()
	rs := RequesterScope{Rule: Rule{Kind: RuleFutureOnly}, Anchor: *date("2026-05-01")}

	got := wantedSN(eps, InScope(rs, eps, now))
	want := [][2]int{{2, 1}, {2, 2}, {2, 3}} // S1 episodes pre-date the anchor
	if !equalSN(got, want) {
		t.Errorf("future_only mid-anchor = %v, want %v", got, want)
	}
}

func TestInScope_PresetSeason(t *testing.T) {
	t.Parallel()
	eps := twoSeasons()
	rs := RequesterScope{Rule: Rule{Kind: RuleSeason, Season: 2}, Anchor: anchor}

	got := wantedSN(eps, InScope(rs, eps, now))
	want := [][2]int{{2, 1}, {2, 2}, {2, 3}}
	if !equalSN(got, want) {
		t.Errorf("season(2) = %v, want %v", got, want)
	}
}

func TestInScope_SeasonZeroSelectsNothingViaPreset(t *testing.T) {
	t.Parallel()
	// season(0) is a degenerate request — presets never auto-select specials,
	// but RuleSeason matches on the literal number, so season(0) does select
	// the special. This documents that the specials-exclusion lives in the
	// other presets, not in RuleSeason.
	eps := twoSeasons()
	rs := RequesterScope{Rule: Rule{Kind: RuleSeason, Season: 0}, Anchor: anchor}

	got := wantedSN(eps, InScope(rs, eps, now))
	want := [][2]int{{0, 1}}
	if !equalSN(got, want) {
		t.Errorf("season(0) = %v, want %v", got, want)
	}
}

func TestInScope_PresetPilot(t *testing.T) {
	t.Parallel()
	eps := twoSeasons()
	rs := RequesterScope{Rule: Rule{Kind: RulePilot}, Anchor: anchor}

	got := wantedSN(eps, InScope(rs, eps, now))
	want := [][2]int{{1, 1}} // lowest (season, number) among seasons >= 1
	if !equalSN(got, want) {
		t.Errorf("pilot = %v, want %v", got, want)
	}
}

func TestInScope_PilotSkipsSpecials(t *testing.T) {
	t.Parallel()
	// A special (S0E1) sorts below S1E1 numerically but must not be the pilot.
	eps := []Episode{
		ep(0, 1, date("2025-01-01")),
		ep(2, 5, date("2026-01-01")),
		ep(1, 3, date("2026-01-01")), // lowest real episode
	}
	rs := RequesterScope{Rule: Rule{Kind: RulePilot}, Anchor: anchor}

	got := wantedSN(eps, InScope(rs, eps, now))
	want := [][2]int{{1, 3}}
	if !equalSN(got, want) {
		t.Errorf("pilot = %v, want %v", got, want)
	}
}

func TestInScope_LatestSeasonPlusFuture(t *testing.T) {
	t.Parallel()
	// S1 and S2 have begun (episodes aired <= now); S3 entirely future. L = 2,
	// so S2 and S3 are in scope, S1 is not.
	eps := []Episode{
		ep(1, 1, date("2026-01-10")),
		ep(2, 1, date("2026-05-01")), // aired, highest begun season
		ep(2, 2, date("2026-09-01")), // future
		ep(3, 1, nil),                // unaired
	}
	rs := RequesterScope{Rule: Rule{Kind: RuleLatestSeasonPlusFuture}, Anchor: anchor}

	got := wantedSN(eps, InScope(rs, eps, now))
	want := [][2]int{{2, 1}, {2, 2}, {3, 1}}
	if !equalSN(got, want) {
		t.Errorf("latest_season_plus_future = %v, want %v", got, want)
	}
}

func TestInScope_LatestSeasonPlusFutureFallback(t *testing.T) {
	t.Parallel()
	// Nothing has aired yet (all future): L falls back to the lowest season >= 1.
	eps := []Episode{
		ep(1, 1, date("2026-12-01")),
		ep(2, 1, date("2027-01-01")),
		ep(0, 1, nil), // special ignored
	}
	rs := RequesterScope{Rule: Rule{Kind: RuleLatestSeasonPlusFuture}, Anchor: anchor}

	got := wantedSN(eps, InScope(rs, eps, now))
	want := [][2]int{{1, 1}, {2, 1}} // L = 1, everything from S1 up
	if !equalSN(got, want) {
		t.Errorf("latest_season_plus_future fallback = %v, want %v", got, want)
	}
}

func TestInScope_OverrideInclude(t *testing.T) {
	t.Parallel()
	// pilot preset + an explicit Include for the special pulls in S0E1.
	eps := twoSeasons()
	idx := bySN(eps)
	rs := RequesterScope{
		Rule:      Rule{Kind: RulePilot},
		Overrides: Overrides{Include: []uuid.UUID{idx[[2]int{0, 1}], idx[[2]int{2, 2}]}},
		Anchor:    anchor,
	}

	got := wantedSN(eps, InScope(rs, eps, now))
	want := [][2]int{{0, 1}, {1, 1}, {2, 2}} // pilot S1E1 + the two includes
	if !equalSN(got, want) {
		t.Errorf("pilot + include = %v, want %v", got, want)
	}
}

func TestInScope_OverrideExclude(t *testing.T) {
	t.Parallel()
	// all preset minus one excluded episode.
	eps := twoSeasons()
	idx := bySN(eps)
	rs := RequesterScope{
		Rule:      Rule{Kind: RuleAll},
		Overrides: Overrides{Exclude: []uuid.UUID{idx[[2]int{2, 2}]}},
		Anchor:    anchor,
	}

	got := wantedSN(eps, InScope(rs, eps, now))
	want := [][2]int{{1, 1}, {1, 2}, {2, 1}, {2, 3}}
	if !equalSN(got, want) {
		t.Errorf("all - exclude = %v, want %v", got, want)
	}
}

func TestInScope_IncludeBeatsExclude(t *testing.T) {
	t.Parallel()
	// An episode in both Include and Exclude is wanted: Include wins per the
	// composition (preset && !exclude) || include.
	eps := twoSeasons()
	idx := bySN(eps)
	target := idx[[2]int{2, 2}]
	rs := RequesterScope{
		Rule: Rule{Kind: RuleSeason, Season: 9}, // matches nothing
		Overrides: Overrides{
			Include: []uuid.UUID{target},
			Exclude: []uuid.UUID{target},
		},
		Anchor: anchor,
	}

	got := wantedSN(eps, InScope(rs, eps, now))
	want := [][2]int{{2, 2}}
	if !equalSN(got, want) {
		t.Errorf("include beats exclude = %v, want %v", got, want)
	}
}

func TestInScope_EmptyEpisodeList(t *testing.T) {
	t.Parallel()
	rs := RequesterScope{Rule: Rule{Kind: RuleAll}, Anchor: anchor}
	got := InScope(rs, nil, now)
	if len(got) != 0 {
		t.Errorf("empty episode list = %v, want empty map", got)
	}
}

func TestEffectiveInScope_Union(t *testing.T) {
	t.Parallel()
	// One requester wants season 1, another wants the pilot + a future include.
	// The union is the distinct set, sorted by (season, number).
	eps := twoSeasons()
	idx := bySN(eps)
	scopes := []RequesterScope{
		{Rule: Rule{Kind: RuleSeason, Season: 1}, Anchor: anchor},
		{
			Rule:      Rule{Kind: RuleSeason, Season: 2},
			Overrides: Overrides{Include: []uuid.UUID{idx[[2]int{0, 1}]}},
			Anchor:    anchor,
		},
	}

	got := EffectiveInScope(scopes, eps, now)
	want := []uuid.UUID{
		idx[[2]int{0, 1}],
		idx[[2]int{1, 1}],
		idx[[2]int{1, 2}],
		idx[[2]int{2, 1}],
		idx[[2]int{2, 2}],
		idx[[2]int{2, 3}],
	}
	if len(got) != len(want) {
		t.Fatalf("union length = %d, want %d (%v)", len(got), len(want), wantedSN(eps, mapTrue(got)))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("union[%d] = %s, want %s (order or membership wrong: %v)", i, got[i], want[i], wantedSN(eps, mapTrue(got)))
		}
	}
}

func TestEffectiveInScope_Empty(t *testing.T) {
	t.Parallel()
	if got := EffectiveInScope(nil, twoSeasons(), now); len(got) != 0 {
		t.Errorf("no requesters = %v, want empty", got)
	}
}

func mapTrue(ids []uuid.UUID) map[uuid.UUID]bool {
	m := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}
