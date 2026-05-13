package model

import "fmt"

// ContentKind specifies what type of content a row can contain
type ContentKind string

const (
	ContentKindMovie ContentKind = "movie"
	ContentKindTV    ContentKind = "tv"
	ContentKindMixed ContentKind = "mixed"
)

// RankingStrategy defines how items within a row should be ranked
type RankingStrategy string

const (
	RankingDefault    RankingStrategy = "default"    // keep source order
	RankingPopularity RankingStrategy = "popularity" // TMDB popularity
	RankingRating     RankingStrategy = "rating"     // TMDB vote_average
	RankingRecent     RankingStrategy = "recent"     // by release date
)

// Row intent - semantic, not TMDB-centric
type RowIntent string

const (
	IntentTrendingWeek        RowIntent = "trending_week"
	IntentNewAndNoteworthy    RowIntent = "new_noteworthy"
	IntentHiddenGems          RowIntent = "hidden_gems"
	IntentCriticallyAcclaimed RowIntent = "critically_acclaimed"
	IntentJustReleased        RowIntent = "just_released"
	IntentComingSoon          RowIntent = "coming_soon"
	IntentBingeWorthy         RowIntent = "binge_worthy"
	IntentFanFavorites        RowIntent = "fan_favorites"
	// Personalized intents (require signals)
	IntentContinueWatching  RowIntent = "continue_watching"
	IntentBecauseYouWatched RowIntent = "because_you_watched"
)

// DiversityRules defines constraints to enforce variety within a row
type DiversityRules struct {
	MaxPerGenre    int `json:"maxPerGenre,omitempty"`    // max items sharing a genre
	MaxPerLanguage int `json:"maxPerLanguage,omitempty"` // max items in same language
}

// SourceConfig defines how to fetch candidates (implementation detail)
type SourceConfig struct {
	Provider string            `json:"provider"` // e.g., "tmdb"
	Endpoint string            `json:"endpoint"` // e.g., "trending", "discover"
	Params   map[string]string `json:"params"`   // endpoint-specific params
}

// RowDefinition is the declarative configuration for a feed row
type RowDefinition struct {
	Intent         RowIntent       `json:"intent"`
	Title          string          `json:"title"`
	Subtitle       string          `json:"subtitle,omitempty"`
	ContentKind    ContentKind     `json:"contentKind"`
	Sources        []SourceConfig  `json:"sources"`    // can compose multiple sources
	TargetSize     int             `json:"targetSize"` // final items in row
	FetchSize      int             `json:"fetchSize"`  // over-fetch for dedupe headroom
	Ranking        RankingStrategy `json:"ranking"`
	Diversity      *DiversityRules `json:"diversity,omitempty"`
	RequiresSignal bool            `json:"requiresSignal"` // skip if no user signals
	Weight         float64         `json:"weight"`         // hint for ordering, not rigid priority
}

// Title is the unified type for movies and TV series in the feed (NO user-specific state)
type Title struct {
	TmdbID       int64     `json:"tmdbId"`
	MediaType    MediaType `json:"mediaType"` // "movie" or "series"
	Title        string    `json:"title"`
	Overview     string    `json:"overview"`
	PosterPath   string    `json:"posterPath"`
	BackdropPath string    `json:"backdropPath,omitempty"`
	ReleaseDate  string    `json:"releaseDate"`
	Year         *int32    `json:"year,omitempty"`
	GenreIDs     []int64   `json:"genreIds,omitempty"`
	Language     string    `json:"language,omitempty"`
	Popularity   float64   `json:"popularity,omitempty"`
	VoteAverage  float64   `json:"voteAverage,omitempty"`
}

// TitleKey creates a unique identifier for deduplication
func (t Title) TitleKey() string {
	return fmt.Sprintf("%s:%d", t.MediaType, t.TmdbID)
}

// HydratedTitle includes optional user overlay applied during hydration
type HydratedTitle struct {
	Title
	IsInLibrary   bool `json:"isInLibrary,omitempty"`
	IsDownloading bool `json:"isDownloading,omitempty"`
}

// FeedRow is a populated row ready for the frontend
type FeedRow struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	Subtitle string          `json:"subtitle,omitempty"`
	Items    []HydratedTitle `json:"items"`
}

// HeroItem is the featured item at the top of the feed
type HeroItem struct {
	Title        string `json:"title"`
	Overview     string `json:"overview"`
	BackdropPath string `json:"backdropPath"`
	PosterPath   string `json:"posterPath"`
	TmdbID       int64  `json:"tmdbId"`
	MediaType    string `json:"mediaType"`
	TrailerURL   string `json:"trailerUrl,omitempty"`
}

// Feed is the complete home feed response
type Feed struct {
	Hero *HeroItem `json:"hero,omitempty"`
	Rows []FeedRow `json:"rows"`
}
