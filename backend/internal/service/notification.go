package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/authz"
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
// typed notifications.Event into outbox rows. Producers resolve recipients (the
// requester for user-audience events; the users permitted to decide a request
// for admin-audience ones), Enqueue checks each recipient's per-channel
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
	// authz answers "may this user decide requests?", which is who the
	// admin-audience bundles and events belong to.
	authz *AuthzService
}

func NewNotificationService(r *repo.Repository, az *AuthzService) *NotificationService {
	return &NotificationService{
		repo:             r,
		authz:            az,
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
			ID:         row.ID,
			EventType:  row.EventType,
			Title:      title,
			Body:       body,
			Payload:    row.Payload,
			CreatedAt:  row.CreatedAt,
			ReadAt:     row.ReadAt,
			ResolvedAt: row.ResolvedAt,
			Resolution: row.Resolution,
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

// NotifyRequestPending enqueues request.pending to every active user who may
// decide the request (holds the approve or deny key for its media type), except
// the requester. media is the title the request is for; the caller resolves it
// because a pending request has no media item yet.
func (s *NotificationService) NotifyRequestPending(ctx context.Context, req model.Request, media notifications.MediaRef) error {
	requester, err := s.repo.GetUser(ctx, req.RequestedBy)
	if err != nil {
		return err
	}
	users, err := s.repo.ListUsers(ctx)
	if err != nil {
		return err
	}
	var deciders []uuid.UUID
	for _, u := range users {
		if !u.IsActive || u.ID == req.RequestedBy {
			continue
		}
		ok, err := s.mayDecide(ctx, u.ID, model.MediaType(req.Type))
		if err != nil {
			return err
		}
		if ok {
			deciders = append(deciders, u.ID)
		}
	}
	return s.Enqueue(ctx, notifications.RequestPending{
		Deciders:  deciders,
		RequestID: req.ID,
		Media:     media,
		Requester: requester.Username,
	})
}

// NotifyRequestApproved tells the requester an operator approved their request.
func (s *NotificationService) NotifyRequestApproved(ctx context.Context, req model.Request, media notifications.MediaRef) error {
	return s.Enqueue(ctx, notifications.RequestApproved{
		Recipient: req.RequestedBy,
		RequestID: req.ID,
		Media:     media,
	})
}

// NotifyRequestDenied tells the requester an operator declined their request,
// carrying the operator's reason.
func (s *NotificationService) NotifyRequestDenied(ctx context.Context, req model.Request, media notifications.MediaRef) error {
	reason := ""
	if req.DeniedReason != nil {
		reason = *req.DeniedReason
	}
	return s.Enqueue(ctx, notifications.RequestDenied{
		Recipient: req.RequestedBy,
		RequestID: req.ID,
		Media:     media,
		Reason:    reason,
	})
}

// ResolveRequestPending clears a request's request.pending from every reviewer's
// attention once the request stops being pending: each recipient's bell row is
// resolved with the outcome, and push/email rows not yet sent are superseded so
// no one is alerted afterwards. A push already delivered is left alone — it can't
// be reliably withdrawn, and its tap target (the approval queue) no longer lists
// the request.
func (s *NotificationService) ResolveRequestPending(ctx context.Context, requestID uuid.UUID, outcome model.NotificationResolution) error {
	key := notifications.RequestPendingDedupKey(requestID)
	return s.repo.InTx(ctx, func(r *repo.Repository) error {
		if _, err := r.ResolveInAppByDedupKey(ctx, key, outcome); err != nil {
			return err
		}
		_, err := r.SupersedeUndeliveredByDedupKey(ctx, key)
		return err
	})
}

// mayDecide reports whether the user may approve or deny a request of the given
// media type.
func (s *NotificationService) mayDecide(ctx context.Context, userID uuid.UUID, t model.MediaType) (bool, error) {
	for _, key := range []string{authz.RequestApprove(t), authz.RequestDeny(t)} {
		ok, err := s.authz.Can(ctx, userID, key, nil)
		if err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

// mayDecideAny reports whether the user may decide requests of any media type —
// the audience of the admin bundles.
func (s *NotificationService) mayDecideAny(ctx context.Context, userID uuid.UUID) (bool, error) {
	for _, t := range []model.MediaType{model.MediaTypeMovie, model.MediaTypeSeries} {
		ok, err := s.mayDecide(ctx, userID, t)
		if err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

// bundlesFor returns the preference bundles the user may see and toggle: every
// user-audience bundle, plus the admin-audience ones when the user may decide
// requests.
func (s *NotificationService) bundlesFor(ctx context.Context, userID uuid.UUID) ([]notifications.Bundle, error) {
	out := notifications.UserBundles()
	decider, err := s.mayDecideAny(ctx, userID)
	if err != nil {
		return nil, err
	}
	if decider {
		out = append(out, notifications.AdminBundles()...)
	}
	return out, nil
}

// Preferences returns the user's per-bundle preference view: one BundlePreference
// per bundle the user may see (bundlesFor), each carrying the resolved subscription (the master —
// in-app follows it) plus the resolved enablement of every outbound amplifier
// channel (s.outboundChannels). Resolution is a stored override else the in-code
// default. Available is left false — the handler fills channel deliverability,
// since the email-provider read is its concern, not the service's.
func (s *NotificationService) Preferences(ctx context.Context, userID uuid.UUID) ([]model.BundlePreference, error) {
	prefs, err := s.repo.ListPreferences(ctx, userID)
	if err != nil {
		return nil, err
	}
	bundles, err := s.bundlesFor(ctx, userID)
	if err != nil {
		return nil, err
	}
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
// channels ("email"/"push"). bundle must be one the user may see (bundlesFor). Invalid
// input collects every field problem into one Validation error; a valid write
// upserts just the targeted column (idempotent), leaving the rest to keep tracking
// their defaults.
func (s *NotificationService) SetPreference(ctx context.Context, userID uuid.UUID, bundle, target string, enabled bool) error {
	visible, err := s.maySeeBundle(ctx, userID, bundle)
	if err != nil {
		return err
	}
	var fields []apperrors.FieldError
	if !visible {
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

// maySeeBundle reports whether name is a bundle the user may see and toggle: any
// user-audience bundle, or an admin-audience one when the user may decide
// requests. An unknown bundle is false without consulting permissions.
func (s *NotificationService) maySeeBundle(ctx context.Context, userID uuid.UUID, name string) (bool, error) {
	if hasBundle(notifications.UserBundles(), name) {
		return true, nil
	}
	if hasBundle(notifications.AdminBundles(), name) {
		return s.mayDecideAny(ctx, userID)
	}
	return false, nil
}

// hasBundle reports whether name is one of bundles.
func hasBundle(bundles []notifications.Bundle, name string) bool {
	for _, b := range bundles {
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

	// Recipients are carried on the event: the producer resolved them (for the
	// admin audience, from permissions) before building it.
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

// EnqueueTransactionalEmail queues one email to a literal address, bypassing the
// preference gate entirely (there is no user row and no bundle — a transactional
// email must send, not be opt-in-gated). It performs the synchronous
// IsConfigured precheck the caller needs for immediate "emailed vs copy-the-link"
// feedback: with SMTP unconfigured it enqueues nothing and reports enqueued=false,
// so a doomed transactional row is never written. With SMTP configured it writes a
// single transactional email row the worker delivers (and, being transactional,
// never parks — see the worker). "configured" mirrors email.Manager.IsConfigured
// (a provider row exists and is enabled); it's inlined here to avoid a manager
// dependency on this enqueue-only service.
func (s *NotificationService) EnqueueTransactionalEmail(ctx context.Context, eventType, recipientEmail string, payload any) (enqueued bool, err error) {
	const op = "NotificationService.EnqueueTransactionalEmail"

	cfg, found, err := s.repo.GetEmailProvider(ctx)
	if err != nil {
		return false, err
	}
	if !found || !cfg.Enabled {
		return false, nil // SMTP not configured → caller falls back to the copyable link.
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return false, apperrors.Internalf("marshal %q payload: %v", eventType, err).Op(op)
	}
	if _, err := s.repo.EnqueueOutbox(ctx, repo.EnqueueOutboxParams{
		EventType:      eventType,
		Audience:       string(model.AudienceSystem),
		RecipientEmail: &recipientEmail,
		Channel:        string(model.ChannelEmail),
		Payload:        body,
		Transactional:  true,
	}); err != nil {
		return false, err
	}
	return true, nil
}

// RenderTestEmail renders the house-styled "send test email" probe (subject +
// HTML body). Unlike the other transactional emails this one is sent synchronously
// by the email-provider settings handler rather than enqueued, so it renders here
// through the shared renderer instead of going through the outbox.
func (s *NotificationService) RenderTestEmail() (subject, htmlBody string, err error) {
	return s.renderer.RenderEmail(notifications.EventEmailTest, nil)
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
