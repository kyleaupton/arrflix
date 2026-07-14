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

	UpsertPushSubscription(ctx context.Context, params UpsertPushSubscriptionParams) (model.PushSubscription, error)
	ListPushSubscriptionsByUser(ctx context.Context, userID uuid.UUID) ([]model.PushSubscription, error)
	DeletePushSubscriptionByEndpoint(ctx context.Context, endpoint string) (int64, error)
	DeletePushSubscriptionForUser(ctx context.Context, userID uuid.UUID, endpoint string) (int64, error)
}

// UpsertPushSubscriptionParams is the domain-shaped input for registering a
// browser's subscription.
type UpsertPushSubscriptionParams struct {
	UserID    uuid.UUID
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
		ID:         uuidFromPgtype(row.ID),
		UserID:     uuidFromPgtype(row.UserID),
		Endpoint:   row.Endpoint,
		P256dh:     row.P256dh,
		Auth:       row.Auth,
		UserAgent:  row.UserAgent,
		CreatedAt:  row.CreatedAt,
		LastUsedAt: row.LastUsedAt,
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

func (r *Repository) UpsertPushSubscription(ctx context.Context, params UpsertPushSubscriptionParams) (model.PushSubscription, error) {
	row, err := r.Q.UpsertPushSubscription(ctx, dbgen.UpsertPushSubscriptionParams{
		UserID:    pgtypeFromUUID(params.UserID),
		Endpoint:  params.Endpoint,
		P256dh:    params.P256dh,
		Auth:      params.Auth,
		UserAgent: params.UserAgent,
	})
	if err != nil {
		return model.PushSubscription{}, apperrors.FromPg(err, "upsert push subscription")
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

// DeletePushSubscriptionForUser unsubscribes one of the caller's own devices,
// scoped to the owner so a user cannot remove another's subscription.
func (r *Repository) DeletePushSubscriptionForUser(ctx context.Context, userID uuid.UUID, endpoint string) (int64, error) {
	n, err := r.Q.DeletePushSubscriptionForUser(ctx, dbgen.DeletePushSubscriptionForUserParams{
		Endpoint: endpoint,
		UserID:   pgtypeFromUUID(userID),
	})
	if err != nil {
		return 0, apperrors.FromPg(err, "delete push subscription for user %s", userID)
	}
	return n, nil
}
