package model

type MovieRail struct {
	TmdbID        int64   `json:"tmdbId"`
	Title         string  `json:"title"`
	Overview      string  `json:"overview"`
	PosterPath    string  `json:"posterPath"`
	ReleaseDate   string  `json:"releaseDate"`
	Year          *int32  `json:"year,omitempty"`
	Genres        []int64 `json:"genres,omitempty"`
	Tagline       string  `json:"tagline,omitempty"`
	IsInLibrary   bool    `json:"isInLibrary"`
	IsDownloading bool    `json:"isDownloading"`
}

type Movie struct {
	TmdbID int64 `json:"tmdbId"`

	Title    string `json:"title"`
	Overview string `json:"overview"`
	Tagline  string `json:"tagline"`

	// Stats
	Status              string              `json:"status"`
	ReleaseDate         string              `json:"releaseDate"`
	Runtime             int                 `json:"runtime"`
	OriginalLanguage    string              `json:"originalLanguage"`
	OriginCountry       []string            `json:"originCountry"`
	ProductionCompanies []ProductionCompany `json:"productionCompanies"`
	ProductionCountries []ProductionCountry `json:"productionCountries"`

	PosterPath   string `json:"posterPath"`
	BackdropPath string `json:"backdropPath"`
}

type SeriesRail struct {
	TmdbID        int64   `json:"tmdbId"`
	Title         string  `json:"title"`
	Overview      string  `json:"overview"`
	PosterPath    string  `json:"posterPath"`
	ReleaseDate   string  `json:"releaseDate"`
	Year          *int32  `json:"year,omitempty"`
	Genres        []int64 `json:"genres,omitempty"`
	Tagline       string  `json:"tagline,omitempty"`
	IsInLibrary   bool    `json:"isInLibrary"`
	IsDownloading bool    `json:"isDownloading"`
}

type Series struct {
	TmdbID int64 `json:"tmdbId"`

	Title    string `json:"title"`
	Overview string `json:"overview"`
	Tagline  string `json:"tagline"`
	Status   string `json:"status"`

	Seasons []Season `json:"seasons"`

	PosterPath   string `json:"posterPath"`
	BackdropPath string `json:"backdropPath"`

	FirstAirDate string `json:"firstAirDate"`
	LastAirDate  string `json:"lastAirDate"`
	InProduction bool   `json:"inProduction"`

	CreatedBy []Person  `json:"createdBy"`
	Networks  []Network `json:"networks"`
	Genres    []Genre   `json:"genres"`
}

type Season struct {
	TmdbID       int64  `json:"tmdbId"`
	SeasonNumber int64  `json:"seasonNumber"`
	Title        string `json:"title"`
	Overview     string `json:"overview"`
	PosterPath   string `json:"posterPath"`
	AirDate      string `json:"airDate"`
}

type Person struct {
	TmdbID      int64  `json:"tmdbId"`
	Name        string `json:"name"`
	ProfilePath string `json:"profilePath"`
}

type PersonDetail struct {
	TmdbID             int64    `json:"tmdbId"`
	Name               string   `json:"name"`
	Biography          string   `json:"biography"`
	Birthday           *string  `json:"birthday,omitempty"`
	Deathday           *string  `json:"deathday,omitempty"`
	PlaceOfBirth       *string  `json:"placeOfBirth,omitempty"`
	KnownForDepartment string   `json:"knownForDepartment"`
	ProfilePath        string   `json:"profilePath,omitempty"`
	Popularity         float32  `json:"popularity"`
	AlsoKnownAs        []string `json:"alsoKnownAs,omitempty"`
	Homepage           *string  `json:"homepage,omitempty"`
	IMDbID             *string  `json:"imdbId,omitempty"`
}

type Network struct {
	TmdbID   int64  `json:"tmdbId"`
	Name     string `json:"name"`
	LogoPath string `json:"logoPath"`
}

type Genre struct {
	TmdbID int64  `json:"tmdbId"`
	Name   string `json:"name"`
}

type ProductionCompany struct {
	TmdbID        int64  `json:"tmdbId"`
	Name          string `json:"name"`
	LogoPath      string `json:"logoPath"`
	OriginCountry string `json:"originCountry"`
}

type ProductionCountry struct {
	Iso3166_1 string `json:"iso3166_1"`
	Name      string `json:"name"`
}

type CastMember struct {
	TmdbID      int64  `json:"tmdbId"`
	Name        string `json:"name"`
	Character   string `json:"character"` // role name for cast
	ProfilePath string `json:"profilePath,omitempty"`
	Order       int    `json:"order"` // for cast ordering
}

type CrewMember struct {
	TmdbID      int64  `json:"tmdbId"`
	Name        string `json:"name"`
	Job         string `json:"job"`        // e.g., "Director", "Producer"
	Department  string `json:"department"` // e.g., "Directing", "Production"
	ProfilePath string `json:"profilePath,omitempty"`
}

type Credits struct {
	Cast []CastMember `json:"cast"`
	Crew []CrewMember `json:"crew"`
}

type Video struct {
	TmdbID            string `json:"tmdbId"`
	Key               string `json:"key"` // YouTube/Vimeo video ID
	Name              string `json:"name"`
	Site              string `json:"site"` // "YouTube", "Vimeo", etc.
	Type              string `json:"type"` // "Trailer", "Teaser", "Clip", etc.
	Size              int    `json:"size"` // 360, 480, 720, 1080
	PublishedAt       string `json:"publishedAt,omitempty"`
	IsOfficialTrailer bool   `json:"isOfficialTrailer"`
}

type MediaType string

const (
	MediaTypeMovie  MediaType = "movie"
	MediaTypeSeries MediaType = "series"
)

type WatchProvider struct {
	ProviderID      int    `json:"providerId"`
	ProviderName    string `json:"providerName"`
	LogoPath        string `json:"logoPath"`
	DisplayPriority int    `json:"displayPriority"`
}

type WatchProviders struct {
	Link     string          `json:"link,omitempty"`
	Flatrate []WatchProvider `json:"flatrate,omitempty"`
	Rent     []WatchProvider `json:"rent,omitempty"`
	Buy      []WatchProvider `json:"buy,omitempty"`
}
