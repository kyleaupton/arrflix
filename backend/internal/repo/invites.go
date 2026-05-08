package repo

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

func (r *Repository) CreateInvite(ctx context.Context, email string, invitedBy pgtype.UUID) (dbgen.UserInvite, error) {
	inv, err := r.Q.CreateInvite(ctx, dbgen.CreateInviteParams{
		Email:     email,
		InvitedBy: invitedBy,
	})
	return inv, apperrors.FromPg(err, "create invite for %q", email)
}

func (r *Repository) GetInviteByEmail(ctx context.Context, email string) (dbgen.UserInvite, error) {
	inv, err := r.Q.GetInviteByEmail(ctx, email)
	return inv, apperrors.FromPg(err, "invite for %q not found", email)
}

func (r *Repository) ClaimInvite(ctx context.Context, id pgtype.UUID) error {
	return apperrors.FromPg(r.Q.ClaimInvite(ctx, id), "claim invite %s", id)
}

func (r *Repository) ListInvites(ctx context.Context) ([]dbgen.UserInvite, error) {
	invs, err := r.Q.ListInvites(ctx)
	return invs, apperrors.FromPg(err, "list invites")
}

func (r *Repository) DeleteInvite(ctx context.Context, id pgtype.UUID) error {
	return apperrors.FromPg(r.Q.DeleteInvite(ctx, id), "delete invite %s", id)
}
