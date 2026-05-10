package model

// SuggestedMatch represents a potential TMDB match for an unmatched file.
type SuggestedMatch struct {
	TmdbID int64  `json:"tmdbId"`
	Title  string `json:"title"`
	Year   int    `json:"year,omitempty"`
	Type   string `json:"type"` // "movie" or "series"
	Score  int    `json:"score"`
}
