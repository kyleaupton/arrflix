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

		// Dedup: at most one tracking per media item. A NotFound is the
		// "not yet tracked" signal and takes the create branch; a hit is the
		// join path (a second requester for an already-tracked movie).
		tracking, err := r.FindTrackingByMediaItem(ctx, mediaItem.ID)
		trackingCreated := false
		switch {
		case apperrors.IsNotFound(err):
			// quality_profile_id is single-valued, set from this request's
			// tier. Two requesters at different tiers (HD vs 4K) on one movie
			// is real multi-tier reconciliation, deferred — the first spawn
			// wins the profile.
			tracking, err = r.CreateTracking(ctx, repo.CreateTrackingParams{
				MediaItemID:      mediaItem.ID,
				QualityProfileID: profile.ID,
				State:            string(model.TrackingActive),
				Scope:            "self",
				UpgradeBehavior:  "none",
				ScheduleStrategy: "smart",
			})
			if err != nil {
				return err
			}
			trackingCreated = true
		case err != nil:
			return err
		}

		// The requester joins whether the tracking was created or already
		// existed; upsert on (tracking, user) refreshes the tier on re-request.
		if _, err := r.AddRequester(ctx, repo.AddRequesterParams{
			TrackingID: tracking.ID,
			UserID:     in.RequestedBy,
			Tier:       in.Tier,
		}); err != nil {
			return err
		}

		// One want per tracking: only a freshly-created tracking gets a want.
		// A second requester joining an existing tracking adds a requester row
		// but no second want — the dedup invariant Story 1 requires.
		if trackingCreated {
			if _, err := r.CreateWant(ctx, repo.CreateWantParams{
				TrackingID:       tracking.ID,
				MediaItemID:      mediaItem.ID,
				QualityProfileID: profile.ID,
				Status:           string(model.WantPending),
			}); err != nil {
				return err
			}
		}

		spawned, err = r.SetRequestSpawned(ctx, req.ID, tracking.ID)
		return err
	})
	if err != nil {
		return model.Request{}, err
	}
	return spawned, nil
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
