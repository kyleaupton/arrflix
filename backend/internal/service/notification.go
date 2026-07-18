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
	// outboundChannels is the set of amplifier channels enqueue fans out to once a
	// recipient is subscribed to the event's bundle. in_app is NOT here: it is the
	// subscription itself and is written unconditionally for a subscribed recipient.
	// Email rows are written only for users who opt in (email defaults off in the
	// bundle catalog); push defaults on for my_requests, but a recipient with no
	// registered subscription is a delivery no-op at the adapter, so lighting up
	// either channel here is safe — an unconfigured SMTP relay parks mail as
	// awaiting_config, and a subscription-less push settles delivered.
	outboundChannels []model.NotificationChannel
	// renderer renders the bell-icon read projection (title + body). The worker
	// holds its own renderer for delivery-side needs; this one serves the read
	// path. Both parse the same embedded templates.
	renderer *notifications.Renderer
}

func NewNotificationService(r *repo.Repository) *NotificationService {
	return &NotificationService{
		repo:             r,
		outboundChannels: []model.NotificationChannel{model.ChannelEmail, model.ChannelPush},
		renderer:         notifications.MustNewRenderer(),
	}
}

// RegisterPushSubscription binds (or refreshes) a browser's Web Push subscription
// to the caller's current session. Clearing any prior row for the session or the
// endpoint first enforces the 1:1 session<->push invariant and re-homes an
// endpoint when the same browser signs into a new session.
func (s *NotificationService) RegisterPushSubscription(ctx context.Context, sessionID uuid.UUID, endpoint, p256dh, auth string, userAgent *string) error {
	const op = "NotificationService.RegisterPushSubscription"
	if endpoint == "" || p256dh == "" || auth == "" {
		return apperrors.Validation("push subscription requires endpoint, p256dh, and auth").Op(op)
	}
	// Clear + insert run in one transaction so they can't half-apply: a failed
	// insert after a committed clear would leave the device with its prior push
	// row deleted and no replacement — push silently off until re-subscribe.
	return s.repo.InTx(ctx, func(r *repo.Repository) error {
		if _, err := r.ClearPushForSessionOrEndpoint(ctx, sessionID, endpoint); err != nil {
			return err
		}
		_, err := r.InsertPushSubscription(ctx, repo.InsertPushSubscriptionParams{
			SessionID: sessionID,
			Endpoint:  endpoint,
			P256dh:    p256dh,
			Auth:      auth,
			UserAgent: userAgent,
		})
		return err
	})
}

// GetPushSubscription loads one of the caller's own devices by id, returning a
// NotFound when the id isn't theirs (or doesn't exist) — the owner scope is
// enforced in the query, so this never reveals another user's subscription.
func (s *NotificationService) GetPushSubscription(ctx context.Context, userID, id uuid.UUID) (model.PushSubscription, error) {
	sub, found, err := s.repo.GetPushSubscriptionByIDForUser(ctx, userID, id)
	if err != nil {
		return model.PushSubscription{}, err
	}
	if !found {
		return model.PushSubscription{}, apperrors.NotFoundf("push subscription %s not found", id).
			Op("NotificationService.GetPushSubscription")
	}
	return sub, nil
}

// RemovePushSubscription deletes one of the caller's devices by id, scoped to the
// owner. Idempotent — a missing row is not an error (the browser may have already
// been pruned server-side after a 410, or removed from another device).
func (s *NotificationService) RemovePushSubscription(ctx context.Context, userID, id uuid.UUID) error {
	_, err := s.repo.DeletePushSubscriptionByIDForUser(ctx, userID, id)
	return err
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

	media := notifications.MediaRef{Title: item.Title, Type: model.MediaType(item.Type)}
	if item.Year != nil {
		media.Year = int(*item.Year)
	}
	if item.TmdbID != nil {
		media.TmdbID = *item.TmdbID
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

// Preferences returns the user's per-bundle preference view: one BundlePreference
// per user-audience bundle, each carrying the resolved subscription (the master —
// in-app follows it) plus the resolved enablement of every outbound amplifier
// channel (s.outboundChannels). Resolution is a stored override else the in-code
// default. Available is left false — the handler fills channel deliverability,
// since the email-provider read is its concern, not the service's.
func (s *NotificationService) Preferences(ctx context.Context, userID uuid.UUID) ([]model.BundlePreference, error) {
	prefs, err := s.repo.ListPreferences(ctx, userID)
	if err != nil {
		return nil, err
	}
	bundles := notifications.UserBundles()
	out := make([]model.BundlePreference, 0, len(bundles))
	for _, bundle := range bundles {
		channels := make([]model.ChannelPreference, 0, len(s.outboundChannels))
		for _, ch := range s.outboundChannels {
			channels = append(channels, model.ChannelPreference{
				Channel: string(ch),
				Enabled: notifications.ChannelEnabled(prefs, bundle.Name, ch),
			})
		}
		out = append(out, model.BundlePreference{
			Bundle:     bundle.Name,
			Subscribed: notifications.Subscribed(prefs, bundle.Name),
			Channels:   channels,
		})
	}
	return out, nil
}

// Preference targets a SetPreference write may address.
const (
	prefTargetSubscribed = "subscribed"
)

// SetPreference writes one preference toggle for the user: the bundle's
// subscription master ("subscribed", which governs in-app) or one of its outbound
// channels ("email"/"push"). value must be a user-audience bundle name. Invalid
// input collects every field problem into one Validation error; a valid write
// upserts just the targeted column (idempotent), leaving the rest to keep tracking
// their defaults.
func (s *NotificationService) SetPreference(ctx context.Context, userID uuid.UUID, bundle, target string, enabled bool) error {
	var fields []apperrors.FieldError
	if !isUserBundle(bundle) {
		fields = append(fields, apperrors.Field("body.bundle", "must be a known bundle"))
	}
	if !s.isValidTarget(target) {
		fields = append(fields, apperrors.Field("body.target", "must be 'subscribed', 'email', or 'push'"))
	}
	if len(fields) > 0 {
		return apperrors.Validation("invalid preference", fields...).Op("NotificationService.SetPreference")
	}
	return s.repo.SetBundlePreference(ctx, userID, bundle, target, enabled)
}

// isValidTarget reports whether target names the subscription master or a known
// outbound channel — the fields SetPreference may write.
func (s *NotificationService) isValidTarget(target string) bool {
	if target == prefTargetSubscribed {
		return true
	}
	for _, ch := range s.outboundChannels {
		if string(ch) == target {
			return true
		}
	}
	return false
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

// Enqueue writes outbox rows for an event. For each recipient subscribed to the
// event's bundle it writes the in_app row unconditionally (the subscription is the
// bell), then one row per enabled outbound channel. An unsubscribed recipient gets
// nothing. It is the single entry point producers use — there is no path to the
// outbox that doesn't go through a typed Event.
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
		if !notifications.Subscribed(prefs, ev.Bundle()) {
			continue // unsubscribed from the bundle → silent everywhere, in-app included.
		}
		// Subscribed ⇒ the bell always gets it, plus each opted-in outbound channel.
		channels := append([]model.NotificationChannel{model.ChannelInApp}, s.enabledOutbound(prefs, ev.Bundle())...)
		for _, ch := range channels {
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

// enabledOutbound returns the outbound channels enabled for a bundle given the
// recipient's resolved preferences.
func (s *NotificationService) enabledOutbound(prefs []model.NotificationPreference, bundle string) []model.NotificationChannel {
	out := make([]model.NotificationChannel, 0, len(s.outboundChannels))
	for _, ch := range s.outboundChannels {
		if notifications.ChannelEnabled(prefs, bundle, ch) {
			out = append(out, ch)
		}
	}
	return out
}
