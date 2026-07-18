package repo

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

func toModelSession(row dbgen.UserSession) model.Session {
	return model.Session{
		ID:               uuidFromPgtype(row.ID),
		UserID:           uuidFromPgtype(row.UserID),
		RefreshHash:      row.RefreshHash,
		PrevRefreshHash:  row.PrevRefreshHash,
		RotatedAt:        timePtrFromPgTimestamptz(row.RotatedAt),
		RefreshExpiresAt: row.RefreshExpiresAt,
		CreatedAt:        row.CreatedAt,
		LastUsedAt:       row.LastUsedAt,
		RevokedAt:        timePtrFromPgTimestamptz(row.RevokedAt),
		UserAgent:        row.UserAgent,
		IP:               row.Ip,
		Label:            row.Label,
	}
}

// CreateSessionParams is the domain-shaped input for CreateSession. The refresh
// hash + absolute expiry are computed by the service; server-managed fields (id,
// timestamps, revoked_at) are omitted.
type CreateSessionParams struct {
	UserID           uuid.UUID
	RefreshHash      []byte
	RefreshExpiresAt time.Time
	UserAgent        *string
	IP               *string
}

func (r *Repository) CreateSession(ctx context.Context, params CreateSessionParams) (model.Session, error) {
	row, err := r.Q.CreateSession(ctx, dbgen.CreateSessionParams{
		UserID:           pgtypeFromUUID(params.UserID),
		RefreshHash:      params.RefreshHash,
		RefreshExpiresAt: params.RefreshExpiresAt,
		UserAgent:        params.UserAgent,
		Ip:               params.IP,
	})
	if err != nil {
		return model.Session{}, apperrors.FromPg(err, "create session for user %s", params.UserID)
	}
	return toModelSession(row), nil
}

// GetSession loads a session by id. The bool reports existence: false (no row) is
// the not-found state, not an error — the refresh flow maps it to Unauthenticated
// rather than leaking whether a session id exists.
func (r *Repository) GetSession(ctx context.Context, id uuid.UUID) (model.Session, bool, error) {
	row, err := r.Q.GetSession(ctx, pgtypeFromUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Session{}, false, nil
		}
		return model.Session{}, false, apperrors.FromPg(err, "get session %s", id)
	}
	return toModelSession(row), true, nil
}

func (r *Repository) ListActiveSessionsByUser(ctx context.Context, userID uuid.UUID) ([]model.Session, error) {
	rows, err := r.Q.ListActiveSessionsByUser(ctx, pgtypeFromUUID(userID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list sessions for user %s", userID)
	}
	sessions := make([]model.Session, 0, len(rows))
	for _, row := range rows {
		sessions = append(sessions, toModelSession(row))
	}
	return sessions, nil
}

// RotateSession installs newHash as the current refresh hash, shifting the old
// current into prev. The bool reports whether a row was updated: false means the
// session was revoked between the read and the rotate, which the service maps to
// Unauthenticated.
func (r *Repository) RotateSession(ctx context.Context, id uuid.UUID, newHash []byte) (model.Session, bool, error) {
	row, err := r.Q.RotateSession(ctx, dbgen.RotateSessionParams{
		ID:          pgtypeFromUUID(id),
		RefreshHash: newHash,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Session{}, false, nil
		}
		return model.Session{}, false, apperrors.FromPg(err, "rotate session %s", id)
	}
	return toModelSession(row), true, nil
}

// RevokeSession soft-revokes one session. Returns the rows affected; 0 is not an
// error (already revoked or already gone — revocation is idempotent).
func (r *Repository) RevokeSession(ctx context.Context, id uuid.UUID) (int64, error) {
	n, err := r.Q.RevokeSession(ctx, pgtypeFromUUID(id))
	if err != nil {
		return 0, apperrors.FromPg(err, "revoke session %s", id)
	}
	return n, nil
}

// RevokeAllSessionsForUser soft-revokes every active session for a user. Returns
// the rows affected.
func (r *Repository) RevokeAllSessionsForUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	n, err := r.Q.RevokeAllSessionsForUser(ctx, pgtypeFromUUID(userID))
	if err != nil {
		return 0, apperrors.FromPg(err, "revoke sessions for user %s", userID)
	}
	return n, nil
}
