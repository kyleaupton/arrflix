package authzgate

import (
	"sort"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/kyleaupton/arrflix/internal/http/handlers"
	"github.com/kyleaupton/arrflix/internal/http/middlewares"

	// Side-effect import: installs apperrors.ToProblem as huma's NewError,
	// mirroring the live server and genspec so registration behaves identically.
	_ "github.com/kyleaupton/arrflix/internal/http/humaerr"
)

// registeredOps reconstructs the full humachi surface genspec-style (a nil
// Deps — registration only inspects type metadata) and returns every operation
// ID mapped to its path. This is the authoritative operation set the table is
// checked against.
func registeredOps(t *testing.T) map[string]string {
	t.Helper()
	api := humachi.New(chi.NewRouter(), huma.DefaultConfig("Arrflix API", "0.0.1"))
	handlers.RegisterHumachiHandlers(api, handlers.Deps{})

	ops := make(map[string]string)
	for path, item := range api.OpenAPI().Paths {
		for _, op := range []*huma.Operation{
			item.Get, item.Put, item.Post, item.Delete,
			item.Options, item.Head, item.Patch, item.Trace,
		} {
			if op == nil || op.OperationID == "" {
				continue
			}
			ops[op.OperationID] = path
		}
	}
	return ops
}

// TestOperationPerms_EveryOperationClassified is the fail-closed backstop: a new
// endpoint with no entry in operationPerms fails CI here (naming it) rather than
// silently falling through to the gate's 403 in production.
func TestOperationPerms_EveryOperationClassified(t *testing.T) {
	t.Parallel()
	ops := registeredOps(t)

	var missing []string
	for id := range ops {
		if _, ok := operationPerms[id]; !ok {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	for _, id := range missing {
		t.Errorf("operation %q is registered but not classified in operationPerms", id)
	}
}

// TestOperationPerms_NoStaleKeys asserts the table has no entry for an operation
// that no longer exists — a renamed or deleted endpoint should drop its entry.
func TestOperationPerms_NoStaleKeys(t *testing.T) {
	t.Parallel()
	ops := registeredOps(t)

	var stale []string
	for id := range operationPerms {
		if _, ok := ops[id]; !ok {
			stale = append(stale, id)
		}
	}
	sort.Strings(stale)
	for _, id := range stale {
		t.Errorf("operationPerms has %q, which is not a registered operation", id)
	}
}

// TestOperationPerms_PublicMatchesChiAllowlist asserts the huma-layer Public
// classification agrees with the chi-layer JWT allowlist for every operation: a
// Public op's path must bypass JWT, and a non-Public op's path must not. This
// keeps the two gates from drifting — e.g. classifying a JWT-bypassed path as
// Authenticated would 401 it for everyone, since no claims are ever attached.
func TestOperationPerms_PublicMatchesChiAllowlist(t *testing.T) {
	t.Parallel()
	ops := registeredOps(t)

	for id, path := range ops {
		req, ok := operationPerms[id]
		if !ok {
			continue // reported by TestOperationPerms_EveryOperationClassified
		}
		isPublicClass := req.kind == reqPublic
		bypassesJWT := middlewares.IsPublicPath(path)
		if isPublicClass != bypassesJWT {
			t.Errorf("operation %q (%s): Public-classified=%v but chi JWT bypass=%v — the two gates disagree",
				id, path, isPublicClass, bypassesJWT)
		}
	}
}
