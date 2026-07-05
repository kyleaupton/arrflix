package jobutil

import (
	"testing"
	"time"
)

// TestBackoffCapped checks the ramp: BackoffCapped tracks 2^attempt seconds up to
// the cap, then plateaus at the cap. attempt 0 is the floor (1s), and a large
// attempt — whose uncapped delay would dwarf the cap — clamps exactly to it.
func TestBackoffCapped(t *testing.T) {
	t.Parallel()

	const ceiling = time.Hour
	cases := []struct {
		name    string
		attempt int
		max     time.Duration
		want    time.Duration
	}{
		{"attempt 0 is the 1s floor", 0, ceiling, time.Second},
		{"ramps below the cap", 5, ceiling, 32 * time.Second},
		{"last step under the cap", 11, ceiling, 2048 * time.Second},
		{"plateaus at the cap once exceeded", 12, ceiling, ceiling},
		{"large attempt clamps to the cap", 40, ceiling, ceiling},
		{"exactly at the cap", 2, 4 * time.Second, 4 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := BackoffCapped(tc.attempt, tc.max); got != tc.want {
				t.Errorf("BackoffCapped(%d, %v) = %v, want %v", tc.attempt, tc.max, got, tc.want)
			}
		})
	}
}
