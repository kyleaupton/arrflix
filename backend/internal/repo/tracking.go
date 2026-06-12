package repo

import (
	"context"

	"github.com/google/uuid"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

// CreateTrackingParams is the domain-shaped input for CreateTracking. Mirrors
// the writeable subset of model.Tracking (omits server-managed ID/timestamps).
type CreateTrackingParams struct {
	MediaItemID      uuid.UUID
	QualityProfileID uuid.UUID
	State            string
	Scope            string
	UpgradeBehavior  string
	ScheduleStrategy string
}

// toModelTracking translates a persistence-shaped dbgen.Tracking into the
// domain-shaped model.Tracking.
func toModelTracking(row dbgen.Tracking) model.Tracking {
	return model.Tracking{
		ID:               uuidFromPgtype(row.ID),
		MediaItemID:      uuidFromPgtype(row.MediaItemID),
		QualityProfileID: uuidFromPgtype(row.QualityProfileID),
		State:            row.State,
		Scope:            row.Scope,
		UpgradeBehavior:  row.UpgradeBehavior,
		ScheduleStrategy: row.ScheduleStrategy,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
}

func (r *Repository) CreateTracking(ctx context.Context, params CreateTrackingParams) (model.Tracking, error) {
	row, err := r.Q.CreateTracking(ctx, dbgen.CreateTrackingParams{
		MediaItemID:      pgtypeFromUUID(params.MediaItemID),
		QualityProfileID: pgtypeFromUUID(params.QualityProfileID),
		State:            params.State,
		Scope:            params.Scope,
		UpgradeBehavior:  params.UpgradeBehavior,
		ScheduleStrategy: params.ScheduleStrategy,
	})
	if err != nil {
		return model.Tracking{}, apperrors.FromPg(err, "create tracking for media item %s", params.MediaItemID)
	}
	return toModelTracking(row), nil
}

func (r *Repository) GetTracking(ctx context.Context, id uuid.UUID) (model.Tracking, error) {
	row, err := r.Q.GetTracking(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.Tracking{}, apperrors.FromPg(err, "tracking %s not found", id)
	}
	return toModelTracking(row), nil
}

func (r *Repository) ListTrackings(ctx context.Context) ([]model.Tracking, error) {
	rows, err := r.Q.ListTrackings(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list trackings")
	}
	out := make([]model.Tracking, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelTracking(row))
	}
	return out, nil
}

// FindTrackingByMediaItem is the dedup lookup. A pgx.ErrNoRows flows through
// FromPg as NotFound — the spawn logic reads that as "not yet tracked" and
// takes the create branch.
func (r *Repository) FindTrackingByMediaItem(ctx context.Context, mediaItemID uuid.UUID) (model.Tracking, error) {
	row, err := r.Q.FindTrackingByMediaItem(ctx, pgtypeFromUUID(mediaItemID))
	if err != nil {
		return model.Tracking{}, apperrors.FromPg(err, "tracking for media item %s not found", mediaItemID)
	}
	return toModelTracking(row), nil
}

func (r *Repository) SetTrackingState(ctx context.Context, id uuid.UUID, state string) (model.Tracking, error) {
	row, err := r.Q.SetTrackingState(ctx, dbgen.SetTrackingStateParams{
		ID:    pgtypeFromUUID(id),
		State: state,
	})
	if err != nil {
		return model.Tracking{}, apperrors.FromPg(err, "set state for tracking %s", id)
	}
	return toModelTracking(row), nil
}
