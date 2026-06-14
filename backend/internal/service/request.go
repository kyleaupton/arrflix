package service

import (
	"context"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/metadata"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/qualityprofile"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/scope"
)

// RequestService owns request reads and the spawn orchestration that turns a
// movie request into a tracking + want. It is the orchestration heart of the
// request → tracking → want flow: the frozen-request-vs-live-requester split,
// one-tracking-per-media-item dedup, tier → profile resolution, and producing
// the persisted want all live here.
type RequestService struct {
	repo    *repo.Repository
	tmdb    *TmdbService
	quality *QualityProfileService
}

func NewRequestService(r *repo.Repository, tmdb *TmdbService, quality *QualityProfileService) *RequestService {
	return &RequestService{repo: r, tmdb: tmdb, quality: quality}
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

// Create turns a movie request into a tracking + want. Reads and the TMDB fetch
// happen outside the transaction; every write is inside one InTx so the spawn is
// atomic. The flow stops at "want row exists in pending" — the row is the durable
// acquisition signal; there is no event emission yet.
//
// Approval gates the spawn: an unapproved request is persisted as 'pending' with
// no tracking. An approved request runs the full spawn, deduping onto an existing
// tracking when the movie is already tracked (a second requester joins; no second
// want is created).
func (s *RequestService) Create(ctx context.Context, in CreateRequestInput) (model.Request, error) {
	const op = "RequestService.Create"

	var fields []apperrors.FieldError
	if in.Type != string(parsing.DomainMovie) {
		fields = append(fields, apperrors.Field("body.type", "must be 'movie' (series is not supported in the PoC)"))
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

	// Approved: spawn atomically.
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

		// Dedup: at most one tracking per media item. ensureMovieTracking finds
		// the existing tracking (join path: a second requester for an
		// already-tracked movie) or creates a fresh 'active' one. The profile
		// is single-valued, set from this request's tier on creation; two
		// requesters at different tiers (HD vs 4K) on one movie is real
		// multi-tier reconciliation, deferred — the first spawn wins the profile.
		tracking, trackingCreated, err := ensureMovieTracking(ctx, r, mediaItem.ID, profile.ID)
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
			// carries the 'all' default and never consults it. Series spawn
			// seeds real scope in Phase 3.
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
			if _, err := r.CreateWant(ctx, repo.CreateWantParams{
				TrackingID:       tracking.ID,
				MediaItemID:      mediaItem.ID,
				QualityProfileID: profile.ID,
				Status:           string(model.WantPending),
			}); err != nil {
				return err
			}
		} else if err := rearmMovieWant(ctx, r, tracking.ID, mediaItem.ID, profile.ID); err != nil {
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

// ensureMovieTracking gets-or-creates the tracking for a media item, returning
// (tracking, created, err). The dedup boundary is UNIQUE(media_item_id), and the
// get-or-create is race-safe: CreateTrackingIfAbsent inserts on the winning
// spawn (created=true) and no-ops on a concurrent or prior one, where the loser
// re-reads the existing row (created=false) instead of rolling back with a 409.
// A created tracking is born 'active'/'self'/'none'/'smart' with the given
// profile. The helper is neutral — it never reactivates a terminal tracking;
// re-arming a terminal want on the join path is the caller's concern
// (rearmMovieWant here, GrabManualWant on the manual-grab path).
func ensureMovieTracking(ctx context.Context, r *repo.Repository, mediaItemID, profileID uuid.UUID) (model.Tracking, bool, error) {
	tracking, created, err := r.CreateTrackingIfAbsent(ctx, repo.CreateTrackingParams{
		MediaItemID:      mediaItemID,
		QualityProfileID: profileID,
		State:            string(model.TrackingActive),
		Scope:            "self",
		UpgradeBehavior:  "none",
		ScheduleStrategy: "smart",
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
func rearmMovieWant(ctx context.Context, r *repo.Repository, trackingID, mediaItemID, profileID uuid.UUID) error {
	wants, err := r.ListWantsByTracking(ctx, trackingID)
	if err != nil {
		return err
	}
	if len(wants) == 0 {
		_, err := r.CreateWant(ctx, repo.CreateWantParams{
			TrackingID:       trackingID,
			MediaItemID:      mediaItemID,
			QualityProfileID: profileID,
			Status:           string(model.WantPending),
		})
		return err
	}

	_, ok, err := r.RearmWant(ctx, wants[0].ID)
	if err != nil {
		return err
	}
	if !ok {
		// In-flight or already 'available' — acquisition is already handled.
		return nil
	}
	// The re-armed want was terminal; restore a tracking CancelWant left
	// 'canceled'. A no-op for a still-active tracking (the failed-want case).
	_, err = r.SetTrackingState(ctx, trackingID, string(model.TrackingActive))
	return err
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
