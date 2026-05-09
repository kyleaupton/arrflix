// setup.go is the humachi-shaped setup handler. The system can be in one of
// two modes: uninitialized (no admin yet) or initialized (admin + TMDB key
// in place). The setup endpoints are reachable only in uninitialized mode —
// once the system is initialized, every setup route emits 409 Conflict.
//
// Pre-migration this gate was group-scoped Echo middleware (SetupOnly).
// Humachi handlers don't share a chi group with custom middleware here, so
// the equivalent check now lives at the top of each handler. Wire behavior
// is unchanged.
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

// guardUninitialized returns Conflict if the system is already initialized.
// Mirrors the pre-migration SetupOnly Echo middleware exactly: any setup
// route hit after init responds 409.
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

// SetupStatusInput is empty.
type SetupStatusInput struct{}

// SetupStatusResponse is the wire shape returned by GET /setup/status.
type SetupStatusResponse struct {
	Initialized bool                `json:"initialized" doc:"Whether the system has completed setup"`
	Steps       *service.SetupSteps `json:"steps,omitempty" doc:"Per-step completion (only when not yet initialized)"`
}

// SetupStatusOutput wraps SetupStatusResponse.
type SetupStatusOutput struct {
	Body SetupStatusResponse
}

// GetStatus reports overall init status plus, when not yet initialized, the
// per-step completion of the setup wizard. Once the system is initialized
// the SetupOnly equivalent kicks in and the route returns 409 — matching the
// pre-migration Echo behavior exactly.
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

// SetupInitializeInput carries the bootstrap admin payload.
type SetupInitializeInput struct {
	Body struct {
		Email    string `json:"email" required:"true" minLength:"1" format:"email" doc:"Admin email"`
		Username string `json:"username" required:"true" minLength:"1" doc:"Admin username"`
		Password string `json:"password" required:"true" minLength:"8" doc:"Admin password (>= 8 chars)"`
	}
}

// SetupInitializeResponse is the wire shape returned by POST /setup/initialize.
type SetupInitializeResponse struct {
	Success bool `json:"success" doc:"Always true on success"`
}

// SetupInitializeOutput wraps SetupInitializeResponse.
type SetupInitializeOutput struct {
	Body SetupInitializeResponse
}

// Initialize creates the first admin and marks the system initialized. The
// service's repo.InitializeSystem returns Conflict if already initialized,
// so we don't need a separate guard on this op.
func (h *Setup) Initialize(ctx context.Context, input *SetupInitializeInput) (*SetupInitializeOutput, error) {
	if err := h.svc.Setup.Initialize(ctx, input.Body.Email, input.Body.Username, input.Body.Password); err != nil {
		return nil, err
	}
	return &SetupInitializeOutput{Body: SetupInitializeResponse{Success: true}}, nil
}

// ----- TMDB key -----

// SetupTmdbInput carries the TMDB API key payload.
type SetupTmdbInput struct {
	Body struct {
		ApiKey string `json:"api_key" required:"true" minLength:"1" doc:"TMDB API v3 key"`
	}
}

// SetupTmdbResponse is the wire shape returned by POST /setup/tmdb.
type SetupTmdbResponse struct {
	Success bool `json:"success" doc:"Always true on success"`
}

// SetupTmdbOutput wraps SetupTmdbResponse.
type SetupTmdbOutput struct {
	Body SetupTmdbResponse
}

// SetTmdbKey validates and persists the TMDB key during the setup wizard.
// Subject to the same SetupOnly equivalent as GetStatus: once the system is
// initialized this route 409s.
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

// RegisterHumachi wires the setup operations onto the humachi API. All three
// routes are public (bypass JWT) — the setup wizard runs before there's any
// admin to authenticate. Public-path entries live in middlewares/chi.go.
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
