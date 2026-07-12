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

// Preference bundle names group event types for channel toggling. They are stable
// strings (stored in notification_preference.value at scope='bundle'); renaming one
// is a data migration, so treat them as frozen. v1 ships the two user-audience
// bundles; the admin bundles are declared for forward reference and light up with
// the admin audience (v1.1).
const (
	BundleMyRequests      = "my_requests"
	BundleLibraryActivity = "library_activity"
	BundleAdminAlerts     = "admin_alerts"
	BundleAdminSummaries  = "admin_summaries"
)

// Bundle is a static preference group: the audience it belongs to and its default
// channel enablement for a brand-new user who hasn't toggled anything. Defaults are
// the floor of the resolution chain (event pref → bundle pref → these).
type Bundle struct {
	Name     string
	Audience model.NotificationAudience
	Defaults map[model.NotificationChannel]bool
}

// bundles is the static bundle catalog. Defaults follow the spec: my_requests
// pushes by default, everything is on in-app, and email defaults off (inert until
// SMTP is configured). Only user-audience bundles are populated in v1.
var bundles = map[string]Bundle{
	BundleMyRequests: {
		Name:     BundleMyRequests,
		Audience: model.AudienceUser,
		Defaults: map[model.NotificationChannel]bool{
			model.ChannelInApp: true,
			model.ChannelPush:  true,
			model.ChannelEmail: false,
		},
	},
	BundleLibraryActivity: {
		Name:     BundleLibraryActivity,
		Audience: model.AudienceUser,
		Defaults: map[model.NotificationChannel]bool{
			model.ChannelInApp: true,
			model.ChannelPush:  false,
			model.ChannelEmail: false,
		},
	},
}

// UserBundles returns the user-audience bundles in a stable (name-sorted) order.
// It is the single source of truth for "which preference groups does a v1 user
// toggle?" — both SeedDefaults (materializing a new user's default rows) and the
// prefs read API iterate it, so a bundle added to the catalog with AudienceUser
// is seeded and surfaced without touching either caller. Admin-audience bundles
// are excluded until the admin audience lands (v1.1).
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

// ChannelEnabled resolves whether an event routes to a channel for a user, given
// that user's stored preferences. Precedence: an event-scope row wins outright;
// else a bundle-scope row; else the bundle's in-code default. An unknown bundle
// (no catalog entry) resolves false — fail closed rather than spam.
func ChannelEnabled(prefs []model.NotificationPreference, eventType, bundle string, ch model.NotificationChannel) bool {
	channel := string(ch)
	var bundleRow *bool
	for i := range prefs {
		p := prefs[i]
		if p.Channel != channel {
			continue
		}
		switch {
		case p.Scope == string(model.PreferenceScopeEvent) && p.Value == eventType:
			return p.Enabled // event-scope override wins outright
		case p.Scope == string(model.PreferenceScopeBundle) && p.Value == bundle:
			enabled := p.Enabled
			bundleRow = &enabled
		}
	}
	if bundleRow != nil {
		return *bundleRow
	}
	if b, ok := bundles[bundle]; ok {
		return b.Defaults[ch]
	}
	return false
}

// MediaRef is the compact media descriptor events embed for templates and the
// bell-icon UI — enough to render a title line and a thumbnail without a lookup.
type MediaRef struct {
	Title      string `json:"title"`
	Year       int    `json:"year,omitempty"`
	PosterPath string `json:"posterPath,omitempty"`
}
