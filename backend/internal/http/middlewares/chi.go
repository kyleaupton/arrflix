// Package middlewares contains the cross-cutting HTTP gates. ChiJWT and
// ChiSetupMode are wired as chi.Router.Use middleware so they apply once
// per request regardless of whether the route is humachi-shaped or a plain
// chi handler.
package middlewares

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ctxKey is a private type so the context value cannot collide with anything
// else in the codebase.
type ctxKey int

const (
	// claimsCtxKey stores the parsed jwt.MapClaims after successful auth.
	claimsCtxKey ctxKey = iota
	// clientIPCtxKey stores the request's client IP (host only, no port).
	clientIPCtxKey
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

// ChiClientIP stashes the request's client IP (host only) into the context so
// humachi handlers — which never see the *http.Request — can record it on a
// session. It relies on chi's middleware.RealIP having already rewritten
// RemoteAddr from the proxy's X-Forwarded-For/X-Real-IP; behind our own nginx
// that first hop is the real client. It runs on every path (login/refresh are
// JWT-public, so this can't hang off ChiJWT).
func ChiClientIP() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), clientIPCtxKey, hostOnly(r.RemoteAddr))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClientIPFromContext returns the client IP ChiClientIP recorded, or nil when
// none was captured — the *string shape drops straight into the nullable
// session ip column.
func ClientIPFromContext(ctx context.Context) *string {
	v, _ := ctx.Value(clientIPCtxKey).(string)
	if v == "" {
		return nil
	}
	return &v
}

// hostOnly strips the port from a "host:port" RemoteAddr. RealIP-rewritten
// values already carry no port (returned as-is); raw TCP addrs are split.
func hostOnly(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

// UserIDFromContext extracts the caller's user id from the JWT subject claim
// ChiJWT attached, parsing it as a UUID. It sits next to ClaimsFromContext
// because both read the same context value the JWT middleware populated; the
// authz gate and the handlers share this one implementation. The returned
// errors are Unauthenticated (no op annotation — callers that want one wrap it).
func UserIDFromContext(ctx context.Context) (uuid.UUID, error) {
	claims, ok := ClaimsFromContext(ctx)
	if !ok {
		return uuid.Nil, apperrors.Unauthenticatedf("missing credentials")
	}
	sub, ok := claims["sub"].(string)
	if !ok {
		return uuid.Nil, apperrors.Unauthenticatedf("invalid token subject")
	}
	id, err := uuid.Parse(sub)
	if err != nil {
		return uuid.Nil, apperrors.Unauthenticatedf("invalid token")
	}
	return id, nil
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
//   - /api/v1/auth/refresh
//   - /api/v1/auth/logout
//   - /api/v1/setup/status
//   - /api/v1/setup/initialize
//   - /api/v1/setup/tmdb
//
// The refresh/logout routes are cookie-authed rather than bearer-authed, so they
// bypass the JWT middleware and validate the refresh cookie in the service.
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
	"/api/v1/auth/refresh":       {},
	"/api/v1/auth/logout":        {},
	"/api/v1/setup/status":       {},
	"/api/v1/setup/initialize":   {},
	"/api/v1/setup/tmdb":         {},
}

// publicPathPrefixes are URL-path prefixes that bypass JWT validation.
// Used for the dev routes (only registered when cfg.Env == "dev").
var publicPathPrefixes = []string{
	"/dev/",
}

// IsPublicPath reports whether the given URL path bypasses JWT validation at
// the chi layer. Exported for the authz coverage test, which asserts the
// huma-layer Public classification (operationPerms) stays aligned with this
// allowlist so the two gates never drift.
func IsPublicPath(path string) bool { return isPublicPath(path) }

// isPublicPath reports whether the given URL path bypasses JWT validation.
// Anything outside /api/v1/* isn't the API surface, so we let it through to
// reach chi's NotFound (or whatever else is mounted).
func isPublicPath(path string) bool {
	if _, ok := publicPathSet[path]; ok {
		return true
	}
	for _, prefix := range publicPathPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return !strings.HasPrefix(path, "/api/v1/")
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
			tok, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
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
