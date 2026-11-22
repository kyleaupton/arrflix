package notifications

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/model"
)

// TestAdminBundles proves admin_alerts is a live admin-audience bundle,
// subscribed by default with push on and email off, and that it stays out of
// UserBundles.
func TestAdminBundles(t *testing.T) {
	t.Parallel()

	got := AdminBundles()
	if len(got) != 1 || got[0].Name != BundleAdminAlerts || got[0].Audience != model.AudienceAdmin {
		t.Fatalf("AdminBundles() = %+v, want only admin_alerts", got)
	}
	for _, b := range UserBundles() {
		if b.Name == BundleAdminAlerts {
			t.Fatalf("UserBundles() includes admin_alerts")
		}
	}
	if !Subscribed(nil, BundleAdminAlerts) {
		t.Errorf("admin_alerts not subscribed by default")
	}
	if !ChannelEnabled(nil, BundleAdminAlerts, model.ChannelPush) {
		t.Errorf("admin_alerts push off by default, want on")
	}
	if ChannelEnabled(nil, BundleAdminAlerts, model.ChannelEmail) {
		t.Errorf("admin_alerts email on by default, want off")
	}
}

// TestRequestEvents_Contract checks each request event's routing metadata and
// its request:<id>:<event> dedup key, and that routing-only fields stay out of
// the payload.
func TestRequestEvents_Contract(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	user := uuid.New()
	cases := []struct {
		ev       Event
		typ      string
		audience model.NotificationAudience
		bundle   string
	}{
		{RequestPending{Deciders: []uuid.UUID{user}, RequestID: id}, "request.pending", model.AudienceAdmin, BundleAdminAlerts},
		{RequestApproved{Recipient: user, RequestID: id}, "request.approved", model.AudienceUser, BundleMyRequests},
		{RequestDenied{Recipient: user, RequestID: id, Reason: "no"}, "request.denied", model.AudienceUser, BundleMyRequests},
	}
	for _, c := range cases {
		t.Run(c.typ, func(t *testing.T) {
			t.Parallel()
			if c.ev.EventType() != c.typ || c.ev.Audience() != c.audience || c.ev.Bundle() != c.bundle {
				t.Fatalf("metadata = %q/%s/%s", c.ev.EventType(), c.ev.Audience(), c.ev.Bundle())
			}
			if got := c.ev.Recipients(); len(got) != 1 || got[0] != user {
				t.Fatalf("recipients = %v, want [%s]", got, user)
			}
			if want := "request:" + id.String() + ":" + c.typ; c.ev.DedupKey() != want {
				t.Fatalf("dedup key = %q, want %q", c.ev.DedupKey(), want)
			}
			raw, err := json.Marshal(c.ev.Payload())
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			for _, leaked := range []string{"Recipient", "Deciders"} {
				if _, ok := decoded[leaked]; ok {
					t.Fatalf("payload leaked routing field %s: %s", leaked, raw)
				}
			}
		})
	}
}

// TestRequestEvents_Render proves the spec copy on the text channels: the
// pending title carries the year only when known, and the denial body is the
// operator's reason.
func TestRequestEvents_Render(t *testing.T) {
	t.Parallel()

	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	cases := []struct {
		name      string
		ev        Event
		wantTitle string
		wantBody  string
	}{
		{"pending with year", RequestPending{Media: MediaRef{Title: "Dune", Year: 2021}, Requester: "alice"},
			"New request: Dune (2021)", "From alice. Tap to review."},
		{"pending without year", RequestPending{Media: MediaRef{Title: "Dune"}, Requester: "alice"},
			"New request: Dune", "From alice. Tap to review."},
		{"approved", RequestApproved{Media: MediaRef{Title: "Dune"}},
			"Your request for Dune was approved", "We'll let you know when it's ready."},
		{"denied", RequestDenied{Media: MediaRef{Title: "Dune"}, Reason: "Already in 4K."},
			"Your request for Dune was declined", "Already in 4K."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			payload, _ := json.Marshal(c.ev.Payload())
			for _, ch := range []model.NotificationChannel{model.ChannelInApp, model.ChannelPush} {
				title, body, err := r.Render(c.ev.EventType(), ch, payload)
				if err != nil {
					t.Fatalf("render %s: %v", ch, err)
				}
				if title != c.wantTitle || body != c.wantBody {
					t.Fatalf("%s = %q / %q, want %q / %q", ch, title, body, c.wantTitle, c.wantBody)
				}
			}
			if _, _, err := r.RenderEmail(c.ev.EventType(), payload); err != nil {
				t.Fatalf("render email: %v", err)
			}
		})
	}
}
