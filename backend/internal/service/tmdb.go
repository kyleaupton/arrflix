package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	tmdb "github.com/cyruzin/golang-tmdb"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// TTL for static data (7 days)
const STATIC_TTL = (24 * time.Hour) * 7

// TTL for dynamic data (1 hour)
const DYNAMIC_TTL = time.Hour

type TmdbService struct {
	repo   *repo.Repository
	client *tmdb.Client
	logger *logger.Logger
	mu     sync.RWMutex
}

func NewTmdbService(r *repo.Repository, l *logger.Logger, apiKey string) *TmdbService {
	s := &TmdbService{
		repo:   r,
		logger: l,
	}
	if apiKey != "" {
		client, err := tmdb.Init(apiKey)
		if err != nil {
			l.Error().Err(err).Msg("failed to initialize TMDB client")
		} else {
			s.client = client
		}
	}
	return s
}

// InitClient creates or replaces the TMDB client with a new API key.
func (s *TmdbService) InitClient(apiKey string) error {
	client, err := tmdb.Init(apiKey)
	if err != nil {
		return apperrors.Validation("invalid TMDB API key",
			apperrors.Field("body.api_key", err.Error()),
		).Op("TmdbService.InitClient")
	}
	s.mu.Lock()
	s.client = client
	s.mu.Unlock()
	return nil
}

func (s *TmdbService) getClient() (*tmdb.Client, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.client == nil {
		return nil, apperrors.Conflictf("TMDB API key not configured").
			Op("TmdbService.getClient")
	}
	return s.client, nil
}

// ValidateTmdbKey creates a temporary client and makes a lightweight call to verify the key.
func ValidateTmdbKey(apiKey string) error {
	client, err := tmdb.Init(apiKey)
	if err != nil {
		return apperrors.Validation("invalid TMDB API key",
			apperrors.Field("body.api_key", err.Error()),
		).Op("ValidateTmdbKey")
	}
	_, err = client.GetMovieDetails(550, map[string]string{})
	if err != nil {
		return apperrors.BadGatewayf("TMDB API key validation failed: %v", err).
			Op("ValidateTmdbKey")
	}
	return nil
}

func (s *TmdbService) FindByID(ctx context.Context, id, source string) (tmdb.FindByID, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.FindByID
		return zero, err
	}
	cacheKey := fmt.Sprintf("tmdb_find_by_id_%s_%s", id, source)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.FindByID, error) {
		return c.GetFindByID(id, map[string]string{
			"external_source": source,
		})
	}, STATIC_TTL)
}

func (s *TmdbService) GetMovieDetails(ctx context.Context, id int64) (tmdb.MovieDetails, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.MovieDetails
		return zero, err
	}
	cacheKey := fmt.Sprintf("tmdb_movie_details_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.MovieDetails, error) {
		return c.GetMovieDetails(int(id), map[string]string{})
	}, STATIC_TTL)
}

// GetMovieDetailsWithExtras fetches movie details with appended release dates and watch providers.
// Uses DYNAMIC_TTL (1 hour) since watch providers can change frequently.
func (s *TmdbService) GetMovieDetailsWithExtras(ctx context.Context, id int64) (tmdb.MovieDetails, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.MovieDetails
		return zero, err
	}
	cacheKey := fmt.Sprintf("tmdb_movie_details_extras_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.MovieDetails, error) {
		return c.GetMovieDetails(int(id), map[string]string{
			"append_to_response": "release_dates,watch/providers",
		})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetSeriesDetails(ctx context.Context, id int64) (tmdb.TVDetails, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.TVDetails
		return zero, err
	}
	cacheKey := fmt.Sprintf("tmdb_series_details_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.TVDetails, error) {
		return c.GetTVDetails(int(id), map[string]string{})
	}, STATIC_TTL)
}

// GetSeriesDetailsWithExtras fetches series details with appended content ratings and watch providers.
// Uses DYNAMIC_TTL (1 hour) since watch providers can change frequently.
func (s *TmdbService) GetSeriesDetailsWithExtras(ctx context.Context, id int64) (tmdb.TVDetails, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.TVDetails
		return zero, err
	}
	cacheKey := fmt.Sprintf("tmdb_series_details_extras_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.TVDetails, error) {
		return c.GetTVDetails(int(id), map[string]string{
			"append_to_response": "content_ratings,watch/providers",
		})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetTVSeasonDetails(ctx context.Context, id int64, seasonNumber int) (tmdb.TVSeasonDetails, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.TVSeasonDetails
		return zero, err
	}
	cacheKey := fmt.Sprintf("tmdb_tv_season_details_%d_%d", id, seasonNumber)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.TVSeasonDetails, error) {
		return c.GetTVSeasonDetails(int(id), seasonNumber, map[string]string{})
	}, STATIC_TTL)
}

func (s *TmdbService) GetEpisodeDetails(ctx context.Context, id int64, season int64, episode int64) (tmdb.TVEpisodeDetails, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.TVEpisodeDetails
		return zero, err
	}
	cacheKey := fmt.Sprintf("tmdb_episode_details_%d_%d_%d", id, season, episode)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.TVEpisodeDetails, error) {
		return c.GetTVEpisodeDetails(int(id), int(season), int(episode), map[string]string{})
	}, STATIC_TTL)
}

func (s *TmdbService) GetTrendingMovies(ctx context.Context) (tmdb.Trending, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.Trending
		return zero, err
	}
	cacheKey := "tmdb_trending_movies"
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.Trending, error) {
		return c.GetTrending("movie", "day", map[string]string{})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetTrendingSeries(ctx context.Context) (tmdb.Trending, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.Trending
		return zero, err
	}
	cacheKey := "tmdb_trending_series"
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.Trending, error) {
		return c.GetTrending("tv", "day", map[string]string{})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetPopularMovies(ctx context.Context) (tmdb.MoviePopular, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.MoviePopular
		return zero, err
	}
	cacheKey := "tmdb_popular_movies"
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.MoviePopular, error) {
		return c.GetMoviePopular(map[string]string{})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetTopRatedMovies(ctx context.Context) (tmdb.MovieTopRated, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.MovieTopRated
		return zero, err
	}
	cacheKey := "tmdb_top_rated_movies"
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.MovieTopRated, error) {
		return c.GetMovieTopRated(map[string]string{})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetNowPlayingMovies(ctx context.Context) (tmdb.MovieNowPlaying, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.MovieNowPlaying
		return zero, err
	}
	cacheKey := "tmdb_now_playing_movies"
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.MovieNowPlaying, error) {
		return c.GetMovieNowPlaying(map[string]string{})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetUpcomingMovies(ctx context.Context) (tmdb.MovieUpcoming, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.MovieUpcoming
		return zero, err
	}
	cacheKey := "tmdb_upcoming_movies"
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.MovieUpcoming, error) {
		return c.GetMovieUpcoming(map[string]string{})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetPopularSeries(ctx context.Context) (tmdb.TVPopular, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.TVPopular
		return zero, err
	}
	cacheKey := "tmdb_popular_series"
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.TVPopular, error) {
		return c.GetTVPopular(map[string]string{})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetTopRatedSeries(ctx context.Context) (tmdb.TVTopRated, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.TVTopRated
		return zero, err
	}
	cacheKey := "tmdb_top_rated_series"
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.TVTopRated, error) {
		return c.GetTVTopRated(map[string]string{})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetOnTheAirSeries(ctx context.Context) (tmdb.TVOnTheAir, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.TVOnTheAir
		return zero, err
	}
	cacheKey := "tmdb_on_the_air_series"
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.TVOnTheAir, error) {
		return c.GetTVOnTheAir(map[string]string{})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetMovieCredits(ctx context.Context, id int64) (tmdb.MovieCredits, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.MovieCredits
		return zero, err
	}
	cacheKey := fmt.Sprintf("tmdb_movie_credits_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.MovieCredits, error) {
		return c.GetMovieCredits(int(id), map[string]string{})
	}, STATIC_TTL)
}

func (s *TmdbService) GetMovieVideos(ctx context.Context, id int64) (tmdb.VideoResults, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.VideoResults
		return zero, err
	}
	cacheKey := fmt.Sprintf("tmdb_movie_videos_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.VideoResults, error) {
		return c.GetMovieVideos(int(id), map[string]string{})
	}, STATIC_TTL)
}

func (s *TmdbService) GetTVCredits(ctx context.Context, id int64) (tmdb.TVCredits, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.TVCredits
		return zero, err
	}
	cacheKey := fmt.Sprintf("tmdb_tv_credits_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.TVCredits, error) {
		return c.GetTVCredits(int(id), map[string]string{})
	}, STATIC_TTL)
}

func (s *TmdbService) GetTVVideos(ctx context.Context, id int64) (tmdb.VideoResults, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.VideoResults
		return zero, err
	}
	cacheKey := fmt.Sprintf("tmdb_tv_videos_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.VideoResults, error) {
		return c.GetTVVideos(int(id), map[string]string{})
	}, STATIC_TTL)
}

func (s *TmdbService) GetMovieRecommendations(ctx context.Context, id int64) (tmdb.MovieRecommendations, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.MovieRecommendations
		return zero, err
	}
	cacheKey := fmt.Sprintf("tmdb_movie_recommendations_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.MovieRecommendations, error) {
		return c.GetMovieRecommendations(int(id), map[string]string{})
	}, STATIC_TTL)
}

func (s *TmdbService) GetPersonDetails(ctx context.Context, id int64) (tmdb.PersonDetails, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.PersonDetails
		return zero, err
	}
	cacheKey := fmt.Sprintf("tmdb_person_details_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.PersonDetails, error) {
		return c.GetPersonDetails(int(id), map[string]string{})
	}, STATIC_TTL)
}

func (s *TmdbService) MultiSearch(ctx context.Context, query string, page int) (tmdb.SearchMulti, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.SearchMulti
		return zero, err
	}
	cacheKey := fmt.Sprintf("tmdb_multi_search_%s_page_%d", query, page)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.SearchMulti, error) {
		return c.GetSearchMulti(query, map[string]string{
			"page": fmt.Sprintf("%d", page),
		})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetMovieReleaseDates(ctx context.Context, id int64) (tmdb.MovieReleaseDates, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.MovieReleaseDates
		return zero, err
	}
	cacheKey := fmt.Sprintf("tmdb_movie_release_dates_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.MovieReleaseDates, error) {
		return c.GetMovieReleaseDates(int(id))
	}, STATIC_TTL)
}

func (s *TmdbService) GetTVContentRatings(ctx context.Context, id int64) (tmdb.TVContentRatings, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.TVContentRatings
		return zero, err
	}
	cacheKey := fmt.Sprintf("tmdb_tv_content_ratings_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.TVContentRatings, error) {
		return c.GetTVContentRatings(int(id), map[string]string{})
	}, STATIC_TTL)
}

// GetMovieDetailsForEnrichment fetches movie details with release_dates appended
// for certification extraction. Uses STATIC_TTL since enrichment data is stable.
func (s *TmdbService) GetMovieDetailsForEnrichment(ctx context.Context, id int64) (tmdb.MovieDetails, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.MovieDetails
		return zero, err
	}
	cacheKey := fmt.Sprintf("tmdb_movie_enrich_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.MovieDetails, error) {
		return c.GetMovieDetails(int(id), map[string]string{
			"append_to_response": "release_dates",
		})
	}, STATIC_TTL)
}

// GetSeriesDetailsForEnrichment fetches series details with content_ratings and external_ids
// appended for certification and IMDB ID extraction. Uses STATIC_TTL.
func (s *TmdbService) GetSeriesDetailsForEnrichment(ctx context.Context, id int64) (tmdb.TVDetails, error) {
	c, err := s.getClient()
	if err != nil {
		var zero tmdb.TVDetails
		return zero, err
	}
	cacheKey := fmt.Sprintf("tmdb_series_enrich_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.TVDetails, error) {
		return c.GetTVDetails(int(id), map[string]string{
			"append_to_response": "content_ratings,external_ids",
		})
	}, STATIC_TTL)
}

// getOrFetchFromCache encapsulates the pattern of:
// 1) checking API cache
// 2) calling the provided fetch function on cache miss
// 3) storing the fresh response back into the cache
// 4) returning the typed result
//
// Fetch failures are surfaced as BadGateway so upstream-API calls (TMDB,
// GitHub) get the right wire status and retry semantics. Marshal/unmarshal
// failures are Internal — they indicate a programming error, not transient.
func getOrFetchFromCache[T any](ctx context.Context, r *repo.Repository, l *logger.Logger, cacheKey string, fetch func() (*T, error), ttl time.Duration) (T, error) {
	cacheEntry, found, err := r.GetApiCache(ctx, cacheKey)
	if err != nil {
		var zero T
		return zero, err
	}

	if !found {
		l.Debug().Str("cache_key", cacheKey).Msg("Cache miss, fetching from API")
		res, err := fetch()
		if err != nil {
			var zero T
			return zero, apperrors.BadGatewayf("upstream fetch %q failed: %v", cacheKey, err)
		}

		category := "tmdb"
		contentType := "application/json"

		// Convert the result to json to be stored in the cache
		jsonRes, err := json.Marshal(res)
		if err != nil {
			var zero T
			return zero, apperrors.Internalf("marshal upstream response %q: %v", cacheKey, err).
				NotRetryable()
		}

		// Note: pass nil for headers so the DB receives NULL (valid for jsonb)
		if err := r.UpsertApiCache(ctx, repo.UpsertApiCacheParams{
			Key:         cacheKey,
			Category:    &category,
			Response:    jsonRes,
			Status:      200,
			ContentType: &contentType,
			Headers:     nil,
			TTL:         ttl,
		}); err != nil {
			l.Error().Err(err).Str("cache_key", cacheKey).Msg("Failed upserting api cache")
		}

		// Return the result
		return *res, nil
	}

	var out T
	err = json.Unmarshal(cacheEntry.Response, &out)
	if err != nil {
		var zero T
		return zero, apperrors.Internalf("unmarshal cached response %q: %v", cacheKey, err).
			NotRetryable()
	}
	return out, nil
}
