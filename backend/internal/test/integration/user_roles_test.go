//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
	"github.com/kyleaupton/arrflix/internal/token"
)

// TestReqRole001_OnlyRequesterAndAdminAreAssignable — REQ-ROLE-001 — Every user
// holds exactly one role, requester or admin. Inviting, creating or assigning a
// user with any other role is refused with 422 naming the role field, and an
// unknown role never results in a user with no role.
func TestReqRole001_OnlyRequesterAndAdminAreAssignable(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	refused := []string{"viewer", "co_admin", "user", "superuser"}

	t.Run("invite", func(t *testing.T) {
		for _, role := range refused {
			var pd apperrors.ProblemDetails
			app.POST(t, "/api/v1/invites", map[string]any{
				"email": "invitee-" + role + "@test.local",
				"role":  role,
			}, &pd, http.StatusUnprocessableEntity)
			findFieldError(t, pd, "body.role")
		}
		for _, role := range []string{"requester", "admin"} {
			app.POST(t, "/api/v1/invites", map[string]any{
				"email": "invitee-" + role + "@test.local",
				"role":  role,
			}, nil, http.StatusCreated)
		}
	})

	t.Run("assign", func(t *testing.T) {
		user, _ := app.UserToken(t, ctx, "assignee", "requester")
		path := "/api/v1/users/" + user.ID.String() + "/role"
		for _, role := range refused {
			var pd apperrors.ProblemDetails
			app.PUT(t, path, map[string]any{"role": role}, &pd, http.StatusUnprocessableEntity)
			findFieldError(t, pd, "body.role")
		}
		assertSoleRole(t, app, user.ID.String(), "requester")

		app.PUT(t, path, map[string]any{"role": "admin"}, nil, http.StatusNoContent)
		assertSoleRole(t, app, user.ID.String(), "admin")
	})

	t.Run("create", func(t *testing.T) {
		for _, role := range refused {
			email := "created-" + role + "@test.local"
			_, err := app.Services.Users.Create(ctx, email, "created-"+role, "test-password-123", role, true)
			if !apperrors.IsValidation(err) {
				t.Fatalf("Create with role %q: err = %v, want a validation error", role, err)
			}
			if _, err := app.Repo.GetUserByEmail(ctx, email); !apperrors.IsNotFound(err) {
				t.Fatalf("Create with role %q left a user behind (lookup err = %v)", role, err)
			}
		}
	})

	// An invite stored with a hidden role can't be redeemed into a user with that
	// role, or with none.
	t.Run("accept legacy invite", func(t *testing.T) {
		inviter, _ := app.UserToken(t, ctx, "inviter", "admin")
		raw, hash, err := token.Generate()
		if err != nil {
			t.Fatalf("generate token: %v", err)
		}
		if _, err := app.Repo.UpsertInvite(ctx, repo.CreateInviteParams{
			Email:     "legacy@test.local",
			InvitedBy: inviter.ID,
			TokenHash: hash,
			ExpiresAt: time.Now().Add(time.Hour),
			Role:      "viewer",
		}); err != nil {
			t.Fatalf("upsert invite: %v", err)
		}

		var pd apperrors.ProblemDetails
		app.POST(t, "/api/v1/auth/accept-invite", map[string]any{
			"token":    raw,
			"username": "legacy",
			"password": "test-password-123",
		}, &pd, http.StatusUnprocessableEntity)
		findFieldError(t, pd, "body.role")

		if _, err := app.Repo.GetUserByEmail(ctx, "legacy@test.local"); !apperrors.IsNotFound(err) {
			t.Fatalf("accepting a viewer invite created a user (lookup err = %v)", err)
		}
	})
}

func assertSoleRole(t *testing.T, app *testapp.App, userID, want string) {
	t.Helper()
	var user model.User
	app.GET(t, "/api/v1/users/"+userID, &user, http.StatusOK)
	if len(user.Roles) != 1 || user.Roles[0].Name != want {
		t.Fatalf("user roles = %+v, want exactly [%s]", user.Roles, want)
	}
}
