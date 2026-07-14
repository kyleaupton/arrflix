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
		UserID:     uuidFromPgtype(row.UserID),
		Bundle:     row.Bundle,
		Subscribed: row.Subscribed,
		Email:      row.Email,
		Push:       row.Push,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
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

// SetBundlePreference upserts one column of a (user, bundle) preference row: the
// subscription master ("subscribed") or an outbound channel ("email"/"push"). Only
// the targeted column is written, so the others keep their value (or stay NULL =
// defer to the registry default). The service validates target before calling, so
// an unknown target here is a programming error surfaced as an Internal error.
func (r *Repository) SetBundlePreference(ctx context.Context, userID uuid.UUID, bundle, target string, enabled bool) error {
	uid := pgtypeFromUUID(userID)
	flag := &enabled
	var err error
	switch target {
	case "subscribed":
		_, err = r.Q.SetBundleSubscribed(ctx, dbgen.SetBundleSubscribedParams{UserID: uid, Bundle: bundle, Subscribed: flag})
	case string(model.ChannelEmail):
		_, err = r.Q.SetBundleEmail(ctx, dbgen.SetBundleEmailParams{UserID: uid, Bundle: bundle, Email: flag})
	case string(model.ChannelPush):
		_, err = r.Q.SetBundlePush(ctx, dbgen.SetBundlePushParams{UserID: uid, Bundle: bundle, Push: flag})
	default:
		return apperrors.Internalf("unknown preference target %q", target).Op("Repository.SetBundlePreference")
	}
	return apperrors.FromPg(err, "set %q preference on bundle %q for user %s", target, bundle, userID)
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
