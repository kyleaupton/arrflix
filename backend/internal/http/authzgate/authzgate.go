// Package authzgate is the operation-level authorization layer for the humachi
// API. It owns the policy table (operationPerms — every operation ID mapped to
// the permission it requires) and the fail-closed huma middleware that enforces
// it. Authorization is neither a handler concern nor a chi-middleware concern:
// the handlers stay ignorant of permissions, and the chi gates (JWT, setup-mode)
// operate on raw http.Handlers below huma. This package sits between them,
// keyed on huma operation IDs.
//
// The gate runs *before* the request body is parsed, so a requirement that
// depends on the body (requests-create's exact type:tier) or on the target
// entity's owner (own/any) is enforced here only as a floor and refined in the
// service. An operation missing from the table is denied; the coverage test
// (map_test.go) turns that from a silent runtime hole into red CI.
package authzgate

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/http/middlewares"
	"github.com/kyleaupton/arrflix/internal/service"
)

// requirementKind is the four-way classification every huma operation carries.
// It decides what the gate does before the handler runs:
//
//   - reqPublic: skip entirely (the chi JWT layer already bypasses these paths).
//   - reqAuthenticated: require a valid login, no permission key.
//   - reqPerm: require exactly one global permission key.
//   - reqAnyOf: require at least one of several keys — a floor the gate enforces,
//     with the service refining it (ownership or exact body-derived tier).
type requirementKind int

const (
	reqPublic requirementKind = iota
	reqAuthenticated
	reqPerm
	reqAnyOf
)

// requirement is one operation's authorization contract. keys is empty for
// Public/Authenticated, one entry for Perm, and one-or-more for AnyOf.
type requirement struct {
	kind requirementKind
	keys []string
}

// authPublic marks an operation the gate skips (login, setup, health, version).
// Public classification MUST stay aligned with middlewares.publicPathSet — the
// coverage test asserts the two agree so the chi and huma gates never drift.
func authPublic() requirement { return requirement{kind: reqPublic} }

// authUser marks an operation that needs a valid login but no permission key
// (self-profile, /auth/me, events/SSE).
func authUser() requirement { return requirement{kind: reqAuthenticated} }

// perm marks an operation the gate fully enforces with one global key.
func perm(key string) requirement { return requirement{kind: reqPerm, keys: []string{key}} }

// anyOf marks an operation whose gate is a floor: the caller must hold at least
// one of keys, and the service then refines the decision (own/any or exact
// type:tier). Never a silent bypass — each anyOf op is backed by a mandatory
// service check.
func anyOf(keys ...string) requirement { return requirement{kind: reqAnyOf, keys: keys} }

// Register installs the fail-closed authorization gate as a huma middleware. It
// runs on every registered operation, mapping the operation ID to its
// requirement (operationPerms) and short-circuiting with 403 when the caller
// lacks the required permission — an unmapped operation is denied, so a new
// endpoint with no classification is closed by default.
func Register(api huma.API, authz *service.AuthzService) {
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		req, ok := operationPerms[ctx.Operation().OperationID]
		if !ok {
			// Fail-closed: an operation absent from the table is denied. The
			// coverage test converts this from a silent runtime hole into red CI.
			writeForbidden(api, ctx)
			return
		}

		if req.kind == reqPublic {
			next(ctx)
			return
		}

		userID, err := middlewares.UserIDFromContext(ctx.Context())
		if err != nil {
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "unauthorized", err)
			return
		}

		switch req.kind {
		case reqAuthenticated:
			// Valid login is enough; no permission key to check.
		case reqPerm:
			if err := authz.Require(ctx.Context(), userID, req.keys[0], nil); err != nil {
				_ = huma.WriteErr(api, ctx, http.StatusForbidden, "forbidden", err)
				return
			}
		case reqAnyOf:
			if !anyKeyPasses(ctx.Context(), authz, userID, req.keys) {
				writeForbidden(api, ctx)
				return
			}
		}

		next(ctx)
	})
}

// anyKeyPasses reports whether the user holds at least one of keys. A Can error
// (e.g. the grant load failing) is treated as "does not pass" so the gate stays
// closed on failure.
func anyKeyPasses(ctx context.Context, authz *service.AuthzService, userID uuid.UUID, keys []string) bool {
	for _, key := range keys {
		ok, err := authz.Can(ctx, userID, key, nil)
		if err != nil {
			return false
		}
		if ok {
			return true
		}
	}
	return false
}

// writeForbidden renders a generic 403. The checked key is deliberately kept
// off the wire (it lives in the op/logs) so the keyspace isn't leaked to
// clients.
func writeForbidden(api huma.API, ctx huma.Context) {
	_ = huma.WriteErr(api, ctx, http.StatusForbidden, "forbidden",
		apperrors.Forbiddenf("insufficient permissions").Op("authzgate.Register"))
}
