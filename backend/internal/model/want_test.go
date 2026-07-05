package model

import (
	"testing"
	"time"
)

func TestSegmentFor(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	before := created.Add(-24 * time.Hour)
	after := created.Add(24 * time.Hour)

	cases := []struct {
		name string
		date *time.Time
		want WantSegment
	}{
		{"nil date is ongoing", nil, WantSegmentOngoing},
		{"before createdAt is backfill", &before, WantSegmentBackfill},
		{"equal to createdAt is ongoing", &created, WantSegmentOngoing},
		{"after createdAt is ongoing", &after, WantSegmentOngoing},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := SegmentFor(tc.date, created); got != tc.want {
				t.Fatalf("SegmentFor(%v, %v) = %q, want %q", tc.date, created, got, tc.want)
			}
		})
	}
}
