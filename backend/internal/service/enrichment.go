package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/cadence"
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

	// Schedule the next refresh from the movie's state (release age drives the
	// cadence). Stamped here alongside metadata_updated_at so born-at-spawn and
	// worker-refresh schedule uniformly through the one write.
	refreshAt := cadence.MetadataRefreshAt(cadence.MetadataRefreshInput{
		Type:         item.Type,
		Status:       deref(params.Status),
		ReleaseDate:  releaseDate,
		InProduction: inProd,
	}, time.Now())
	params.NextRefreshAt = &refreshAt

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

	// Schedule the next refresh from the series' state. The cadence reads the
	// air-activity pointers straight off the details payload (no tree query) —
	// an airing show lands on the daily tier, an ended one on monthly. Stamped
	// here so born-at-spawn and worker-refresh schedule uniformly.
	refreshAt := cadence.MetadataRefreshAt(cadence.MetadataRefreshInput{
		Type:           item.Type,
		Status:         deref(params.Status),
		ReleaseDate:    firstAirDate,
		LastAirDate:    lastAirDate,
		NextEpisodeAir: parseDateToTimePtr(details.NextEpisodeToAir.AirDate),
		LastEpisodeAir: parseDateToTimePtr(details.LastEpisodeToAir.AirDate),
		InProduction:   details.InProduction,
	}, time.Now())
	params.NextRefreshAt = &refreshAt

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

// SyncSeriesStructure upserts the season/episode tree from TMDB, including
// unaired episodes (air_date in the future or NULL). It is the structural half
// of series sync — the data tracking and "coming soon" UI operate on. Episodes
// are keyed on the stable TMDB episode id, so a renumber updates rows in place
// rather than orphaning them. Best-effort: a season fetch or upsert failing
// logs and continues so one bad season doesn't block the rest of the tree.
//
// It fetches only the seasons that can still change — the mutable set (current
// / next-airing / just-added, see mutableSeasons) plus any season we've never
// synced. An immutable, already-synced season (a frozen back-catalog) is
// skipped: re-pulling its episode rows is pure TMDB waste, and this bounds both
// routine call volume and born-at-spawn latency for long-running airing series.
// First sync fetches everything (no season exists yet), which is what enables
// the deprecation pass below.
func (s *EnrichmentService) SyncSeriesStructure(ctx context.Context, item model.MediaItem, details tmdb.TVDetails) {
	// Existing season numbers — a cheap indexed read. A season absent here has
	// never been synced and is always fetched; a listing error degrades to
	// "treat all as unseen" (over-fetch, never under-fetch).
	existingSeasons := make(map[int32]bool)
	if seasons, err := s.repo.ListSeasonsForMedia(ctx, item.ID); err != nil {
		s.logger.Warn().Err(err).Str("title", item.Title).
			Msg("series structure sync: list existing seasons failed, fetching all")
	} else {
		for _, se := range seasons {
			existingSeasons[se.SeasonNumber] = true
		}
	}
	toFetch := seasonsToFetch(details, existingSeasons)

	syncedIDs := make([]int64, 0, details.NumberOfEpisodes)

	// Deprecation keys off the synced set, so a partial pass would read a
	// skipped (or failed) season's still-live episodes as "removed". Only a full
	// pass — every season fetched (first sync), no failures — may deprecate; a
	// skip (fetch set smaller than the season list) or any error flips this false.
	complete := len(toFetch) == len(details.Seasons)

	for _, season := range details.Seasons {
		// A frozen, already-synced season is skipped (see seasonsToFetch); the
		// deprecation guard above already accounts for it.
		if !toFetch[season.SeasonNumber] {
			continue
		}

		// forceRefresh: this is a canonical-materializing read, so it bypasses
		// the response cache and consults TMDB directly — the freshness invariant.
		seasonDetails, err := s.tmdb.GetTVSeasonDetails(ctx, *item.TmdbID, season.SeasonNumber, true)
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

// seasonsToFetch decides which of a series' seasons a structure sync pulls from
// TMDB: a season is fetched iff it can still change (in the mutable set) or has
// never been synced (absent from existing). Pure — this is the per-season
// fetch/skip decision SyncSeriesStructure applies, lifted out for testing. On
// first sync `existing` is empty, so every season is fetched; on a routine
// refresh of an established series only the mutable set is fetched; a
// newly-added season (absent from existing) is always included.
func seasonsToFetch(details tmdb.TVDetails, existing map[int32]bool) map[int]bool {
	mutable := mutableSeasons(details)
	out := make(map[int]bool, len(details.Seasons))
	for _, se := range details.Seasons {
		if mutable[se.SeasonNumber] || !existing[int32(se.SeasonNumber)] {
			out[se.SeasonNumber] = true
		}
	}
	return out
}

// mutableSeasons returns the season numbers a routine refresh must re-fetch
// because their contents can still change, derived purely from the series
// payload (no per-season fetch): the season carrying the next episode to air,
// the season carrying the last episode aired, and the highest-numbered season.
// The max catches a just-added, still-undated season that has no next/last
// pointer yet. Every other season is treated as frozen. The next/last pointers
// are guarded on a non-zero id so an absent pointer (zero struct) doesn't mark
// season 0 mutable.
func mutableSeasons(details tmdb.TVDetails) map[int]bool {
	m := make(map[int]bool, 3)
	if details.NextEpisodeToAir.ID != 0 {
		m[details.NextEpisodeToAir.SeasonNumber] = true
	}
	if details.LastEpisodeToAir.ID != 0 {
		m[details.LastEpisodeToAir.SeasonNumber] = true
	}
	maxSeason, have := 0, false
	for _, se := range details.Seasons {
		if !have || se.SeasonNumber > maxSeason {
			maxSeason, have = se.SeasonNumber, true
		}
	}
	if have {
		m[maxSeason] = true
	}
	return m
}

// EnrichBatch drains the due queue (items whose next_refresh_at has passed as of
// now) and enriches each, returning the count successfully processed. A failed
// enrich records back-off state (RecordMetadataFailure) so the item drops out of
// the due set for exponentially longer rather than being retried every tick; the
// success path reschedules via next_refresh_at inside applyMovieMetadata /
// applySeriesMetadata.
func (s *EnrichmentService) EnrichBatch(ctx context.Context, now time.Time, batchSize int32) (int, error) {
	items, err := s.repo.ListDueMediaItems(ctx, now, batchSize)
	if err != nil {
		return 0, err
	}

	enriched := 0
	for _, item := range items {
		if ctx.Err() != nil {
			return enriched, ctx.Err()
		}

		if err := s.EnrichMediaItem(ctx, item); err != nil {
			// Back off: push next_refresh_at out by base·2^(attempt-1) so a
			// persistently-failing item (rate limit, gone-upstream) doesn't burn
			// a fetch every tick. Born-at-spawn failures roll back their tx, so
			// only the worker path records back-off.
			backoffAt := cadence.MetadataBackoffAt(int(item.MetadataAttemptCount)+1, now)
			if rerr := s.repo.RecordMetadataFailure(ctx, item.ID, backoffAt, err.Error()); rerr != nil {
				s.logger.Warn().Err(rerr).
					Str("media_item_id", item.ID.String()).
					Msg("enrichment: record failure back-off failed")
			}
			s.logger.Warn().Err(err).
				Str("media_item_id", item.ID.String()).
				Str("title", item.Title).
				Msg("enrichment failed, backing off")
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
