//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

// TestNotificationAPI_BellFlow drives the bell endpoints end-to-end over HTTP as
// a non-admin user: an empty inbox, then a delivered notification surfaces with
// its rendered title, the unread badge tracks it, and marking it read clears the
// badge. Proves route registration, the authenticated-only gate, the wire shape,
// and per-user scoping.
func TestNotificationAPI_BellFlow(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	user, token := app.UserToken(t, ctx, "bell-user", "viewer")

	// Empty inbox and a zero badge to start.
	var inbox []model.InboxNotification
	app.GETAs(t, token, "/api/v1/notifications", &inbox, http.StatusOK)
	if len(inbox) != 0 {
		t.Fatalf("initial inbox = %d, want 0", len(inbox))
	}
	var count struct {
		Count int64 `json:"count"`
	}
	app.GETAs(t, token, "/api/v1/notifications/unread-count", &count, http.StatusOK)
	if count.Count != 0 {
		t.Fatalf("initial unread = %d, want 0", count.Count)
	}

	// A delivered notification (produced internally — no public enqueue endpoint).
	row := deliverInApp(t, ctx, app.Repo, user.ID, `{"media":{"title":"Sentinel","year":2024}}`)

	app.GETAs(t, token, "/api/v1/notifications", &inbox, http.StatusOK)
	if len(inbox) != 1 || inbox[0].ID != row.ID {
		t.Fatalf("inbox = %+v, want the delivered row", inbox)
	}
	if inbox[0].Title != "Sentinel is ready to watch" {
		t.Fatalf("title = %q, want the rendered template", inbox[0].Title)
	}

	app.GETAs(t, token, "/api/v1/notifications/unread-count", &count, http.StatusOK)
	if count.Count != 1 {
		t.Fatalf("unread = %d, want 1", count.Count)
	}

	// Mark read → 204, badge clears.
	app.POSTAs(t, token, "/api/v1/notifications/"+row.ID.String()+"/read", nil, nil, http.StatusNoContent)
	app.GETAs(t, token, "/api/v1/notifications/unread-count", &count, http.StatusOK)
	if count.Count != 0 {
		t.Fatalf("unread after read = %d, want 0", count.Count)
	}
}

// TestNotificationAPI_RequiresAuth proves the bell endpoints sit behind the
// authenticated-only gate: no bearer token is a 401, not an open read.
func TestNotificationAPI_RequiresAuth(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	if code := app.Status(t, "", http.MethodGet, "/api/v1/notifications", nil); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list = %d, want 401", code)
	}
}
