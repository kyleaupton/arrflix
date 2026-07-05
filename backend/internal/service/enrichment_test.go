package service

import (
	"sort"
	"testing"

	tmdb "github.com/cyruzin/golang-tmdb"
)

// seasonSet turns a fetch-set map into a sorted slice for stable comparison.
func seasonSet(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

func equalInts(a, b []int) bool {
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

func TestSeasonsToFetch(t *testing.T) {
	t.Parallel()

	// A 3-season show still airing: next episode is in season 3, last aired in
	// season 3. Seasons 1 and 2 are the frozen back-catalog.
	airing := tmdb.TVDetails{
		Seasons: []tmdb.Season{
			{SeasonNumber: 1}, {SeasonNumber: 2}, {SeasonNumber: 3},
		},
		NextEpisodeToAir: tmdb.NextEpisodeToAir{ID: 5001, SeasonNumber: 3},
		LastEpisodeToAir: tmdb.LastEpisodeToAir{ID: 5000, SeasonNumber: 3},
	}

	// An ended show: no next/last pointer that TMDB still populates for airing.
	// LastEpisodeToAir stays set to the finale (season 2).
	ended := tmdb.TVDetails{
		Seasons: []tmdb.Season{
			{SeasonNumber: 1}, {SeasonNumber: 2},
		},
		LastEpisodeToAir: tmdb.LastEpisodeToAir{ID: 4000, SeasonNumber: 2},
	}

	tests := []struct {
		name     string
		details  tmdb.TVDetails
		existing map[int32]bool
		want     []int
	}{
		{
			name:     "first sync fetches every season",
			details:  airing,
			existing: map[int32]bool{},
			want:     []int{1, 2, 3},
		},
		{
			name:     "routine refresh of airing show fetches only mutable (current) season",
			details:  airing,
			existing: map[int32]bool{1: true, 2: true, 3: true},
			want:     []int{3}, // next/last-to-air season + max
		},
		{
			name:     "newly-added season is fetched even if not mutable",
			details:  airing,
			existing: map[int32]bool{1: true, 2: true}, // season 3 is new
			want:     []int{3},
		},
		{
			name:    "brand-new undated season (no next/last pointer) caught by max",
			details: tmdb.TVDetails{Seasons: []tmdb.Season{{SeasonNumber: 1}, {SeasonNumber: 2}, {SeasonNumber: 3}}},
			// only season 3 (the max) is mutable; 1,2 already synced
			existing: map[int32]bool{1: true, 2: true, 3: true},
			want:     []int{3},
		},
		{
			name:     "routine refresh of ended show fetches finale + max season",
			details:  ended,
			existing: map[int32]bool{1: true, 2: true},
			want:     []int{2}, // last-episode season and max are both season 2
		},
		{
			name:     "no next/last pointer, absent season 0 not force-fetched",
			details:  ended,
			existing: map[int32]bool{1: true, 2: true},
			want:     []int{2}, // season 0 never appears; zero-struct pointer must not add it
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := seasonSet(seasonsToFetch(tt.details, tt.existing))
			if !equalInts(got, tt.want) {
				t.Errorf("seasonsToFetch = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMutableSeasons_AbsentPointersDoNotMarkSeasonZero(t *testing.T) {
	t.Parallel()
	// A show with only season 1 and no next/last-episode pointers: the zero-value
	// NextEpisodeToAir/LastEpisodeToAir (ID 0, SeasonNumber 0) must NOT mark
	// season 0 mutable — only the max season (1) is.
	details := tmdb.TVDetails{Seasons: []tmdb.Season{{SeasonNumber: 1}}}
	m := mutableSeasons(details)
	if m[0] {
		t.Error("season 0 marked mutable from an absent (zero-value) episode pointer")
	}
	if !m[1] {
		t.Error("max season (1) should be mutable")
	}
}
