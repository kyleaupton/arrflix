package service

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/indexer"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/policy"
	"github.com/kyleaupton/arrflix/internal/release"
	"github.com/kyleaupton/arrflix/internal/repo"
)

const cacheTTL = 5 * time.Minute

// cachedSearchResult stores a search result with its expiration time
type cachedSearchResult struct {
	result    indexer.SearchResult
	expiresAt time.Time
}

// DownloadCandidatesService handles download candidate search and enqueueing
type DownloadCandidatesService struct {
	repo         *repo.Repository
	logger       *logger.Logger
	source       indexer.IndexerSource
	media        *MediaService
	policyEngine *policy.Engine
	cache        map[string]*cachedSearchResult
	cacheMu      sync.RWMutex
}

// NewDownloadCandidatesService creates a new download candidates service
func NewDownloadCandidatesService(r *repo.Repository, l *logger.Logger, source indexer.IndexerSource, media *MediaService, engine *policy.Engine) *DownloadCandidatesService {
	return &DownloadCandidatesService{
		repo:         r,
		logger:       l,
		source:       source,
		media:        media,
		policyEngine: engine,
		cache:        make(map[string]*cachedSearchResult),
	}
}

// candidateNotFoundErr returns the typed NotFound error used when a cached
// candidate has been evicted, expired, or never existed. The handler maps this
// to 404 via the kind, regardless of the precise reason.
func candidateNotFoundErr(indexerID int64, guid, op string) error {
	return apperrors.NotFoundf("candidate %d/%q not in cache (may have expired)", indexerID, guid).
		Op(op)
}

// SearchDownloadCandidates searches for download candidates for a movie
func (s *DownloadCandidatesService) SearchDownloadCandidates(ctx context.Context, movieID int64) ([]model.DownloadCandidate, error) {
	// Get movie details to construct search query
	movie, err := s.media.GetMovie(ctx, movieID)
	if err != nil {
		return nil, err
	}

	// Construct search query: "Title Year"
	year := ""
	if movie.ReleaseDate != "" {
		// Extract year from release date (format: "2006-01-02")
		if len(movie.ReleaseDate) >= 4 {
			year = movie.ReleaseDate[:4]
		}
	}
	query := movie.Title
	if year != "" {
		query = fmt.Sprintf("%s %s", movie.Title, year)
	}

	searchQuery := indexer.SearchQuery{
		Query:     query,
		MediaType: indexer.MediaTypeMovie,
		Limit:     100,
	}

	return s.searchAndCache(ctx, searchQuery)
}

// SearchSeriesDownloadCandidates searches for download candidates for a series, season, or episode
func (s *DownloadCandidatesService) SearchSeriesDownloadCandidates(ctx context.Context, seriesID int64, season *int, episode *int) ([]model.DownloadCandidate, error) {
	series, err := s.media.GetSeries(ctx, seriesID)
	if err != nil {
		return nil, err
	}

	query := series.Title
	if season != nil {
		if episode != nil {
			query = fmt.Sprintf("%s S%02dE%02d", series.Title, *season, *episode)
		} else {
			query = fmt.Sprintf("%s S%02d", series.Title, *season)
		}
	}

	searchQuery := indexer.SearchQuery{
		Query:     query,
		MediaType: indexer.MediaTypeSeries,
		Season:    season,
		Episode:   episode,
		Limit:     100,
	}

	return s.searchAndCache(ctx, searchQuery)
}

func (s *DownloadCandidatesService) searchAndCache(ctx context.Context, query indexer.SearchQuery) ([]model.DownloadCandidate, error) {
	results, err := s.source.Search(ctx, query)
	if err != nil {
		s.logger.Error().Err(err).Str("query", query.Query).Msg("Failed to search indexer")
		return nil, apperrors.BadGatewayf("indexer search %q: %v", query.Query, err).
			Op("DownloadCandidatesService.searchAndCache")
	}

	// Clear expired entries from cache
	s.cleanExpiredCache()

	// Store results in cache and transform to DownloadCandidate
	candidates := make([]model.DownloadCandidate, 0, len(results))
	for _, result := range results {
		// Cache the result
		cacheKey := s.cacheKey(result.IndexerID, result.GUID)
		s.cacheMu.Lock()
		s.cache[cacheKey] = &cachedSearchResult{
			result:    result,
			expiresAt: time.Now().Add(cacheTTL),
		}
		s.cacheMu.Unlock()

		// Transform to DownloadCandidate for API response
		candidate := searchResultToCandidate(result)
		candidates = append(candidates, candidate)
	}

	return candidates, nil
}

// EvaluateCandidate returns the evaluation trace for a candidate
func (s *DownloadCandidatesService) EvaluateCandidate(ctx context.Context, movieID int64, indexerID int64, guid string) (model.EvaluationTrace, error) {
	// Lookup torrent from cache
	cacheKey := s.cacheKey(indexerID, guid)
	s.cacheMu.RLock()
	cached, exists := s.cache[cacheKey]
	s.cacheMu.RUnlock()

	if !exists {
		return model.EvaluationTrace{}, candidateNotFoundErr(indexerID, guid, "DownloadCandidatesService.EvaluateCandidate")
	}

	// Check if expired
	if time.Now().After(cached.expiresAt) {
		s.cacheMu.Lock()
		delete(s.cache, cacheKey)
		s.cacheMu.Unlock()
		return model.EvaluationTrace{}, candidateNotFoundErr(indexerID, guid, "DownloadCandidatesService.EvaluateCandidate")
	}

	// Transform to DownloadCandidate
	candidate := searchResultToCandidate(cached.result)

	// Build evaluation context with media info
	evalCtx := s.buildMovieEvaluationContext(ctx, candidate, movieID)

	trace, err := s.policyEngine.Evaluate(ctx, evalCtx)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to evaluate policy")
		return model.EvaluationTrace{}, apperrors.Internalf("policy evaluation failed: %v", err).
			Op("DownloadCandidatesService.EvaluateCandidate")
	}

	return trace, nil
}

// PreviewCandidate previews what will happen when a candidate is enqueued.
func (s *DownloadCandidatesService) PreviewCandidate(ctx context.Context, movieID int64, indexerID int64, guid string) (model.EvaluationTrace, error) {
	return s.EvaluateCandidate(ctx, movieID, indexerID, guid)
}

// EnqueueCandidate creates a durable download job for a candidate (movies-only).
func (s *DownloadCandidatesService) EnqueueCandidate(ctx context.Context, movieID int64, indexerID int64, guid string) (model.EvaluationTrace, dbgen.DownloadJob, error) {
	// Lookup candidate from cache
	cacheKey := s.cacheKey(indexerID, guid)
	s.cacheMu.RLock()
	cached, exists := s.cache[cacheKey]
	s.cacheMu.RUnlock()

	if !exists {
		return model.EvaluationTrace{}, dbgen.DownloadJob{}, candidateNotFoundErr(indexerID, guid, "DownloadCandidatesService.EnqueueCandidate")
	}

	// Check if expired
	if time.Now().After(cached.expiresAt) {
		s.cacheMu.Lock()
		delete(s.cache, cacheKey)
		s.cacheMu.Unlock()
		return model.EvaluationTrace{}, dbgen.DownloadJob{}, candidateNotFoundErr(indexerID, guid, "DownloadCandidatesService.EnqueueCandidate")
	}

	candidate := searchResultToCandidate(cached.result)

	// Build evaluation context with media info
	evalCtx := s.buildMovieEvaluationContext(ctx, candidate, movieID)

	trace, err := s.policyEngine.Evaluate(ctx, evalCtx)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to evaluate policy")
		return model.EvaluationTrace{}, dbgen.DownloadJob{}, apperrors.Internalf("policy evaluation failed: %v", err).
			Op("DownloadCandidatesService.EnqueueCandidate")
	}

	var downloaderID, libraryID, nameTemplateID pgtype.UUID
	if err := downloaderID.Scan(trace.FinalPlan.DownloaderID); err != nil {
		return trace, dbgen.DownloadJob{}, apperrors.Internalf("policy returned invalid downloader id %q: %v", trace.FinalPlan.DownloaderID, err).
			Op("DownloadCandidatesService.EnqueueCandidate").
			NotRetryable()
	}
	if err := libraryID.Scan(trace.FinalPlan.LibraryID); err != nil {
		return trace, dbgen.DownloadJob{}, apperrors.Internalf("policy returned invalid library id %q: %v", trace.FinalPlan.LibraryID, err).
			Op("DownloadCandidatesService.EnqueueCandidate").
			NotRetryable()
	}
	if err := nameTemplateID.Scan(trace.FinalPlan.NameTemplateID); err != nil {
		return trace, dbgen.DownloadJob{}, apperrors.Internalf("policy returned invalid name template id %q: %v", trace.FinalPlan.NameTemplateID, err).
			Op("DownloadCandidatesService.EnqueueCandidate").
			NotRetryable()
	}

	// Ensure media_item exists for this movie/library and link the job to it.
	mi, err := s.repo.GetMediaItemByTmdbID(ctx, movieID)
	if err != nil {
		if apperrors.IsNotFound(err) {
			movie, err := s.media.GetMovie(ctx, movieID)
			if err != nil {
				return trace, dbgen.DownloadJob{}, err
			}
			var yearInt *int32
			if len(movie.ReleaseDate) >= 4 {
				if y, err := strconv.Atoi(movie.ReleaseDate[:4]); err == nil {
					yy := int32(y)
					yearInt = &yy
				}
			}
			tmdb := movieID
			mi, err = s.repo.CreateMediaItem(ctx, "movie", movie.Title, yearInt, &tmdb)
			if err != nil {
				return trace, dbgen.DownloadJob{}, err
			}
		} else {
			return trace, dbgen.DownloadJob{}, err
		}
	}

	job, err := s.repo.CreateDownloadJob(ctx, dbgen.CreateDownloadJobParams{
		Protocol:       candidate.Protocol,
		MediaType:      "movie",
		MediaItemID:    mi.ID,
		EpisodeID:      pgtype.UUID{},
		IndexerID:      indexerID,
		Guid:           guid,
		CandidateTitle: candidate.Title,
		CandidateLink:  candidate.Link,
		DownloaderID:   downloaderID,
		LibraryID:      libraryID,
		NameTemplateID: nameTemplateID,
	})
	if err != nil {
		return trace, dbgen.DownloadJob{}, err
	}

	return trace, job, nil
}

// EnqueueSeriesCandidate creates a durable download job for a series candidate.
func (s *DownloadCandidatesService) EnqueueSeriesCandidate(ctx context.Context, seriesID int64, indexerID int64, guid string, seasonNumber *int, episodeNumber *int) (model.EvaluationTrace, dbgen.DownloadJob, error) {
	// Lookup candidate from cache
	cacheKey := s.cacheKey(indexerID, guid)
	s.cacheMu.RLock()
	cached, exists := s.cache[cacheKey]
	s.cacheMu.RUnlock()

	if !exists {
		return model.EvaluationTrace{}, dbgen.DownloadJob{}, candidateNotFoundErr(indexerID, guid, "DownloadCandidatesService.EnqueueSeriesCandidate")
	}

	// Check if expired
	if time.Now().After(cached.expiresAt) {
		s.cacheMu.Lock()
		delete(s.cache, cacheKey)
		s.cacheMu.Unlock()
		return model.EvaluationTrace{}, dbgen.DownloadJob{}, candidateNotFoundErr(indexerID, guid, "DownloadCandidatesService.EnqueueSeriesCandidate")
	}

	candidate := searchResultToCandidate(cached.result)

	// Build evaluation context with media info
	evalCtx := s.buildSeriesEvaluationContext(ctx, candidate, seriesID, seasonNumber, episodeNumber)

	trace, err := s.policyEngine.Evaluate(ctx, evalCtx)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to evaluate policy")
		return model.EvaluationTrace{}, dbgen.DownloadJob{}, apperrors.Internalf("policy evaluation failed: %v", err).
			Op("DownloadCandidatesService.EnqueueSeriesCandidate")
	}

	var downloaderID, libraryID, nameTemplateID pgtype.UUID
	if err := downloaderID.Scan(trace.FinalPlan.DownloaderID); err != nil {
		return trace, dbgen.DownloadJob{}, apperrors.Internalf("policy returned invalid downloader id %q: %v", trace.FinalPlan.DownloaderID, err).
			Op("DownloadCandidatesService.EnqueueSeriesCandidate").
			NotRetryable()
	}
	if err := libraryID.Scan(trace.FinalPlan.LibraryID); err != nil {
		return trace, dbgen.DownloadJob{}, apperrors.Internalf("policy returned invalid library id %q: %v", trace.FinalPlan.LibraryID, err).
			Op("DownloadCandidatesService.EnqueueSeriesCandidate").
			NotRetryable()
	}
	if err := nameTemplateID.Scan(trace.FinalPlan.NameTemplateID); err != nil {
		return trace, dbgen.DownloadJob{}, apperrors.Internalf("policy returned invalid name template id %q: %v", trace.FinalPlan.NameTemplateID, err).
			Op("DownloadCandidatesService.EnqueueSeriesCandidate").
			NotRetryable()
	}

	// Ensure media_item exists for this series and link the job to it.
	mi, err := s.repo.GetMediaItemByTmdbIDAndType(ctx, seriesID, string(model.MediaTypeSeries))
	if err != nil {
		if apperrors.IsNotFound(err) {
			series, err := s.media.GetSeries(ctx, seriesID)
			if err != nil {
				return trace, dbgen.DownloadJob{}, err
			}
			var yearInt *int32
			if len(series.FirstAirDate) >= 4 {
				if y, err := strconv.Atoi(series.FirstAirDate[:4]); err == nil {
					yy := int32(y)
					yearInt = &yy
				}
			}
			tmdb := seriesID
			mi, err = s.repo.CreateMediaItem(ctx, "series", series.Title, yearInt, &tmdb)
			if err != nil {
				return trace, dbgen.DownloadJob{}, err
			}
		} else {
			return trace, dbgen.DownloadJob{}, err
		}
	}

	var seasonID, episodeID pgtype.UUID
	if seasonNumber != nil {
		// Ensure season exists
		season, err := s.repo.UpsertSeason(ctx, mi.ID, int32(*seasonNumber), pgtype.Date{Valid: false})
		if err != nil {
			return trace, dbgen.DownloadJob{}, err
		}
		seasonID = season.ID

		if episodeNumber != nil {
			// Ensure episode exists
			title := ""
			tmdbEpisode, err := s.media.tmdb.GetEpisodeDetails(ctx, seriesID, int64(*seasonNumber), int64(*episodeNumber))
			if err == nil {
				title = tmdbEpisode.Name
			}

			episode, err := s.repo.UpsertEpisode(ctx, seasonID, int32(*episodeNumber), &title, pgtype.Date{Valid: false}, nil, nil)
			if err != nil {
				return trace, dbgen.DownloadJob{}, err
			}
			episodeID = episode.ID
		}
	}

	job, err := s.repo.CreateDownloadJob(ctx, dbgen.CreateDownloadJobParams{
		Protocol:       candidate.Protocol,
		MediaType:      "series",
		MediaItemID:    mi.ID,
		SeasonID:       seasonID,
		EpisodeID:      episodeID,
		IndexerID:      indexerID,
		Guid:           guid,
		CandidateTitle: candidate.Title,
		CandidateLink:  candidate.Link,
		DownloaderID:   downloaderID,
		LibraryID:      libraryID,
		NameTemplateID: nameTemplateID,
	})
	if err != nil {
		return trace, dbgen.DownloadJob{}, err
	}

	return trace, job, nil
}

// searchResultToCandidate converts an indexer.SearchResult to a model.DownloadCandidate
func searchResultToCandidate(result indexer.SearchResult) model.DownloadCandidate {
	// Handle optional pointer fields
	var seeders, peers int
	if result.Seeders != nil {
		seeders = *result.Seeders
	}
	if result.Leechers != nil {
		peers = *result.Leechers
	}

	return model.DownloadCandidate{
		Protocol:    result.Protocol,
		Link:        result.DownloadURL,
		Indexer:     result.IndexerName,
		IndexerID:   result.IndexerID,
		GUID:        result.GUID,
		Peers:       peers,
		Seeders:     seeders,
		Age:         result.Age,
		AgeHours:    result.AgeHours,
		Size:        result.Size,
		Grabs:       result.Grabs,
		Categories:   result.Categories,
		IndexerFlags: result.IndexerFlags,
		PublishDate:  result.PublishDate,
		Title:        result.Title,
	}
}

// cacheKey generates a cache key from indexer ID and GUID
func (s *DownloadCandidatesService) cacheKey(indexerID int64, guid string) string {
	return fmt.Sprintf("%d:%s", indexerID, guid)
}

// cleanExpiredCache removes expired entries from the cache
func (s *DownloadCandidatesService) cleanExpiredCache() {
	now := time.Now()
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	for key, cached := range s.cache {
		if now.After(cached.expiresAt) {
			delete(s.cache, key)
		}
	}
}

// buildMovieEvaluationContext creates an EvaluationContext for a movie candidate
func (s *DownloadCandidatesService) buildMovieEvaluationContext(ctx context.Context, candidate model.DownloadCandidate, movieID int64) model.EvaluationContext {
	q := release.Parse(candidate.Title)
	evalCtx := model.NewEvaluationContext(candidate, q)

	// Try to get movie details from TMDB to populate media fields
	movie, err := s.media.GetMovie(ctx, movieID)
	if err == nil {
		year := 0
		if len(movie.ReleaseDate) >= 4 {
			if y, err := strconv.Atoi(movie.ReleaseDate[:4]); err == nil {
				year = y
			}
		}
		evalCtx = evalCtx.WithMedia(model.MediaTypeMovie, movie.Title, year, movieID)
	}

	return evalCtx
}

// buildSeriesEvaluationContext creates an EvaluationContext for a series candidate
func (s *DownloadCandidatesService) buildSeriesEvaluationContext(ctx context.Context, candidate model.DownloadCandidate, seriesID int64, seasonNumber *int, episodeNumber *int) model.EvaluationContext {
	q := release.Parse(candidate.Title)
	evalCtx := model.NewEvaluationContext(candidate, q)

	// Try to get series details from TMDB to populate media fields
	series, err := s.media.GetSeries(ctx, seriesID)
	if err == nil {
		year := 0
		if len(series.FirstAirDate) >= 4 {
			if y, err := strconv.Atoi(series.FirstAirDate[:4]); err == nil {
				year = y
			}
		}
		evalCtx = evalCtx.WithMedia(model.MediaTypeSeries, series.Title, year, seriesID)
	}

	// Add season/episode info if available
	var episodeTitle *string
	if seasonNumber != nil && episodeNumber != nil {
		// Try to get episode title from TMDB
		tmdbEpisode, err := s.media.tmdb.GetEpisodeDetails(ctx, seriesID, int64(*seasonNumber), int64(*episodeNumber))
		if err == nil && tmdbEpisode.Name != "" {
			episodeTitle = &tmdbEpisode.Name
		}
	}
	evalCtx = evalCtx.WithSeriesInfo(seasonNumber, episodeNumber, episodeTitle)

	return evalCtx
}
