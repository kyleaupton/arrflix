package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/kyleaupton/arrflix/internal/config"
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
}

func NewTmdbService(r *repo.Repository, l *logger.Logger) *TmdbService {
	client, err := tmdb.Init(config.Load(l).TmdbAPIKey)
	if err != nil {
		panic(err)
	}

	return &TmdbService{
		repo:   r,
		logger: l,
		client: client,
	}
}

func (s *TmdbService) FindByID(ctx context.Context, id, source string) (tmdb.FindByID, error) {
	cacheKey := fmt.Sprintf("tmdb_find_by_id_%s_%s", id, source)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.FindByID, error) {
		return s.client.GetFindByID(id, map[string]string{
			"external_source": source,
		})
	}, STATIC_TTL)
}

func (s *TmdbService) GetMovieDetails(ctx context.Context, id int64) (tmdb.MovieDetails, error) {
	cacheKey := fmt.Sprintf("tmdb_movie_details_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.MovieDetails, error) {
		return s.client.GetMovieDetails(int(id), map[string]string{})
	}, STATIC_TTL)
}

// GetMovieDetailsWithExtras fetches movie details with appended release dates and watch providers.
// Uses DYNAMIC_TTL (1 hour) since watch providers can change frequently.
func (s *TmdbService) GetMovieDetailsWithExtras(ctx context.Context, id int64) (tmdb.MovieDetails, error) {
	cacheKey := fmt.Sprintf("tmdb_movie_details_extras_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.MovieDetails, error) {
		return s.client.GetMovieDetails(int(id), map[string]string{
			"append_to_response": "release_dates,watch/providers",
		})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetSeriesDetails(ctx context.Context, id int64) (tmdb.TVDetails, error) {
	cacheKey := fmt.Sprintf("tmdb_series_details_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.TVDetails, error) {
		return s.client.GetTVDetails(int(id), map[string]string{})
	}, STATIC_TTL)
}

// GetSeriesDetailsWithExtras fetches series details with appended content ratings and watch providers.
// Uses DYNAMIC_TTL (1 hour) since watch providers can change frequently.
func (s *TmdbService) GetSeriesDetailsWithExtras(ctx context.Context, id int64) (tmdb.TVDetails, error) {
	cacheKey := fmt.Sprintf("tmdb_series_details_extras_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.TVDetails, error) {
		return s.client.GetTVDetails(int(id), map[string]string{
			"append_to_response": "content_ratings,watch/providers",
		})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetTVSeasonDetails(ctx context.Context, id int64, seasonNumber int) (tmdb.TVSeasonDetails, error) {
	cacheKey := fmt.Sprintf("tmdb_tv_season_details_%d_%d", id, seasonNumber)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.TVSeasonDetails, error) {
		return s.client.GetTVSeasonDetails(int(id), seasonNumber, map[string]string{})
	}, STATIC_TTL)
}

func (s *TmdbService) GetEpisodeDetails(ctx context.Context, id int64, season int64, episode int64) (tmdb.TVEpisodeDetails, error) {
	cacheKey := fmt.Sprintf("tmdb_episode_details_%d_%d_%d", id, season, episode)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.TVEpisodeDetails, error) {
		return s.client.GetTVEpisodeDetails(int(id), int(season), int(episode), map[string]string{})
	}, STATIC_TTL)
}

func (s *TmdbService) GetTrendingMovies(ctx context.Context) (tmdb.Trending, error) {
	cacheKey := "tmdb_trending_movies"
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.Trending, error) {
		return s.client.GetTrending("movie", "day", map[string]string{})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetTrendingSeries(ctx context.Context) (tmdb.Trending, error) {
	cacheKey := "tmdb_trending_series"
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.Trending, error) {
		return s.client.GetTrending("tv", "day", map[string]string{})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetPopularMovies(ctx context.Context) (tmdb.MoviePopular, error) {
	cacheKey := "tmdb_popular_movies"
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.MoviePopular, error) {
		return s.client.GetMoviePopular(map[string]string{})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetTopRatedMovies(ctx context.Context) (tmdb.MovieTopRated, error) {
	cacheKey := "tmdb_top_rated_movies"
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.MovieTopRated, error) {
		return s.client.GetMovieTopRated(map[string]string{})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetNowPlayingMovies(ctx context.Context) (tmdb.MovieNowPlaying, error) {
	cacheKey := "tmdb_now_playing_movies"
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.MovieNowPlaying, error) {
		return s.client.GetMovieNowPlaying(map[string]string{})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetUpcomingMovies(ctx context.Context) (tmdb.MovieUpcoming, error) {
	cacheKey := "tmdb_upcoming_movies"
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.MovieUpcoming, error) {
		return s.client.GetMovieUpcoming(map[string]string{})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetPopularSeries(ctx context.Context) (tmdb.TVPopular, error) {
	cacheKey := "tmdb_popular_series"
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.TVPopular, error) {
		return s.client.GetTVPopular(map[string]string{})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetTopRatedSeries(ctx context.Context) (tmdb.TVTopRated, error) {
	cacheKey := "tmdb_top_rated_series"
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.TVTopRated, error) {
		return s.client.GetTVTopRated(map[string]string{})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetOnTheAirSeries(ctx context.Context) (tmdb.TVOnTheAir, error) {
	cacheKey := "tmdb_on_the_air_series"
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.TVOnTheAir, error) {
		return s.client.GetTVOnTheAir(map[string]string{})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetMovieCredits(ctx context.Context, id int64) (tmdb.MovieCredits, error) {
	cacheKey := fmt.Sprintf("tmdb_movie_credits_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.MovieCredits, error) {
		return s.client.GetMovieCredits(int(id), map[string]string{})
	}, STATIC_TTL)
}

func (s *TmdbService) GetMovieVideos(ctx context.Context, id int64) (tmdb.VideoResults, error) {
	cacheKey := fmt.Sprintf("tmdb_movie_videos_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.VideoResults, error) {
		return s.client.GetMovieVideos(int(id), map[string]string{})
	}, STATIC_TTL)
}

func (s *TmdbService) GetTVCredits(ctx context.Context, id int64) (tmdb.TVCredits, error) {
	cacheKey := fmt.Sprintf("tmdb_tv_credits_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.TVCredits, error) {
		return s.client.GetTVCredits(int(id), map[string]string{})
	}, STATIC_TTL)
}

func (s *TmdbService) GetTVVideos(ctx context.Context, id int64) (tmdb.VideoResults, error) {
	cacheKey := fmt.Sprintf("tmdb_tv_videos_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.VideoResults, error) {
		return s.client.GetTVVideos(int(id), map[string]string{})
	}, STATIC_TTL)
}

func (s *TmdbService) GetMovieRecommendations(ctx context.Context, id int64) (tmdb.MovieRecommendations, error) {
	cacheKey := fmt.Sprintf("tmdb_movie_recommendations_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.MovieRecommendations, error) {
		return s.client.GetMovieRecommendations(int(id), map[string]string{})
	}, STATIC_TTL)
}

func (s *TmdbService) GetPersonDetails(ctx context.Context, id int64) (tmdb.PersonDetails, error) {
	cacheKey := fmt.Sprintf("tmdb_person_details_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.PersonDetails, error) {
		return s.client.GetPersonDetails(int(id), map[string]string{})
	}, STATIC_TTL)
}

func (s *TmdbService) MultiSearch(ctx context.Context, query string, page int) (tmdb.SearchMulti, error) {
	cacheKey := fmt.Sprintf("tmdb_multi_search_%s_page_%d", query, page)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.SearchMulti, error) {
		return s.client.GetSearchMulti(query, map[string]string{
			"page": fmt.Sprintf("%d", page),
		})
	}, DYNAMIC_TTL)
}

func (s *TmdbService) GetMovieReleaseDates(ctx context.Context, id int64) (tmdb.MovieReleaseDates, error) {
	cacheKey := fmt.Sprintf("tmdb_movie_release_dates_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.MovieReleaseDates, error) {
		return s.client.GetMovieReleaseDates(int(id))
	}, STATIC_TTL)
}

func (s *TmdbService) GetTVContentRatings(ctx context.Context, id int64) (tmdb.TVContentRatings, error) {
	cacheKey := fmt.Sprintf("tmdb_tv_content_ratings_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.TVContentRatings, error) {
		return s.client.GetTVContentRatings(int(id), map[string]string{})
	}, STATIC_TTL)
}

// GetMovieDetailsForEnrichment fetches movie details with release_dates appended
// for certification extraction. Uses STATIC_TTL since enrichment data is stable.
func (s *TmdbService) GetMovieDetailsForEnrichment(ctx context.Context, id int64) (tmdb.MovieDetails, error) {
	cacheKey := fmt.Sprintf("tmdb_movie_enrich_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.MovieDetails, error) {
		return s.client.GetMovieDetails(int(id), map[string]string{
			"append_to_response": "release_dates",
		})
	}, STATIC_TTL)
}

// GetSeriesDetailsForEnrichment fetches series details with content_ratings and external_ids
// appended for certification and IMDB ID extraction. Uses STATIC_TTL.
func (s *TmdbService) GetSeriesDetailsForEnrichment(ctx context.Context, id int64) (tmdb.TVDetails, error) {
	cacheKey := fmt.Sprintf("tmdb_series_enrich_%d", id)
	return getOrFetchFromCache(ctx, s.repo, s.logger, cacheKey, func() (*tmdb.TVDetails, error) {
		return s.client.GetTVDetails(int(id), map[string]string{
			"append_to_response": "content_ratings,external_ids",
		})
	}, STATIC_TTL)
}

// getOrFetchFromCache encapsulates the pattern of:
// 1) checking API cache
// 2) calling the provided fetch function on cache miss
// 3) storing the fresh response back into the cache
// 4) returning the typed result
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
			return zero, err
		}

		category := "tmdb"
		contentType := "application/json"

		// Convert the result to json to be stored in the cache
		jsonRes, err := json.Marshal(res)
		if err != nil {
			var zero T
			return zero, err
		}

		// Note: pass nil for headers so the DB receives NULL (valid for jsonb)
		if err := r.UpsertApiCache(ctx, cacheKey, &category, jsonRes, 200, &contentType, nil, ttl); err != nil {
			l.Error().Err(err).Str("cache_key", cacheKey).Msg("Failed upserting api cache")
		}

		// Return the result
		return *res, nil
	}

	var out T
	err = json.Unmarshal(cacheEntry.Response, &out)
	if err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}
