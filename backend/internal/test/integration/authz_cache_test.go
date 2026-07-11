//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/kyleaupton/arrflix/internal/authz"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

// TestAuthz_BustOnRoleChange proves the primitive end-to-end against real
// grants: a requester holds exactly its seeded permissions, and promoting them
// to admin (through UsersService.AssignRole, which calls Bust) invalidates the
// cache so the next check reflects the new role.
func TestAuthz_BustOnRoleChange(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	user, err := app.Repo.CreateUser(ctx, repo.CreateUserParams{
		Email:    "requester@test.local",
		Username: "requester",
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := app.Services.Users.AssignRole(ctx, user.ID, "requester"); err != nil {
		t.Fatalf("assign requester: %v", err)
	}

	can := func(key string) bool {
		ok, err := app.Services.Authz.Can(ctx, user.ID, key, nil)
		if err != nil {
			t.Fatalf("Can(%q): %v", key, err)
		}
		return ok
	}

	// requester's seeded grants: HD requests only, no 4K, no approve.
	if !can(authz.RequestCreate(model.MediaTypeMovie, "HD")) {
		t.Error("requester should hold requests.create:movie:hd")
	}
	if can(authz.RequestCreate(model.MediaTypeMovie, "4K")) {
		t.Error("requester should NOT hold requests.create:movie:4k")
	}
	if can(authz.RequestApprove(model.MediaTypeMovie)) {
		t.Error("requester should NOT hold requests.approve:movie")
	}

	// Promote to admin. AssignRole busts the cache; the next check must reflect
	// admin's full catalog.
	if err := app.Services.Users.AssignRole(ctx, user.ID, "admin"); err != nil {
		t.Fatalf("assign admin: %v", err)
	}
	if !can(authz.RequestApprove(model.MediaTypeMovie)) {
		t.Error("admin should hold requests.approve:movie after promotion")
	}
}

// TestAuthz_StaleUntilBust proves the cache has no TTL: a grant inserted behind
// the service is invisible until Bust is called explicitly, then visible.
func TestAuthz_StaleUntilBust(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	user, err := app.Repo.CreateUser(ctx, repo.CreateUserParams{
		Email:    "viewer@test.local",
		Username: "viewer",
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := app.Services.Users.AssignRole(ctx, user.ID, "viewer"); err != nil {
		t.Fatalf("assign viewer: %v", err)
	}

	can := func() bool {
		ok, err := app.Services.Authz.Can(ctx, user.ID, authz.JobsManage, nil)
		if err != nil {
			t.Fatalf("Can(jobs.manage): %v", err)
		}
		return ok
	}

	// First check loads and caches the (empty) grant set for jobs.manage.
	if can() {
		t.Fatal("viewer should not hold jobs.manage")
	}

	// Insert a direct user grant behind the cache.
	if _, err := pool.Exec(ctx,
		"INSERT INTO permission_grant (subject_type, subject_id, permission_key, effect) VALUES ('user', $1, 'jobs.manage', 'allow')",
		user.ID.String(),
	); err != nil {
		t.Fatalf("insert grant: %v", err)
	}

	// Still stale — no TTL, no Bust yet.
	if can() {
		t.Fatal("grant should be invisible until Bust")
	}

	app.Services.Authz.Bust(user.ID)
	if !can() {
		t.Fatal("grant should be visible after Bust")
	}
}
