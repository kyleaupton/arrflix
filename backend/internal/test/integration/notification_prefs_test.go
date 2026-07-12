//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/notifications"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

// prefsBody mirrors the GET /notifications/preferences response envelope.
type prefsBody struct {
	Bundles []model.BundlePreference `json:"bundles"`
}

// setPrefBody mirrors the PUT /notifications/preferences request.
type setPrefBody struct {
	Scope   string `json:"scope"`
	Value   string `json:"value"`
	Channel string `json:"channel"`
	Enabled bool   `json:"enabled"`
}

// channelPref returns a bundle's channel entry from a prefs response, failing the
// test if it isn't present.
func channelPref(t *testing.T, bundles []model.BundlePreference, bundle, channel string) model.ChannelPreference {
	t.Helper()
	for _, b := range bundles {
		if b.Bundle != bundle {
			continue
		}
		for _, c := range b.Channels {
			if c.Channel == channel {
				return c
			}
		}
	}
	t.Fatalf("channel %q not found in bundle %q: %+v", channel, bundle, bundles)
	return model.ChannelPreference{}
}

// TestNotificationPrefs_SeededOnCreate proves the user-create flow materializes a
// new user's default preferences: creating a user through UsersService writes
// bundle-scope rows matching the catalog defaults (my_requests: in_app on,
// email off).
func TestNotificationPrefs_SeededOnCreate(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	user, err := app.Services.Users.Create(ctx, "seeded@test.local", "seeded", "test-password-123", "requester", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	prefs, err := app.Repo.ListPreferences(ctx, user.ID)
	if err != nil {
		t.Fatalf("list preferences: %v", err)
	}
	if len(prefs) == 0 {
		t.Fatalf("no preferences seeded for new user")
	}

	byKey := map[string]bool{}
	for _, p := range prefs {
		if p.Scope != string(model.PreferenceScopeBundle) {
			t.Fatalf("seeded a non-bundle-scope row: %+v", p)
		}
		byKey[p.Value+"|"+p.Channel] = p.Enabled
	}

	if got, ok := byKey[notifications.BundleMyRequests+"|"+string(model.ChannelInApp)]; !ok || !got {
		t.Fatalf("my_requests/in_app = %v (present=%v), want enabled", got, ok)
	}
	if got, ok := byKey[notifications.BundleMyRequests+"|"+string(model.ChannelEmail)]; !ok || got {
		t.Fatalf("my_requests/email = %v (present=%v), want disabled", got, ok)
	}
}

// TestNotificationPrefs_GetReflectsState drives GET /notifications/preferences as
// a normal user: in_app is enabled and available; email is disabled and
// unavailable while SMTP is unconfigured; once an enabled SMTP provider is saved,
// email reads available (still disabled — availability is not enablement).
func TestNotificationPrefs_GetReflectsState(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	_, token := app.UserToken(t, ctx, "prefs-get", "viewer")

	var body prefsBody
	app.GETAs(t, token, "/api/v1/notifications/preferences", &body, http.StatusOK)

	inApp := channelPref(t, body.Bundles, notifications.BundleMyRequests, string(model.ChannelInApp))
	if !inApp.Enabled || !inApp.Available {
		t.Fatalf("in_app = %+v, want enabled+available", inApp)
	}
	email := channelPref(t, body.Bundles, notifications.BundleMyRequests, string(model.ChannelEmail))
	if email.Enabled || email.Available {
		t.Fatalf("email (no SMTP) = %+v, want disabled+unavailable", email)
	}

	// Configure an enabled SMTP provider → email becomes deliverable (available),
	// though still disabled by preference.
	seedEmailProvider(t, ctx, app.Repo, "localhost", 2525, true)

	app.GETAs(t, token, "/api/v1/notifications/preferences", &body, http.StatusOK)
	email = channelPref(t, body.Bundles, notifications.BundleMyRequests, string(model.ChannelEmail))
	if email.Enabled {
		t.Fatalf("email still disabled by pref, got enabled: %+v", email)
	}
	if !email.Available {
		t.Fatalf("email available after SMTP configured, got %+v", email)
	}
}

// TestNotificationPrefs_OptInLightsUpEmail is the slice's payoff: with email off
// (the default) enqueue writes no email row; after the user opts in via PUT,
// enqueue writes an email-channel outbox row for that user. This is what makes the
// email channel non-inert.
func TestNotificationPrefs_OptInLightsUpEmail(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	user, token := app.UserToken(t, ctx, "opt-in", "viewer")

	enqueue := func() {
		if err := app.Services.Notifications.Enqueue(ctx, notifications.WantAvailable{
			Recipient: user.ID,
			Media:     notifications.MediaRef{Title: "Sentinel", Year: 2024},
		}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}
	emailRows := func() int {
		due, err := app.Repo.ListDueOutbox(ctx, 100)
		if err != nil {
			t.Fatalf("list due: %v", err)
		}
		n := 0
		for _, row := range due {
			if row.Channel == string(model.ChannelEmail) && row.RecipientUserID != nil && *row.RecipientUserID == user.ID {
				n++
			}
		}
		return n
	}

	// Before opt-in: email defaults off, so no email row is written.
	enqueue()
	if n := emailRows(); n != 0 {
		t.Fatalf("email rows before opt-in = %d, want 0", n)
	}

	// Opt in over HTTP, then configure a deliverable SMTP provider.
	app.DoAs(t, token, http.MethodPut, "/api/v1/notifications/preferences", setPrefBody{
		Scope:   string(model.PreferenceScopeBundle),
		Value:   notifications.BundleMyRequests,
		Channel: string(model.ChannelEmail),
		Enabled: true,
	}, nil, http.StatusNoContent)
	seedEmailProvider(t, ctx, app.Repo, "localhost", 2525, true)

	// After opt-in: enqueue now writes an email row for the user.
	enqueue()
	if n := emailRows(); n != 1 {
		t.Fatalf("email rows after opt-in = %d, want 1", n)
	}
}

// TestNotificationPrefs_ScopedToCaller proves the prefs ops key off the caller's
// context user id, never a body/path id: user A's PUT and GET never read or write
// user B's preferences.
func TestNotificationPrefs_ScopedToCaller(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	_, tokenA := app.UserToken(t, ctx, "scoped-a", "viewer")
	userB, tokenB := app.UserToken(t, ctx, "scoped-b", "viewer")

	// A enables email on my_requests.
	app.DoAs(t, tokenA, http.MethodPut, "/api/v1/notifications/preferences", setPrefBody{
		Scope:   string(model.PreferenceScopeBundle),
		Value:   notifications.BundleMyRequests,
		Channel: string(model.ChannelEmail),
		Enabled: true,
	}, nil, http.StatusNoContent)

	// A's view reflects the opt-in.
	var aBody prefsBody
	app.GETAs(t, tokenA, "/api/v1/notifications/preferences", &aBody, http.StatusOK)
	if !channelPref(t, aBody.Bundles, notifications.BundleMyRequests, string(model.ChannelEmail)).Enabled {
		t.Fatalf("A's email should be enabled after opt-in")
	}

	// B's view is untouched (still the default: email off).
	var bBody prefsBody
	app.GETAs(t, tokenB, "/api/v1/notifications/preferences", &bBody, http.StatusOK)
	if channelPref(t, bBody.Bundles, notifications.BundleMyRequests, string(model.ChannelEmail)).Enabled {
		t.Fatalf("B's email should be unaffected by A's write")
	}

	// And no preference row exists for B at the DB level.
	bPrefs, err := app.Repo.ListPreferences(ctx, userB.ID)
	if err != nil {
		t.Fatalf("list B prefs: %v", err)
	}
	if len(bPrefs) != 0 {
		t.Fatalf("B has %d preference rows, want 0 (A's write must not touch B)", len(bPrefs))
	}
}

// TestNotificationPrefs_RequiresAuth proves the prefs ops sit behind the
// authenticated-only gate: no bearer token is a 401 on both read and write.
func TestNotificationPrefs_RequiresAuth(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	if code := app.Status(t, "", http.MethodGet, "/api/v1/notifications/preferences", nil); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET = %d, want 401", code)
	}
	if code := app.Status(t, "", http.MethodPut, "/api/v1/notifications/preferences", setPrefBody{
		Scope:   string(model.PreferenceScopeBundle),
		Value:   notifications.BundleMyRequests,
		Channel: string(model.ChannelInApp),
		Enabled: false,
	}); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated PUT = %d, want 401", code)
	}
}
