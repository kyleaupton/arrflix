//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

func TestApp_Health(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	app.GET(t, "/health", nil, http.StatusOK)
}

// TestApp_Unauthenticated bypasses app.GET (which always sets the Bearer
// header) to hit a protected route with no Authorization and assert 401.
func TestApp_Unauthenticated(t *testing.T) {
	t.Parallel()
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

func TestApp_AuthenticatedListLibraries(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	var libs []model.Library
	app.GET(t, "/api/v1/libraries", &libs, http.StatusOK)
}
