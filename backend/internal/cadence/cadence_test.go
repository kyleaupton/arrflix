package cadence

import (
	"testing"
	"time"
)

// Fixed reference time so tests never touch the wall clock.
var now = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

// ago returns an anchor d before now — a positive d means "aired d ago".
func ago(d time.Duration) time.Time { return now.Add(-d) }

func TestNextRunAt_Smart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		anchor time.Time
		want   time.Time
	}{
		{
			// Not yet aired: fire exactly at the anchor (dormant tier).
			name:   "future anchor fires at air",
			anchor: now.Add(48 * time.Hour),
			want:   now.Add(48 * time.Hour),
		},
		{
			name:   "just aired (30m) -> low tier 1h",
			anchor: ago(30 * time.Minute),
			want:   now.Add(tierLow),
		},
		{
			name:   "lower boundary 0 -> low tier 1h",
			anchor: now,
			want:   now.Add(tierLow),
		},
		{
			name:   "peak window (2h) -> 15m",
			anchor: ago(2 * time.Hour),
			want:   now.Add(tierPeak),
		},
		{
			name:   "peak boundary (exactly 1h) -> 15m",
			anchor: ago(1 * time.Hour),
			want:   now.Add(tierPeak),
		},
		{
			name:   "medium (12h) -> 1h",
			anchor: ago(12 * time.Hour),
			want:   now.Add(tierMedium),
		},
		{
			name:   "medium boundary (exactly 6h) -> 1h",
			anchor: ago(6 * time.Hour),
			want:   now.Add(tierMedium),
		},
		{
			name:   "catch-up (3d) -> 6h",
			anchor: ago(3 * 24 * time.Hour),
			want:   now.Add(tierCatch),
		},
		{
			name:   "catch-up boundary (exactly 24h) -> 6h",
			anchor: ago(24 * time.Hour),
			want:   now.Add(tierCatch),
		},
		{
			name:   "cold (10d) -> 24h",
			anchor: ago(10 * 24 * time.Hour),
			want:   now.Add(tierCold),
		},
		{
			name:   "cold boundary (exactly 7d) -> 24h",
			anchor: ago(7 * 24 * time.Hour),
			want:   now.Add(tierCold),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NextRunAt(StrategySmart, tt.anchor, now)
			if !got.Equal(tt.want) {
				t.Errorf("NextRunAt(smart) = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNextRunAt_Fixed(t *testing.T) {
	t.Parallel()
	// Fixed ignores the anchor curve entirely: now + the flat interval.
	got := NextRunAt(StrategyFixed, ago(2*time.Hour), now)
	if want := now.Add(fixedInterval); !got.Equal(want) {
		t.Errorf("NextRunAt(fixed) = %v, want %v", got, want)
	}
}

func TestNextRunAt_UnknownStrategyFallsBackToSmart(t *testing.T) {
	t.Parallel()
	anchor := ago(2 * time.Hour) // peak window
	got := NextRunAt(Strategy("bogus"), anchor, now)
	if want := NextRunAt(StrategySmart, anchor, now); !got.Equal(want) {
		t.Errorf("unknown strategy = %v, want smart %v", got, want)
	}
}

// tp is a *time.Time helper: an offset from now (negative = in the past).
func tp(d time.Duration) *time.Time { t := now.Add(d); return &t }

func TestMetadataRefreshAt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   MetadataRefreshInput
		want time.Duration // expected interval from now
	}{
		// ── Series ──
		{
			name: "ended -> monthly",
			in:   MetadataRefreshInput{Type: "series", Status: statusEnded},
			want: metadataMonthly,
		},
		{
			name: "canceled -> monthly (even with recent air activity)",
			in:   MetadataRefreshInput{Type: "series", Status: statusCanceled, LastEpisodeAir: tp(-1 * 24 * time.Hour)},
			want: metadataMonthly,
		},
		{
			name: "continuing, next episode soon -> daily",
			in:   MetadataRefreshInput{Type: "series", Status: statusContinuing, NextEpisodeAir: tp(3 * 24 * time.Hour)},
			want: metadataDaily,
		},
		{
			name: "continuing, next-air boundary (exactly 30d) -> daily",
			in:   MetadataRefreshInput{Type: "series", Status: statusContinuing, NextEpisodeAir: tp(metadataAiringLead)},
			want: metadataDaily,
		},
		{
			name: "continuing, aired recently -> daily",
			in:   MetadataRefreshInput{Type: "series", Status: statusContinuing, LastEpisodeAir: tp(-2 * 24 * time.Hour)},
			want: metadataDaily,
		},
		{
			name: "continuing, last-air boundary (exactly 14d ago) -> daily",
			in:   MetadataRefreshInput{Type: "series", Status: statusContinuing, LastEpisodeAir: tp(-metadataAiringTrail)},
			want: metadataDaily,
		},
		{
			name: "in-production (upcoming premiere) within window -> daily",
			in:   MetadataRefreshInput{Type: "series", Status: "upcoming", InProduction: true, NextEpisodeAir: tp(5 * 24 * time.Hour)},
			want: metadataDaily,
		},
		{
			name: "continuing but quiet (next-air far, no recent) -> weekly",
			in:   MetadataRefreshInput{Type: "series", Status: statusContinuing, NextEpisodeAir: tp(90 * 24 * time.Hour), LastEpisodeAir: tp(-90 * 24 * time.Hour)},
			want: metadataWeekly,
		},
		{
			name: "not-in-production, air window present -> weekly (window gated on airing)",
			in:   MetadataRefreshInput{Type: "series", Status: "unknown", InProduction: false, NextEpisodeAir: tp(3 * 24 * time.Hour)},
			want: metadataWeekly,
		},
		{
			name: "upcoming, no air dates -> weekly",
			in:   MetadataRefreshInput{Type: "series", Status: "upcoming"},
			want: metadataWeekly,
		},
		{
			name: "empty status, no signals -> weekly",
			in:   MetadataRefreshInput{Type: "series"},
			want: metadataWeekly,
		},
		// ── Movie ──
		{
			name: "movie unreleased (future) -> weekly",
			in:   MetadataRefreshInput{Type: "movie", ReleaseDate: tp(30 * 24 * time.Hour)},
			want: metadataWeekly,
		},
		{
			name: "movie unknown release date -> weekly",
			in:   MetadataRefreshInput{Type: "movie"},
			want: metadataWeekly,
		},
		{
			name: "movie released < 1y -> monthly",
			in:   MetadataRefreshInput{Type: "movie", ReleaseDate: tp(-100 * 24 * time.Hour)},
			want: metadataMonthly,
		},
		{
			name: "movie released exactly 1y -> monthly (boundary)",
			in:   MetadataRefreshInput{Type: "movie", ReleaseDate: tp(-metadataMovieRecent)},
			want: metadataMonthly,
		},
		{
			name: "movie released > 1y -> quarterly",
			in:   MetadataRefreshInput{Type: "movie", ReleaseDate: tp(-400 * 24 * time.Hour)},
			want: metadataQuarterly,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := MetadataRefreshAt(tt.in, now)
			if want := now.Add(tt.want); !got.Equal(want) {
				t.Errorf("MetadataRefreshAt = %v (Δ%v), want %v (Δ%v)", got, got.Sub(now), want, tt.want)
			}
		})
	}
}

func TestMetadataBackoffAt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, metadataBackoffBase},   // clamped up to 1
		{1, metadataBackoffBase},   // 15m
		{2, 30 * time.Minute},      // 15m·2
		{3, 60 * time.Minute},      // 15m·4
		{4, 2 * time.Hour},         // 15m·8
		{8, metadataBackoffCap},    // 15m·128 = 32h → capped
		{20, metadataBackoffCap},   // far past cap, no overflow
		{1000, metadataBackoffCap}, // exponent clamp guard
	}

	var prev time.Duration
	for _, tt := range tests {
		got := MetadataBackoffAt(tt.attempt, now).Sub(now)
		if got != tt.want {
			t.Errorf("MetadataBackoffAt(%d) Δ = %v, want %v", tt.attempt, got, tt.want)
		}
		// Monotonic non-decreasing growth up to the cap.
		if tt.attempt > 1 && got < prev {
			t.Errorf("MetadataBackoffAt(%d) = %v decreased from prior %v", tt.attempt, got, prev)
		}
		prev = got
	}
}
