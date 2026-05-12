//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

// TestApp_Health exercises the /health public path. Validates that the
// httptest server is up, chi routing is wired, and setup-mode middleware
// does not interfere with public routes.
func TestApp_Health(t *testing.T) {
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	app.GET(t, "/health", nil, http.StatusOK)
}

// TestApp_Unauthenticated hits a protected route with no Authorization header
// and asserts the JWT middleware emits 401. We bypass app.GET because it
// always sets the Bearer header.
func TestApp_Unauthenticated(t *testing.T) {
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	resp, err := http.Get(app.URL + "/api/v1/libraries")
	if err != nil {
		t.Fatalf("GET /api/v1/libraries: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// TestApp_AuthenticatedListLibraries hits a protected route with the
// pre-issued admin token. Validates the full chain: route registration,
// auth middleware, setup-mode passthrough (system is initialized), handler
// execution, and repo round-trip returning an empty list.
func TestApp_AuthenticatedListLibraries(t *testing.T) {
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	var libs []model.Library
	app.GET(t, "/api/v1/libraries", &libs, http.StatusOK)
}
