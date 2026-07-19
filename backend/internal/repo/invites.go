package repo

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

type InvitesRepo interface {
	UpsertInvite(ctx context.Context, params CreateInviteParams) (model.Invite, error)
	GetInviteByEmail(ctx context.Context, email string) (model.Invite, error)
	GetInviteByTokenHash(ctx context.Context, tokenHash []byte) (model.Invite, error)
	ClaimInvite(ctx context.Context, id uuid.UUID) error
	ListInvites(ctx context.Context) ([]model.Invite, error)
	DeleteInvite(ctx context.Context, id uuid.UUID) error
}

// CreateInviteParams is the domain-shaped input for UpsertInvite. Mirrors the
// writeable subset of model.Invite plus the token hash (a secret the model omits).
type CreateInviteParams struct {
	Email     string
	InvitedBy uuid.UUID
	TokenHash []byte
	ExpiresAt time.Time
	Role      string
}

// toModelInvite translates the persistence-shaped dbgen.UserInvite into the
// domain-shaped model.Invite. The pgtype.Timestamptz claimed_at/expires_at collapse
// to *time.Time (nil when NULL); token_hash is dropped (secret, never surfaced).
func toModelInvite(row dbgen.UserInvite) model.Invite {
	var expiresAt *time.Time
	if row.ExpiresAt.Valid {
		t := row.ExpiresAt.Time
		expiresAt = &t
	}
	var claimedAt *time.Time
	if row.ClaimedAt.Valid {
		t := row.ClaimedAt.Time
		claimedAt = &t
	}
	return model.Invite{
		ID:        uuidFromPgtype(row.ID),
		Email:     row.Email,
		Role:      row.Role,
		InvitedBy: uuidFromPgtype(row.InvitedBy),
		CreatedAt: row.CreatedAt,
		ExpiresAt: expiresAt,
		ClaimedAt: claimedAt,
	}
}

func (r *Repository) UpsertInvite(ctx context.Context, params CreateInviteParams) (model.Invite, error) {
	row, err := r.Q.UpsertInvite(ctx, dbgen.UpsertInviteParams{
		Email:     params.Email,
		InvitedBy: pgtypeFromUUID(params.InvitedBy),
		TokenHash: params.TokenHash,
		ExpiresAt: pgtype.Timestamptz{Time: params.ExpiresAt, Valid: true},
		Role:      params.Role,
	})
	if err != nil {
		return model.Invite{}, apperrors.FromPg(err, "create invite for %q", params.Email)
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

func (r *Repository) GetInviteByTokenHash(ctx context.Context, tokenHash []byte) (model.Invite, error) {
	row, err := r.Q.GetInviteByTokenHash(ctx, tokenHash)
	if err != nil {
		// The token is a secret — keep it out of the error detail.
		return model.Invite{}, apperrors.FromPg(err, "invite not found")
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
