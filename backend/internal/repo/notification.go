package repo

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

// toModelOutbox translates the persistence-shaped dbgen.NotificationOutbox into
// the domain-shaped model.NotificationOutbox. Nullable columns collapse to
// pointers; the JSONB payload passes through as json.RawMessage.
func toModelOutbox(row dbgen.NotificationOutbox) model.NotificationOutbox {
	return model.NotificationOutbox{
		ID:              uuidFromPgtype(row.ID),
		EventType:       row.EventType,
		Audience:        row.Audience,
		RecipientUserID: uuidPtrFromPgtype(row.RecipientUserID),
		Channel:         row.Channel,
		Payload:         json.RawMessage(row.Payload),
		DedupKey:        row.DedupKey,
		Status:          row.Status,
		Attempts:        row.Attempts,
		NextAttemptAt:   row.NextAttemptAt,
		LastError:       row.LastError,
		CreatedAt:       row.CreatedAt,
		DeliveredAt:     timePtrFromPgTimestamptz(row.DeliveredAt),
		ReadAt:          timePtrFromPgTimestamptz(row.ReadAt),
	}
}

// toModelPreference translates the persistence-shaped dbgen.NotificationPreference
// into the domain-shaped model.NotificationPreference.
func toModelPreference(row dbgen.NotificationPreference) model.NotificationPreference {
	return model.NotificationPreference{
		UserID:    uuidFromPgtype(row.UserID),
		Scope:     row.Scope,
		Value:     row.Value,
		Channel:   row.Channel,
		Enabled:   row.Enabled,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

// EnqueueOutboxParams is the domain-shaped input for EnqueueOutbox. Mirrors the
// producer-supplied subset of a notification_outbox row; status, attempts, and
// timestamps take their column defaults.
type EnqueueOutboxParams struct {
	EventType       string
	Audience        string
	RecipientUserID *uuid.UUID
	Channel         string
	Payload         json.RawMessage
	DedupKey        *string
}

func (r *Repository) EnqueueOutbox(ctx context.Context, params EnqueueOutboxParams) (model.NotificationOutbox, error) {
	row, err := r.Q.EnqueueOutbox(ctx, dbgen.EnqueueOutboxParams{
		EventType:       params.EventType,
		Audience:        params.Audience,
		RecipientUserID: pgtypeFromUUIDPtr(params.RecipientUserID),
		Channel:         params.Channel,
		Payload:         []byte(params.Payload),
		DedupKey:        params.DedupKey,
	})
	if err != nil {
		return model.NotificationOutbox{}, apperrors.FromPg(err, "enqueue %q notification", params.EventType)
	}
	return toModelOutbox(row), nil
}

func (r *Repository) ListDueOutbox(ctx context.Context, batchSize int32) ([]model.NotificationOutbox, error) {
	rows, err := r.Q.ListDueOutbox(ctx, batchSize)
	if err != nil {
		return nil, apperrors.FromPg(err, "list due notifications")
	}
	out := make([]model.NotificationOutbox, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelOutbox(row))
	}
	return out, nil
}

func (r *Repository) MarkOutboxDelivering(ctx context.Context, id uuid.UUID) (model.NotificationOutbox, error) {
	row, err := r.Q.MarkOutboxDelivering(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.NotificationOutbox{}, apperrors.FromPg(err, "notification %s not found", id)
	}
	return toModelOutbox(row), nil
}

func (r *Repository) MarkOutboxDelivered(ctx context.Context, id uuid.UUID) (model.NotificationOutbox, error) {
	row, err := r.Q.MarkOutboxDelivered(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.NotificationOutbox{}, apperrors.FromPg(err, "notification %s not found", id)
	}
	return toModelOutbox(row), nil
}

// MarkOutboxAwaitingConfig parks a queued row awaiting channel configuration.
// A missing row (already claimed elsewhere) surfaces as NotFound, which the
// worker treats as "another worker beat me to it" and skips.
func (r *Repository) MarkOutboxAwaitingConfig(ctx context.Context, id uuid.UUID) (model.NotificationOutbox, error) {
	row, err := r.Q.MarkOutboxAwaitingConfig(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.NotificationOutbox{}, apperrors.FromPg(err, "notification %s not found", id)
	}
	return toModelOutbox(row), nil
}

// RequeueAwaitingConfig returns awaiting_config rows parked since `since` to the
// queue and reports how many were requeued.
func (r *Repository) RequeueAwaitingConfig(ctx context.Context, since time.Time) (int64, error) {
	n, err := r.Q.RequeueAwaitingConfig(ctx, since)
	if err != nil {
		return 0, apperrors.FromPg(err, "requeue awaiting-config notifications")
	}
	return n, nil
}

// RescheduleOutboxParams is the domain-shaped input for RescheduleOutbox — the
// transient-retry transition. NextAttemptAt is the worker-computed backoff time.
type RescheduleOutboxParams struct {
	ID            uuid.UUID
	NextAttemptAt time.Time
	LastError     *string
}

func (r *Repository) RescheduleOutbox(ctx context.Context, params RescheduleOutboxParams) (model.NotificationOutbox, error) {
	row, err := r.Q.RescheduleOutbox(ctx, dbgen.RescheduleOutboxParams{
		ID:            pgtypeFromUUID(params.ID),
		NextAttemptAt: params.NextAttemptAt,
		LastError:     params.LastError,
	})
	if err != nil {
		return model.NotificationOutbox{}, apperrors.FromPg(err, "notification %s not found", params.ID)
	}
	return toModelOutbox(row), nil
}

func (r *Repository) MarkOutboxDead(ctx context.Context, id uuid.UUID, lastError *string) (model.NotificationOutbox, error) {
	row, err := r.Q.MarkOutboxDead(ctx, dbgen.MarkOutboxDeadParams{
		ID:        pgtypeFromUUID(id),
		LastError: lastError,
	})
	if err != nil {
		return model.NotificationOutbox{}, apperrors.FromPg(err, "notification %s not found", id)
	}
	return toModelOutbox(row), nil
}

func (r *Repository) GetOutbox(ctx context.Context, id uuid.UUID) (model.NotificationOutbox, error) {
	row, err := r.Q.GetOutbox(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.NotificationOutbox{}, apperrors.FromPg(err, "notification %s not found", id)
	}
	return toModelOutbox(row), nil
}

func (r *Repository) ListInbox(ctx context.Context, userID uuid.UUID, limit int32) ([]model.NotificationOutbox, error) {
	rows, err := r.Q.ListInbox(ctx, dbgen.ListInboxParams{
		UserID:   pgtypeFromUUID(userID),
		RowLimit: limit,
	})
	if err != nil {
		return nil, apperrors.FromPg(err, "list inbox for user %s", userID)
	}
	out := make([]model.NotificationOutbox, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelOutbox(row))
	}
	return out, nil
}

func (r *Repository) CountUnreadInbox(ctx context.Context, userID uuid.UUID) (int64, error) {
	n, err := r.Q.CountUnreadInbox(ctx, pgtypeFromUUID(userID))
	return n, apperrors.FromPg(err, "count unread inbox for user %s", userID)
}

func (r *Repository) MarkInboxRead(ctx context.Context, id, userID uuid.UUID) error {
	return apperrors.FromPg(r.Q.MarkInboxRead(ctx, dbgen.MarkInboxReadParams{
		ID:     pgtypeFromUUID(id),
		UserID: pgtypeFromUUID(userID),
	}), "mark notification %s read", id)
}

func (r *Repository) MarkAllInboxRead(ctx context.Context, userID uuid.UUID) error {
	return apperrors.FromPg(r.Q.MarkAllInboxRead(ctx, pgtypeFromUUID(userID)),
		"mark all inbox read for user %s", userID)
}

// UpsertPreferenceParams is the domain-shaped input for UpsertPreference. Mirrors
// the full notification_preference key plus the toggle it sets.
type UpsertPreferenceParams struct {
	UserID  uuid.UUID
	Scope   string
	Value   string
	Channel string
	Enabled bool
}

func (r *Repository) UpsertPreference(ctx context.Context, params UpsertPreferenceParams) (model.NotificationPreference, error) {
	row, err := r.Q.UpsertPreference(ctx, dbgen.UpsertPreferenceParams{
		UserID:  pgtypeFromUUID(params.UserID),
		Scope:   params.Scope,
		Value:   params.Value,
		Channel: params.Channel,
		Enabled: params.Enabled,
	})
	if err != nil {
		return model.NotificationPreference{}, apperrors.FromPg(err,
			"upsert %q preference for user %s", params.Value, params.UserID)
	}
	return toModelPreference(row), nil
}

func (r *Repository) ListPreferences(ctx context.Context, userID uuid.UUID) ([]model.NotificationPreference, error) {
	rows, err := r.Q.ListPreferences(ctx, pgtypeFromUUID(userID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list preferences for user %s", userID)
	}
	out := make([]model.NotificationPreference, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelPreference(row))
	}
	return out, nil
}
