package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kyleaupton/arrflix/internal/config"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/http/middlewares"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/plex"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

type Auth struct {
	cfg  config.Config
	log  *logger.Logger
	pool *pgxpool.Pool
	svc  *service.Services
}

func NewAuth(cfg config.Config, log *logger.Logger, pool *pgxpool.Pool, svc *service.Services) *Auth {
	return &Auth{cfg: cfg, log: log, pool: pool, svc: svc}
}

// ----- Session cookie helpers -----

// refreshCookieName is the HttpOnly cookie carrying the session refresh token.
// It's Path-scoped to the auth routes so it rides only the refresh/logout
// requests, never every API call.
const refreshCookieName = "arrflix_rt"

// refreshCookie builds the Set-Cookie for a live refresh token. Secure is gated on
// prod: in dev the app is served over http://localhost, where a Secure cookie is
// dropped, so requiring it would break the dev refresh flow.
func (h *Auth) refreshCookie(value string) http.Cookie {
	return http.Cookie{
		Name:     refreshCookieName,
		Value:    value,
		Path:     "/api/v1/auth",
		HttpOnly: true,
		Secure:   h.cfg.Env == "prod",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(h.cfg.SessionTTL.Seconds()),
	}
}

// clearRefreshCookie expires the refresh cookie on logout.
func (h *Auth) clearRefreshCookie() http.Cookie {
	return http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     "/api/v1/auth",
		HttpOnly: true,
		Secure:   h.cfg.Env == "prod",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
}

// sessionMeta captures best-effort device context for a new session.
func sessionMeta(userAgent string) service.SessionMeta {
	var ua *string
	if userAgent != "" {
		ua = &userAgent
	}
	return service.SessionMeta{UserAgent: ua}
}

// ----- Login -----

type LoginInput struct {
	UserAgent string `header:"User-Agent" doc:"Device label, captured server-side"`
	Body      struct {
		Login    string `json:"login" required:"true" minLength:"1" doc:"Username or email"`
		Password string `json:"password" required:"true" minLength:"1" doc:"Password"`
	}
}

type LoginResponse struct {
	Token string `json:"token" doc:"JWT bearer token"`
}

type LoginOutput struct {
	Body      LoginResponse
	SetCookie http.Cookie `header:"Set-Cookie"`
}

func (h *Auth) Login(ctx context.Context, input *LoginInput) (*LoginOutput, error) {
	user, err := h.svc.Auth.Login(ctx, input.Body.Login, input.Body.Password)
	if err != nil {
		return nil, err
	}
	issued, err := h.svc.Sessions.Create(ctx, user, sessionMeta(input.UserAgent))
	if err != nil {
		return nil, err
	}
	return &LoginOutput{
		Body:      LoginResponse{Token: issued.AccessToken},
		SetCookie: h.refreshCookie(issued.RefreshToken),
	}, nil
}

// ----- Signup -----

type SignupInput struct {
	Body struct {
		Email    string `json:"email" required:"true" minLength:"1" format:"email" doc:"Email address"`
		Username string `json:"username" required:"true" minLength:"1" doc:"Username"`
		Password string `json:"password" required:"true" minLength:"8" doc:"Password (>= 8 chars)"`
	}
}

type SignupResponse struct {
	Success bool `json:"success" doc:"Always true on success"`
}

type SignupOutput struct {
	Body SignupResponse
}

// Signup honors the configured signup strategy: invite_only requires a
// matching invite (Forbidden if missing); open allows arbitrary signups.
func (h *Auth) Signup(ctx context.Context, input *SignupInput) (*SignupOutput, error) {
	all, err := h.svc.Settings.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	strategy, _ := all["auth.signup_strategy"].(string)
	if strategy == "" {
		strategy = "invite_only"
	}

	if strategy == "invite_only" {
		if err := h.svc.Invites.CheckAndClaim(ctx, input.Body.Email); err != nil {
			return nil, err
		}
	}

	if _, err := h.svc.Users.Create(ctx, input.Body.Email, input.Body.Username, input.Body.Password, "requester", true); err != nil {
		return nil, err
	}
	return &SignupOutput{Body: SignupResponse{Success: true}}, nil
}

// ----- Plex Exchange -----

type PlexExchangeInput struct {
	UserAgent string `header:"User-Agent" doc:"Device label, captured server-side"`
	Body      struct {
		PinID int `json:"pin_id" required:"true" minimum:"1" doc:"Plex pin id returned from /auth/plex/start"`
	}
}

type PlexExchangeResponse struct {
	Token string `json:"token" doc:"JWT bearer token"`
}

type PlexExchangeOutput struct {
	Body      PlexExchangeResponse
	SetCookie http.Cookie `header:"Set-Cookie"`
}

// PlexExchange polls Plex for the PIN's auth token and, if claimed,
// upserts/looks-up the local user and issues a JWT. Handler-level errors
// match the original Echo implementation:
//   - 400 if the upstream Plex check fails (network / unparseable)
//   - 401 if the PIN hasn't been claimed yet
//   - service errors flow through unchanged
//
// The 401 here is handler-emitted (PIN-not-yet-claimed), distinct from the
// middleware-emitted 401 on protected routes. It stays in the Errors slice.
func (h *Auth) PlexExchange(ctx context.Context, input *PlexExchangeInput) (*PlexExchangeOutput, error) {
	pc := plex.NewClient()

	pin, err := pc.CheckPin(input.Body.PinID)
	if err != nil {
		return nil, apperrors.BadGatewayf("plex check pin failed").
			Op("AuthHandler.PlexExchange")
	}
	if pin.AuthToken == "" {
		return nil, apperrors.Unauthenticatedf("plex authorization not completed").
			Op("AuthHandler.PlexExchange")
	}

	plexUser, err := pc.GetUser(pin.AuthToken)
	if err != nil {
		return nil, apperrors.BadGatewayf("plex get user failed").
			Op("AuthHandler.PlexExchange")
	}

	raw, _ := json.Marshal(plexUser)
	plexSubject := strconv.Itoa(plexUser.ID)

	user, err := h.svc.Auth.LoginWithPlex(ctx, plexSubject, plexUser.Email, plexUser.Username, pin.AuthToken, raw)
	if err != nil {
		return nil, err
	}
	issued, err := h.svc.Sessions.Create(ctx, user, sessionMeta(input.UserAgent))
	if err != nil {
		return nil, err
	}
	return &PlexExchangeOutput{
		Body:      PlexExchangeResponse{Token: issued.AccessToken},
		SetCookie: h.refreshCookie(issued.RefreshToken),
	}, nil
}

// ----- Me -----

type AuthMeInput struct{}

type MeResponse struct {
	Sub         string   `json:"sub" doc:"User id (UUID string from JWT subject)"`
	Email       *string  `json:"email,omitempty" doc:"Email from JWT claims"`
	Name        *string  `json:"name,omitempty" doc:"Username from JWT claims"`
	Roles       []string `json:"roles" doc:"Role names assigned to the user"`
	Permissions []string `json:"permissions" doc:"Effective global permission keys the user holds"`
}

type AuthMeOutput struct {
	Body MeResponse
}

// Me returns the authenticated principal. Identity fields (email/name) come from
// the JWT claims for free; roles and the effective permission-key list are
// loaded from the DB so the frontend can gate controls and pick the
// "Add to Library" vs "Request" face without a second round-trip.
func (h *Auth) Me(ctx context.Context, _ *AuthMeInput) (*AuthMeOutput, error) {
	claims, ok := middlewares.ClaimsFromContext(ctx)
	if !ok {
		return nil, apperrors.Unauthenticatedf("missing credentials").
			Op("AuthHandler.Me")
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return nil, apperrors.Unauthenticatedf("invalid token subject").
			Op("AuthHandler.Me")
	}
	userID, err := uuid.Parse(sub)
	if err != nil {
		return nil, apperrors.Unauthenticatedf("invalid token subject").
			Op("AuthHandler.Me")
	}

	resp := MeResponse{Sub: sub, Roles: []string{}, Permissions: []string{}}
	if v, ok := claims["email"].(string); ok && v != "" {
		resp.Email = &v
	}
	if v, ok := claims["name"].(string); ok && v != "" {
		resp.Name = &v
	}

	user, err := h.svc.Users.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, role := range user.Roles {
		resp.Roles = append(resp.Roles, role.Name)
	}

	perms, err := h.svc.Authz.EffectiveKeys(ctx, userID)
	if err != nil {
		return nil, err
	}
	resp.Permissions = perms

	return &AuthMeOutput{Body: resp}, nil
}

// ----- Plex Start (plain chi, 302 redirect) -----

// PlexStart is a plain chi handler (not humachi) because it returns a 302
// redirect — huma is JSON-first and modelling a "Location header + 302
// status" op is awkward. The FE relies on the browser following the redirect,
// not on a typed client, so this surface stays outside the OpenAPI spec.
//
// Errors render as plain text via http.Error: a 302-redirect endpoint failing
// on input never has a structured-error consumer, so JSON would be unmotivated.
func (h *Auth) PlexStart(w http.ResponseWriter, r *http.Request) {
	redirectURI := r.URL.Query().Get("redirect_uri")
	if redirectURI == "" {
		http.Error(w, "redirect_uri required", http.StatusBadRequest)
		return
	}

	pc := plex.NewClient()
	pin, err := pc.CreatePin()
	if err != nil {
		http.Error(w, "failed to create plex pin", http.StatusInternalServerError)
		return
	}

	sep := "?"
	if strings.Contains(redirectURI, "?") {
		sep = "&"
	}
	forwardURL := fmt.Sprintf("%s%spinId=%d", redirectURI, sep, pin.ID)

	authURL := plex.AuthURL(pin.Code, forwardURL)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// ----- Refresh -----

type RefreshInput struct {
	Cookie http.Cookie `cookie:"arrflix_rt"`
}

type RefreshResponse struct {
	Token string `json:"token" doc:"JWT bearer token"`
}

type RefreshOutput struct {
	Body      RefreshResponse
	SetCookie http.Cookie `header:"Set-Cookie"`
}

// Refresh rotates the refresh-token cookie and returns a fresh access token. It is
// authenticated by the cookie, not a bearer token, so an expired access token can
// still renew. A missing/invalid/reused refresh token is a 401 the frontend treats
// as "log out".
func (h *Auth) Refresh(ctx context.Context, input *RefreshInput) (*RefreshOutput, error) {
	issued, err := h.svc.Sessions.Refresh(ctx, input.Cookie.Value)
	if err != nil {
		return nil, err
	}
	return &RefreshOutput{
		Body:      RefreshResponse{Token: issued.AccessToken},
		SetCookie: h.refreshCookie(issued.RefreshToken),
	}, nil
}

// ----- Logout -----

type LogoutInput struct {
	Cookie http.Cookie `cookie:"arrflix_rt"`
}

type LogoutOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
}

// Logout revokes the session the cookie belongs to and clears the cookie.
// Idempotent: an absent or already-revoked session still succeeds.
func (h *Auth) Logout(ctx context.Context, input *LogoutInput) (*LogoutOutput, error) {
	if err := h.svc.Sessions.RevokeByRefreshToken(ctx, input.Cookie.Value); err != nil {
		return nil, err
	}
	return &LogoutOutput{SetCookie: h.clearRefreshCookie()}, nil
}

// ----- Register -----

// RegisterHumachi registers the auth operations. PlexStart is registered
// separately on the chi router (see registry.go) because it returns a 302.
func (h *Auth) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "auth-login",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/login",
		Summary:     "Login",
		Description: "Validate username/email + password and issue a JWT.",
		Tags:        []string{"auth"},
		// 401 here is handler-emitted (bad credentials), not middleware.
		Errors: []int{http.StatusUnauthorized, http.StatusBadRequest, http.StatusUnprocessableEntity},
	}, h.Login)

	huma.Register(api, huma.Operation{
		OperationID:   "auth-signup",
		Method:        http.MethodPost,
		Path:          "/api/v1/auth/signup",
		Summary:       "Signup",
		Description:   "Create a new user. Honors the configured signup strategy (invite_only or open).",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusCreated,
		Errors:        errs(errsWrite, errsForbidden),
	}, h.Signup)

	huma.Register(api, huma.Operation{
		OperationID: "auth-plex-exchange",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/plex/exchange",
		Summary:     "Exchange Plex PIN for JWT",
		Description: "Polls the Plex API for the PIN's auth token and, if claimed, returns a JWT. Returns 401 when the PIN has not been claimed yet.",
		Tags:        []string{"auth"},
		// 401 = PIN-not-yet-claimed (handler-emitted, not middleware).
		Errors: errs([]int{http.StatusUnauthorized}, errsWrite, errsForbidden, errsUpstream),
	}, h.PlexExchange)

	huma.Register(api, huma.Operation{
		OperationID: "auth-me",
		Method:      http.MethodGet,
		Path:        "/api/v1/auth/me",
		Summary:     "Current user",
		Description: "Returns the authenticated principal (from JWT claims).",
		Tags:        []string{"auth"},
	}, h.Me)

	huma.Register(api, huma.Operation{
		OperationID: "auth-refresh",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/refresh",
		Summary:     "Refresh session",
		Description: "Rotates the refresh-token cookie and returns a fresh access token. Authenticated by the refresh cookie, not a bearer token.",
		Tags:        []string{"auth"},
		// 401 = missing/invalid/reused refresh token (the FE treats it as logout).
		Errors: []int{http.StatusUnauthorized},
	}, h.Refresh)

	huma.Register(api, huma.Operation{
		OperationID:   "auth-logout",
		Method:        http.MethodPost,
		Path:          "/api/v1/auth/logout",
		Summary:       "Logout",
		Description:   "Revokes the current session and clears the refresh cookie.",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, h.Logout)
}
