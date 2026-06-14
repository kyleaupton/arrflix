package indexer

import "time"

// MediaType represents the type of media being searched for.
type MediaType string

const (
	MediaTypeMovie  MediaType = "movie"
	MediaTypeSeries MediaType = "series"
)

// SearchQuery represents a search request to an indexer source.
type SearchQuery struct {
	Query     string
	MediaType MediaType
	Season    *int
	Episode   *int
	Limit     int

	// Structured identifiers for ID-precise search. The prowlarr adapter
	// composes these into Prowlarr's query-token syntax when present; absent
	// (nil) IDs fall back to the free-text Query.
	TmdbID *int64
	ImdbID *string
}

// SearchResult represents a validated search result from an indexer.
// All required fields are guaranteed to be non-empty after validation.
type SearchResult struct {
	// Identity (required)
	IndexerID   int64
	IndexerName string
	GUID        string

	// Structured identifiers Prowlarr echoes from capable indexers. 0 means
	// the indexer didn't report that id; the gate uses them to verify a result
	// refers to the wanted movie.
	TmdbID int64
	ImdbID int64

	// Required - validated at adapter boundary
	Title       string // MUST be non-empty
	DownloadURL string // MUST be non-empty
	Protocol    string // "torrent" or "usenet"

	// Metadata
	Size         int64
	Seeders      *int
	Leechers     *int
	Age          int64
	AgeHours     float64
	PublishDate  time.Time
	Categories   []string
	Grabs        int
	IndexerFlags []string
}

// IndexerInfo provides information about a configured indexer.
type IndexerInfo struct {
	ID       int64
	Name     string
	Protocol string
	Enabled  bool
}
