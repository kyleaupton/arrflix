package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/golang-jwt/jwt/v4"
	"github.com/kyleaupton/arrflix/internal/config"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

type Bootstrap struct {
	cfg config.Config
	svc *service.Services
}

func NewBootstrap(cfg config.Config, svc *service.Services) *Bootstrap {
	return &Bootstrap{cfg: cfg, svc: svc}
}

// ----- Bootstrap response shape -----

// BootstrapUser is derived from the Bearer token only — not from session
// state. Absent on missing/invalid token (degraded path, not 401).
type BootstrapUser struct {
	ID       string  `json:"id" doc:"User id (JWT subject)"`
	Email    *string `json:"email" doc:"Email from JWT claims"`
	Username *string `json:"username" doc:"Username from JWT claims"`
}

type BootstrapConfig struct {
	SiteTitle      string `json:"siteTitle" doc:"Display name of the site"`
	SignupStrategy string `json:"signupStrategy" doc:"Configured signup strategy (open / invite_only)"`
	Version        string `json:"version" doc:"Build version string"`
}

type BootstrapResponse struct {
	Initialized bool                `json:"initialized" doc:"Whether the system has completed setup"`
	User        *BootstrapUser      `json:"user" doc:"Authenticated principal, when a valid token was supplied; null otherwise"`
	Config      BootstrapConfig     `json:"config" doc:"Public app config"`
	Setup       *service.SetupSteps `json:"setup,omitempty" doc:"Per-step setup completion (only present when not yet initialized)"`
}

// ----- Get -----

// BootstrapGetInput pulls the optional Authorization header. Invalid/missing
// token degrades to no user instead of 401 — bootstrap is a public endpoint.
type BootstrapGetInput struct {
	Authorization string `header:"Authorization" doc:"Optional Bearer token; if valid the response includes the authenticated user"`
}

type BootstrapGetOutput struct {
	Body BootstrapResponse
}

func (h *Bootstrap) GetBootstrap(ctx context.Context, input *BootstrapGetInput) (*BootstrapGetOutput, error) {
	initialized, err := h.svc.Setup.IsInitialized(ctx)
	if err != nil {
		return nil, err
	}

	var setupSteps *service.SetupSteps
	if !initialized {
		steps, err := h.svc.Setup.GetSetupSteps(ctx)
		if err != nil {
			return nil, err
		}
		setupSteps = &steps
	}

	var user *BootstrapUser
	if len(input.Authorization) > 7 && input.Authorization[:7] == "Bearer " {
		user = h.tryParseUser(input.Authorization[7:])
	}

	cfg := h.getPublicConfig(ctx)

	return &BootstrapGetOutput{Body: BootstrapResponse{
		Initialized: initialized,
		User:        user,
		Config:      cfg,
		Setup:       setupSteps,
	}}, nil
}

// tryParseUser parses the JWT inline. Returns nil on any failure — bootstrap
// is public, so a bad token degrades to "no user" rather than 401.
func (h *Bootstrap) tryParseUser(raw string) *BootstrapUser {
	tok, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(h.cfg.JWTSecret), nil
	})
	if err != nil || !tok.Valid {
		return nil
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return nil
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return nil
	}
	email, _ := claims["email"].(string)
	name, _ := claims["name"].(string)
	return &BootstrapUser{
		ID:       sub,
		Email:    strPtr(email),
		Username: strPtr(name),
	}
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// getPublicConfig assembles the public-config slice. Settings failures
// degrade to defaults rather than failing the bootstrap call — losing the
// site title shouldn't 500 the page load.
func (h *Bootstrap) getPublicConfig(ctx context.Context) BootstrapConfig {
	cfg := BootstrapConfig{
		SiteTitle:      "Arrflix",
		SignupStrategy: "invite_only",
		Version:        h.svc.Version.GetBuildInfo().Version,
	}
	all, err := h.svc.Settings.GetAll(ctx)
	if err != nil {
		return cfg
	}
	if v, ok := all["site.title"].(string); ok && v != "" {
		cfg.SiteTitle = v
	}
	if v, ok := all["auth.signup_strategy"].(string); ok && v != "" {
		cfg.SignupStrategy = v
	}
	return cfg
}

// ----- Register -----

func (h *Bootstrap) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "bootstrap-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/bootstrap",
		Summary:     "Bootstrap application",
		Description: "Returns init status, current user (if a valid Bearer token is supplied), public config, and per-step setup progress when not yet initialized.",
		Tags:        []string{"bootstrap"},
	}, h.GetBootstrap)
}
