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
	"github.com/kyleaupton/arrflix/internal/authz"
	"github.com/kyleaupton/arrflix/internal/config"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/http/middlewares"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
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

// ----- Login -----

type LoginInput struct {
	Body struct {
		Login    string `json:"login" required:"true" minLength:"1" doc:"Username or email"`
		Password string `json:"password" required:"true" minLength:"1" doc:"Password"`
	}
}

type LoginResponse struct {
	Token string `json:"token" doc:"JWT bearer token"`
}

type LoginOutput struct {
	Body LoginResponse
}

func (h *Auth) Login(ctx context.Context, input *LoginInput) (*LoginOutput, error) {
	signed, err := h.svc.Auth.Login(ctx, input.Body.Login, input.Body.Password)
	if err != nil {
		return nil, err
	}
	return &LoginOutput{Body: LoginResponse{Token: signed}}, nil
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
	Body struct {
		PinID int `json:"pin_id" required:"true" minimum:"1" doc:"Plex pin id returned from /auth/plex/start"`
	}
}

type PlexExchangeResponse struct {
	Token string `json:"token" doc:"JWT bearer token"`
}

type PlexExchangeOutput struct {
	Body PlexExchangeResponse
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

	signed, err := h.svc.Auth.LoginWithPlex(ctx, plexSubject, plexUser.Email, plexUser.Username, pin.AuthToken, raw)
	if err != nil {
		return nil, err
	}
	return &PlexExchangeOutput{Body: PlexExchangeResponse{Token: signed}}, nil
}

// ----- Me -----

type AuthMeInput struct{}

type MeResponse struct {
	Sub                 string   `json:"sub" doc:"User id (UUID string from JWT subject)"`
	Email               *string  `json:"email,omitempty" doc:"Email from JWT claims"`
	Name                *string  `json:"name,omitempty" doc:"Username from JWT claims"`
	Roles               []string `json:"roles" doc:"Role names assigned to the user"`
	CanAutoApproveMovie bool     `json:"canAutoApproveMovie" doc:"Whether a movie request by this user auto-approves into a tracking"`
}

type AuthMeOutput struct {
	Body MeResponse
}

// Me returns the authenticated principal. Identity fields (email/name) come from
// the JWT claims for free; roles and the auto-approve capability are loaded from
// the DB so the frontend can gate admin-only controls and pick the
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

	resp := MeResponse{Sub: sub, Roles: []string{}}
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

	// A movie request omits the tier (requesters don't pick quality), defaulting
	// to HD — so "can auto-approve a movie" is the HD-movie auto-approve grant.
	// Phase 4 replaces this single bool with the full per-tier capability set.
	canAuto, err := h.svc.Authz.Can(ctx, userID,
		authz.RequestAutoApprove(model.MediaTypeMovie, "HD"), nil)
	if err != nil {
		return nil, err
	}
	resp.CanAutoApproveMovie = canAuto

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
}
