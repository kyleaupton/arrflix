package notifications

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/model"
)

func bp(b bool) *bool { return &b }

// pref builds a columnar (user, bundle) preference row; nil columns mean "no
// override, defer to the registry default".
func pref(bundle string, subscribed, email, push *bool) model.NotificationPreference {
	return model.NotificationPreference{Bundle: bundle, Subscribed: subscribed, Email: email, Push: push}
}

// TestSubscribed locks the master resolution: a stored 'subscribed' override wins,
// else the bundle's in-code default (true for user bundles); an unknown bundle
// fails closed.
func TestSubscribed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		prefs  []model.NotificationPreference
		bundle string
		want   bool
	}{
		{"default subscribed for my_requests", nil, BundleMyRequests, true},
		{"default subscribed for library_activity", nil, BundleLibraryActivity, true},
		{"override unsubscribes", []model.NotificationPreference{pref(BundleMyRequests, bp(false), nil, nil)}, BundleMyRequests, false},
		{"override re-subscribes", []model.NotificationPreference{pref(BundleMyRequests, bp(true), nil, nil)}, BundleMyRequests, true},
		{"row present but subscribed nil → default", []model.NotificationPreference{pref(BundleMyRequests, nil, bp(true), nil)}, BundleMyRequests, true},
		{"unknown bundle fails closed", nil, "no_such_bundle", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Subscribed(tc.prefs, tc.bundle); got != tc.want {
				t.Fatalf("Subscribed(%s) = %v, want %v", tc.bundle, got, tc.want)
			}
		})
	}
}

// TestChannelEnabled locks outbound resolution: a stored per-channel override wins
// over the in-code bundle default. in_app is not an outbound channel (Subscribed
// governs it), so it always resolves false here; an unknown bundle fails closed.
func TestChannelEnabled(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		prefs   []model.NotificationPreference
		bundle  string
		channel model.NotificationChannel
		want    bool
	}{
		{"default push on for my_requests", nil, BundleMyRequests, model.ChannelPush, true},
		{"default push off for library_activity", nil, BundleLibraryActivity, model.ChannelPush, false},
		{"default email off", nil, BundleMyRequests, model.ChannelEmail, false},
		{"push override off", []model.NotificationPreference{pref(BundleMyRequests, nil, nil, bp(false))}, BundleMyRequests, model.ChannelPush, false},
		{"email override on", []model.NotificationPreference{pref(BundleMyRequests, nil, bp(true), nil)}, BundleMyRequests, model.ChannelEmail, true},
		{"push column nil → default on", []model.NotificationPreference{pref(BundleMyRequests, nil, bp(true), nil)}, BundleMyRequests, model.ChannelPush, true},
		{"in_app is not an outbound channel", nil, BundleMyRequests, model.ChannelInApp, false},
		{"unknown bundle fails closed", nil, "no_such_bundle", model.ChannelPush, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ChannelEnabled(tc.prefs, tc.bundle, tc.channel); got != tc.want {
				t.Fatalf("ChannelEnabled(%s, %s) = %v, want %v", tc.bundle, tc.channel, got, tc.want)
			}
		})
	}
}

// TestUserBundles proves the seeding/read source of truth returns exactly the two
// user-audience bundles in stable (name-sorted) order and excludes the admin
// bundles.
func TestUserBundles(t *testing.T) {
	t.Parallel()

	got := UserBundles()
	// Sorted by name: "library_activity" < "my_requests".
	want := []string{BundleLibraryActivity, BundleMyRequests}
	if len(got) != len(want) {
		t.Fatalf("UserBundles() = %d bundles, want %d: %+v", len(got), len(want), got)
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Fatalf("UserBundles()[%d] = %q, want %q (stable name order)", i, got[i].Name, name)
		}
		if got[i].Audience != model.AudienceUser {
			t.Fatalf("UserBundles()[%d] audience = %q, want user", i, got[i].Audience)
		}
	}
}

// TestWantAvailable_EventContract checks the event metadata and that Payload
// marshals to just the template variables — the routing-only Recipient field is
// excluded.
func TestWantAvailable_EventContract(t *testing.T) {
	t.Parallel()

	ev := WantAvailable{
		Recipient: uuid.New(),
		Media:     MediaRef{Title: "Sentinel", Year: 2024},
		PlexLink:  "https://plex/watch/1",
	}
	if ev.EventType() != "want.available" || ev.Audience() != model.AudienceUser || ev.Bundle() != BundleMyRequests {
		t.Fatalf("metadata = %q/%s/%s", ev.EventType(), ev.Audience(), ev.Bundle())
	}
	if got := ev.Recipients(); len(got) != 1 || got[0] != ev.Recipient {
		t.Fatalf("recipients = %v, want [%s]", got, ev.Recipient)
	}

	raw, err := json.Marshal(ev.Payload())
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if _, leaked := decoded["Recipient"]; leaked {
		t.Fatalf("payload leaked routing field Recipient: %s", raw)
	}
	if _, ok := decoded["media"]; !ok {
		t.Fatalf("payload missing media: %s", raw)
	}
}
