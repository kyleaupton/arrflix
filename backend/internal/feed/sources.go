package feed

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/kyleaupton/arrflix/internal/model"
)

// TMDBClient defines the interface for TMDB operations needed by the feed
type TMDBClient interface {
	GetTrendingMovies(ctx context.Context) (tmdb.Trending, error)
	GetTrendingSeries(ctx context.Context) (tmdb.Trending, error)
	GetPopularMovies(ctx context.Context) (tmdb.MoviePopular, error)
	GetPopularSeries(ctx context.Context) (tmdb.TVPopular, error)
	GetTopRatedMovies(ctx context.Context) (tmdb.MovieTopRated, error)
	GetTopRatedSeries(ctx context.Context) (tmdb.TVTopRated, error)
	GetNowPlayingMovies(ctx context.Context) (tmdb.MovieNowPlaying, error)
	GetUpcomingMovies(ctx context.Context) (tmdb.MovieUpcoming, error)
	GetOnTheAirSeries(ctx context.Context) (tmdb.TVOnTheAir, error)
}

// SourceProvider fetches candidate titles for a row (returns Titles without user state)
type SourceProvider interface {
	Fetch(ctx context.Context, limit int) ([]model.Title, error)
}

// TMDBSourceFactory creates source providers from SourceConfig
type TMDBSourceFactory struct {
	tmdb TMDBClient
}

// NewTMDBSourceFactory creates a new TMDB source factory
func NewTMDBSourceFactory(tmdb TMDBClient) *TMDBSourceFactory {
	return &TMDBSourceFactory{tmdb: tmdb}
}

// GetProvider returns the appropriate provider for a SourceConfig
func (f *TMDBSourceFactory) GetProvider(config model.SourceConfig) (SourceProvider, error) {
	if config.Provider != "tmdb" {
		return nil, fmt.Errorf("unsupported provider: %s", config.Provider)
	}

	// Parse media_type from params if present
	mediaType := config.Params["media_type"]

	switch config.Endpoint {
	case "trending":
		switch mediaType {
		case "movie":
			return &trendingMoviesProvider{tmdb: f.tmdb}, nil
		case "tv":
			return &trendingSeriesProvider{tmdb: f.tmdb}, nil
		}
		return nil, fmt.Errorf("trending endpoint requires media_type param (movie or tv)")

	case "popular":
		if mediaType == "tv" {
			return &popularSeriesProvider{tmdb: f.tmdb}, nil
		}
		return &popularMoviesProvider{tmdb: f.tmdb}, nil

	case "top_rated":
		if mediaType == "tv" {
			return &topRatedSeriesProvider{tmdb: f.tmdb}, nil
		}
		return &topRatedMoviesProvider{tmdb: f.tmdb}, nil

	case "now_playing":
		return &nowPlayingProvider{tmdb: f.tmdb}, nil

	case "upcoming":
		return &upcomingProvider{tmdb: f.tmdb}, nil

	case "on_the_air":
		return &onTheAirProvider{tmdb: f.tmdb}, nil

	case "discover":
		// For now, discover endpoint maps to popular/top_rated based on filters
		// This is a simplified implementation - full discover API would require more work
		return f.getDiscoverFallback(mediaType, config.Params)

	default:
		return nil, fmt.Errorf("unsupported endpoint: %s", config.Endpoint)
	}
}

// getDiscoverFallback maps discover queries to existing endpoints for now
func (f *TMDBSourceFactory) getDiscoverFallback(mediaType string, params map[string]string) (SourceProvider, error) {
	// Check for vote_average filter to determine if it's hidden gems or fan favorites
	if voteAvg, ok := params["vote_average.gte"]; ok {
		avgFloat, _ := strconv.ParseFloat(voteAvg, 64)
		if avgFloat >= 8.0 {
			// Fan favorites - use top rated
			if mediaType == "tv" {
				return &topRatedSeriesProvider{tmdb: f.tmdb}, nil
			}
			return &topRatedMoviesProvider{tmdb: f.tmdb}, nil
		} else if avgFloat >= 7.5 {
			// Hidden gems - use top rated but could filter differently
			if mediaType == "tv" {
				return &topRatedSeriesProvider{tmdb: f.tmdb}, nil
			}
			return &topRatedMoviesProvider{tmdb: f.tmdb}, nil
		}
	}

	// For date-based discover (just released), fall back to now playing/on the air
	if _, ok := params["primary_release_date"]; ok {
		return &nowPlayingProvider{tmdb: f.tmdb}, nil
	}
	if _, ok := params["first_air_date"]; ok {
		return &onTheAirProvider{tmdb: f.tmdb}, nil
	}

	// Default fallback to popular
	if mediaType == "tv" {
		return &popularSeriesProvider{tmdb: f.tmdb}, nil
	}
	return &popularMoviesProvider{tmdb: f.tmdb}, nil
}

// Provider implementations

type trendingMoviesProvider struct {
	tmdb TMDBClient
}

func (p *trendingMoviesProvider) Fetch(ctx context.Context, limit int) ([]model.Title, error) {
	res, err := p.tmdb.GetTrendingMovies(ctx)
	if err != nil {
		return nil, err
	}

	titles := make([]model.Title, 0)
	for i, item := range res.Results {
		if limit > 0 && i >= limit {
			break
		}
		titles = append(titles, model.Title{
			TmdbID:       item.ID,
			MediaType:    model.MediaTypeMovie,
			Title:        item.Title,
			Overview:     item.Overview,
			PosterPath:   item.PosterPath,
			BackdropPath: item.BackdropPath,
			ReleaseDate:  item.ReleaseDate,
			Year:         parseYearFromDate(item.ReleaseDate),
			GenreIDs:     item.GenreIDs,
			Language:     item.OriginalLanguage,
			Popularity:   float64(item.Popularity),
			VoteAverage:  float64(item.VoteAverage),
		})
	}
	return titles, nil
}

type trendingSeriesProvider struct {
	tmdb TMDBClient
}

func (p *trendingSeriesProvider) Fetch(ctx context.Context, limit int) ([]model.Title, error) {
	res, err := p.tmdb.GetTrendingSeries(ctx)
	if err != nil {
		return nil, err
	}

	titles := make([]model.Title, 0)
	for i, item := range res.Results {
		if limit > 0 && i >= limit {
			break
		}
		// For TV shows from trending, we use Name instead of Title
		titleStr := item.Title
		if titleStr == "" {
			titleStr = item.Name
		}
		releaseDate := item.ReleaseDate
		if releaseDate == "" {
			releaseDate = item.FirstAirDate
		}

		titles = append(titles, model.Title{
			TmdbID:       item.ID,
			MediaType:    model.MediaTypeSeries,
			Title:        titleStr,
			Overview:     item.Overview,
			PosterPath:   item.PosterPath,
			BackdropPath: item.BackdropPath,
			ReleaseDate:  releaseDate,
			Year:         parseYearFromDate(releaseDate),
			GenreIDs:     item.GenreIDs,
			Language:     item.OriginalLanguage,
			Popularity:   float64(item.Popularity),
			VoteAverage:  float64(item.VoteAverage),
		})
	}
	return titles, nil
}

type popularMoviesProvider struct {
	tmdb TMDBClient
}

func (p *popularMoviesProvider) Fetch(ctx context.Context, limit int) ([]model.Title, error) {
	res, err := p.tmdb.GetPopularMovies(ctx)
	if err != nil {
		return nil, err
	}

	titles := make([]model.Title, 0)
	for i, m := range res.Results {
		if limit > 0 && i >= limit {
			break
		}
		genreIDs := make([]int64, len(m.Genres))
		for j, g := range m.Genres {
			genreIDs[j] = int64(g.ID)
		}
		titles = append(titles, model.Title{
			TmdbID:       m.ID,
			MediaType:    model.MediaTypeMovie,
			Title:        m.Title,
			Overview:     m.Overview,
			PosterPath:   m.PosterPath,
			BackdropPath: m.BackdropPath,
			ReleaseDate:  m.ReleaseDate,
			Year:         parseYearFromDate(m.ReleaseDate),
			GenreIDs:     genreIDs,
			Language:     m.OriginalLanguage,
			Popularity:   float64(m.Popularity),
			VoteAverage:  float64(m.VoteAverage),
		})
	}
	return titles, nil
}

type popularSeriesProvider struct {
	tmdb TMDBClient
}

func (p *popularSeriesProvider) Fetch(ctx context.Context, limit int) ([]model.Title, error) {
	res, err := p.tmdb.GetPopularSeries(ctx)
	if err != nil {
		return nil, err
	}

	titles := make([]model.Title, 0)
	for i, s := range res.Results {
		if limit > 0 && i >= limit {
			break
		}
		titles = append(titles, model.Title{
			TmdbID:       s.ID,
			MediaType:    model.MediaTypeSeries,
			Title:        s.Name,
			Overview:     s.Overview,
			PosterPath:   s.PosterPath,
			BackdropPath: s.BackdropPath,
			ReleaseDate:  s.FirstAirDate,
			Year:         parseYearFromDate(s.FirstAirDate),
			GenreIDs:     s.GenreIDs,
			Language:     s.OriginalLanguage,
			Popularity:   float64(s.Popularity),
			VoteAverage:  float64(s.VoteAverage),
		})
	}
	return titles, nil
}

type topRatedMoviesProvider struct {
	tmdb TMDBClient
}

func (p *topRatedMoviesProvider) Fetch(ctx context.Context, limit int) ([]model.Title, error) {
	res, err := p.tmdb.GetTopRatedMovies(ctx)
	if err != nil {
		return nil, err
	}

	titles := make([]model.Title, 0)
	for i, m := range res.Results {
		if limit > 0 && i >= limit {
			break
		}
		genreIDs := make([]int64, len(m.Genres))
		for j, g := range m.Genres {
			genreIDs[j] = int64(g.ID)
		}
		titles = append(titles, model.Title{
			TmdbID:       m.ID,
			MediaType:    model.MediaTypeMovie,
			Title:        m.Title,
			Overview:     m.Overview,
			PosterPath:   m.PosterPath,
			BackdropPath: m.BackdropPath,
			ReleaseDate:  m.ReleaseDate,
			Year:         parseYearFromDate(m.ReleaseDate),
			GenreIDs:     genreIDs,
			Language:     m.OriginalLanguage,
			Popularity:   float64(m.Popularity),
			VoteAverage:  float64(m.VoteAverage),
		})
	}
	return titles, nil
}

type topRatedSeriesProvider struct {
	tmdb TMDBClient
}

func (p *topRatedSeriesProvider) Fetch(ctx context.Context, limit int) ([]model.Title, error) {
	res, err := p.tmdb.GetTopRatedSeries(ctx)
	if err != nil {
		return nil, err
	}

	titles := make([]model.Title, 0)
	for i, s := range res.Results {
		if limit > 0 && i >= limit {
			break
		}
		titles = append(titles, model.Title{
			TmdbID:       s.ID,
			MediaType:    model.MediaTypeSeries,
			Title:        s.Name,
			Overview:     s.Overview,
			PosterPath:   s.PosterPath,
			BackdropPath: s.BackdropPath,
			ReleaseDate:  s.FirstAirDate,
			Year:         parseYearFromDate(s.FirstAirDate),
			GenreIDs:     s.GenreIDs,
			Language:     s.OriginalLanguage,
			Popularity:   float64(s.Popularity),
			VoteAverage:  float64(s.VoteAverage),
		})
	}
	return titles, nil
}

type nowPlayingProvider struct {
	tmdb TMDBClient
}

func (p *nowPlayingProvider) Fetch(ctx context.Context, limit int) ([]model.Title, error) {
	res, err := p.tmdb.GetNowPlayingMovies(ctx)
	if err != nil {
		return nil, err
	}

	titles := make([]model.Title, 0)
	for i, m := range res.Results {
		if limit > 0 && i >= limit {
			break
		}
		genreIDs := make([]int64, len(m.Genres))
		for j, g := range m.Genres {
			genreIDs[j] = int64(g.ID)
		}
		titles = append(titles, model.Title{
			TmdbID:       m.ID,
			MediaType:    model.MediaTypeMovie,
			Title:        m.Title,
			Overview:     m.Overview,
			PosterPath:   m.PosterPath,
			BackdropPath: m.BackdropPath,
			ReleaseDate:  m.ReleaseDate,
			Year:         parseYearFromDate(m.ReleaseDate),
			GenreIDs:     genreIDs,
			Language:     m.OriginalLanguage,
			Popularity:   float64(m.Popularity),
			VoteAverage:  float64(m.VoteAverage),
		})
	}
	return titles, nil
}

type upcomingProvider struct {
	tmdb TMDBClient
}

func (p *upcomingProvider) Fetch(ctx context.Context, limit int) ([]model.Title, error) {
	res, err := p.tmdb.GetUpcomingMovies(ctx)
	if err != nil {
		return nil, err
	}

	titles := make([]model.Title, 0)
	for i, m := range res.Results {
		if limit > 0 && i >= limit {
			break
		}
		genreIDs := make([]int64, len(m.Genres))
		for j, g := range m.Genres {
			genreIDs[j] = int64(g.ID)
		}
		titles = append(titles, model.Title{
			TmdbID:       m.ID,
			MediaType:    model.MediaTypeMovie,
			Title:        m.Title,
			Overview:     m.Overview,
			PosterPath:   m.PosterPath,
			BackdropPath: m.BackdropPath,
			ReleaseDate:  m.ReleaseDate,
			Year:         parseYearFromDate(m.ReleaseDate),
			GenreIDs:     genreIDs,
			Language:     m.OriginalLanguage,
			Popularity:   float64(m.Popularity),
			VoteAverage:  float64(m.VoteAverage),
		})
	}
	return titles, nil
}

type onTheAirProvider struct {
	tmdb TMDBClient
}

func (p *onTheAirProvider) Fetch(ctx context.Context, limit int) ([]model.Title, error) {
	res, err := p.tmdb.GetOnTheAirSeries(ctx)
	if err != nil {
		return nil, err
	}

	titles := make([]model.Title, 0)
	for i, s := range res.Results {
		if limit > 0 && i >= limit {
			break
		}
		titles = append(titles, model.Title{
			TmdbID:       s.ID,
			MediaType:    model.MediaTypeSeries,
			Title:        s.Name,
			Overview:     s.Overview,
			PosterPath:   s.PosterPath,
			BackdropPath: s.BackdropPath,
			ReleaseDate:  s.FirstAirDate,
			Year:         parseYearFromDate(s.FirstAirDate),
			GenreIDs:     s.GenreIDs,
			Language:     s.OriginalLanguage,
			Popularity:   float64(s.Popularity),
			VoteAverage:  float64(s.VoteAverage),
		})
	}
	return titles, nil
}

func parseYearFromDate(dateStr string) *int32 {
	if len(dateStr) < 4 {
		return nil
	}
	parts := strings.Split(dateStr, "-")
	if len(parts) == 0 {
		return nil
	}
	y, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil
	}
	yy := int32(y)
	return &yy
}
