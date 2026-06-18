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
