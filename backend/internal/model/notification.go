package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Notification audiences, channels, delivery statuses, and preference scopes. The
// DB stores each as TEXT + CHECK; these typed consts give services and tests safe
// names. The full vocabulary lives here even though v1 exercises only the `user`
// audience, the `in_app` channel, and the queued→delivering→delivered/dead
// statuses — later slices (admin audience, push, email, the resolution lifecycle)
// light up the rest.
type (
	NotificationAudience string
	NotificationChannel  string
	NotificationStatus   string
	PreferenceScope      string
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

	PreferenceScopeBundle PreferenceScope = "bundle"
	PreferenceScopeEvent  PreferenceScope = "event"
)

// NotificationOutbox is the domain shape for a notification_outbox row — one
// delivery attempt and, once delivered on the in_app channel, one bell-icon
// history entry. RecipientUserID is nil only for the system-audience literal-email
// recipients v1 doesn't yet produce. DeliveredAt/ReadAt are nil until the worker
// delivers and the user reads. Mirrors dbgen.NotificationOutbox.
type NotificationOutbox struct {
	ID              uuid.UUID       `json:"id"`
	EventType       string          `json:"eventType"`
	Audience        string          `json:"audience"`
	RecipientUserID *uuid.UUID      `json:"recipientUserId"`
	Channel         string          `json:"channel"`
	Payload         json.RawMessage `json:"payload"`
	DedupKey        *string         `json:"dedupKey"`
	Status          string          `json:"status"`
	Attempts        int32           `json:"attempts"`
	NextAttemptAt   time.Time       `json:"nextAttemptAt"`
	LastError       *string         `json:"lastError"`
	CreatedAt       time.Time       `json:"createdAt"`
	DeliveredAt     *time.Time      `json:"deliveredAt"`
	ReadAt          *time.Time      `json:"readAt"`
}

// NotificationPreference is the domain shape for a notification_preference row —
// one channel toggle at bundle or event scope. Value is a bundle name when
// Scope is 'bundle', an event-type string when 'event'. Mirrors
// dbgen.NotificationPreference.
type NotificationPreference struct {
	UserID    uuid.UUID `json:"userId"`
	Scope     string    `json:"scope"`
	Value     string    `json:"value"`
	Channel   string    `json:"channel"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
