// Package middlewares contains the HTTP middleware chain. As of phase 4 of
// the humachi migration, chi is the only router and runs middleware before
// any handler dispatch. ChiJWT and ChiSetupMode are the cross-cutting
// gates; both run as chi.Router.Use middleware so they apply once per
// request regardless of whether the route is humachi-shaped or a plain
// chi handler.

package middlewares

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v4"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ctxKey is a private type so the context value cannot collide with anything
// else in the codebase.
type ctxKey int

const (
	// claimsCtxKey stores the parsed jwt.MapClaims after successful auth.
	claimsCtxKey ctxKey = iota
)

// ClaimsFromContext extracts the JWT claims previously stuffed into the
// request context by ChiJWT. Returns nil + false when no claims are present
// (i.e., the request didn't go through the auth middleware, or auth failed).
//
// Usage from a chi or humachi handler:
//
//	claims, ok := middlewares.ClaimsFromContext(r.Context())
func ClaimsFromContext(ctx context.Context) (jwt.MapClaims, bool) {
	v := ctx.Value(claimsCtxKey)
	if v == nil {
		return nil, false
	}
	claims, ok := v.(jwt.MapClaims)
	return claims, ok
}

// withClaims returns ctx with claims attached.
func withClaims(ctx context.Context, claims jwt.MapClaims) context.Context {
	return context.WithValue(ctx, claimsCtxKey, claims)
}

// publicPathSet is the set of exact paths that bypass JWT validation in
// ChiJWT. These mirror the un-authenticated surface: bootstrap probes,
// version, health, login/signup/plex flows, and setup endpoints (which
// must be reachable before a user exists).
//
// Public exact paths:
//   - /health
//   - /api/v1/bootstrap
//   - /api/v1/version
//   - /api/v1/auth/login
//   - /api/v1/auth/signup
//   - /api/v1/auth/plex/start
//   - /api/v1/auth/plex/exchange
//   - /api/v1/setup/status
//   - /api/v1/setup/initialize
//   - /api/v1/setup/tmdb
//
// Plus the /dev/* tree is JWT-bypassed when registered (dev env). Matched
// by prefix.
var publicPathSet = map[string]struct{}{
	"/health":                    {},
	"/api/v1/bootstrap":          {},
	"/api/v1/version":            {},
	"/api/v1/auth/login":         {},
	"/api/v1/auth/signup":        {},
	"/api/v1/auth/plex/start":    {},
	"/api/v1/auth/plex/exchange": {},
	"/api/v1/setup/status":       {},
	"/api/v1/setup/initialize":   {},
	"/api/v1/setup/tmdb":         {},
}

// publicPathPrefixes are URL-path prefixes that bypass JWT validation.
// Used for the dev routes (only registered when cfg.Env == "dev").
var publicPathPrefixes = []string{
	"/dev/",
}

// isPublicPath reports whether the given URL path is a public route that
// should bypass JWT validation.
//
// Note: any path that isn't /api/v1/* and isn't /health or /dev/* falls
// outside the API surface entirely; we still let those bypass so static
// asset routes (none today) and unknown paths reach chi's NotFound (404).
func isPublicPath(path string) bool {
	if _, ok := publicPathSet[path]; ok {
		return true
	}
	for _, prefix := range publicPathPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	// Anything outside /api/v1/* is not the API surface — don't gate it.
	// (Echo will 404 / handle as appropriate.)
	if !strings.HasPrefix(path, "/api/v1/") {
		return true
	}
	return false
}

// ChiJWT is the chi-shaped JWT auth middleware. Validates the Bearer
// token, parses the claims, and attaches them to the request context.
// Returns 401 (RFC 9457 application/problem+json) on missing or invalid
// credentials.
//
// Public paths (see publicPathSet / publicPathPrefixes) bypass validation
// entirely: the next handler is invoked with the request context unchanged.
//
// Humachi handlers read claims via ClaimsFromContext(ctx); plain chi
// handlers do the same off r.Context().
func ChiJWT(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isPublicPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			authz := r.Header.Get("Authorization")
			if authz == "" || len(authz) < 8 || !strings.HasPrefix(authz, "Bearer ") {
				writeProblem(w, apperrors.Unauthenticatedf("missing bearer token"))
				return
			}
			raw := strings.TrimPrefix(authz, "Bearer ")
			tok, err := jwt.Parse(raw, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, apperrors.Unauthenticatedf("bad token method")
				}
				return []byte(secret), nil
			})
			if err != nil || !tok.Valid {
				writeProblem(w, apperrors.Unauthenticatedf("invalid token"))
				return
			}
			claims, ok := tok.Claims.(jwt.MapClaims)
			if !ok {
				writeProblem(w, apperrors.Unauthenticatedf("invalid claims"))
				return
			}
			ctx := withClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ChiSetupMode is the global setup-mode gate. When the system is not
// initialized, only setup routes, /health, and /api/v1/bootstrap are
// allowed; everything else returns 503.
func ChiSetupMode(services *service.Services) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if path == "/health" || path == "/api/v1/bootstrap" {
				next.ServeHTTP(w, r)
				return
			}
			initialized, err := services.Setup.IsInitialized(r.Context())
			if err != nil {
				writeProblem(w, apperrors.Internalf("setup check failed"))
				return
			}
			if !initialized {
				isSetupRoute := strings.HasPrefix(path, "/api/v1/setup/")
				if !isSetupRoute {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusServiceUnavailable)
					_ = json.NewEncoder(w).Encode(map[string]string{
						"error":     "setup required",
						"setup_url": "/api/v1/setup/status",
					})
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// writeProblem renders an apperrors error as RFC 9457 application/problem+json.
func writeProblem(w http.ResponseWriter, err error) {
	pd := apperrors.ToProblem(err)
	w.Header().Set("Content-Type", apperrors.ContentType)
	w.WriteHeader(pd.Status)
	_ = json.NewEncoder(w).Encode(pd)
}
