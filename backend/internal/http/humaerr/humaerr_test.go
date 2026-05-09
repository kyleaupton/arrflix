package humaerr

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

// TestApperrorsStatusError_Schema verifies the wrapper implements
// huma.SchemaProvider and returns a non-nil schema that references
// apperrors.ProblemDetails. Without this, every error response across every
// operation gets an empty {} schema in the generated OpenAPI.
func TestApperrorsStatusError_Schema(t *testing.T) {
	se := huma.NewError(http.StatusNotFound, "x", apperrors.NotFoundf("y"))
	provider, ok := se.(huma.SchemaProvider)
	if !ok {
		t.Fatalf("huma.NewError result %T does not implement huma.SchemaProvider", se)
	}
	r := huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer)
	s := provider.Schema(r)
	if s == nil {
		t.Fatalf("Schema() returned nil")
	}
	if _, ok := r.Map()["ProblemDetails"]; !ok {
		t.Fatalf("ProblemDetails not registered in components; got %v", keys(r.Map()))
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// marshalNewError invokes the package-installed huma.NewError hook and
// returns its JSON wire form. The init() in humaerr.go is what installs the
// hook; the import is enough to make this work.
func marshalNewError(t *testing.T, status int, message string, errs ...error) map[string]any {
	t.Helper()
	se := huma.NewError(status, message, errs...)
	body, err := json.Marshal(se)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

// TestNewError_TypedShortCircuit verifies that a service-emitted *apperrors.Error
// in the chain is rendered through apperrors.ToProblem (preserving kind, type
// URI, and the no-leak rule for KindInternal).
func TestNewError_TypedShortCircuit(t *testing.T) {
	src := apperrors.NotFoundf("library %q does not exist", "movies")
	out := marshalNewError(t, http.StatusInternalServerError /* deliberately wrong */, "ignored", src)

	if got, want := out["type"], "/errors/not-found"; got != want {
		t.Errorf("type = %q, want %q", got, want)
	}
	if got, want := out["title"], "Not Found"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	// Status comes from apperrors.HTTPStatus(*Error), NOT from the status arg.
	if got, want := out["status"].(float64), float64(http.StatusNotFound); got != want {
		t.Errorf("status = %v, want %v", got, want)
	}
	if got, want := out["detail"], `library "movies" does not exist`; got != want {
		t.Errorf("detail = %q, want %q", got, want)
	}
}

// TestNewError_FallbackHumaErrorDetail verifies that *huma.ErrorDetail entries
// in the error chain are surfaced as FieldErrors in the ProblemDetails body
// (location, message, value all preserved).
func TestNewError_FallbackHumaErrorDetail(t *testing.T) {
	det := &huma.ErrorDetail{
		Location: "body.name",
		Message:  "expected string",
		Value:    42,
	}
	out := marshalNewError(t, http.StatusUnprocessableEntity, "validation failed", det)

	if got, want := out["type"], "/errors/validation"; got != want {
		t.Errorf("type = %q, want %q", got, want)
	}
	if got, want := out["title"], "Unprocessable Entity"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	if got, want := out["status"].(float64), float64(http.StatusUnprocessableEntity); got != want {
		t.Errorf("status = %v, want %v", got, want)
	}
	if got, want := out["detail"], "validation failed"; got != want {
		t.Errorf("detail = %q, want %q", got, want)
	}
	errs, ok := out["errors"].([]any)
	if !ok || len(errs) != 1 {
		t.Fatalf("errors = %v, want one entry", out["errors"])
	}
	field := errs[0].(map[string]any)
	if got, want := field["location"], "body.name"; got != want {
		t.Errorf("errors[0].location = %q, want %q", got, want)
	}
	if got, want := field["message"], "expected string"; got != want {
		t.Errorf("errors[0].message = %q, want %q", got, want)
	}
	if got, want := field["value"].(float64), float64(42); got != want {
		t.Errorf("errors[0].value = %v, want %v", got, want)
	}
}

// TestNewError_FallbackKindMapping verifies the status-to-kind mapping for the
// fallback path: 422 → KindValidation (type URI "/errors/validation"), 500
// (default) → KindInternal (detail masked to the no-leak string).
func TestNewError_FallbackKindMapping(t *testing.T) {
	t.Run("422 → validation", func(t *testing.T) {
		out := marshalNewError(t, http.StatusUnprocessableEntity, "bad input")
		if got, want := out["type"], "/errors/validation"; got != want {
			t.Errorf("type = %q, want %q", got, want)
		}
	})

	t.Run("500 → internal masks detail", func(t *testing.T) {
		out := marshalNewError(t, http.StatusInternalServerError, "leaky internal message")
		if got, want := out["type"], "/errors/internal"; got != want {
			t.Errorf("type = %q, want %q", got, want)
		}
		if got, want := out["detail"], "an internal error occurred"; got != want {
			t.Errorf("detail = %q, want %q (must NOT echo huma message)", got, want)
		}
	})
}
