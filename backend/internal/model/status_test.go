package model

import "testing"

func TestCanonicalizeStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw  string
		want MediaStatus
	}{
		{"Released", StatusReleased},
		{"Returning Series", StatusContinuing},
		{"Ended", StatusEnded},
		{"Canceled", StatusCanceled},
		{"Planned", StatusUpcoming},
		{"Rumored", StatusUpcoming},
		{"In Production", StatusUpcoming},
		{"Post Production", StatusUpcoming},
		{"Pilot", StatusUpcoming},
		{"", StatusUnknown},
		{"  Released  ", StatusReleased},
		{"Something Else", StatusUnknown},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			if got := CanonicalizeStatus(tc.raw); got != tc.want {
				t.Errorf("CanonicalizeStatus(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
