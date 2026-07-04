package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/metadata"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/qualityprofile"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/scope"
)

// RequestService owns request reads and the spawn orchestration that turns a
// request into a tracking. It is the orchestration heart of the
// request → tracking → want flow: the frozen-request-vs-live-requester split,
// one-tracking-per-media-item dedup, and tier → profile resolution all live
// here. A movie spawn produces its single want inline; a series spawn defers
// want production to the reconciler, which runs post-commit against the synced
// episode tree.
type RequestService struct {
	repo       *repo.Repository
	log        *logger.Logger
	tmdb       *TmdbService
	quality    *QualityProfileService
	enrichment *EnrichmentService
	reconcile  *ReconcileService
}

func NewRequestService(r *repo.Repository, l *logger.Logger, tmdb *TmdbService, quality *QualityProfileService, enrichment *EnrichmentService, reconcile *ReconcileService) *RequestService {
	return &RequestService{repo: r, log: l, tmdb: tmdb, quality: quality, enrichment: enrichment, reconcile: reconcile}
}

// CreateRequestInput is the writeable shape for a new request. Tier is the
// user-picked quality tier (resolved to a profile at spawn); Type is the media
// domain (movie-only in the PoC).
type CreateRequestInput struct {
	RequestedBy uuid.UUID
	TmdbID      int64
	Type        string
	Tier        string
}

// Create turns a request into a tracking. Validation and approval are shared
// across domains; the spawn itself forks by domain. Approval gates the spawn: an
// unapproved request is persisted as 'pending' with no tracking. An approved
// request runs the full spawn, deduping onto an existing tracking when the media
// is already tracked (a second requester joins).
//
// Movie spawn produces its single want inside the transaction. Series spawn
// persists only the tracking in the transaction and defers want production to a
// post-commit reconcile against the synced episode tree.
func (s *RequestService) Create(ctx context.Context, in CreateRequestInput) (model.Request, error) {
	const op = "RequestService.Create"

	var fields []apperrors.FieldError
	if in.Type != string(parsing.DomainMovie) && in.Type != string(parsing.DomainSeries) {
		fields = append(fields, apperrors.Field("body.type", "must be 'movie' or 'series'"))
	}
	tier, tierOK := parseTier(in.Tier)
	if !tierOK {
		fields = append(fields, apperrors.Field("body.tier", "must be a known tier ('HD' or '4K')"))
	}
	if in.TmdbID <= 0 {
		fields = append(fields, apperrors.Field("body.tmdbId", "must be a positive TMDB id"))
	}
	if len(fields) > 0 {
		return model.Request{}, apperrors.Validation("invalid request", fields...).Op(op)
	}

	// Read approval. A user with no policy row is not auto-approved (default-deny).
	userPolicy, err := s.repo.GetUserPolicy(ctx, in.RequestedBy)
	if apperrors.IsNotFound(err) {
		userPolicy = model.UserPolicy{}
	} else if err != nil {
		return model.Request{}, err
	}

	// Not approved: persist the request as pending, no spawn.
	if !evaluateApproval(userPolicy) {
		return s.repo.CreateRequest(ctx, repo.CreateRequestParams{
			RequestedBy: in.RequestedBy,
			TmdbID:      in.TmdbID,
			Type:        in.Type,
			Tier:        in.Tier,
			Status:      string(model.RequestPending),
		})
	}

	if in.Type == string(parsing.DomainSeries) {
		return s.spawnSeries(ctx, in, tier)
	}
	return s.spawnMovie(ctx, in, tier)
}

// spawnMovie spawns an approved movie request into a tracking + want. Reads and
// the TMDB fetch happen outside the transaction; every write is inside one InTx
// so the spawn is atomic. The flow stops at "want row exists in pending" — the
// row is the durable acquisition signal.
func (s *RequestService) spawnMovie(ctx context.Context, in CreateRequestInput, tier qualityprofile.Tier) (model.Request, error) {
	const op = "RequestService.spawnMovie"

	// Outside the tx: fetch movie details for the media_item title/year.
	details, err := s.tmdb.GetMovieDetails(ctx, in.TmdbID)
	if err != nil {
		return model.Request{}, apperrors.BadGatewayf("fetch tmdb movie %d: %v", in.TmdbID, err).Op(op)
	}
	title := details.Title
	year := parseYear(details.ReleaseDate)

	// Resolve the tier → profile binding. A NotFound (tier/domain unbound)
	// surfaces through unchanged.
	profile, err := s.quality.ResolveByTier(ctx, tier, parsing.DomainMovie)
	if err != nil {
		return model.Request{}, err
	}

	tmdbID := in.TmdbID
	var spawned model.Request
	err = s.repo.InTx(ctx, func(r *repo.Repository) error {
		req, err := r.CreateRequest(ctx, repo.CreateRequestParams{
			RequestedBy: in.RequestedBy,
			TmdbID:      in.TmdbID,
			Type:        in.Type,
			Tier:        in.Tier,
			Status:      string(model.RequestApproved),
		})
		if err != nil {
			return err
		}

		mediaItem, err := r.UpsertMediaItem(ctx, repo.UpsertMediaItemParams{
			Type:   string(parsing.DomainMovie),
			Title:  title,
			Year:   year,
			TmdbID: &tmdbID,
		})
		if err != nil {
			return err
		}

		// Persist the imdb id now (the details fetch already carries it) so the
		// acquisition gate can ID-match on the first search, rather than waiting
		// for the async enrichment worker to backfill it. Full enrichment
		// (poster, runtime, …) still lands later via that worker.
		if details.IMDbID != "" {
			if err := r.UpsertMediaItemExternalID(ctx, mediaItem.ID, string(metadata.SourceIMDB), details.IMDbID); err != nil {
				return err
			}
		}

		// Dedup: at most one tracking per media item. ensureTracking finds
		// the existing tracking (join path: a second requester for an
		// already-tracked movie) or creates a fresh 'active' one. The profile
		// is single-valued, set from this request's tier on creation; two
		// requesters at different tiers (HD vs 4K) on one movie is real
		// multi-tier reconciliation, deferred — the first spawn wins the profile.
		tracking, trackingCreated, err := ensureTracking(ctx, r, mediaItem.ID, profile.ID)
		if err != nil {
			return err
		}

		// The requester joins whether the tracking was created or already
		// existed; upsert on (tracking, user) refreshes the tier on re-request.
		if _, err := r.AddRequester(ctx, repo.AddRequesterParams{
			TrackingID: tracking.ID,
			UserID:     in.RequestedBy,
			Tier:       in.Tier,
			// Movie tracking is single-atom: scope is inert, so the requester
			// carries the 'all' default and never consults it.
			ScopeRule: string(scope.RuleAll),
		}); err != nil {
			return err
		}

		// One want per tracking. A freshly-created tracking gets a new pending
		// want. A request that joined an existing tracking adds a requester row
		// but no second want (the dedup invariant Story 1 requires) — instead it
		// re-arms the existing want if a prior attempt left it terminal, so a
		// re-request resumes acquisition rather than silently dead-ending.
		if trackingCreated {
			// Segment the movie against its own tracking's creation: a released
			// movie is backfill, an unreleased one ongoing (an unparseable date
			// falls to ongoing). Hold is nil in practice — a fresh tracking is
			// born auto/auto — but the helper keeps the born-held rule uniform.
			seg := model.SegmentFor(parseReleaseDate(details.ReleaseDate), tracking.CreatedAt)
			if _, err := r.CreateWant(ctx, repo.CreateWantParams{
				TrackingID:       tracking.ID,
				MediaItemID:      mediaItem.ID,
				QualityProfileID: profile.ID,
				Status:           string(model.WantPending),
				Segment:          string(seg),
				Hold:             model.HoldForNewWant(tracking, seg),
			}); err != nil {
				return err
			}
		} else if err := rearmMovieWant(ctx, r, tracking, mediaItem.ID, profile.ID, parseReleaseDate(details.ReleaseDate)); err != nil {
			return err
		}

		spawned, err = r.SetRequestSpawned(ctx, req.ID, tracking.ID)
		return err
	})
	if err != nil {
		return model.Request{}, err
	}
	return spawned, nil
}

// spawnSeries spawns an approved series request. The transaction persists only
// the frozen request, the media_item, the imdb cross-ref, the tracking, and the
// requester (scope 'all') — no want. Structure sync makes O(seasons) TMDB calls
// and must not run inside a DB transaction, so it and the want-producing
// reconcile happen post-commit. Both are best-effort: the reconciler is
// re-runnable and idempotent, so a post-commit failure self-heals on a
// re-request (or the enrichment trigger), and the tracking already exists, so the
// request still returns spawned.
func (s *RequestService) spawnSeries(ctx context.Context, in CreateRequestInput, tier qualityprofile.Tier) (model.Request, error) {
	const op = "RequestService.spawnSeries"

	// Outside the tx: fetch series details for the media_item title/year and the
	// season list the post-commit structure sync consumes.
	details, err := s.tmdb.GetSeriesDetailsForEnrichment(ctx, in.TmdbID)
	if err != nil {
		return model.Request{}, apperrors.BadGatewayf("fetch tmdb series %d: %v", in.TmdbID, err).Op(op)
	}
	title := details.Name
	year := parseYear(details.FirstAirDate)

	// Resolve the tier → series profile binding. A NotFound surfaces unchanged.
	profile, err := s.quality.ResolveByTier(ctx, tier, parsing.DomainSeries)
	if err != nil {
		return model.Request{}, err
	}

	tmdbID := in.TmdbID
	var spawned model.Request
	var mediaItem model.MediaItem
	var tracking model.Tracking
	err = s.repo.InTx(ctx, func(r *repo.Repository) error {
		req, err := r.CreateRequest(ctx, repo.CreateRequestParams{
			RequestedBy: in.RequestedBy,
			TmdbID:      in.TmdbID,
			Type:        in.Type,
			Tier:        in.Tier,
			Status:      string(model.RequestApproved),
		})
		if err != nil {
			return err
		}

		mediaItem, err = r.UpsertMediaItem(ctx, repo.UpsertMediaItemParams{
			Type:   string(parsing.DomainSeries),
			Title:  title,
			Year:   year,
			TmdbID: &tmdbID,
		})
		if err != nil {
			return err
		}

		// Persist the imdb id now so acquisition can ID-match on the first search;
		// the imdb id rides on the appended external_ids of the details fetch. The
		// guard mirrors enrichSeries: IMDbID is promoted from a *TVExternalIDs that
		// is nil unless the append actually came back.
		if details.TVExternalIDsAppend != nil && details.TVExternalIDs != nil && details.IMDbID != "" {
			if err := r.UpsertMediaItemExternalID(ctx, mediaItem.ID, string(metadata.SourceIMDB), details.IMDbID); err != nil {
				return err
			}
		}

		tracking, _, err = ensureTracking(ctx, r, mediaItem.ID, profile.ID)
		if err != nil {
			return err
		}

		// Seed the requester at scope 'all' — the only preset until a
		// scope-selection API lands. The reconciler reads this to decide which
		// episodes to want.
		if _, err := r.AddRequester(ctx, repo.AddRequesterParams{
			TrackingID: tracking.ID,
			UserID:     in.RequestedBy,
			Tier:       in.Tier,
			ScopeRule:  string(scope.RuleAll),
		}); err != nil {
			return err
		}

		spawned, err = r.SetRequestSpawned(ctx, req.ID, tracking.ID)
		return err
	})
	if err != nil {
		return model.Request{}, err
	}

	// Post-commit, best-effort: sync the episode tree, then reconcile it into
	// per-episode wants. A failure here leaves the tracking in place; the next
	// reconcile (re-request or enrichment trigger) heals it.
	s.enrichment.SyncSeriesStructure(ctx, mediaItem, details)
	if err := s.reconcile.Reconcile(ctx, tracking.ID); err != nil {
		s.log.Warn().Err(err).
			Str("title", title).Str("tracking", tracking.ID.String()).
			Msg("series spawn: post-commit reconcile failed, will heal on next reconcile")
	}

	return spawned, nil
}

// ensureTracking gets-or-creates the tracking for a media item, returning
// (tracking, created, err). The dedup boundary is UNIQUE(media_item_id), and the
// get-or-create is race-safe: CreateTrackingIfAbsent inserts on the winning
// spawn (created=true) and no-ops on a concurrent or prior one, where the loser
// re-reads the existing row (created=false) instead of rolling back with a 409.
// A created tracking is born 'active'/'self'/'none'/'smart' with the given
// profile. The helper is neutral — it never reactivates a terminal tracking;
// re-arming a terminal want on the join path is the caller's concern
// (rearmMovieWant here, GrabManualWant on the manual-grab path).
func ensureTracking(ctx context.Context, r *repo.Repository, mediaItemID, profileID uuid.UUID) (model.Tracking, bool, error) {
	tracking, created, err := r.CreateTrackingIfAbsent(ctx, repo.CreateTrackingParams{
		MediaItemID:      mediaItemID,
		QualityProfileID: profileID,
		State:            string(model.TrackingActive),
		Scope:            "self",
		UpgradeBehavior:  "none",
		ScheduleStrategy: "smart",
		// A new tracking is born fully autonomous on both segments. Settings-backed
		// defaults (per-user auto/propose/manual) are deferred; the user dials
		// autonomy per tracking via SetAutonomy after the fact.
		AutonomyBackfill: string(model.AutonomyAuto),
		AutonomyOngoing:  string(model.AutonomyAuto),
	})
	if err != nil {
		return model.Tracking{}, false, err
	}
	if created {
		return tracking, true, nil
	}
	tracking, err = r.FindTrackingByMediaItem(ctx, mediaItemID)
	if err != nil {
		return model.Tracking{}, false, err
	}
	return tracking, false, nil
}

// rearmMovieWant resumes acquisition when an already-tracked movie is requested
// again. The single-atom invariant caps a movie tracking at one want, so a
// terminal want ('failed'/'canceled') is re-armed in place rather than
// duplicated; an in-flight or 'available' want is left untouched (RearmWant's
// CAS no-ops). A tracking that somehow has no want gets a fresh pending one. A
// re-armed want that was 'canceled' also flips its tracking — which CancelWant
// parked in 'canceled' — back to 'active'.
//
// RearmWant clears any hold as part of the reset, so if the want's segment is on
// a manual dial the hold is re-stamped here — a re-request must land back under
// the manual gate, not slip through auto.
func rearmMovieWant(ctx context.Context, r *repo.Repository, tracking model.Tracking, mediaItemID, profileID uuid.UUID, releaseDate *time.Time) error {
	wants, err := r.ListWantsByTracking(ctx, tracking.ID)
	if err != nil {
		return err
	}
	if len(wants) == 0 {
		seg := model.SegmentFor(releaseDate, tracking.CreatedAt)
		_, err := r.CreateWant(ctx, repo.CreateWantParams{
			TrackingID:       tracking.ID,
			MediaItemID:      mediaItemID,
			QualityProfileID: profileID,
			Status:           string(model.WantPending),
			Segment:          string(seg),
			Hold:             model.HoldForNewWant(tracking, seg),
		})
		return err
	}

	want := wants[0]
	_, ok, err := r.RearmWant(ctx, want.ID)
	if err != nil {
		return err
	}
	if !ok {
		// In-flight or already 'available' — acquisition is already handled.
		return nil
	}
	// The re-armed want was terminal; restore a tracking CancelWant left
	// 'canceled'. A no-op for a still-active tracking (the failed-want case).
	if _, err := r.SetTrackingState(ctx, tracking.ID, string(model.TrackingActive)); err != nil {
		return err
	}
	// Re-stamp the hold the rearm cleared when the want's segment is manual.
	if hold := model.HoldForNewWant(tracking, model.WantSegment(want.Segment)); hold != nil {
		if _, err := r.SetWantHold(ctx, want.ID, hold); err != nil {
			return err
		}
	}
	return nil
}

func (s *RequestService) Get(ctx context.Context, id uuid.UUID) (model.Request, error) {
	return s.repo.GetRequest(ctx, id)
}

func (s *RequestService) List(ctx context.Context) ([]model.Request, error) {
	return s.repo.ListRequests(ctx)
}

// evaluateApproval is the approval seam. For the PoC it is a one-line read of the
// user's auto-approve-movie flag; the can_request_movie permission gate and the
// full per-tier/per-type policy matrix fatten this function later.
func evaluateApproval(p model.UserPolicy) bool {
	return p.AutoApproveMovie
}

// parseReleaseDate parses a TMDB "2006-01-02" date into a *time.Time, returning
// nil for an empty or malformed value — which SegmentFor reads as "undated →
// ongoing." Feeds the movie want's segment classification.
func parseReleaseDate(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &t
}

// parseTier maps a tier string to a known qualityprofile.Tier. The PoC accepts
// the seeded HD/4K tiers.
func parseTier(s string) (qualityprofile.Tier, bool) {
	switch qualityprofile.Tier(s) {
	case qualityprofile.TierHD, qualityprofile.Tier4K:
		return qualityprofile.Tier(s), true
	default:
		return "", false
	}
}
