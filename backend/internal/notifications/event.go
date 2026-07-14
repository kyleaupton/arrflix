// Package notifications is the pure registry and routing core of arrflix's
// notification system: the typed event catalog, the preference bundles, and the
// pure resolution of "does this event route to this channel for this user?"
//
// It is pure in the domain-module sense — it imports only model, google/uuid, and
// stdlib. Persistence (writing outbox rows, reading a user's preferences) and
// recipient resolution live on NotificationService in internal/service; this
// package holds no repo and no state. The set of Event types here IS the registry:
// a producer can't emit a notification without a typed event, the compiler enforces
// its payload, and grep finds every event.
package notifications

import (
	"sort"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/model"
)

// Preference bundle names group event types for toggling. They are stable strings
// (stored in notification_preference.bundle); renaming one is a data migration, so
// treat them as frozen. v1 ships the two user-audience bundles; the admin bundles
// are declared for forward reference and light up with the admin audience (v1.1).
const (
	BundleMyRequests      = "my_requests"
	BundleLibraryActivity = "library_activity"
	BundleAdminAlerts     = "admin_alerts"
	BundleAdminSummaries  = "admin_summaries"
)

// Bundle is a static preference group. Subscribed is whether a brand-new user is
// subscribed to the bundle before touching anything — the master toggle's default,
// and the floor for in-app (subscribed ⇒ the bell shows it). Defaults holds the
// per-outbound-channel default (email, push) for a subscribed user; in_app is not a
// channel here. Both are the floor of the resolution chain (stored override → these).
type Bundle struct {
	Name       string
	Audience   model.NotificationAudience
	Subscribed bool
	Defaults   map[model.NotificationChannel]bool
}

// bundles is the static bundle catalog. Every user-audience bundle is subscribed by
// default (a new user is in the bell for it). Outbound defaults follow the spec:
// my_requests pushes by default; library_activity does not; email defaults off
// everywhere (inert until SMTP is configured). Only user-audience bundles are
// populated in v1.
var bundles = map[string]Bundle{
	BundleMyRequests: {
		Name:       BundleMyRequests,
		Audience:   model.AudienceUser,
		Subscribed: true,
		Defaults: map[model.NotificationChannel]bool{
			model.ChannelPush:  true,
			model.ChannelEmail: false,
		},
	},
	BundleLibraryActivity: {
		Name:       BundleLibraryActivity,
		Audience:   model.AudienceUser,
		Subscribed: true,
		Defaults: map[model.NotificationChannel]bool{
			model.ChannelPush:  false,
			model.ChannelEmail: false,
		},
	},
}

// UserBundles returns the user-audience bundles in a stable (name-sorted) order.
// It is the single source of truth for "which preference groups does a v1 user
// toggle?" — the prefs read API iterates it, so a bundle added to the catalog with
// AudienceUser is surfaced without touching the caller. Admin-audience bundles are
// excluded until the admin audience lands (v1.1).
func UserBundles() []Bundle {
	out := make([]Bundle, 0, len(bundles))
	for _, b := range bundles {
		if b.Audience == model.AudienceUser {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Event is the typed unit a producer hands NotificationService.Enqueue. Each
// concrete type carries its payload as exported fields and declares immutable
// routing metadata via these methods. Routing-only fields (recipient, dedup key)
// are tagged json:"-" so Payload marshals to just the template variables.
type Event interface {
	// EventType is the stable dotted identifier, e.g. "want.available". It keys
	// templates and event-scope preferences.
	EventType() string
	// Audience fixes the recipient-resolution strategy. v1 events are all
	// AudienceUser.
	Audience() model.NotificationAudience
	// Bundle is the preference group this event toggles under.
	Bundle() string
	// Recipients is the user-audience target set (one or more users). Empty for
	// non-user audiences, whose recipients NotificationService resolves itself.
	Recipients() []uuid.UUID
	// DedupKey is the optional coalescing key; "" means no coalescing.
	DedupKey() string
	// Payload is marshaled to the outbox payload JSONB and is exactly the set of
	// variables a template for this event may reference.
	Payload() any
}

// prefFor returns the user's stored row for a bundle, or nil if they've never
// overridden anything in it (the lazy case — resolution falls back to defaults).
func prefFor(prefs []model.NotificationPreference, bundle string) *model.NotificationPreference {
	for i := range prefs {
		if prefs[i].Bundle == bundle {
			return &prefs[i]
		}
	}
	return nil
}

// Subscribed resolves whether the user is subscribed to a bundle — the master
// toggle, and the floor for in-app (subscribed ⇒ the bell shows the bundle's
// events). Precedence: a stored subscribed override, else the bundle's in-code
// default. An unknown bundle resolves false — fail closed rather than spam.
func Subscribed(prefs []model.NotificationPreference, bundle string) bool {
	b, ok := bundles[bundle]
	if !ok {
		return false
	}
	if p := prefFor(prefs, bundle); p != nil && p.Subscribed != nil {
		return *p.Subscribed
	}
	return b.Subscribed
}

// ChannelEnabled resolves whether an outbound channel (email or push) is enabled
// for a bundle. Precedence: a stored per-channel override, else the bundle's
// in-code default. It is the amplifier question only — in_app is governed by
// Subscribed, not here — and an unknown bundle resolves false (fail closed).
//
// This is orthogonal to Subscribed: a channel can read enabled while the bundle is
// unsubscribed (the stored toggle is remembered), so Enqueue must gate on Subscribed
// first, then consult ChannelEnabled for the outbound fan-out.
func ChannelEnabled(prefs []model.NotificationPreference, bundle string, ch model.NotificationChannel) bool {
	b, ok := bundles[bundle]
	if !ok {
		return false
	}
	if p := prefFor(prefs, bundle); p != nil {
		if v := channelOverride(p, ch); v != nil {
			return *v
		}
	}
	return b.Defaults[ch]
}

// channelOverride returns the stored *bool for an outbound channel column, or nil
// (no override) for a nil column or a non-outbound channel.
func channelOverride(p *model.NotificationPreference, ch model.NotificationChannel) *bool {
	switch ch {
	case model.ChannelEmail:
		return p.Email
	case model.ChannelPush:
		return p.Push
	default:
		return nil
	}
}

// MediaRef is the compact media descriptor events embed for templates and the
// bell-icon UI — enough to render a title line and a thumbnail without a lookup.
type MediaRef struct {
	Title      string `json:"title"`
	Year       int    `json:"year,omitempty"`
	PosterPath string `json:"posterPath,omitempty"`
}
