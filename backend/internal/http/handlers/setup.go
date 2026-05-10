package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

type Setup struct{ svc *service.Services }

func NewSetup(s *service.Services) *Setup { return &Setup{svc: s} }

func (h *Setup) guardUninitialized(ctx context.Context, op string) error {
	initialized, err := h.svc.Setup.IsInitialized(ctx)
	if err != nil {
		return err
	}
	if initialized {
		return apperrors.Conflictf("system already initialized").Op(op)
	}
	return nil
}

// ----- Status -----

type SetupStatusInput struct{}

type SetupStatusResponse struct {
	Initialized bool                `json:"initialized" doc:"Whether the system has completed setup"`
	Steps       *service.SetupSteps `json:"steps,omitempty" doc:"Per-step completion (only when not yet initialized)"`
}

type SetupStatusOutput struct {
	Body SetupStatusResponse
}

func (h *Setup) GetStatus(ctx context.Context, _ *SetupStatusInput) (*SetupStatusOutput, error) {
	if err := h.guardUninitialized(ctx, "SetupHandler.GetStatus"); err != nil {
		return nil, err
	}
	steps, err := h.svc.Setup.GetSetupSteps(ctx)
	if err != nil {
		return nil, err
	}
	return &SetupStatusOutput{Body: SetupStatusResponse{
		Initialized: false,
		Steps:       &steps,
	}}, nil
}

// ----- Initialize -----

type SetupInitializeInput struct {
	Body struct {
		Email    string `json:"email" required:"true" minLength:"1" format:"email" doc:"Admin email"`
		Username string `json:"username" required:"true" minLength:"1" doc:"Admin username"`
		Password string `json:"password" required:"true" minLength:"8" doc:"Admin password (>= 8 chars)"`
	}
}

type SetupInitializeResponse struct {
	Success bool `json:"success" doc:"Always true on success"`
}

type SetupInitializeOutput struct {
	Body SetupInitializeResponse
}

// Initialize: no guardUninitialized call — repo.InitializeSystem returns
// Conflict if already initialized.
func (h *Setup) Initialize(ctx context.Context, input *SetupInitializeInput) (*SetupInitializeOutput, error) {
	if err := h.svc.Setup.Initialize(ctx, input.Body.Email, input.Body.Username, input.Body.Password); err != nil {
		return nil, err
	}
	return &SetupInitializeOutput{Body: SetupInitializeResponse{Success: true}}, nil
}

// ----- TMDB key -----

type SetupTmdbInput struct {
	Body struct {
		ApiKey string `json:"api_key" required:"true" minLength:"1" doc:"TMDB API v3 key"`
	}
}

type SetupTmdbResponse struct {
	Success bool `json:"success" doc:"Always true on success"`
}

type SetupTmdbOutput struct {
	Body SetupTmdbResponse
}

func (h *Setup) SetTmdbKey(ctx context.Context, input *SetupTmdbInput) (*SetupTmdbOutput, error) {
	if err := h.guardUninitialized(ctx, "SetupHandler.SetTmdbKey"); err != nil {
		return nil, err
	}
	if err := h.svc.Setup.SetTmdbKey(ctx, input.Body.ApiKey); err != nil {
		return nil, err
	}
	return &SetupTmdbOutput{Body: SetupTmdbResponse{Success: true}}, nil
}

// ----- Register -----

// All three routes are public (bypass JWT) — the setup wizard runs before
// there's any admin to authenticate. Public-path entries live in
// middlewares/chi.go.
func (h *Setup) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "setup-status",
		Method:      http.MethodGet,
		Path:        "/api/v1/setup/status",
		Summary:     "Get setup status",
		Description: "Reports init status and per-step completion. Returns 409 once the system is initialized.",
		Tags:        []string{"setup"},
		Errors:      []int{http.StatusConflict},
	}, h.GetStatus)

	huma.Register(api, huma.Operation{
		OperationID: "setup-initialize",
		Method:      http.MethodPost,
		Path:        "/api/v1/setup/initialize",
		Summary:     "Initialize system",
		Description: "Creates the first admin and marks the system initialized.",
		Tags:        []string{"setup"},
		Errors:      errsWrite,
	}, h.Initialize)

	huma.Register(api, huma.Operation{
		OperationID: "setup-tmdb",
		Method:      http.MethodPost,
		Path:        "/api/v1/setup/tmdb",
		Summary:     "Set TMDB API key",
		Description: "Validates and stores the TMDB v3 key during setup. Returns 409 once the system is initialized.",
		Tags:        []string{"setup"},
		Errors:      errs(errsWrite, errsUpstream),
	}, h.SetTmdbKey)
}
