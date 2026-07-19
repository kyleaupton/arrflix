package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Notification audiences, channels, and delivery statuses. The DB stores each as
// TEXT + CHECK; these typed consts give services and tests safe names. The full
// vocabulary lives here even though v1 exercises only the `user` audience and the
// queued→delivering→delivered/dead statuses — later slices (admin audience, the
// resolution lifecycle) light up the rest.
//
// ChannelInApp is deliberately NOT a preference the user toggles: it is the
// inherent face of a bundle subscription (see BundlePreference). It remains a
// delivery channel (the bell's outbox rows carry channel='in_app') and an adapter
// name, but preferences model it as "subscribed", not as a switchable channel.
type (
	NotificationAudience string
	NotificationChannel  string
	NotificationStatus   string
)

const (
	AudienceUser   NotificationAudience = "user"
	AudienceAdmin  NotificationAudience = "admin"
	AudienceSystem NotificationAudience = "system"

	ChannelPush  NotificationChannel = "push"
	ChannelInApp NotificationChannel = "in_app"
	ChannelEmail NotificationChannel = "email"

	OutboxQueued         NotificationStatus = "queued"
	OutboxDelivering     NotificationStatus = "delivering"
	OutboxDelivered      NotificationStatus = "delivered"
	OutboxFailed         NotificationStatus = "failed"
	OutboxDead           NotificationStatus = "dead"
	OutboxAwaitingConfig NotificationStatus = "awaiting_config"
	OutboxSuperseded     NotificationStatus = "superseded"
)

// NotificationOutbox is the domain shape for a notification_outbox row — one
// delivery attempt and, once delivered on the in_app channel, one bell-icon
// history entry. A row targets either a known user (RecipientUserID) or a literal
// address (RecipientEmail — a transactional email to someone with no account, e.g.
// an invitee); exactly one is set. Transactional marks a row that bypassed
// preference-gating at enqueue and must never park as awaiting_config at delivery.
// DeliveredAt/ReadAt are nil until the worker delivers and the user reads. ClaimedAt
// is the worker's claim stamp, meaningful only while Status is 'delivering' — it is
// how the crash-window reaper spots a row whose worker died mid-delivery. Mirrors
// dbgen.NotificationOutbox.
type NotificationOutbox struct {
	ID              uuid.UUID       `json:"id"`
	EventType       string          `json:"eventType"`
	Audience        string          `json:"audience"`
	RecipientUserID *uuid.UUID      `json:"recipientUserId"`
	RecipientEmail  *string         `json:"recipientEmail"`
	Channel         string          `json:"channel"`
	Payload         json.RawMessage `json:"payload"`
	DedupKey        *string         `json:"dedupKey"`
	Transactional   bool            `json:"transactional"`
	Status          string          `json:"status"`
	Attempts        int32           `json:"attempts"`
	NextAttemptAt   time.Time       `json:"nextAttemptAt"`
	LastError       *string         `json:"lastError"`
	CreatedAt       time.Time       `json:"createdAt"`
	ClaimedAt       *time.Time      `json:"claimedAt"`
	DeliveredAt     *time.Time      `json:"deliveredAt"`
	ReadAt          *time.Time      `json:"readAt"`
}

// InboxNotification is the bell-icon read shape: a delivered in_app outbox row
// with its title and body already rendered from the event's template, so the
// frontend renders text server-side (one templating syntax across the app) while
// Payload still carries the structured extras (poster path, deep link) a rich
// card wants. It is a read projection of NotificationOutbox, not a stored row.
type InboxNotification struct {
	ID        uuid.UUID       `json:"id"`
	EventType string          `json:"eventType"`
	Title     string          `json:"title"`
	Body      string          `json:"body"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"createdAt"`
	ReadAt    *time.Time      `json:"readAt,omitempty"`
}

// BundlePreference is the prefs read view for one preference bundle: whether the
// user is subscribed (the master — in-app follows it), plus the resolved state of
// the outbound amplifier channels (email, push). It is a resolved projection (row
// value or in-code default), not a stored row — the prefs UI renders one card per
// bundle with a master switch and a row per outbound channel. in_app is not a
// channel here; being subscribed IS being in the bell.
type BundlePreference struct {
	Bundle     string              `json:"bundle"`
	Subscribed bool                `json:"subscribed"`
	Channels   []ChannelPreference `json:"channels"`
}

// ChannelPreference is one outbound channel's state within a BundlePreference.
// Enabled is the resolved toggle (a stored row wins over the in-code default);
// Available is whether the channel can actually deliver right now (email ⇐ SMTP
// configured), so the UI can render an enabled-but-undeliverable channel
// distinctly.
type ChannelPreference struct {
	Channel   string `json:"channel"`
	Enabled   bool   `json:"enabled"`
	Available bool   `json:"available"`
}

// NotificationPreference is the domain shape for a notification_preference row —
// one row per (user, bundle) carrying the columnar toggles. Each flag is a *bool:
// nil means "no override, defer to the bundle's in-code registry default", so
// retuning a default reaches every user who never set that flag. Subscribed is the
// master (in-app follows it); Email/Push are the outbound amplifiers. Mirrors
// dbgen.NotificationPreference.
type NotificationPreference struct {
	UserID     uuid.UUID `json:"userId"`
	Bundle     string    `json:"bundle"`
	Subscribed *bool     `json:"subscribed"`
	Email      *bool     `json:"email"`
	Push       *bool     `json:"push"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
