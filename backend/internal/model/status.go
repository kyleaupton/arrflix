package model

import "strings"

// MediaStatus is the canonical, source-agnostic lifecycle state for a media
// item. TMDB (and any future metadata source) reports its own status wording;
// CanonicalizeStatus maps those down to this small, stable set at the provider
// boundary so UI labels and the refresh-cadence policy key on a fixed
// vocabulary rather than a provider's strings.
type MediaStatus string

const (
	StatusUpcoming   MediaStatus = "upcoming"
	StatusReleased   MediaStatus = "released"
	StatusContinuing MediaStatus = "continuing"
	StatusEnded      MediaStatus = "ended"
	StatusCanceled   MediaStatus = "canceled"
	StatusUnknown    MediaStatus = "unknown"
)

// canonicalStatusByTMDB maps TMDB's status strings (canonical TMDB casing, as
// returned by the SDK) to our canonical set. `In Production` maps to upcoming:
// it's overwhelmingly a not-yet-released signal.
var canonicalStatusByTMDB = map[string]MediaStatus{
	"Released":         StatusReleased,
	"Returning Series": StatusContinuing,
	"Ended":            StatusEnded,
	"Canceled":         StatusCanceled,
	"Planned":          StatusUpcoming,
	"Rumored":          StatusUpcoming,
	"In Production":    StatusUpcoming,
	"Post Production":  StatusUpcoming,
	"Pilot":            StatusUpcoming,
}

// CanonicalizeStatus maps a raw TMDB status string to a MediaStatus. It is pure
// and total: empty or unmapped input yields StatusUnknown, never an error.
func CanonicalizeStatus(raw string) MediaStatus {
	if s, ok := canonicalStatusByTMDB[strings.TrimSpace(raw)]; ok {
		return s
	}
	return StatusUnknown
}
