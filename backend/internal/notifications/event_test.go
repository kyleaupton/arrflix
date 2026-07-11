package notifications

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/model"
)

func bundlePref(bundle, channel string, enabled bool) model.NotificationPreference {
	return model.NotificationPreference{Scope: string(model.PreferenceScopeBundle), Value: bundle, Channel: channel, Enabled: enabled}
}

func eventPref(event, channel string, enabled bool) model.NotificationPreference {
	return model.NotificationPreference{Scope: string(model.PreferenceScopeEvent), Value: event, Channel: channel, Enabled: enabled}
}

// TestChannelEnabled locks the resolution precedence: event-scope row beats
// bundle-scope row beats the in-code bundle default, per channel.
func TestChannelEnabled(t *testing.T) {
	t.Parallel()

	const event = "want.available"
	cases := []struct {
		name    string
		prefs   []model.NotificationPreference
		bundle  string
		channel model.NotificationChannel
		want    bool
	}{
		{"default in_app on", nil, BundleMyRequests, model.ChannelInApp, true},
		{"default push on for my_requests", nil, BundleMyRequests, model.ChannelPush, true},
		{"default push off for library_activity", nil, BundleLibraryActivity, model.ChannelPush, false},
		{"default email off", nil, BundleMyRequests, model.ChannelEmail, false},
		{"bundle row overrides default", []model.NotificationPreference{bundlePref(BundleMyRequests, "in_app", false)}, BundleMyRequests, model.ChannelInApp, false},
		{"event row beats bundle row", []model.NotificationPreference{bundlePref(BundleMyRequests, "in_app", true), eventPref(event, "in_app", false)}, BundleMyRequests, model.ChannelInApp, false},
		{"event row beats bundle row (enabling)", []model.NotificationPreference{bundlePref(BundleMyRequests, "in_app", false), eventPref(event, "in_app", true)}, BundleMyRequests, model.ChannelInApp, true},
		{"other-channel row is ignored", []model.NotificationPreference{bundlePref(BundleMyRequests, "push", false)}, BundleMyRequests, model.ChannelInApp, true},
		{"unknown bundle fails closed", nil, "no_such_bundle", model.ChannelInApp, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ChannelEnabled(tc.prefs, event, tc.bundle, tc.channel); got != tc.want {
				t.Fatalf("ChannelEnabled(%s, %s) = %v, want %v", tc.bundle, tc.channel, got, tc.want)
			}
		})
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
