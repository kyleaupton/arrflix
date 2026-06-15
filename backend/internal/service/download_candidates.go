package service

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/indexer"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/routing"
)

const cacheTTL = 5 * time.Minute

// cachedSearchResult stores a search result with its expiration time
type cachedSearchResult struct {
	result    indexer.SearchResult
	expiresAt time.Time
}

// DownloadCandidatesService handles download candidate search and enqueueing
type DownloadCandidatesService struct {
	repo    *repo.Repository
	logger  *logger.Logger
	source  indexer.IndexerSource
	media   *MediaService
	routing *RoutingService
	cache   map[string]*cachedSearchResult
	cacheMu sync.RWMutex
}

// NewDownloadCandidatesService creates a new download candidates service
func NewDownloadCandidatesService(r *repo.Repository, l *logger.Logger, source indexer.IndexerSource, media *MediaService, routingSvc *RoutingService) *DownloadCandidatesService {
	return &DownloadCandidatesService{
		repo:    r,
		logger:  l,
		source:  source,
		media:   media,
		routing: routingSvc,
		cache:   make(map[string]*cachedSearchResult),
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

// EvaluateCandidate returns the routing dispatch record for a candidate
func (s *DownloadCandidatesService) EvaluateCandidate(ctx context.Context, movieID int64, indexerID int64, guid string) (routing.Evaluation, error) {
	// Lookup torrent from cache
	cacheKey := s.cacheKey(indexerID, guid)
	s.cacheMu.RLock()
	cached, exists := s.cache[cacheKey]
	s.cacheMu.RUnlock()

	if !exists {
		return routing.Evaluation{}, candidateNotFoundErr(indexerID, guid, "DownloadCandidatesService.EvaluateCandidate")
	}

	// Check if expired
	if time.Now().After(cached.expiresAt) {
		s.cacheMu.Lock()
		delete(s.cache, cacheKey)
		s.cacheMu.Unlock()
		return routing.Evaluation{}, candidateNotFoundErr(indexerID, guid, "DownloadCandidatesService.EvaluateCandidate")
	}

	// Transform to DownloadCandidate
	candidate := searchResultToCandidate(cached.result)

	// Build the subject with media info
	subject := s.buildMovieSubject(ctx, candidate, movieID)

	_, eval, err := s.routing.Dispatch(ctx, subject)
	if err != nil {
		return routing.Evaluation{}, err
	}

	return eval, nil
}

// PreviewCandidate previews what will happen when a candidate is enqueued.
func (s *DownloadCandidatesService) PreviewCandidate(ctx context.Context, movieID int64, indexerID int64, guid string) (routing.Evaluation, error) {
	return s.EvaluateCandidate(ctx, movieID, indexerID, guid)
}

// EnqueueCandidate creates a durable download job for a candidate (movies-only).
func (s *DownloadCandidatesService) EnqueueCandidate(ctx context.Context, movieID int64, indexerID int64, guid string) (routing.Evaluation, model.DownloadJob, error) {
	// Lookup candidate from cache
	cacheKey := s.cacheKey(indexerID, guid)
	s.cacheMu.RLock()
	cached, exists := s.cache[cacheKey]
	s.cacheMu.RUnlock()

	if !exists {
		return routing.Evaluation{}, model.DownloadJob{}, candidateNotFoundErr(indexerID, guid, "DownloadCandidatesService.EnqueueCandidate")
	}

	// Check if expired
	if time.Now().After(cached.expiresAt) {
		s.cacheMu.Lock()
		delete(s.cache, cacheKey)
		s.cacheMu.Unlock()
		return routing.Evaluation{}, model.DownloadJob{}, candidateNotFoundErr(indexerID, guid, "DownloadCandidatesService.EnqueueCandidate")
	}

	candidate := searchResultToCandidate(cached.result)

	// Build the subject with media info
	subject := s.buildMovieSubject(ctx, candidate, movieID)

	// Dispatch at grab: every action slot is resolved (rule-set or default).
	actions, eval, err := s.routing.Dispatch(ctx, subject)
	if err != nil {
		return routing.Evaluation{}, model.DownloadJob{}, err
	}
	downloaderID := *actions.DownloaderID
	libraryID := *actions.LibraryID
	nameTemplateID := *actions.NameTemplateID

	// Ensure media_item exists for this movie/library and link the job to it.
	mi, err := s.repo.GetMediaItemByTmdbID(ctx, movieID)
	needsCreate := apperrors.IsNotFound(err)
	if err != nil && !needsCreate {
		return eval, model.DownloadJob{}, err
	}

	var createParams repo.CreateMediaItemParams
	if needsCreate {
		movie, err := s.media.GetMovie(ctx, movieID)
		if err != nil {
			return eval, model.DownloadJob{}, err
		}
		var yearInt *int32
		if len(movie.ReleaseDate) >= 4 {
			if y, err := strconv.Atoi(movie.ReleaseDate[:4]); err == nil {
				yy := int32(y)
				yearInt = &yy
			}
		}
		tmdb := movieID
		createParams = repo.CreateMediaItemParams{
			Type:   "movie",
			Title:  movie.Title,
			Year:   yearInt,
			TmdbID: &tmdb,
		}
	}

	var job model.DownloadJob
	err = s.repo.InTx(ctx, func(r *repo.Repository) error {
		if needsCreate {
			var cerr error
			mi, cerr = r.CreateMediaItem(ctx, createParams)
			if cerr != nil {
				return cerr
			}
		}

		// Enter the same want spine as the autonomous flow, but at 'grabbed':
		// the user picked the release, so selection is skipped. A fresh tracking
		// and want are born with a NULL profile (uuid.Nil) — a manual grab holds
		// no selection policy. The lifecycle mirror and cancel then apply
		// uniformly to manual and autonomous grabs.
		tracking, created, terr := ensureTracking(ctx, r, mi.ID, uuid.Nil)
		if terr != nil {
			return terr
		}
		want, werr := GrabManualWant(ctx, r, tracking.ID, mi.ID, created)
		if werr != nil {
			return werr
		}

		var jerr error
		job, jerr = r.CreateDownloadJob(ctx, repo.CreateDownloadJobParams{
			Protocol:       candidate.Protocol,
			MediaType:      "movie",
			MediaItemID:    mi.ID,
			EpisodeID:      uuid.Nil,
			WantID:         want.ID,
			IndexerID:      indexerID,
			Guid:           guid,
			CandidateTitle: candidate.Title,
			CandidateLink:  candidate.Link,
			DownloaderID:   downloaderID,
			LibraryID:      libraryID,
			NameTemplateID: nameTemplateID,
		})
		return jerr
	})
	if err != nil {
		return eval, model.DownloadJob{}, err
	}

	return eval, job, nil
}

// GrabManualWant drives a manual grab into the want spine at 'grabbed',
// returning the want the download_job links to. It encodes the manual-grab
// decision table over the movie's current want:
//
//   - fresh tracking (created) or no existing want → create a want directly
//     'grabbed' with a NULL profile (uuid.Nil): a manual grab replaces selection
//     entirely, so it holds no autonomous selection policy.
//   - 'pending'/'searching' → CAS-flip to 'grabbed', pre-empting the autonomous
//     search (the worker's later grab CAS then no-ops).
//   - 'failed'/'canceled' → reactivate: CAS-flip to 'grabbed'; a 'canceled' want
//     also flips its tracking back to 'active'.
//   - 'grabbed'/'downloading'/'imported'/'available' → Conflict: already
//     acquiring or in the library, cancel first.
//
// GrabWantManual's CAS is the authoritative race guard — a prior-status read
// drives the reject/reactivate branches, but a want the worker advanced between
// the read and the CAS loses cleanly (ok=false) and surfaces as the same
// Conflict.
func GrabManualWant(ctx context.Context, r *repo.Repository, trackingID, mediaItemID uuid.UUID, created bool) (model.Want, error) {
	const op = "DownloadCandidatesService.GrabManualWant"

	if !created {
		wants, err := r.ListWantsByTracking(ctx, trackingID)
		if err != nil {
			return model.Want{}, err
		}
		if len(wants) > 0 {
			prior := wants[0]
			switch prior.Status {
			case string(model.WantGrabbed), string(model.WantDownloading), string(model.WantImported), string(model.WantAvailable):
				return model.Want{}, apperrors.Conflictf("movie is already being acquired (want is %q); cancel it before grabbing manually", prior.Status).Op(op)
			}

			grabbed, ok, err := r.GrabWantManual(ctx, prior.ID)
			if err != nil {
				return model.Want{}, err
			}
			if !ok {
				return model.Want{}, apperrors.Conflictf("movie is already being acquired; cancel it before grabbing manually").Op(op)
			}
			if prior.Status == string(model.WantCanceled) {
				if _, err := r.SetTrackingState(ctx, trackingID, string(model.TrackingActive)); err != nil {
					return model.Want{}, err
				}
			}
			return grabbed, nil
		}
	}

	return r.CreateWant(ctx, repo.CreateWantParams{
		TrackingID:       trackingID,
		MediaItemID:      mediaItemID,
		QualityProfileID: uuid.Nil,
		Status:           string(model.WantGrabbed),
	})
}

// EnqueueSeriesCandidate creates a durable download job for a series candidate.
func (s *DownloadCandidatesService) EnqueueSeriesCandidate(ctx context.Context, seriesID int64, indexerID int64, guid string, seasonNumber *int, episodeNumber *int) (routing.Evaluation, model.DownloadJob, error) {
	// Lookup candidate from cache
	cacheKey := s.cacheKey(indexerID, guid)
	s.cacheMu.RLock()
	cached, exists := s.cache[cacheKey]
	s.cacheMu.RUnlock()

	if !exists {
		return routing.Evaluation{}, model.DownloadJob{}, candidateNotFoundErr(indexerID, guid, "DownloadCandidatesService.EnqueueSeriesCandidate")
	}

	// Check if expired
	if time.Now().After(cached.expiresAt) {
		s.cacheMu.Lock()
		delete(s.cache, cacheKey)
		s.cacheMu.Unlock()
		return routing.Evaluation{}, model.DownloadJob{}, candidateNotFoundErr(indexerID, guid, "DownloadCandidatesService.EnqueueSeriesCandidate")
	}

	candidate := searchResultToCandidate(cached.result)

	// Build the subject with media info
	subject := s.buildSeriesSubject(ctx, candidate, seriesID, seasonNumber, episodeNumber)

	// Dispatch at grab: every action slot is resolved (rule-set or default).
	actions, eval, err := s.routing.Dispatch(ctx, subject)
	if err != nil {
		return routing.Evaluation{}, model.DownloadJob{}, err
	}
	downloaderID := *actions.DownloaderID
	libraryID := *actions.LibraryID
	nameTemplateID := *actions.NameTemplateID

	// Ensure media_item exists for this series and link the job to it.
	mi, err := s.repo.GetMediaItemByTmdbIDAndType(ctx, seriesID, string(model.MediaTypeSeries))
	needsCreateItem := apperrors.IsNotFound(err)
	if err != nil && !needsCreateItem {
		return eval, model.DownloadJob{}, err
	}

	var createItemParams repo.CreateMediaItemParams
	if needsCreateItem {
		series, gerr := s.media.GetSeries(ctx, seriesID)
		if gerr != nil {
			return eval, model.DownloadJob{}, gerr
		}
		var yearInt *int32
		if len(series.FirstAirDate) >= 4 {
			if y, perr := strconv.Atoi(series.FirstAirDate[:4]); perr == nil {
				yy := int32(y)
				yearInt = &yy
			}
		}
		tmdb := seriesID
		createItemParams = repo.CreateMediaItemParams{
			Type:   "series",
			Title:  series.Title,
			Year:   yearInt,
			TmdbID: &tmdb,
		}
	}

	// best-effort: empty title/nil id on TMDB failure. The episode tmdb_id is
	// the upsert key, so passing it converges onto the row structure-sync owns
	// instead of inserting a duplicate (the season+number index isn't unique).
	episodeTitle := ""
	var episodeTmdbID *int64
	if seasonNumber != nil && episodeNumber != nil {
		if tmdbEpisode, terr := s.media.tmdb.GetEpisodeDetails(ctx, seriesID, int64(*seasonNumber), int64(*episodeNumber)); terr == nil {
			episodeTitle = tmdbEpisode.Name
			id := tmdbEpisode.ID
			episodeTmdbID = &id
		}
	}

	var job model.DownloadJob
	err = s.repo.InTx(ctx, func(r *repo.Repository) error {
		if needsCreateItem {
			var cerr error
			mi, cerr = r.CreateMediaItem(ctx, createItemParams)
			if cerr != nil {
				return cerr
			}
		}

		var seasonID, episodeID uuid.UUID
		if seasonNumber != nil {
			season, serr := r.UpsertSeason(ctx, repo.UpsertSeasonParams{
				MediaItemID:  mi.ID,
				SeasonNumber: int32(*seasonNumber),
			})
			if serr != nil {
				return serr
			}
			seasonID = season.ID

			if episodeNumber != nil {
				episode, eerr := r.UpsertEpisode(ctx, repo.UpsertEpisodeParams{
					SeasonID:      season.ID,
					EpisodeNumber: int32(*episodeNumber),
					Title:         &episodeTitle,
					TmdbID:        episodeTmdbID,
				})
				if eerr != nil {
					return eerr
				}
				episodeID = episode.ID
			}
		}

		var jerr error
		job, jerr = r.CreateDownloadJob(ctx, repo.CreateDownloadJobParams{
			Protocol:       candidate.Protocol,
			MediaType:      "series",
			MediaItemID:    mi.ID,
			SeasonID:       seasonID,
			EpisodeID:      episodeID,
			WantID:         uuid.Nil,
			IndexerID:      indexerID,
			Guid:           guid,
			CandidateTitle: candidate.Title,
			CandidateLink:  candidate.Link,
			DownloaderID:   downloaderID,
			LibraryID:      libraryID,
			NameTemplateID: nameTemplateID,
		})
		return jerr
	})
	if err != nil {
		return eval, model.DownloadJob{}, err
	}

	return eval, job, nil
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
		Protocol:     result.Protocol,
		Link:         result.DownloadURL,
		Indexer:      result.IndexerName,
		IndexerID:    result.IndexerID,
		GUID:         result.GUID,
		Peers:        peers,
		Seeders:      seeders,
		Age:          result.Age,
		AgeHours:     result.AgeHours,
		Size:         result.Size,
		Grabs:        result.Grabs,
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

// buildMovieSubject creates a Subject for a movie candidate
func (s *DownloadCandidatesService) buildMovieSubject(ctx context.Context, candidate model.DownloadCandidate, movieID int64) model.Subject {
	q := parsing.Parse(candidate.Title, parsing.DomainMovie)
	evalCtx := model.NewSubject(candidate, q)

	// Try to get movie details from TMDB to populate media fields
	movie, err := s.media.GetMovie(ctx, movieID)
	if err == nil {
		year := 0
		if len(movie.ReleaseDate) >= 4 {
			if y, err := strconv.Atoi(movie.ReleaseDate[:4]); err == nil {
				year = y
			}
		}
		evalCtx = evalCtx.WithMedia(model.MediaTypeMovie, movie.Title, year, movieID, &movie.Runtime)
	}

	return evalCtx
}

// buildSeriesSubject creates a Subject for a series candidate
func (s *DownloadCandidatesService) buildSeriesSubject(ctx context.Context, candidate model.DownloadCandidate, seriesID int64, seasonNumber *int, episodeNumber *int) model.Subject {
	q := parsing.Parse(candidate.Title, parsing.DomainSeries)
	evalCtx := model.NewSubject(candidate, q)

	// Episode details give both the title and the per-episode runtime size bands
	// scale against. Fetched once, before WithMedia so runtime rides along.
	var episodeTitle *string
	var runtime *int
	if seasonNumber != nil && episodeNumber != nil {
		tmdbEpisode, err := s.media.tmdb.GetEpisodeDetails(ctx, seriesID, int64(*seasonNumber), int64(*episodeNumber))
		if err == nil {
			if tmdbEpisode.Name != "" {
				episodeTitle = &tmdbEpisode.Name
			}
			if tmdbEpisode.Runtime > 0 {
				r := tmdbEpisode.Runtime
				runtime = &r
			}
		}
	}

	// Try to get series details from TMDB to populate media fields
	series, err := s.media.GetSeries(ctx, seriesID)
	if err == nil {
		year := 0
		if len(series.FirstAirDate) >= 4 {
			if y, err := strconv.Atoi(series.FirstAirDate[:4]); err == nil {
				year = y
			}
		}
		evalCtx = evalCtx.WithMedia(model.MediaTypeSeries, series.Title, year, seriesID, runtime)
	}

	evalCtx = evalCtx.WithSeriesInfo(seasonNumber, episodeNumber, episodeTitle)

	return evalCtx
}
