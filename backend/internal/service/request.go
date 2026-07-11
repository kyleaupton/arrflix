package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/authz"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
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
	authz      *AuthzService
	wants      *WantService
}

func NewRequestService(r *repo.Repository, l *logger.Logger, tmdb *TmdbService, quality *QualityProfileService, enrichment *EnrichmentService, reconcile *ReconcileService, authz *AuthzService, wants *WantService) *RequestService {
	return &RequestService{repo: r, log: l, tmdb: tmdb, quality: quality, enrichment: enrichment, reconcile: reconcile, authz: authz, wants: wants}
}

// CreateRequestInput is the writeable shape for a new request. Type is the
// media domain. Every other field is optional intent with a default: Tier is
// the quality tier resolved to a profile at spawn (empty → 'HD'); ScopeRule is
// the requester's series-scope choice, persisted on the request row so it
// survives the pending state (empty → 'all'; inert for movies); the autonomy
// pair is the operator's tracking config, applied only on the auto-approve
// spawn of a fresh tracking (empty → 'auto').
type CreateRequestInput struct {
	RequestedBy      uuid.UUID
	TmdbID           int64
	Type             string
	Tier             string
	ScopeRule        string
	BackfillAutonomy string
	OngoingAutonomy  string
}

// Create turns a request into a tracking. Validation and approval are shared
// across domains; the spawn itself forks by domain. Approval gates the spawn: a
// request the caller can't auto-approve is persisted as 'pending' with no
// tracking, for an operator to approve or deny later. An auto-approved request
// runs the full spawn, deduping onto an existing tracking when the media is
// already tracked (a second requester joins).
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
	tier, tierOK := normalizeTier(in.Tier)
	if !tierOK {
		fields = append(fields, apperrors.Field("body.tier", "must be a known tier ('HD' or '4K')"))
	}
	if in.TmdbID <= 0 {
		fields = append(fields, apperrors.Field("body.tmdbId", "must be a positive TMDB id"))
	}
	scopeRule, scopeOK := normalizeSeriesScope(in.ScopeRule)
	if !scopeOK {
		fields = append(fields, apperrors.Field("body.scopeRule", "must be 'all' or 'future_only'"))
	}
	backfill, backfillOK := normalizeAutonomy(in.BackfillAutonomy)
	if !backfillOK {
		fields = append(fields, apperrors.Field("body.backfillAutonomy", "must be 'auto', 'propose', or 'manual'"))
	}
	ongoing, ongoingOK := normalizeAutonomy(in.OngoingAutonomy)
	if !ongoingOK {
		fields = append(fields, apperrors.Field("body.ongoingAutonomy", "must be 'auto', 'propose', or 'manual'"))
	}
	if len(fields) > 0 {
		return model.Request{}, apperrors.Validation("invalid request", fields...).Op(op)
	}
	// Replace the raw inputs with their normalized (defaulted) forms; the spawn
	// path reads these, not the wire strings.
	in.Tier = string(tier)
	in.ScopeRule, in.BackfillAutonomy, in.OngoingAutonomy = scopeRule, backfill, ongoing

	// Exact per-type/per-tier gate. The middleware floor only cleared users who
	// hold SOME create key; this is the precise requests.create:<type>:<tier>
	// check against the parsed body the gate can't see (it runs pre-parse). The
	// builder lowercases the tier to match the catalog keyspace.
	if err := s.authz.Require(ctx, in.RequestedBy, authz.RequestCreate(model.MediaType(in.Type), string(tier)), nil); err != nil {
		return model.Request{}, err
	}

	// Auto-approval is the per-tier permission: a caller holding
	// requests.auto_approve:<type>:<tier> spawns immediately; everyone else
	// (default-deny) lands in the pending queue for an operator to decide.
	autoApprove, err := s.authz.Can(ctx, in.RequestedBy,
		authz.RequestAutoApprove(model.MediaType(in.Type), string(tier)), nil)
	if err != nil {
		return model.Request{}, err
	}

	// Not auto-approved: persist the request as pending, no spawn. Scope rides
	// the row so an approval-time spawn honors the requester's choice; autonomy
	// deliberately does not — it's the approving operator's call, not the
	// requester's.
	if !autoApprove {
		return s.repo.CreateRequest(ctx, repo.CreateRequestParams{
			RequestedBy: in.RequestedBy,
			TmdbID:      in.TmdbID,
			Type:        in.Type,
			Tier:        in.Tier,
			Status:      string(model.RequestPending),
			ScopeRule:   in.ScopeRule,
		})
	}

	// Auto path: create the request already approved, with self-decided
	// provenance (decided_by = the requester, decision_auto = true), then spawn.
	// SetRequestSpawned leaves the provenance intact, so the spawned row still
	// reports it auto-approved.
	now := time.Now()
	decisionAuto := true
	req, err := s.repo.CreateRequest(ctx, repo.CreateRequestParams{
		RequestedBy:  in.RequestedBy,
		TmdbID:       in.TmdbID,
		Type:         in.Type,
		Tier:         in.Tier,
		Status:       string(model.RequestApproved),
		ScopeRule:    in.ScopeRule,
		DecidedBy:    &in.RequestedBy,
		DecidedAt:    &now,
		DecisionAuto: &decisionAuto,
	})
	if err != nil {
		return model.Request{}, err
	}
	return s.spawnFor(ctx, req, tier, in.BackfillAutonomy, in.OngoingAutonomy)
}

// spawnFor runs the full spawn for an already-approved request row, forking by
// media type. It is the shared tail of the auto-approve (Create) and manual
// approve paths: the row already exists and is approved, so spawnFor only turns
// it into a tracking (media_item → ensureTracking → AddRequester → want →
// SetRequestSpawned). The autonomy pair is the operator's tracking config,
// applied only when this spawn creates a fresh tracking (a join re-reads the
// existing one and leaves its autonomy untouched).
//
// The request-status transition (create-as-approved, or SetRequestApproved) is
// the caller's concern and commits before spawnFor's own transaction — a spawn
// that fails leaves the request in the recoverable 'approved' state rather than
// rolling back the decision.
func (s *RequestService) spawnFor(ctx context.Context, req model.Request, tier qualityprofile.Tier, backfillAutonomy, ongoingAutonomy string) (model.Request, error) {
	if req.Type == string(parsing.DomainSeries) {
		return s.spawnSeries(ctx, req, tier, backfillAutonomy, ongoingAutonomy)
	}
	return s.spawnMovie(ctx, req, tier, backfillAutonomy, ongoingAutonomy)
}

// spawnMovie spawns an approved movie request into a tracking + want. Reads and
// the TMDB fetch happen outside the transaction; every write is inside one InTx
// so the spawn is atomic. The flow stops at "want row exists in pending" — the
// row is the durable acquisition signal. The request row already exists and is
// approved; spawnMovie transitions it to 'spawned'.
func (s *RequestService) spawnMovie(ctx context.Context, req model.Request, tier qualityprofile.Tier, backfillAutonomy, ongoingAutonomy string) (model.Request, error) {
	const op = "RequestService.spawnMovie"

	// Outside the tx: fetch the full enrichment payload (release_dates appended
	// for certification) so the spawn can apply metadata born-complete with no
	// extra TMDB call — the same fetch the enrichment worker uses.
	details, err := s.tmdb.GetMovieDetailsForEnrichment(ctx, req.TmdbID)
	if err != nil {
		return model.Request{}, apperrors.BadGatewayf("fetch tmdb movie %d: %v", req.TmdbID, err).Op(op)
	}
	title := details.Title
	year := parseYear(details.ReleaseDate)

	// Resolve the tier → profile binding. A NotFound (tier/domain unbound)
	// surfaces through unchanged.
	profile, err := s.quality.ResolveByTier(ctx, tier, parsing.DomainMovie)
	if err != nil {
		return model.Request{}, err
	}

	tmdbID := req.TmdbID
	var spawned model.Request
	var mediaItem model.MediaItem
	err = s.repo.InTx(ctx, func(r *repo.Repository) error {
		mediaItem, err = r.UpsertMediaItem(ctx, repo.UpsertMediaItemParams{
			Type:   string(parsing.DomainMovie),
			Title:  title,
			Year:   year,
			TmdbID: &tmdbID,
		})
		if err != nil {
			return err
		}

		// Born-complete: apply the full TMDB payload (poster, overview, runtime,
		// genres, …) plus the imdb cross-ref onto the just-created media_item,
		// inside this tx and with no extra TMDB call — the fetch above already
		// carries it. The item is never visible metadata-less, and the advanced
		// metadata_updated_at keeps the enrichment worker from re-fetching it.
		if err := s.enrichment.applyMovieMetadata(ctx, r, mediaItem, details); err != nil {
			return err
		}

		// Dedup: at most one tracking per media item. ensureTracking finds
		// the existing tracking (join path: a second requester for an
		// already-tracked movie) or creates a fresh 'active' one. The profile
		// is single-valued, set from this request's tier on creation; two
		// requesters at different tiers (HD vs 4K) on one movie is real
		// multi-tier reconciliation, deferred — the first spawn wins the profile.
		tracking, trackingCreated, err := ensureTracking(ctx, r, mediaItem.ID, profile.ID, backfillAutonomy, ongoingAutonomy)
		if err != nil {
			return err
		}

		// The requester joins whether the tracking was created or already
		// existed; upsert on (tracking, user) refreshes the tier on re-request.
		if _, err := r.AddRequester(ctx, repo.AddRequesterParams{
			TrackingID: tracking.ID,
			UserID:     req.RequestedBy,
			Tier:       req.Tier,
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

	// Post-commit, best-effort: stash the raw TMDB payload (the ~100KB
	// portability blob) off the atomic spawn path. A failure just defers the raw
	// row to the next enrichment sweep; the user-visible metadata is committed.
	s.enrichment.storeRawPayload(ctx, mediaItem.ID, "tmdb", details)

	return spawned, nil
}

// spawnSeries spawns an approved series request. The transaction persists only
// the media_item, the imdb cross-ref, the tracking, and the requester (scope
// 'all') — no want — and transitions the already-approved request to 'spawned'.
// Structure sync makes O(seasons) TMDB calls and must not run inside a DB
// transaction, so it and the want-producing reconcile happen post-commit. Both
// are best-effort: the reconciler is re-runnable and idempotent, so a post-commit
// failure self-heals on a re-request (or the enrichment trigger), and the
// tracking already exists, so the request still returns spawned.
func (s *RequestService) spawnSeries(ctx context.Context, req model.Request, tier qualityprofile.Tier, backfillAutonomy, ongoingAutonomy string) (model.Request, error) {
	const op = "RequestService.spawnSeries"

	// Outside the tx: fetch series details for the media_item title/year and the
	// season list the post-commit structure sync consumes.
	details, err := s.tmdb.GetSeriesDetailsForEnrichment(ctx, req.TmdbID)
	if err != nil {
		return model.Request{}, apperrors.BadGatewayf("fetch tmdb series %d: %v", req.TmdbID, err).Op(op)
	}
	title := details.Name
	year := parseYear(details.FirstAirDate)

	// Resolve the tier → series profile binding. A NotFound surfaces unchanged.
	profile, err := s.quality.ResolveByTier(ctx, tier, parsing.DomainSeries)
	if err != nil {
		return model.Request{}, err
	}

	tmdbID := req.TmdbID
	var spawned model.Request
	var mediaItem model.MediaItem
	var tracking model.Tracking
	err = s.repo.InTx(ctx, func(r *repo.Repository) error {
		mediaItem, err = r.UpsertMediaItem(ctx, repo.UpsertMediaItemParams{
			Type:   string(parsing.DomainSeries),
			Title:  title,
			Year:   year,
			TmdbID: &tmdbID,
		})
		if err != nil {
			return err
		}

		// Born-complete: apply the full TMDB payload plus the imdb/tvdb cross-refs
		// onto the just-created media_item, inside this tx with no extra TMDB call.
		// Structure sync + reconcile stay post-commit (below) — folding them here
		// would double-sync the episode tree.
		if err := s.enrichment.applySeriesMetadata(ctx, r, mediaItem, details); err != nil {
			return err
		}

		tracking, _, err = ensureTracking(ctx, r, mediaItem.ID, profile.ID, backfillAutonomy, ongoingAutonomy)
		if err != nil {
			return err
		}

		// Seed the requester at the chosen scope ('all' or 'future_only',
		// defaulted to 'all'). The reconciler reads this to decide which episodes
		// to want; 'future_only' skips the aired back-catalog entirely.
		if _, err := r.AddRequester(ctx, repo.AddRequesterParams{
			TrackingID: tracking.ID,
			UserID:     req.RequestedBy,
			Tier:       req.Tier,
			ScopeRule:  req.ScopeRule,
		}); err != nil {
			return err
		}

		spawned, err = r.SetRequestSpawned(ctx, req.ID, tracking.ID)
		return err
	})
	if err != nil {
		return model.Request{}, err
	}

	// Post-commit, best-effort: stash the raw TMDB payload (the ~100KB
	// portability blob) off the atomic path, then sync the episode tree and
	// reconcile it into per-episode wants. A failure here leaves the tracking in
	// place; the next reconcile (re-request or enrichment trigger) heals it.
	s.enrichment.storeRawPayload(ctx, mediaItem.ID, "tmdb", details)
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
func ensureTracking(ctx context.Context, r *repo.Repository, mediaItemID, profileID uuid.UUID, autonomyBackfill, autonomyOngoing string) (model.Tracking, bool, error) {
	tracking, created, err := r.CreateTrackingIfAbsent(ctx, repo.CreateTrackingParams{
		MediaItemID:      mediaItemID,
		QualityProfileID: profileID,
		State:            string(model.TrackingActive),
		Scope:            "self",
		UpgradeBehavior:  "none",
		ScheduleStrategy: "smart",
		// Autonomy is set at add time from the operator's choice (defaulted to
		// 'auto'/'auto' when unspecified). These params apply only on insert; a
		// join onto an existing tracking re-reads it below and leaves its autonomy
		// untouched — autonomy is tracking-level and the first spawn wins.
		AutonomyBackfill: autonomyBackfill,
		AutonomyOngoing:  autonomyOngoing,
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

// Get loads a request, gated own/any: a holder of requests.view.any reads any
// request; otherwise the caller must be the requester. The middleware floor
// already required one of the view keys; this refines it against the loaded
// row's owner.
func (s *RequestService) Get(ctx context.Context, userID, id uuid.UUID) (model.Request, error) {
	req, err := s.repo.GetRequest(ctx, id)
	if err != nil {
		return model.Request{}, err
	}
	ok, err := s.authz.CanOwnAny(ctx, userID, "requests.view", req.RequestedBy == userID)
	if err != nil {
		return model.Request{}, err
	}
	if !ok {
		return model.Request{}, apperrors.Forbiddenf("insufficient permissions").Op("RequestService.Get")
	}
	return req, nil
}

// List returns requests scoped to the caller, optionally narrowed to a single
// status. Ownership is applied first: a holder of requests.view.any sees all;
// otherwise (the view.own floor the gate required) the list is filtered to the
// caller's own requests. The approval queue is GET ?status=pending with
// view.any. Status filtering is in-Go on the low-volume request set; a
// ListRequestsByStatus query is the scale-up path if it ever matters.
func (s *RequestService) List(ctx context.Context, userID uuid.UUID, status *string) ([]model.Request, error) {
	all, err := s.repo.ListRequests(ctx)
	if err != nil {
		return nil, err
	}
	canAny, err := s.authz.Can(ctx, userID, authz.RequestViewAny, nil)
	if err != nil {
		return nil, err
	}
	out := make([]model.Request, 0, len(all))
	for _, r := range all {
		if !canAny && r.RequestedBy != userID {
			continue
		}
		if status != nil && r.Status != *status {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// Approve turns a pending request into a tracking on an operator's decision.
// It refines the coarse approve gate against the request's exact media type,
// stamps the manual decision provenance (decided_by = the approver), then runs
// the shared spawn. A request that isn't pending is a Conflict — only the
// queued state is approvable. Autonomy defaults to auto/auto: the approving
// operator didn't dial it, and the requester's choice deliberately doesn't ride
// the row.
func (s *RequestService) Approve(ctx context.Context, approverID, requestID uuid.UUID) (model.Request, error) {
	const op = "RequestService.Approve"

	req, err := s.repo.GetRequest(ctx, requestID)
	if err != nil {
		return model.Request{}, err
	}
	if req.Status != string(model.RequestPending) {
		return model.Request{}, apperrors.Conflictf("request %s is not pending (status %q)", requestID, req.Status).Op(op)
	}
	if err := s.authz.Require(ctx, approverID, authz.RequestApprove(model.MediaType(req.Type)), nil); err != nil {
		return model.Request{}, err
	}
	tier, ok := normalizeTier(req.Tier)
	if !ok {
		return model.Request{}, apperrors.Internalf("request %s has unknown tier %q", requestID, req.Tier).Op(op)
	}

	approved, err := s.repo.SetRequestApproved(ctx, requestID, approverID)
	if err != nil {
		return model.Request{}, err
	}
	return s.spawnFor(ctx, approved, tier, string(model.AutonomyAuto), string(model.AutonomyAuto))
}

// Deny rejects a pending request with an optional reason, refining the coarse
// deny gate against the request's exact media type and stamping the decision
// provenance (decided_by = the denier). A request that isn't pending is a
// Conflict.
func (s *RequestService) Deny(ctx context.Context, approverID, requestID uuid.UUID, reason *string) (model.Request, error) {
	const op = "RequestService.Deny"

	req, err := s.repo.GetRequest(ctx, requestID)
	if err != nil {
		return model.Request{}, err
	}
	if req.Status != string(model.RequestPending) {
		return model.Request{}, apperrors.Conflictf("request %s is not pending (status %q)", requestID, req.Status).Op(op)
	}
	if err := s.authz.Require(ctx, approverID, authz.RequestDeny(model.MediaType(req.Type)), nil); err != nil {
		return model.Request{}, err
	}
	return s.repo.SetRequestDenied(ctx, requestID, approverID, reason)
}

// Cancel withdraws a request, gated own/any: a holder of requests.cancel.any
// cancels anyone's, requests.cancel.own only the caller's. A pending or
// (transiently) approved request just flips to canceled — nothing spawned. A
// spawned request cascades into the tracking (below). Terminal states are
// idempotent (already canceled) or a Conflict (already denied).
func (s *RequestService) Cancel(ctx context.Context, userID, requestID uuid.UUID) (model.Request, error) {
	const op = "RequestService.Cancel"

	req, err := s.repo.GetRequest(ctx, requestID)
	if err != nil {
		return model.Request{}, err
	}

	// own/any gate: ownership is being the requester. This closes the P2 gap —
	// a requester holds requests.cancel.own and can withdraw their own request.
	ok, err := s.authz.CanOwnAny(ctx, userID, "requests.cancel", req.RequestedBy == userID)
	if err != nil {
		return model.Request{}, err
	}
	if !ok {
		return model.Request{}, apperrors.Forbiddenf("insufficient permissions").Op(op)
	}

	switch model.RequestStatus(req.Status) {
	case model.RequestCanceled:
		return req, nil // idempotent
	case model.RequestDenied:
		return model.Request{}, apperrors.Conflictf("request %s is already denied", requestID).Op(op)
	case model.RequestSpawned:
		return s.cancelSpawned(ctx, userID, req)
	default:
		// pending (nothing spawned) or the transient approved-without-tracking
		// state: just mark canceled.
		return s.repo.SetRequestCanceled(ctx, requestID, userID)
	}
}

// cancelSpawned withdraws a spawned request from its tracking, mirroring
// TrackingService.Cancel's non-transactional own/any + want-cancel cascade. The
// requester drops out of the tracking's live set; only when they were the last
// requester (tracking is shared under the dedup invariant) are the tracking's
// non-terminal wants canceled and the tracking parked 'paused'. A non-last
// withdrawal must NOT touch wants — the remaining requesters still want it.
func (s *RequestService) cancelSpawned(ctx context.Context, userID uuid.UUID, req model.Request) (model.Request, error) {
	canceled, err := s.repo.SetRequestCanceled(ctx, req.ID, userID)
	if err != nil {
		return model.Request{}, err
	}

	if req.SpawnedTrackingID == nil {
		return canceled, nil // spawned status but no tracking recorded — nothing to cascade
	}
	tid := *req.SpawnedTrackingID

	if err := s.repo.RemoveRequester(ctx, tid, req.RequestedBy); err != nil {
		return model.Request{}, err
	}
	remaining, err := s.repo.ListRequestersByTracking(ctx, tid)
	if err != nil {
		return model.Request{}, err
	}
	if len(remaining) > 0 {
		// Shared (deduped) tracking: other requesters still want this, so the
		// wants stay live and the tracking stays active.
		return canceled, nil
	}

	// Last requester withdrew: cancel every non-terminal want (and its in-flight
	// download job) via the tested WantService cascade, then park the tracking.
	// Ordering caveat: CancelWant mirrors the tracking to 'canceled', so 'paused'
	// is set AFTER cancelling wants, or it gets clobbered.
	wants, err := s.repo.ListWantsByTracking(ctx, tid)
	if err != nil {
		return model.Request{}, err
	}
	for _, w := range wants {
		if model.WantStatus(w.Status).IsTerminal() {
			continue
		}
		if _, err := s.wants.CancelWant(ctx, w.ID); err != nil {
			return model.Request{}, err
		}
	}
	if _, err := s.repo.SetTrackingState(ctx, tid, string(model.TrackingPaused)); err != nil {
		return model.Request{}, err
	}
	return canceled, nil
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

// normalizeAutonomy validates an acquisition-autonomy string, mapping the empty
// string (field omitted) to the born default 'auto'. Returns (normalized, ok);
// ok is false for an unrecognized value.
func normalizeAutonomy(s string) (string, bool) {
	switch s {
	case "":
		return string(model.AutonomyAuto), true
	case string(model.AutonomyAuto), string(model.AutonomyPropose), string(model.AutonomyManual):
		return s, true
	default:
		return "", false
	}
}

// normalizeSeriesScope validates a series scope preset, mapping the empty string
// (field omitted) to the default 'all'. Only the two presets the add-a-series
// modal offers are accepted; the richer presets need a param the API doesn't yet
// carry. Returns (normalized, ok).
func normalizeSeriesScope(s string) (string, bool) {
	switch s {
	case "":
		return string(scope.RuleAll), true
	case string(scope.RuleAll), string(scope.RuleFutureOnly):
		return s, true
	default:
		return "", false
	}
}

// normalizeTier maps a tier string to a known qualityprofile.Tier, with the
// empty string (field omitted — requesters don't pick quality) defaulting to
// HD. The PoC accepts the seeded HD/4K tiers. Returns (normalized, ok).
func normalizeTier(s string) (qualityprofile.Tier, bool) {
	switch qualityprofile.Tier(s) {
	case "":
		return qualityprofile.TierHD, true
	case qualityprofile.TierHD, qualityprofile.Tier4K:
		return qualityprofile.Tier(s), true
	default:
		return "", false
	}
}
