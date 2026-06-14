package repo

import (
	"context"

	"github.com/google/uuid"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

// toModelUserPolicy translates a persistence-shaped dbgen.UserPolicy into the
// domain-shaped model.UserPolicy.
func toModelUserPolicy(row dbgen.UserPolicy) model.UserPolicy {
	return model.UserPolicy{
		UserID:           uuidFromPgtype(row.UserID),
		AutoApproveMovie: row.AutoApproveMovie,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
}

func (r *Repository) GetUserPolicy(ctx context.Context, userID uuid.UUID) (model.UserPolicy, error) {
	row, err := r.Q.GetUserPolicy(ctx, pgtypeFromUUID(userID))
	if err != nil {
		return model.UserPolicy{}, apperrors.FromPg(err, "policy for user %s not found", userID)
	}
	return toModelUserPolicy(row), nil
}

func (r *Repository) UpsertUserPolicy(ctx context.Context, userID uuid.UUID, autoApproveMovie bool) (model.UserPolicy, error) {
	row, err := r.Q.UpsertUserPolicy(ctx, dbgen.UpsertUserPolicyParams{
		UserID:           pgtypeFromUUID(userID),
		AutoApproveMovie: autoApproveMovie,
	})
	if err != nil {
		return model.UserPolicy{}, apperrors.FromPg(err, "upsert policy for user %s", userID)
	}
	return toModelUserPolicy(row), nil
}
