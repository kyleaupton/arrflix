package repo

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

type PushRepo interface {
	GetVAPIDConfig(ctx context.Context) (model.VAPIDConfig, bool, error)
	CreateVAPIDConfig(ctx context.Context, publicKey, privateKey, subject string) (model.VAPIDConfig, error)
	UpdateVAPIDSubject(ctx context.Context, id uuid.UUID, subject string) (model.VAPIDConfig, error)

	ClearPushForSessionOrEndpoint(ctx context.Context, sessionID uuid.UUID, endpoint string) (int64, error)
	InsertPushSubscription(ctx context.Context, params InsertPushSubscriptionParams) (model.PushSubscription, error)
	ListPushSubscriptionsByUser(ctx context.Context, userID uuid.UUID) ([]model.PushSubscription, error)
	GetPushSubscriptionByIDForUser(ctx context.Context, userID, id uuid.UUID) (model.PushSubscription, bool, error)
	DeletePushSubscriptionByEndpoint(ctx context.Context, endpoint string) (int64, error)
	DeletePushSubscriptionByIDForUser(ctx context.Context, userID, id uuid.UUID) (int64, error)
	MarkPushSubscriptionNotified(ctx context.Context, endpoint string) error
}

// InsertPushSubscriptionParams is the domain-shaped input for inserting a
// browser's subscription. The caller clears the session's prior row and any row
// holding this endpoint first (ClearPushForSessionOrEndpoint), so a plain insert
// is safe and enforces the 1:1 session<->push invariant.
type InsertPushSubscriptionParams struct {
	SessionID uuid.UUID
	Endpoint  string
	P256dh    string
	Auth      string
	UserAgent *string
}

func toModelVAPIDConfig(row dbgen.VapidConfig) model.VAPIDConfig {
	return model.VAPIDConfig{
		ID:         uuidFromPgtype(row.ID),
		PublicKey:  row.PublicKey,
		PrivateKey: row.PrivateKey,
		Subject:    row.Subject,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}
}

func toModelPushSubscription(row dbgen.PushSubscription) model.PushSubscription {
	return model.PushSubscription{
		ID:             uuidFromPgtype(row.ID),
		SessionID:      uuidFromPgtype(row.SessionID),
		Endpoint:       row.Endpoint,
		P256dh:         row.P256dh,
		Auth:           row.Auth,
		UserAgent:      row.UserAgent,
		CreatedAt:      row.CreatedAt,
		LastUsedAt:     row.LastUsedAt,
		LastNotifiedAt: timePtrFromPgTimestamptz(row.LastNotifiedAt),
	}
}

// GetVAPIDConfig returns the singleton VAPID config row. The bool reports
// whether a row exists: false (no row) is the not-yet-generated state, not an
// error — the caller generates and persists a keypair. Mirrors GetEmailProvider.
func (r *Repository) GetVAPIDConfig(ctx context.Context) (model.VAPIDConfig, bool, error) {
	row, err := r.Q.GetVAPIDConfig(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.VAPIDConfig{}, false, nil
		}
		return model.VAPIDConfig{}, false, apperrors.FromPg(err, "get vapid config")
	}
	return toModelVAPIDConfig(row), true, nil
}

func (r *Repository) CreateVAPIDConfig(ctx context.Context, publicKey, privateKey, subject string) (model.VAPIDConfig, error) {
	row, err := r.Q.CreateVAPIDConfig(ctx, dbgen.CreateVAPIDConfigParams{
		PublicKey:  publicKey,
		PrivateKey: privateKey,
		Subject:    subject,
	})
	if err != nil {
		return model.VAPIDConfig{}, apperrors.FromPg(err, "create vapid config")
	}
	return toModelVAPIDConfig(row), nil
}

func (r *Repository) UpdateVAPIDSubject(ctx context.Context, id uuid.UUID, subject string) (model.VAPIDConfig, error) {
	row, err := r.Q.UpdateVAPIDSubject(ctx, dbgen.UpdateVAPIDSubjectParams{
		ID:      pgtypeFromUUID(id),
		Subject: subject,
	})
	if err != nil {
		return model.VAPIDConfig{}, apperrors.FromPg(err, "update vapid subject %s", id)
	}
	return toModelVAPIDConfig(row), nil
}

// ClearPushForSessionOrEndpoint removes this session's existing push row and any
// row elsewhere holding the endpoint, freeing the subscribe slot before an insert.
// Returns rows removed; 0 is not an error.
func (r *Repository) ClearPushForSessionOrEndpoint(ctx context.Context, sessionID uuid.UUID, endpoint string) (int64, error) {
	n, err := r.Q.ClearPushForSessionOrEndpoint(ctx, dbgen.ClearPushForSessionOrEndpointParams{
		SessionID: pgtypeFromUUID(sessionID),
		Endpoint:  endpoint,
	})
	if err != nil {
		return 0, apperrors.FromPg(err, "clear push subscription for session %s", sessionID)
	}
	return n, nil
}

func (r *Repository) InsertPushSubscription(ctx context.Context, params InsertPushSubscriptionParams) (model.PushSubscription, error) {
	row, err := r.Q.InsertPushSubscription(ctx, dbgen.InsertPushSubscriptionParams{
		SessionID: pgtypeFromUUID(params.SessionID),
		Endpoint:  params.Endpoint,
		P256dh:    params.P256dh,
		Auth:      params.Auth,
		UserAgent: params.UserAgent,
	})
	if err != nil {
		return model.PushSubscription{}, apperrors.FromPg(err, "insert push subscription for session %s", params.SessionID)
	}
	return toModelPushSubscription(row), nil
}

func (r *Repository) ListPushSubscriptionsByUser(ctx context.Context, userID uuid.UUID) ([]model.PushSubscription, error) {
	rows, err := r.Q.ListPushSubscriptionsByUser(ctx, pgtypeFromUUID(userID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list push subscriptions for user %s", userID)
	}
	subs := make([]model.PushSubscription, 0, len(rows))
	for _, row := range rows {
		subs = append(subs, toModelPushSubscription(row))
	}
	return subs, nil
}

// GetPushSubscriptionByIDForUser loads one of the caller's own devices by id.
// The bool reports existence: false (no row, or a row owned by someone else) is
// the not-found state, not an error — the caller maps it to a 404 rather than
// leaking whether the id exists under another owner.
func (r *Repository) GetPushSubscriptionByIDForUser(ctx context.Context, userID, id uuid.UUID) (model.PushSubscription, bool, error) {
	row, err := r.Q.GetPushSubscriptionByIDForUser(ctx, dbgen.GetPushSubscriptionByIDForUserParams{
		ID:     pgtypeFromUUID(id),
		UserID: pgtypeFromUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.PushSubscription{}, false, nil
		}
		return model.PushSubscription{}, false, apperrors.FromPg(err, "get push subscription %s", id)
	}
	return toModelPushSubscription(row), true, nil
}

// DeletePushSubscriptionByEndpoint prunes a dead endpoint (a 404/410 from the
// push service). Returns the number of rows removed; 0 is not an error (the row
// may already be gone).
func (r *Repository) DeletePushSubscriptionByEndpoint(ctx context.Context, endpoint string) (int64, error) {
	n, err := r.Q.DeletePushSubscriptionByEndpoint(ctx, endpoint)
	if err != nil {
		return 0, apperrors.FromPg(err, "delete push subscription")
	}
	return n, nil
}

// MarkPushSubscriptionNotified stamps last_notified_at on a subscription after a
// successful push delivery. Best-effort by design — the push adapter ignores the
// error (a lost stamp only means the devices UI shows a slightly stale "last
// notified"), so this never fails a delivery.
func (r *Repository) MarkPushSubscriptionNotified(ctx context.Context, endpoint string) error {
	return apperrors.FromPg(r.Q.MarkPushSubscriptionNotified(ctx, endpoint), "mark push subscription notified")
}

// DeletePushSubscriptionByIDForUser removes one of the caller's own devices by
// id, scoped to the owner so a user cannot remove another's subscription.
// Returns the rows removed; 0 is not an error (idempotent — already gone, or
// never the caller's).
func (r *Repository) DeletePushSubscriptionByIDForUser(ctx context.Context, userID, id uuid.UUID) (int64, error) {
	n, err := r.Q.DeletePushSubscriptionByIDForUser(ctx, dbgen.DeletePushSubscriptionByIDForUserParams{
		ID:     pgtypeFromUUID(id),
		UserID: pgtypeFromUUID(userID),
	})
	if err != nil {
		return 0, apperrors.FromPg(err, "delete push subscription %s for user %s", id, userID)
	}
	return n, nil
}
