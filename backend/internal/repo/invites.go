package repo

import (
	"context"
	"time"

	"github.com/google/uuid"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

type InvitesRepo interface {
	CreateInvite(ctx context.Context, email string, invitedBy uuid.UUID) (model.Invite, error)
	GetInviteByEmail(ctx context.Context, email string) (model.Invite, error)
	ClaimInvite(ctx context.Context, id uuid.UUID) error
	ListInvites(ctx context.Context) ([]model.Invite, error)
	DeleteInvite(ctx context.Context, id uuid.UUID) error
}

// toModelInvite translates the persistence-shaped dbgen.UserInvite into the
// domain-shaped model.Invite. The pgtype.Timestamptz claimed_at collapses
// to a *time.Time (nil when NULL).
func toModelInvite(row dbgen.UserInvite) model.Invite {
	var claimedAt *time.Time
	if row.ClaimedAt.Valid {
		t := row.ClaimedAt.Time
		claimedAt = &t
	}
	return model.Invite{
		ID:        uuidFromPgtype(row.ID),
		Email:     row.Email,
		InvitedBy: uuidFromPgtype(row.InvitedBy),
		CreatedAt: row.CreatedAt,
		ClaimedAt: claimedAt,
	}
}

func (r *Repository) CreateInvite(ctx context.Context, email string, invitedBy uuid.UUID) (model.Invite, error) {
	row, err := r.Q.CreateInvite(ctx, dbgen.CreateInviteParams{
		Email:     email,
		InvitedBy: pgtypeFromUUID(invitedBy),
	})
	if err != nil {
		return model.Invite{}, apperrors.FromPg(err, "create invite for %q", email)
	}
	return toModelInvite(row), nil
}

func (r *Repository) GetInviteByEmail(ctx context.Context, email string) (model.Invite, error) {
	row, err := r.Q.GetInviteByEmail(ctx, email)
	if err != nil {
		return model.Invite{}, apperrors.FromPg(err, "invite for %q not found", email)
	}
	return toModelInvite(row), nil
}

func (r *Repository) ClaimInvite(ctx context.Context, id uuid.UUID) error {
	return apperrors.FromPg(r.Q.ClaimInvite(ctx, pgtypeFromUUID(id)), "claim invite %s", id)
}

func (r *Repository) ListInvites(ctx context.Context) ([]model.Invite, error) {
	rows, err := r.Q.ListInvites(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list invites")
	}
	out := make([]model.Invite, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelInvite(row))
	}
	return out, nil
}

func (r *Repository) DeleteInvite(ctx context.Context, id uuid.UUID) error {
	return apperrors.FromPg(r.Q.DeleteInvite(ctx, pgtypeFromUUID(id)), "delete invite %s", id)
}
