package model

type FileInfo struct {
	ID            string   `json:"id"`
	LibraryID     string   `json:"libraryId"`
	Path          string   `json:"path"` // relative to library root
	Status        string   `json:"status"`
	SeasonNumber  *int32   `json:"seasonNumber,omitempty"`
	EpisodeNumber *int32   `json:"episodeNumber,omitempty"`
	DownloadJobID *string  `json:"downloadJobId,omitempty"` // For matching with download jobs store
	Progress      *float64 `json:"progress,omitempty"`      // Progress 0-1 for downloading files
}

type LibraryAvailability struct {
	LibraryID    string `json:"libraryId"`
	FileCount    int    `json:"fileCount"`
	StatusRollup string `json:"statusRollup"`
}

type Availability struct {
	IsInLibrary bool                  `json:"isInLibrary"`
	Libraries   []LibraryAvailability `json:"libraries"`
}

type MovieDetail struct {
	TmdbID int64  `json:"tmdbId"`
	Title  string `json:"title"`
	Year   *int32 `json:"year,omitempty"`

	Overview      string      `json:"overview"`
	Tagline       string      `json:"tagline,omitempty"`
	Status        MediaStatus `json:"status"`
	ReleaseDate   string      `json:"releaseDate,omitempty"`
	Runtime       int         `json:"runtime,omitempty"`
	Certification string      `json:"certification,omitempty"`
	VoteAverage   float64     `json:"voteAverage,omitempty"`
	VoteCount     int64       `json:"voteCount,omitempty"`
	Genres        []Genre     `json:"genres,omitempty"`
	PosterPath    string      `json:"posterPath,omitempty"`
	BackdropPath  string      `json:"backdropPath,omitempty"`

	Files           []FileInfo      `json:"files"`
	Credits         *Credits        `json:"credits,omitempty"`
	Videos          []Video         `json:"videos,omitempty"`
	Recommendations []MovieRail     `json:"recommendations,omitempty"`
	WatchProviders  *WatchProviders `json:"watchProviders,omitempty"`
}

type EpisodeAvailability struct {
	SeasonNumber  int32     `json:"seasonNumber"`
	EpisodeNumber int32     `json:"episodeNumber"`
	Title         *string   `json:"title,omitempty"`
	Overview      string    `json:"overview,omitempty"`
	StillPath     string    `json:"stillPath,omitempty"`
	AirDate       *string   `json:"airDate,omitempty"`
	Available     bool      `json:"available"`
	File          *FileInfo `json:"file,omitempty"`
}

type SeasonDetail struct {
	SeasonNumber int32                 `json:"seasonNumber"`
	Overview     string                `json:"overview,omitempty"`
	PosterPath   string                `json:"posterPath,omitempty"`
	AirDate      string                `json:"airDate,omitempty"`
	Episodes     []EpisodeAvailability `json:"episodes"`
}

type NextEpisode struct {
	SeasonNumber  int    `json:"seasonNumber"`
	EpisodeNumber int    `json:"episodeNumber"`
	Title         string `json:"title"`
	AirDate       string `json:"airDate,omitempty"`
	Overview      string `json:"overview,omitempty"`
	StillPath     string `json:"stillPath,omitempty"`
}

type SeriesDetail struct {
	TmdbID int64  `json:"tmdbId"`
	Title  string `json:"title"`
	Year   *int32 `json:"year,omitempty"`

	Overview         string       `json:"overview"`
	Tagline          string       `json:"tagline,omitempty"`
	Status           MediaStatus  `json:"status"`
	FirstAirDate     string       `json:"firstAirDate,omitempty"`
	LastAirDate      string       `json:"lastAirDate,omitempty"`
	InProduction     bool         `json:"inProduction"`
	Certification    string       `json:"certification,omitempty"`
	EpisodeRuntime   *int         `json:"episodeRuntime,omitempty"`
	VoteAverage      float64      `json:"voteAverage,omitempty"`
	VoteCount        int64        `json:"voteCount,omitempty"`
	Genres           []Genre      `json:"genres,omitempty"`
	PosterPath       string       `json:"posterPath,omitempty"`
	BackdropPath     string       `json:"backdropPath,omitempty"`
	NextEpisodeToAir *NextEpisode `json:"nextEpisodeToAir,omitempty"`

	Availability   Availability    `json:"availability"`
	Seasons        []SeasonDetail  `json:"seasons"`
	Credits        *Credits        `json:"credits,omitempty"`
	Videos         []Video         `json:"videos,omitempty"`
	WatchProviders *WatchProviders `json:"watchProviders,omitempty"`
}
