package repo

import (
	"context"

	"github.com/google/uuid"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

// AddRequesterParams is the domain-shaped input for AddRequester. Mirrors the
// writeable subset of model.TrackingRequester (omits server-managed
// timestamps).
type AddRequesterParams struct {
	TrackingID uuid.UUID
	UserID     uuid.UUID
	Tier       string
}

// toModelTrackingRequester translates a persistence-shaped
// dbgen.TrackingRequester into the domain-shaped model.TrackingRequester.
func toModelTrackingRequester(row dbgen.TrackingRequester) model.TrackingRequester {
	return model.TrackingRequester{
		TrackingID: uuidFromPgtype(row.TrackingID),
		UserID:     uuidFromPgtype(row.UserID),
		Tier:       row.Tier,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}
}

func (r *Repository) AddRequester(ctx context.Context, params AddRequesterParams) (model.TrackingRequester, error) {
	row, err := r.Q.AddRequester(ctx, dbgen.AddRequesterParams{
		TrackingID: pgtypeFromUUID(params.TrackingID),
		UserID:     pgtypeFromUUID(params.UserID),
		Tier:       params.Tier,
	})
	if err != nil {
		return model.TrackingRequester{}, apperrors.FromPg(err, "add requester %s to tracking %s", params.UserID, params.TrackingID)
	}
	return toModelTrackingRequester(row), nil
}

// FindRequester checks whether a user is already a requester on a tracking. A
// pgx.ErrNoRows flows through FromPg as NotFound — the spawn logic reads that
// as "not yet a requester" and adds them.
func (r *Repository) FindRequester(ctx context.Context, trackingID, userID uuid.UUID) (model.TrackingRequester, error) {
	row, err := r.Q.FindRequester(ctx, dbgen.FindRequesterParams{
		TrackingID: pgtypeFromUUID(trackingID),
		UserID:     pgtypeFromUUID(userID),
	})
	if err != nil {
		return model.TrackingRequester{}, apperrors.FromPg(err, "requester %s on tracking %s not found", userID, trackingID)
	}
	return toModelTrackingRequester(row), nil
}

func (r *Repository) ListRequestersByTracking(ctx context.Context, trackingID uuid.UUID) ([]model.TrackingRequester, error) {
	rows, err := r.Q.ListRequestersByTracking(ctx, pgtypeFromUUID(trackingID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list requesters for tracking %s", trackingID)
	}
	out := make([]model.TrackingRequester, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelTrackingRequester(row))
	}
	return out, nil
}

func (r *Repository) RemoveRequester(ctx context.Context, trackingID, userID uuid.UUID) error {
	return apperrors.FromPg(r.Q.RemoveRequester(ctx, dbgen.RemoveRequesterParams{
		TrackingID: pgtypeFromUUID(trackingID),
		UserID:     pgtypeFromUUID(userID),
	}), "remove requester %s from tracking %s", userID, trackingID)
}
