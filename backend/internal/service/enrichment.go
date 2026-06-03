package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	tmdb "github.com/cyruzin/golang-tmdb"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/metadata"
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
		Status:        canonicalStatusPtr(details.Status),
		Certification: strPtrIfNotEmpty(certification),
		Genres:        genresFromTmdb(details.Genres),
		ReleaseDate:   releaseDate,
		LastAirDate:   nil,
		InProduction:  &inProd,
	}

	if _, err := s.repo.UpdateMediaItemMetadata(ctx, params); err != nil {
		return err
	}

	// Record the imdb cross-reference in the external_id registry.
	s.upsertExternalID(ctx, item, metadata.SourceIMDB, details.IMDbID)

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
		Status:        canonicalStatusPtr(details.Status),
		Certification: strPtrIfNotEmpty(certification),
		Genres:        genresFromTmdb(details.Genres),
		ReleaseDate:   firstAirDate,
		LastAirDate:   lastAirDate,
		InProduction:  &details.InProduction,
	}

	// Advance metadata_updated_at first, so a later partial structure-sync
	// failure doesn't leave the item pinned stale and re-fetched every tick.
	if _, err := s.repo.UpdateMediaItemMetadata(ctx, params); err != nil {
		return err
	}

	// Record cross-reference namespaces in the external_id registry. The tvdb
	// id (TVExternalIDs.TVDBID) is the series cross-ref acquisition reads at
	// indexer-search time — fetched via the external_ids append, persisted here.
	// The guard ensures the append actually came back before we trust its ids.
	if details.TVExternalIDsAppend != nil && details.TVExternalIDs != nil {
		s.upsertExternalID(ctx, item, metadata.SourceIMDB, details.IMDbID)
		if details.TVDBID != 0 {
			s.upsertExternalID(ctx, item, metadata.SourceTVDB, strconv.FormatInt(details.TVDBID, 10))
		}
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

	// Sync the full season/episode tree (incl. unaired episodes) from the
	// seasons TMDB already returned on `details`. Best-effort: never fails the
	// enrich (item metadata is already committed above).
	s.syncSeriesStructure(ctx, item, details)

	return nil
}

// syncSeriesStructure upserts the full season/episode tree from TMDB,
// including unaired episodes (air_date in the future or NULL). It is the
// structural half of series sync — the data tracking and "coming soon" UI
// operate on. Episodes are keyed on the stable TMDB episode id, so a renumber
// updates rows in place rather than orphaning them. Best-effort: a season
// fetch or upsert failing logs and continues so one bad season doesn't block
// the rest of the tree.
func (s *EnrichmentService) syncSeriesStructure(ctx context.Context, item model.MediaItem, details tmdb.TVDetails) {
	syncedIDs := make([]int64, 0, details.NumberOfEpisodes)

	// complete stays true only if every season fetched and every row upserted.
	// DeprecateRemovedEpisodes keys off the synced set, so a partial sync (one
	// season's fetch/upsert failed) would see that season's still-live episodes
	// as "removed" and wrongly deprecate them. We only deprecate on a full pass.
	complete := true

	for _, season := range details.Seasons {
		seasonDetails, err := s.tmdb.GetTVSeasonDetails(ctx, *item.TmdbID, season.SeasonNumber)
		if err != nil {
			s.logger.Warn().Err(err).
				Str("title", item.Title).Int("season", season.SeasonNumber).
				Msg("series structure sync: season fetch failed, skipping season")
			complete = false
			continue
		}

		seasonRow, err := s.repo.UpsertSeason(ctx, repo.UpsertSeasonParams{
			MediaItemID:  item.ID,
			SeasonNumber: int32(season.SeasonNumber),
			Name:         strPtrIfNotEmpty(season.Name),
			Overview:     strPtrIfNotEmpty(season.Overview),
			PosterPath:   strPtrIfNotEmpty(season.PosterPath),
			AirDate:      parseDateToTimePtr(season.AirDate),
		})
		if err != nil {
			s.logger.Warn().Err(err).
				Str("title", item.Title).Int("season", season.SeasonNumber).
				Msg("series structure sync: season upsert failed, skipping season")
			complete = false
			continue
		}

		for _, ep := range seasonDetails.Episodes {
			epID := ep.ID
			voteAvg := float64(ep.VoteAverage)
			if _, err := s.repo.UpsertEpisode(ctx, repo.UpsertEpisodeParams{
				SeasonID:      seasonRow.ID,
				EpisodeNumber: int32(ep.EpisodeNumber),
				Title:         strPtrIfNotEmpty(ep.Name),
				AirDate:       parseDateToTimePtr(ep.AirDate),
				Overview:      strPtrIfNotEmpty(ep.Overview),
				StillPath:     strPtrIfNotEmpty(ep.StillPath),
				VoteAverage:   &voteAvg,
				Runtime:       int32PtrIfPositive(ep.Runtime),
				TmdbID:        &epID,
			}); err != nil {
				s.logger.Warn().Err(err).
					Str("title", item.Title).Int("season", season.SeasonNumber).Int("episode", ep.EpisodeNumber).
					Msg("series structure sync: episode upsert failed, skipping episode")
				complete = false
				continue
			}
			syncedIDs = append(syncedIDs, epID)
		}
	}

	// Mark episodes TMDB no longer reports as deprecated — but only after a
	// complete sync. A partial sync (any season fetch/upsert failed) would
	// deprecate the missing season's still-live episodes, so we skip it and
	// let the next clean pass reconcile. An empty set means everything failed.
	if complete && len(syncedIDs) > 0 {
		if err := s.repo.DeprecateRemovedEpisodes(ctx, item.ID, syncedIDs); err != nil {
			s.logger.Warn().Err(err).Str("title", item.Title).Msg("series structure sync: deprecate-removed failed")
		}
	}
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

// upsertExternalID records a secondary-namespace cross-reference (imdb/tvdb/…)
// for item in the external_id registry. Best-effort and non-fatal: the item's
// metadata is already committed by the time this runs, so a registry write
// failure logs and returns rather than failing the enrich. An empty value is
// skipped — the namespace simply isn't recorded.
func (s *EnrichmentService) upsertExternalID(ctx context.Context, item model.MediaItem, source metadata.ExternalSource, value string) {
	if value == "" {
		return
	}
	if err := s.repo.UpsertMediaItemExternalID(ctx, item.ID, string(source), value); err != nil {
		s.logger.Warn().Err(err).
			Str("title", item.Title).Str("source", string(source)).
			Msg("enrich: external-id upsert failed")
	}
}

// canonicalStatusPtr maps a raw TMDB status to our canonical token, returning
// nil when the raw is empty so a never-set status persists as NULL — keeping
// "never enriched" distinguishable from a mapped-but-unknown value. A non-empty
// raw that doesn't map stores the canonical "unknown" token.
func canonicalStatusPtr(raw string) *string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	s := string(model.CanonicalizeStatus(raw))
	return &s
}

// int32PtrIfPositive returns a *int32 for a positive value, nil otherwise —
// so a missing/zero runtime (common on unaired episodes) stores as NULL.
func int32PtrIfPositive(n int) *int32 {
	if n <= 0 {
		return nil
	}
	v := int32(n)
	return &v
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
