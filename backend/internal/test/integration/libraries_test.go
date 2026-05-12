//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

// makeCreateBody builds a valid create/update body using the wire field names
// (camelCase) the handler expects. Request bodies stay as map[string]any so
// tests can express shapes the handler's typed input struct couldn't — empty
// fields, wrong types, extras. See CLAUDE.md in this package.
func makeCreateBody(name, typ, rootPath string, enabled, isDefault bool) map[string]any {
	return map[string]any{
		"name":     name,
		"type":     typ,
		"rootPath": rootPath,
		"enabled":  enabled,
		"default":  isDefault,
	}
}

// findFieldError returns the first field error in pd matching loc. Fails the
// test if no matching location is present.
func findFieldError(t *testing.T, pd apperrors.ProblemDetails, loc string) apperrors.FieldError {
	t.Helper()
	for _, e := range pd.Errors {
		if e.Location == loc {
			return e
		}
	}
	t.Fatalf("no field error at location %q in %+v", loc, pd)
	return apperrors.FieldError{} // unreachable
}

// TestLibraries_CreateThenGet drives a full create + get round-trip and
// verifies the persisted fields exactly match the create payload.
func TestLibraries_CreateThenGet(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	rootPath := t.TempDir()
	body := makeCreateBody("Movies", "movie", rootPath, true, false)

	var created model.Library
	app.POST(t, "/api/v1/libraries", body, &created, http.StatusCreated)

	if created.ID == uuid.Nil {
		t.Fatalf("expected non-nil id, got: %+v", created)
	}
	if created.Name != "Movies" {
		t.Fatalf("Name = %q, want %q", created.Name, "Movies")
	}

	var fetched model.Library
	app.GET(t, "/api/v1/libraries/"+created.ID.String(), &fetched, http.StatusOK)

	if fetched.ID != created.ID {
		t.Errorf("ID = %s, want %s", fetched.ID, created.ID)
	}
	if fetched.Name != "Movies" {
		t.Errorf("Name = %q, want %q", fetched.Name, "Movies")
	}
	if fetched.Type != "movie" {
		t.Errorf("Type = %q, want %q", fetched.Type, "movie")
	}
	if fetched.RootPath != rootPath {
		t.Errorf("RootPath = %q, want %q", fetched.RootPath, rootPath)
	}
	if !fetched.Enabled {
		t.Errorf("Enabled = false, want true")
	}
	if fetched.Default {
		t.Errorf("Default = true, want false")
	}
}

// TestLibraries_CreateThenList verifies the list endpoint returns the
// just-created library. The handler returns a flat []model.Library array.
func TestLibraries_CreateThenList(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	rootPath := t.TempDir()
	body := makeCreateBody("Movies", "movie", rootPath, true, false)

	var created model.Library
	app.POST(t, "/api/v1/libraries", body, &created, http.StatusCreated)

	var list []model.Library
	app.GET(t, "/api/v1/libraries", &list, http.StatusOK)

	if len(list) != 1 {
		t.Fatalf("expected 1 library in list, got %d: %+v", len(list), list)
	}
	if list[0].ID != created.ID {
		t.Errorf("list[0].ID = %s, want %s", list[0].ID, created.ID)
	}
	if list[0].Name != "Movies" {
		t.Errorf("list[0].Name = %q, want %q", list[0].Name, "Movies")
	}
}

// TestLibraries_CreateValidation_EmptyName posts an empty name and asserts the
// 422 response includes a body.name field error. The tag-level minLength:"1"
// on the input struct is what catches this, before the service runs.
func TestLibraries_CreateValidation_EmptyName(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	rootPath := t.TempDir()
	body := makeCreateBody("", "movie", rootPath, true, false)

	var pd apperrors.ProblemDetails
	app.POST(t, "/api/v1/libraries", body, &pd, http.StatusUnprocessableEntity)

	fe := findFieldError(t, pd, "body.name")
	if fe.Message == "" {
		t.Errorf("expected non-empty message for body.name field error: %+v", fe)
	}
}

// TestLibraries_CreateValidation_BadType posts an unrecognized type value and
// asserts the 422 response includes a body.type field error. The tag-level
// enum:"movie,series" constraint catches this before the service runs.
func TestLibraries_CreateValidation_BadType(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	rootPath := t.TempDir()
	body := makeCreateBody("Movies", "bogus", rootPath, true, false)

	var pd apperrors.ProblemDetails
	app.POST(t, "/api/v1/libraries", body, &pd, http.StatusUnprocessableEntity)

	fe := findFieldError(t, pd, "body.type")
	if fe.Message == "" {
		t.Errorf("expected non-empty message for body.type field error: %+v", fe)
	}
}

// TestLibraries_GetNotFound asks for a random UUID that doesn't exist and
// asserts the 404 + problem-details NotFound shape.
func TestLibraries_GetNotFound(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	id := uuid.New().String()
	var pd apperrors.ProblemDetails
	app.GET(t, "/api/v1/libraries/"+id, &pd, http.StatusNotFound)

	if pd.Status != http.StatusNotFound {
		t.Errorf("problem.status = %d, want %d", pd.Status, http.StatusNotFound)
	}
	if pd.Type != "/errors/not-found" {
		t.Errorf("problem.type = %q, want %q", pd.Type, "/errors/not-found")
	}
}

// TestLibraries_UpdateThenGet creates a library, PUTs an updated payload, and
// verifies the changes were persisted by reading the row back.
func TestLibraries_UpdateThenGet(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	rootPath := t.TempDir()
	body := makeCreateBody("Movies", "movie", rootPath, true, false)

	var created model.Library
	app.POST(t, "/api/v1/libraries", body, &created, http.StatusCreated)

	updateBody := makeCreateBody("Movies Updated", "movie", rootPath, true, false)
	var updated model.Library
	app.PUT(t, "/api/v1/libraries/"+created.ID.String(), updateBody, &updated, http.StatusOK)

	if updated.Name != "Movies Updated" {
		t.Errorf("after PUT, Name = %q, want %q", updated.Name, "Movies Updated")
	}

	var fetched model.Library
	app.GET(t, "/api/v1/libraries/"+created.ID.String(), &fetched, http.StatusOK)
	if fetched.Name != "Movies Updated" {
		t.Errorf("after GET, Name = %q, want %q", fetched.Name, "Movies Updated")
	}
	if fetched.ID != created.ID {
		t.Errorf("ID changed across update: got %s, want %s", fetched.ID, created.ID)
	}
}
