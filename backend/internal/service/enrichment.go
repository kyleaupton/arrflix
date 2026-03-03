package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/repo"
)

type EnrichmentService struct {
	repo   *repo.Repository
	logger *logger.Logger
	tmdb   *TmdbService
}

func NewEnrichmentService(r *repo.Repository, l *logger.Logger, tmdb *TmdbService) *EnrichmentService {
	return &EnrichmentService{repo: r, logger: l, tmdb: tmdb}
}

// EnrichMediaItem fetches metadata from TMDB and stores it on the media item.
func (s *EnrichmentService) EnrichMediaItem(ctx context.Context, item dbgen.MediaItem) error {
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

func (s *EnrichmentService) enrichMovie(ctx context.Context, item dbgen.MediaItem) error {
	details, err := s.tmdb.GetMovieDetailsForEnrichment(ctx, *item.TmdbID)
	if err != nil {
		return err
	}

	// Extract certification from appended release dates
	var certification string
	if details.MovieReleaseDatesAppend != nil && details.ReleaseDates != nil {
		certification = extractMovieCertification(details.ReleaseDates)
	}

	genres, _ := json.Marshal(details.Genres)

	releaseDate := parseDateToPgtype(details.ReleaseDate)
	runtime := int32(details.Runtime)
	voteAvg := float64(details.VoteAverage)
	voteCount := int32(details.VoteCount)
	inProd := false

	params := dbgen.UpdateMediaItemMetadataParams{
		ID:            item.ID,
		PosterPath:    strPtrIfNotEmpty(details.PosterPath),
		BackdropPath:  strPtrIfNotEmpty(details.BackdropPath),
		Overview:      strPtrIfNotEmpty(details.Overview),
		VoteAverage:   &voteAvg,
		VoteCount:     &voteCount,
		Runtime:       &runtime,
		Status:        strPtrIfNotEmpty(details.Status),
		Certification: strPtrIfNotEmpty(certification),
		Genres:        genres,
		ReleaseDate:   releaseDate,
		LastAirDate:   pgtype.Date{},
		InProduction:  &inProd,
		ImdbID:        strPtrIfNotEmpty(details.IMDbID),
	}

	if _, err := s.repo.UpdateMediaItemMetadata(ctx, params); err != nil {
		return err
	}

	// Store raw source data
	rawJSON, err := json.Marshal(details)
	if err == nil {
		_ = s.repo.UpsertMediaMetadataSource(ctx, item.ID, "tmdb", rawJSON)
	}

	return nil
}

func (s *EnrichmentService) enrichSeries(ctx context.Context, item dbgen.MediaItem) error {
	details, err := s.tmdb.GetSeriesDetailsForEnrichment(ctx, *item.TmdbID)
	if err != nil {
		return err
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

	genres, _ := json.Marshal(details.Genres)

	firstAirDate := parseDateToPgtype(details.FirstAirDate)
	lastAirDate := parseDateToPgtype(details.LastAirDate)

	// Use first episode runtime if available
	var runtime *int32
	if len(details.EpisodeRunTime) > 0 {
		r := int32(details.EpisodeRunTime[0])
		runtime = &r
	}

	voteAvg := float64(details.VoteAverage)
	voteCount := int32(details.VoteCount)

	params := dbgen.UpdateMediaItemMetadataParams{
		ID:            item.ID,
		PosterPath:    strPtrIfNotEmpty(details.PosterPath),
		BackdropPath:  strPtrIfNotEmpty(details.BackdropPath),
		Overview:      strPtrIfNotEmpty(details.Overview),
		VoteAverage:   &voteAvg,
		VoteCount:     &voteCount,
		Runtime:       runtime,
		Status:        strPtrIfNotEmpty(details.Status),
		Certification: strPtrIfNotEmpty(certification),
		Genres:        genres,
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
		_ = s.repo.UpsertMediaMetadataSource(ctx, item.ID, "tmdb", rawJSON)
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

func parseDateToPgtype(dateStr string) pgtype.Date {
	if dateStr == "" {
		return pgtype.Date{}
	}
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return pgtype.Date{}
	}
	return pgtype.Date{
		Time:  t,
		Valid: true,
	}
}
