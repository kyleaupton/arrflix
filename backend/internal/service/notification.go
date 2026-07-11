package service

import (
	"context"
	"encoding/json"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/notifications"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// NotificationService is the enqueue half of the notification system: it turns a
// typed notifications.Event into outbox rows. It resolves recipients (v1: the
// user-audience recipients the event carries), checks each recipient's per-channel
// preferences, and writes one queued outbox row per (recipient, enabled channel).
// The pure registry, bundle catalog, and preference resolution live in
// internal/notifications; delivery (draining the outbox) is the worker's job.
type NotificationService struct {
	repo *repo.Repository
	// channels is the set of channels enqueue considers. v1 is in_app only; push
	// and email join when their adapters land, at which point this becomes
	// adapter-driven rather than a fixed list.
	channels []model.NotificationChannel
}

func NewNotificationService(r *repo.Repository) *NotificationService {
	return &NotificationService{
		repo:     r,
		channels: []model.NotificationChannel{model.ChannelInApp},
	}
}

// Enqueue writes outbox rows for an event: one per (recipient, channel) the
// recipient is subscribed to. It is the single entry point producers use — there
// is no path to the outbox that doesn't go through a typed Event.
func (s *NotificationService) Enqueue(ctx context.Context, ev notifications.Event) error {
	payload, err := json.Marshal(ev.Payload())
	if err != nil {
		return apperrors.Internalf("marshal %q payload: %v", ev.EventType(), err).
			Op("NotificationService.Enqueue")
	}

	var dedup *string
	if k := ev.DedupKey(); k != "" {
		dedup = &k
	}

	// v1 handles the user audience only — recipients are carried on the event. The
	// admin audience (permission-key holders via the reverse resolver) and system
	// audience (literal emails) resolve their recipients here when they land.
	for _, recipient := range ev.Recipients() {
		prefs, err := s.repo.ListPreferences(ctx, recipient)
		if err != nil {
			return err
		}
		for _, ch := range s.channels {
			if !notifications.ChannelEnabled(prefs, ev.EventType(), ev.Bundle(), ch) {
				continue
			}
			if _, err := s.repo.EnqueueOutbox(ctx, repo.EnqueueOutboxParams{
				EventType:       ev.EventType(),
				Audience:        string(ev.Audience()),
				RecipientUserID: &recipient,
				Channel:         string(ch),
				Payload:         payload,
				DedupKey:        dedup,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
