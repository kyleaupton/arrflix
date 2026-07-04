package model

import "testing"

func TestTracking_AutonomyFor(t *testing.T) {
	t.Parallel()

	tr := Tracking{
		AutonomyBackfill: string(AutonomyManual),
		AutonomyOngoing:  string(AutonomyAuto),
	}

	if got := tr.AutonomyFor(WantSegmentBackfill); got != AutonomyManual {
		t.Fatalf("AutonomyFor(backfill) = %q, want %q", got, AutonomyManual)
	}
	if got := tr.AutonomyFor(WantSegmentOngoing); got != AutonomyAuto {
		t.Fatalf("AutonomyFor(ongoing) = %q, want %q", got, AutonomyAuto)
	}
}

func TestHoldForNewWant(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		backfill Autonomy
		ongoing  Autonomy
		seg      WantSegment
		wantHold bool
	}{
		{"manual backfill segment is held", AutonomyManual, AutonomyAuto, WantSegmentBackfill, true},
		{"auto backfill segment is unheld", AutonomyAuto, AutonomyAuto, WantSegmentBackfill, false},
		{"manual ongoing segment is held", AutonomyAuto, AutonomyManual, WantSegmentOngoing, true},
		{"auto ongoing segment is unheld", AutonomyManual, AutonomyAuto, WantSegmentOngoing, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tr := Tracking{
				AutonomyBackfill: string(tc.backfill),
				AutonomyOngoing:  string(tc.ongoing),
			}
			got := HoldForNewWant(tr, tc.seg)
			if tc.wantHold {
				if got == nil || *got != WantHoldNeedsPick {
					t.Fatalf("HoldForNewWant = %v, want %q", got, WantHoldNeedsPick)
				}
			} else if got != nil {
				t.Fatalf("HoldForNewWant = %q, want nil", *got)
			}
		})
	}
}
