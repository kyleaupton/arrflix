package service

import (
	"context"
	"encoding/json"
	"time"

	tmdb "github.com/cyruzin/golang-tmdb"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// genresFromTmdb converts TMDB's genre shape ([]tmdb.Genre with {ID,Name})
// into our domain []model.Genre with {TmdbID,Name}.
func genresFromTmdb(in []tmdb.Genre) []model.Genre {
	if len(in) == 0 {
		return nil
	}
	out := make([]model.Genre, 0, len(in))
	for _, g := range in {
		out = append(out, model.Genre{TmdbID: g.ID, Name: g.Name})
	}
	return out
}

type EnrichmentService struct {
	repo   *repo.Repository
	logger *logger.Logger
	tmdb   *TmdbService
}

func NewEnrichmentService(r *repo.Repository, l *logger.Logger, tmdb *TmdbService) *EnrichmentService {
	return &EnrichmentService{repo: r, logger: l, tmdb: tmdb}
}

// EnrichMediaItem fetches metadata from TMDB and stores it on the media item.
func (s *EnrichmentService) EnrichMediaItem(ctx context.Context, item model.MediaItem) error {
	if item.TmdbID == nil {
		return nil
	}

	switch item.Type {
	case "movie":
		return s.enrichMovie(ctx, item)
	case "series":
		return s.enrichSeries(ctx, item)
	default:
		return nil
	}
}

func (s *EnrichmentService) enrichMovie(ctx context.Context, item model.MediaItem) error {
	details, err := s.tmdb.GetMovieDetailsForEnrichment(ctx, *item.TmdbID)
	if err != nil {
		return apperrors.BadGatewayf("tmdb movie details for %d: %v", *item.TmdbID, err).
			Op("EnrichmentService.enrichMovie")
	}

	// Extract certification from appended release dates
	var certification string
	if details.MovieReleaseDatesAppend != nil && details.ReleaseDates != nil {
		certification = extractMovieCertification(details.ReleaseDates)
	}

	releaseDate := parseDateToTimePtr(details.ReleaseDate)
	runtime := int32(details.Runtime)
	voteAvg := float64(details.VoteAverage)
	voteCount := int32(details.VoteCount)
	inProd := false

	params := repo.UpdateMediaItemMetadataParams{
		ID:            item.ID,
		PosterPath:    strPtrIfNotEmpty(details.PosterPath),
		BackdropPath:  strPtrIfNotEmpty(details.BackdropPath),
		Overview:      strPtrIfNotEmpty(details.Overview),
		VoteAverage:   &voteAvg,
		VoteCount:     &voteCount,
		Runtime:       &runtime,
		Status:        strPtrIfNotEmpty(details.Status),
		Certification: strPtrIfNotEmpty(certification),
		Genres:        genresFromTmdb(details.Genres),
		ReleaseDate:   releaseDate,
		LastAirDate:   nil,
		InProduction:  &inProd,
		ImdbID:        strPtrIfNotEmpty(details.IMDbID),
	}

	if _, err := s.repo.UpdateMediaItemMetadata(ctx, params); err != nil {
		return err
	}

	// Store raw source data
	rawJSON, err := json.Marshal(details)
	if err == nil {
		_ = s.repo.UpsertMediaMetadataSource(ctx, repo.UpsertMediaMetadataSourceParams{
			MediaItemID: item.ID,
			Source:      "tmdb",
			Data:        rawJSON,
		})
	}

	return nil
}

func (s *EnrichmentService) enrichSeries(ctx context.Context, item model.MediaItem) error {
	details, err := s.tmdb.GetSeriesDetailsForEnrichment(ctx, *item.TmdbID)
	if err != nil {
		return apperrors.BadGatewayf("tmdb series details for %d: %v", *item.TmdbID, err).
			Op("EnrichmentService.enrichSeries")
	}

	// Extract certification from appended content ratings
	var certification string
	if details.TVContentRatingsAppend != nil && details.ContentRatings != nil {
		certification = extractTVCertification(details.ContentRatings)
	}

	// Extract IMDB ID from appended external IDs
	var imdbID string
	if details.TVExternalIDsAppend != nil && details.TVExternalIDs != nil {
		imdbID = details.TVExternalIDs.IMDbID
	}

	firstAirDate := parseDateToTimePtr(details.FirstAirDate)
	lastAirDate := parseDateToTimePtr(details.LastAirDate)

	// Use first episode runtime if available
	var runtime *int32
	if len(details.EpisodeRunTime) > 0 {
		r := int32(details.EpisodeRunTime[0])
		runtime = &r
	}

	voteAvg := float64(details.VoteAverage)
	voteCount := int32(details.VoteCount)

	params := repo.UpdateMediaItemMetadataParams{
		ID:            item.ID,
		PosterPath:    strPtrIfNotEmpty(details.PosterPath),
		BackdropPath:  strPtrIfNotEmpty(details.BackdropPath),
		Overview:      strPtrIfNotEmpty(details.Overview),
		VoteAverage:   &voteAvg,
		VoteCount:     &voteCount,
		Runtime:       runtime,
		Status:        strPtrIfNotEmpty(details.Status),
		Certification: strPtrIfNotEmpty(certification),
		Genres:        genresFromTmdb(details.Genres),
		ReleaseDate:   firstAirDate,
		LastAirDate:   lastAirDate,
		InProduction:  &details.InProduction,
		ImdbID:        strPtrIfNotEmpty(imdbID),
	}

	if _, err := s.repo.UpdateMediaItemMetadata(ctx, params); err != nil {
		return err
	}

	// Store raw source data
	rawJSON, err := json.Marshal(details)
	if err == nil {
		_ = s.repo.UpsertMediaMetadataSource(ctx, repo.UpsertMediaMetadataSourceParams{
			MediaItemID: item.ID,
			Source:      "tmdb",
			Data:        rawJSON,
		})
	}

	return nil
}

// EnrichBatch queries stale items and enriches each. Returns the count of items processed.
func (s *EnrichmentService) EnrichBatch(ctx context.Context, staleBefore time.Time, batchSize int32) (int, error) {
	items, err := s.repo.ListStaleMediaItems(ctx, staleBefore, batchSize)
	if err != nil {
		return 0, err
	}

	enriched := 0
	for _, item := range items {
		if ctx.Err() != nil {
			return enriched, ctx.Err()
		}

		if err := s.EnrichMediaItem(ctx, item); err != nil {
			s.logger.Warn().Err(err).
				Str("media_item_id", item.ID.String()).
				Str("title", item.Title).
				Msg("enrichment failed, will retry later")
			continue
		}
		enriched++
	}

	return enriched, nil
}

func strPtrIfNotEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// parseDateToTimePtr parses a YYYY-MM-DD date string into a *time.Time.
// Empty or unparseable inputs return nil — the metadata params struct
// represents absence as nil, which the repo translates to a NULL-shaped
// pgtype.Date.
func parseDateToTimePtr(dateStr string) *time.Time {
	if dateStr == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return nil
	}
	return &t
}
