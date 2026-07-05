package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/google/uuid"
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
	repo      *repo.Repository
	logger    *logger.Logger
	tmdb      *TmdbService
	reconcile *ReconcileService
}

func NewEnrichmentService(r *repo.Repository, l *logger.Logger, tmdb *TmdbService, reconcile *ReconcileService) *EnrichmentService {
	return &EnrichmentService{repo: r, logger: l, tmdb: tmdb, reconcile: reconcile}
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

	if err := s.applyMovieMetadata(ctx, s.repo, item, details); err != nil {
		return err
	}
	s.storeRawPayload(ctx, item.ID, "tmdb", details)
	return nil
}

// applyMovieMetadata writes a movie's already-fetched TMDB payload onto the
// media_item row and records the imdb cross-ref. It runs against the passed repo
// handle `r`, so it composes into either the enrichment worker's plain repo or a
// caller's transaction — the request spawn applies metadata inside its spawn tx,
// born-complete and with no extra TMDB call. Both the item columns and the imdb
// external-id are hard writes: a failure returns and, under the spawn tx, rolls
// the spawn back rather than birthing a half-populated item. The raw-payload
// store is deliberately excluded — it's best-effort and belongs outside any tx
// (see storeRawPayload).
func (s *EnrichmentService) applyMovieMetadata(ctx context.Context, r *repo.Repository, item model.MediaItem, details tmdb.MovieDetails) error {
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

	if _, err := r.UpdateMediaItemMetadata(ctx, params); err != nil {
		return err
	}

	// The imdb id helps acquisition ID-match on the first search. Guarded on a
	// non-empty value so a missing id doesn't write an empty external_id.
	if details.IMDbID != "" {
		if err := r.UpsertMediaItemExternalID(ctx, item.ID, string(metadata.SourceIMDB), details.IMDbID); err != nil {
			return err
		}
	}
	return nil
}

func (s *EnrichmentService) enrichSeries(ctx context.Context, item model.MediaItem) error {
	details, err := s.tmdb.GetSeriesDetailsForEnrichment(ctx, *item.TmdbID)
	if err != nil {
		return apperrors.BadGatewayf("tmdb series details for %d: %v", *item.TmdbID, err).
			Op("EnrichmentService.enrichSeries")
	}

	if err := s.applySeriesMetadata(ctx, s.repo, item, details); err != nil {
		return err
	}

	s.storeRawPayload(ctx, item.ID, "tmdb", details)

	// Sync the full season/episode tree (incl. unaired episodes) from the
	// seasons TMDB already returned on `details`. Best-effort: never fails the
	// enrich. applySeriesMetadata advanced metadata_updated_at above, so a
	// partial structure-sync failure here doesn't leave the item pinned stale
	// and re-fetched every tick.
	s.SyncSeriesStructure(ctx, item, details)

	// Newly-aired / newly-added in-scope episodes become wants on the next sync.
	// Only tracked series reconcile; enrichment also runs for untracked items.
	tracking, err := s.repo.FindTrackingByMediaItem(ctx, item.ID)
	if apperrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.reconcile != nil {
		if rerr := s.reconcile.Reconcile(ctx, tracking.ID); rerr != nil {
			s.logger.Warn().Err(rerr).Str("title", item.Title).
				Msg("series enrich: post-sync reconcile failed, will heal on next pass")
		}
	}
	return nil
}

// applySeriesMetadata writes a series' already-fetched TMDB payload onto the
// media_item row and records the imdb/tvdb cross-refs. Like applyMovieMetadata
// it runs against the passed repo handle `r`, composing into either the worker's
// plain repo or the request spawn's transaction. It is scoped to the media_item
// row and its external ids only — structure sync and want reconciliation stay
// as the separate post-commit calls the callers make, since folding them here
// would double-sync the episode tree. All writes are hard (a failure rolls back
// under the spawn tx); the raw-payload store is excluded (see storeRawPayload).
func (s *EnrichmentService) applySeriesMetadata(ctx context.Context, r *repo.Repository, item model.MediaItem, details tmdb.TVDetails) error {
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
		rt := int32(details.EpisodeRunTime[0])
		runtime = &rt
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

	if _, err := r.UpdateMediaItemMetadata(ctx, params); err != nil {
		return err
	}

	// Record cross-reference namespaces in the external_id registry. The tvdb
	// id (TVExternalIDs.TVDBID) is the series cross-ref acquisition reads at
	// indexer-search time — fetched via the external_ids append, persisted here.
	// The guard ensures the append actually came back before we trust its ids.
	if details.TVExternalIDsAppend != nil && details.TVExternalIDs != nil {
		if details.IMDbID != "" {
			if err := r.UpsertMediaItemExternalID(ctx, item.ID, string(metadata.SourceIMDB), details.IMDbID); err != nil {
				return err
			}
		}
		if details.TVDBID != 0 {
			if err := r.UpsertMediaItemExternalID(ctx, item.ID, string(metadata.SourceTVDB), strconv.FormatInt(details.TVDBID, 10)); err != nil {
				return err
			}
		}
	}
	return nil
}

// storeRawPayload marshals a provider payload and upserts it into
// media_metadata_source. Best-effort: the raw blob is portability/debug
// insurance, never user-visible, so a marshal or write failure logs and
// continues rather than failing the caller. It must be called outside any
// transaction — the ~100KB JSONB blob stays off the atomic spawn path, and a
// swallowed error mid-tx would poison the whole pgx transaction.
func (s *EnrichmentService) storeRawPayload(ctx context.Context, mediaItemID uuid.UUID, source string, payload any) {
	rawJSON, err := json.Marshal(payload)
	if err != nil {
		s.logger.Warn().Err(err).
			Str("media_item_id", mediaItemID.String()).Str("source", source).
			Msg("enrich: marshal raw payload failed")
		return
	}
	if err := s.repo.UpsertMediaMetadataSource(ctx, repo.UpsertMediaMetadataSourceParams{
		MediaItemID: mediaItemID,
		Source:      source,
		Data:        rawJSON,
	}); err != nil {
		s.logger.Warn().Err(err).
			Str("media_item_id", mediaItemID.String()).Str("source", source).
			Msg("enrich: store raw payload failed")
	}
}

// SyncSeriesStructure upserts the full season/episode tree from TMDB,
// including unaired episodes (air_date in the future or NULL). It is the
// structural half of series sync — the data tracking and "coming soon" UI
// operate on. Episodes are keyed on the stable TMDB episode id, so a renumber
// updates rows in place rather than orphaning them. Best-effort: a season
// fetch or upsert failing logs and continues so one bad season doesn't block
// the rest of the tree.
func (s *EnrichmentService) SyncSeriesStructure(ctx context.Context, item model.MediaItem, details tmdb.TVDetails) {
	syncedIDs := make([]int64, 0, details.NumberOfEpisodes)

	// Deprecation keys off the synced set, so a partial sync would read a
	// failed season's still-live episodes as "removed". Only deprecate on a
	// full pass; this flips false on any season fetch/upsert failure.
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

	// Mark episodes TMDB no longer reports as deprecated. Guarded on a complete
	// sync (see above); empty set means everything failed.
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
