package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/notifications"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// AwaitingConfigDrainWindow bounds how far back an email-provider save reaches
// when requeuing parked mail: rows parked within the last week are drained,
// older ones are left. It caps the blast radius of enabling SMTP so a first-time
// setup doesn't resurrect a long-accumulated backlog. The interactive
// "drain N since <date>?" prompt is a later UI slice.
const AwaitingConfigDrainWindow = 7 * 24 * time.Hour

// NotificationService is the enqueue half of the notification system: it turns a
// typed notifications.Event into outbox rows. It resolves recipients (v1: the
// user-audience recipients the event carries), checks each recipient's per-channel
// preferences, and writes one queued outbox row per (recipient, enabled channel).
// The pure registry, bundle catalog, and preference resolution live in
// internal/notifications; delivery (draining the outbox) is the worker's job.
type NotificationService struct {
	repo *repo.Repository
	// channels is the set of channels enqueue considers. in_app and email are
	// active; push joins when its adapter lands, at which point this becomes
	// adapter-driven rather than a fixed list. Email rows are written only for
	// users who opt in (email defaults off in the bundle catalog), so lighting
	// up the channel here is safe: an unconfigured SMTP relay parks those rows
	// at the worker rather than failing them.
	channels []model.NotificationChannel
	// renderer renders the bell-icon read projection (title + body). The worker
	// holds its own renderer for delivery-side needs; this one serves the read
	// path. Both parse the same embedded templates.
	renderer *notifications.Renderer
}

func NewNotificationService(r *repo.Repository) *NotificationService {
	return &NotificationService{
		repo:     r,
		channels: []model.NotificationChannel{model.ChannelInApp, model.ChannelEmail},
		renderer: notifications.MustNewRenderer(),
	}
}

// RequeueAwaitingConfig returns notification rows parked as awaiting_config
// (their channel was unconfigured at delivery time) to the queue, scoped to rows
// created at or after `since`. It is called best-effort after an email provider
// is saved enabled, so a just-configured SMTP relay drains the recent backlog of
// email that piled up unconfigured. Returns the number requeued.
func (s *NotificationService) RequeueAwaitingConfig(ctx context.Context, since time.Time) (int64, error) {
	return s.repo.RequeueAwaitingConfig(ctx, since)
}

// Inbox returns a user's delivered in_app notifications, newest first, each with
// its title and body rendered from the event template. A row whose template
// can't render (should not happen post-startup-verify) falls back to the raw
// event type as title rather than dropping the entry or failing the whole read.
func (s *NotificationService) Inbox(ctx context.Context, userID uuid.UUID, limit int32) ([]model.InboxNotification, error) {
	rows, err := s.repo.ListInbox(ctx, userID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]model.InboxNotification, 0, len(rows))
	for _, row := range rows {
		title, body, err := s.renderer.Render(row.EventType, model.ChannelInApp, row.Payload)
		if err != nil {
			title, body = row.EventType, ""
		}
		out = append(out, model.InboxNotification{
			ID:        row.ID,
			EventType: row.EventType,
			Title:     title,
			Body:      body,
			Payload:   row.Payload,
			CreatedAt: row.CreatedAt,
			ReadAt:    row.ReadAt,
		})
	}
	return out, nil
}

// UnreadCount is the bell badge: the user's delivered-but-unread in_app count.
func (s *NotificationService) UnreadCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	return s.repo.CountUnreadInbox(ctx, userID)
}

// MarkRead marks one of the user's in_app notifications read. Guarded by
// recipient in the repo, so a user can only touch their own; idempotent.
func (s *NotificationService) MarkRead(ctx context.Context, id, userID uuid.UUID) error {
	return s.repo.MarkInboxRead(ctx, id, userID)
}

// MarkAllRead clears the user's unread in_app notifications in one write.
func (s *NotificationService) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	return s.repo.MarkAllInboxRead(ctx, userID)
}

// NotifyWantAvailable enqueues a want.available notification to every requester
// of the want's tracking — the first user-facing producer. The import worker
// calls it when a want reaches its terminal 'available' state (the file is on
// disk). Each requester gets their own event (one outbox row per requester per
// enabled channel); a requester who has muted the my_requests bundle's channel
// is filtered by Enqueue's preference gate.
func (s *NotificationService) NotifyWantAvailable(ctx context.Context, want model.Want) error {
	item, err := s.repo.GetMediaItem(ctx, want.MediaItemID)
	if err != nil {
		return err
	}
	requesters, err := s.repo.ListRequestersByTracking(ctx, want.TrackingID)
	if err != nil {
		return err
	}

	media := notifications.MediaRef{Title: item.Title}
	if item.Year != nil {
		media.Year = int(*item.Year)
	}
	if item.PosterPath != nil {
		media.PosterPath = *item.PosterPath
	}

	for _, req := range requesters {
		if err := s.Enqueue(ctx, notifications.WantAvailable{
			Recipient: req.UserID,
			Media:     media,
		}); err != nil {
			return err
		}
	}
	return nil
}

// SeedDefaults materializes a new user's default notification preferences: for
// each user-audience bundle, one bundle-scope row per channel in that bundle's
// Defaults, set to the default enablement. Callers invoke it best-effort from the
// user-create paths; it is idempotent (UpsertPreference is an upsert), so a
// re-run — or a partial prior run — converges rather than duplicating.
//
// Seeding is a materialization convenience, not a correctness requirement:
// notifications.ChannelEnabled resolves the same in-code defaults with zero rows
// present, so a user whose seed failed (or never ran) still routes correctly and
// lazily writes rows the first time they toggle a channel. That is why the
// bootstrap admin — created inside the serializable tx in repo.InitializeSystem,
// which returns no user id — is intentionally left unseeded: its experience is
// identical unseeded, and opening the prefs UI to toggle creates its rows lazily.
func (s *NotificationService) SeedDefaults(ctx context.Context, userID uuid.UUID) error {
	for _, bundle := range notifications.UserBundles() {
		for ch, enabled := range bundle.Defaults {
			if _, err := s.repo.UpsertPreference(ctx, repo.UpsertPreferenceParams{
				UserID:  userID,
				Scope:   string(model.PreferenceScopeBundle),
				Value:   bundle.Name,
				Channel: string(ch),
				Enabled: enabled,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// Preferences returns the user's bundle-scoped preference view: one
// BundlePreference per user-audience bundle, each carrying the resolved
// enablement of every deliverable channel (s.channels). Enablement comes from
// ChannelEnabled with an empty event type, so the result is the bundle-level
// answer (a stored bundle row, else the in-code default); event-scope overrides
// don't leak into a view the v1 UI can't express. Available is left false — the
// handler fills channel deliverability, since the email-provider read is its
// concern, not the service's.
func (s *NotificationService) Preferences(ctx context.Context, userID uuid.UUID) ([]model.BundlePreference, error) {
	prefs, err := s.repo.ListPreferences(ctx, userID)
	if err != nil {
		return nil, err
	}
	bundles := notifications.UserBundles()
	out := make([]model.BundlePreference, 0, len(bundles))
	for _, bundle := range bundles {
		channels := make([]model.ChannelPreference, 0, len(s.channels))
		for _, ch := range s.channels {
			channels = append(channels, model.ChannelPreference{
				Channel: string(ch),
				Enabled: notifications.ChannelEnabled(prefs, "", bundle.Name, ch),
			})
		}
		out = append(out, model.BundlePreference{Bundle: bundle.Name, Channels: channels})
	}
	return out, nil
}

// SetPreference writes one channel toggle for the user. v1 accepts bundle-scope
// writes only: scope must be "bundle", value a user-audience bundle name, and
// channel one of the deliverable channels (s.channels). Event-scope overrides are
// resolution-ready but rejected here — the bundle-level v1 UI can't surface them,
// so accepting one would strand an override the user can neither see nor clear.
// Invalid input collects every field problem into one Validation error; a valid
// write upserts (idempotent).
func (s *NotificationService) SetPreference(ctx context.Context, userID uuid.UUID, scope, value, channel string, enabled bool) error {
	var fields []apperrors.FieldError
	if scope != string(model.PreferenceScopeBundle) {
		fields = append(fields, apperrors.Field("body.scope", "must be 'bundle'"))
	}
	if !isUserBundle(value) {
		fields = append(fields, apperrors.Field("body.value", "must be a known bundle"))
	}
	if !s.isDeliverableChannel(channel) {
		fields = append(fields, apperrors.Field("body.channel", "must be a deliverable channel"))
	}
	if len(fields) > 0 {
		return apperrors.Validation("invalid preference", fields...).Op("NotificationService.SetPreference")
	}
	_, err := s.repo.UpsertPreference(ctx, repo.UpsertPreferenceParams{
		UserID:  userID,
		Scope:   scope,
		Value:   value,
		Channel: channel,
		Enabled: enabled,
	})
	return err
}

// isUserBundle reports whether name is a known user-audience bundle.
func isUserBundle(name string) bool {
	for _, b := range notifications.UserBundles() {
		if b.Name == name {
			return true
		}
	}
	return false
}

// isDeliverableChannel reports whether channel is one this service enqueues to.
func (s *NotificationService) isDeliverableChannel(channel string) bool {
	for _, ch := range s.channels {
		if string(ch) == channel {
			return true
		}
	}
	return false
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
