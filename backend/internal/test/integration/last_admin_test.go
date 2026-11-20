//go:build integration

package integration

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

// TestUsers_LastActiveAdminCannotBeRemoved — demoting, deactivating or deleting
// a user is refused with 409 when it would leave the install with no active
// admin, and the user is left unchanged. An inactive admin doesn't count.
func TestUsers_LastActiveAdminCannotBeRemoved(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	admin, err := app.Repo.GetUserByLogin(ctx, "admin")
	if err != nil {
		t.Fatalf("lookup setup admin: %v", err)
	}
	adminPath := "/api/v1/users/" + admin.ID.String()

	assertRefusedLeavesSoleAdmin := func(t *testing.T) {
		t.Helper()
		t.Run("demote", func(t *testing.T) {
			var pd apperrors.ProblemDetails
			app.PUT(t, adminPath+"/role", map[string]any{"role": "requester"}, &pd, http.StatusConflict)
			assertSoleRole(t, app, admin.ID.String(), "admin")
		})
		t.Run("deactivate", func(t *testing.T) {
			var pd apperrors.ProblemDetails
			app.PUT(t, adminPath, map[string]any{
				"email":    *admin.Email,
				"username": admin.Username,
				"isActive": false,
			}, &pd, http.StatusConflict)
			var got model.User
			app.GET(t, adminPath, &got, http.StatusOK)
			if !got.IsActive {
				t.Fatalf("refused deactivation left the admin inactive")
			}
		})
		t.Run("delete", func(t *testing.T) {
			app.DELETE(t, adminPath, http.StatusConflict)
			app.GET(t, adminPath, nil, http.StatusOK)
		})
	}

	t.Run("sole admin", assertRefusedLeavesSoleAdmin)

	other, _ := app.UserToken(t, ctx, "second-admin", "admin")
	if _, err := setActive(ctx, app, other, false); err != nil {
		t.Fatalf("deactivating a second admin while another is active: %v", err)
	}

	t.Run("only other admin is inactive", assertRefusedLeavesSoleAdmin)

	t.Run("another active admin remains", func(t *testing.T) {
		if _, err := setActive(ctx, app, other, true); err != nil {
			t.Fatalf("reactivate second admin: %v", err)
		}
		if err := app.Services.Users.AssignRole(ctx, admin.ID, "requester"); err != nil {
			t.Fatalf("demote with another active admin: %v", err)
		}
		if err := app.Services.Users.AssignRole(ctx, admin.ID, "admin"); err != nil {
			t.Fatalf("re-promote: %v", err)
		}
		if _, err := setActive(ctx, app, admin, false); err != nil {
			t.Fatalf("deactivate with another active admin: %v", err)
		}
		// The setup admin is now inactive; the second admin is the last active one.
		if err := app.Services.Users.Delete(ctx, other.ID); !apperrors.IsConflict(err) {
			t.Fatalf("delete last active admin: err = %v, want Conflict", err)
		}
		if _, err := setActive(ctx, app, admin, true); err != nil {
			t.Fatalf("reactivate setup admin: %v", err)
		}
		if err := app.Services.Users.Delete(ctx, other.ID); err != nil {
			t.Fatalf("delete with another active admin: %v", err)
		}
	})

	t.Run("non-admins are unaffected", func(t *testing.T) {
		requester, _ := app.UserToken(t, ctx, "plain-requester", "requester")
		if _, err := setActive(ctx, app, requester, false); err != nil {
			t.Fatalf("deactivate requester: %v", err)
		}
		if err := app.Services.Users.Delete(ctx, requester.ID); err != nil {
			t.Fatalf("delete requester: %v", err)
		}
	})
}

// TestUsers_ConcurrentDemotionsKeepAnAdmin — two admins demoting each other at
// the same moment can't both succeed: the check and the change share a
// transaction that serializes against other admin-affecting changes.
func TestUsers_ConcurrentDemotionsKeepAnAdmin(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	admin, err := app.Repo.GetUserByLogin(ctx, "admin")
	if err != nil {
		t.Fatalf("lookup setup admin: %v", err)
	}

	for i := range 10 {
		other, _ := app.UserToken(t, ctx, "racer-"+uuid.NewString()[:8], "admin")
		ids := []uuid.UUID{admin.ID, other.ID}
		errs := make([]error, len(ids))

		var wg sync.WaitGroup
		for j, id := range ids {
			wg.Go(func() {
				if j%2 == 0 {
					errs[j] = app.Services.Users.AssignRole(ctx, id, "requester")
				} else {
					u, err := app.Services.Users.Get(ctx, id)
					if err != nil {
						errs[j] = err
						return
					}
					_, errs[j] = setActive(ctx, app, u, false)
				}
			})
		}
		wg.Wait()

		succeeded := 0
		for _, err := range errs {
			switch {
			case err == nil:
				succeeded++
			case !apperrors.IsConflict(err):
				t.Fatalf("round %d: unexpected error: %v", i, err)
			}
		}
		if succeeded != 1 {
			t.Fatalf("round %d: %d of 2 concurrent removals succeeded, want exactly 1 (errs = %v)", i, succeeded, errs)
		}

		// Restore the setup admin as the sole active admin for the next round.
		if err := app.Services.Users.AssignRole(ctx, admin.ID, "admin"); err != nil {
			t.Fatalf("round %d: restore admin role: %v", i, err)
		}
		if err := app.Services.Users.Delete(ctx, other.ID); err != nil {
			t.Fatalf("round %d: delete racer: %v", i, err)
		}
	}
}

func setActive(ctx context.Context, app *testapp.App, u model.User, active bool) (model.User, error) {
	return app.Services.Users.Update(ctx, u.ID, *u.Email, u.Username, active)
}
