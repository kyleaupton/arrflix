package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/scope"
)

// ReconcileService is the per-episode want producer for series tracking. Where a
// movie spawn creates one want for its single-atom tracking, a series tracking
// must produce one want per in-scope, aired, file-less episode and cancel wants
// whose episode has fallen out of scope. The reconciler reads the effective
// scope (unioned across requesters) against the synced episode tree and the
// existing wants, then writes the difference — re-runnably and idempotently, so
// the structure-sync/enrichment triggers can call it freely.
type ReconcileService struct {
	repo *repo.Repository
	log  *logger.Logger
}

func NewReconcileService(r *repo.Repository, l *logger.Logger) *ReconcileService {
	return &ReconcileService{repo: r, log: l}
}

// Reconcile brings a series tracking's wants in line with its effective scope.
// Movies are a no-op — their spawn path owns their single want. The whole pass
// runs in one transaction so the create/cancel set is applied atomically against
// the snapshot it was computed from.
//
// Aired-only gate: wants are created only for in-scope episodes that have aired
// (air_date <= now) and lack a file. Unaired or unknown-air-date episodes get no
// want yet; a later reconcile picks them up once they air. "Lacks a file" is
// file_id IS NULL (quality-cutoff upgrades are deferred).
func (s *ReconcileService) Reconcile(ctx context.Context, trackingID uuid.UUID) error {
	const op = "ReconcileService.Reconcile"
	now := time.Now()

	return s.repo.InTx(ctx, func(r *repo.Repository) error {
		tracking, err := r.GetTracking(ctx, trackingID)
		if err != nil {
			return err
		}
		item, err := r.GetMediaItem(ctx, tracking.MediaItemID)
		if err != nil {
			return err
		}
		if item.Type != string(model.MediaTypeSeries) {
			// Movies don't reconcile; their spawn path owns their single want.
			return nil
		}

		// Effective scope: decode each requester's stored scope, then union.
		requesters, err := r.ListRequestersByTracking(ctx, trackingID)
		if err != nil {
			return err
		}
		scopes := make([]scope.RequesterScope, 0, len(requesters))
		for _, req := range requesters {
			rs, err := decodeRequesterScope(req)
			if err != nil {
				return err.Op(op)
			}
			scopes = append(scopes, rs)
		}

		// Episode tree: drop deprecated rows (episodes TMDB removed), build the
		// scope input plus the per-episode hasFile/aired facts the gate needs.
		avail, err := r.ListEpisodeAvailabilityForSeries(ctx, item.ID)
		if err != nil {
			return err
		}
		episodes := make([]scope.Episode, 0, len(avail))
		hasFile := make(map[uuid.UUID]bool, len(avail))
		aired := make(map[uuid.UUID]bool, len(avail))
		for _, ep := range avail {
			if ep.Deprecated {
				continue
			}
			episodes = append(episodes, scope.Episode{
				ID:      ep.EpisodeID,
				Season:  int(ep.SeasonNumber),
				Number:  int(ep.EpisodeNumber),
				AirDate: ep.AirDate,
			})
			hasFile[ep.EpisodeID] = ep.FileID != nil
			aired[ep.EpisodeID] = ep.AirDate != nil && !ep.AirDate.After(now)
		}

		inScope := make(map[uuid.UUID]bool)
		for _, id := range scope.EffectiveInScope(scopes, episodes, now) {
			inScope[id] = true
		}

		// Existing live wants, keyed by episode. Terminal wants are ignored —
		// they're settled and shouldn't block a re-create or be re-canceled.
		wants, err := r.ListWantsByTracking(ctx, trackingID)
		if err != nil {
			return err
		}
		existing := make(map[uuid.UUID]model.Want, len(wants))
		for _, w := range wants {
			if w.EpisodeID == nil || model.WantStatus(w.Status).IsTerminal() {
				continue
			}
			existing[*w.EpisodeID] = w
		}

		// Create: an in-scope, aired, file-less episode with no live want gets a
		// fresh pending one. CreateWantIfAbsent absorbs a concurrent reconcile.
		for _, ep := range episodes {
			if !inScope[ep.ID] || !aired[ep.ID] || hasFile[ep.ID] {
				continue
			}
			if _, ok := existing[ep.ID]; ok {
				continue
			}
			epID := ep.ID
			if _, _, err := r.CreateWantIfAbsent(ctx, repo.CreateWantParams{
				TrackingID:       trackingID,
				MediaItemID:      item.ID,
				EpisodeID:        &epID,
				QualityProfileID: tracking.QualityProfileID,
				Status:           string(model.WantPending),
			}); err != nil {
				return err
			}
		}

		// Cancel: a live want whose episode is no longer in scope is canceled via
		// the CAS primitive directly. Not WantService.CancelWant — its movie-shaped
		// cascade would flip the whole tracking to 'canceled', but a series tracking
		// must stay active when one episode leaves scope.
		for epID, w := range existing {
			if inScope[epID] {
				continue
			}
			if _, _, err := r.CancelWant(ctx, w.ID); err != nil {
				return err
			}
		}

		return nil
	})
}

// decodeRequesterScope turns a stored tracking_requester row into the pure
// scope.RequesterScope the resolver consumes. A malformed scope_overrides blob
// is an invariant violation (we wrote it), so it surfaces as a non-retryable
// Internal, mirroring quality.go's gate decode.
func decodeRequesterScope(req model.TrackingRequester) (scope.RequesterScope, *apperrors.Error) {
	rs := scope.RequesterScope{
		Rule:   scope.Rule{Kind: scope.RuleKind(req.ScopeRule)},
		Anchor: req.CreatedAt,
	}
	if req.ScopeSeason != nil {
		rs.Rule.Season = int(*req.ScopeSeason)
	}
	if len(req.ScopeOverrides) > 0 {
		if err := json.Unmarshal(req.ScopeOverrides, &rs.Overrides); err != nil {
			return scope.RequesterScope{}, apperrors.Internalf(
				"tracking %s requester %s has undecodable scope_overrides: %v",
				req.TrackingID, req.UserID, err).NotRetryable()
		}
	}
	return rs, nil
}
