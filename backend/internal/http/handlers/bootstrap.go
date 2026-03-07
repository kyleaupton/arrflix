package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/golang-jwt/jwt/v4"
	"github.com/kyleaupton/arrflix/internal/config"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/labstack/echo/v4"
)

type Bootstrap struct {
	cfg config.Config
	svc *service.Services
}

func NewBootstrap(cfg config.Config, svc *service.Services) *Bootstrap {
	return &Bootstrap{cfg: cfg, svc: svc}
}

func (h *Bootstrap) RegisterPublic(v1 *echo.Group) {
	v1.GET("/bootstrap", h.GetBootstrap)
}

type BootstrapUser struct {
	ID       string  `json:"id"`
	Email    *string `json:"email"`
	Username *string `json:"username"`
}

type BootstrapConfig struct {
	SiteTitle      string `json:"siteTitle"`
	SignupStrategy string `json:"signupStrategy"`
	Version        string `json:"version"`
}

type BootstrapResponse struct {
	Initialized bool                 `json:"initialized"`
	User        *BootstrapUser       `json:"user"`
	Config      BootstrapConfig      `json:"config"`
	Setup       *service.SetupSteps  `json:"setup,omitempty"`
}

// GetBootstrap returns initialization status, current user (if authenticated), and public app config.
// @Summary  Bootstrap application
// @Tags     bootstrap
// @Produce  json
// @Success  200 {object} handlers.BootstrapResponse
// @Router   /v1/bootstrap [get]
func (h *Bootstrap) GetBootstrap(c echo.Context) error {
	ctx := c.Request().Context()

	// 1. Setup status
	initialized, err := h.svc.Setup.IsInitialized(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "bootstrap failed"})
	}

	// 2. Setup steps (only when not fully initialized)
	var setupSteps *service.SetupSteps
	if !initialized {
		steps, err := h.svc.Setup.GetSetupSteps(ctx)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "bootstrap failed"})
		}
		setupSteps = &steps
	}

	// 3. Opportunistic auth — never returns an error, just nil user
	var user *BootstrapUser
	if authz := c.Request().Header.Get("Authorization"); len(authz) > 7 && authz[:7] == "Bearer " {
		user = h.tryParseUser(authz[7:])
	}

	// 4. Public config
	cfg := h.getPublicConfig(ctx)

	return c.JSON(http.StatusOK, BootstrapResponse{
		Initialized: initialized,
		User:        user,
		Config:      cfg,
		Setup:       setupSteps,
	})
}

func (h *Bootstrap) tryParseUser(raw string) *BootstrapUser {
	tok, err := jwt.Parse(raw, func(t *jwt.Token) (interface{}, error) {
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
