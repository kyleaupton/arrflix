package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

type Version struct{ svc *service.Services }

func NewVersion(s *service.Services) *Version { return &Version{svc: s} }

// ----- Get -----

type VersionGetInput struct{}

type VersionGetOutput struct {
	Body service.VersionInfo
}

func (h *Version) GetVersion(ctx context.Context, _ *VersionGetInput) (*VersionGetOutput, error) {
	info, err := h.svc.Version.GetVersionInfo(ctx)
	if err != nil {
		return nil, err
	}
	return &VersionGetOutput{Body: info}, nil
}

// ----- Register -----

// /api/v1/version is in the publicPathSet allowlist (middlewares/chi.go) so
// it bypasses JWT.
func (h *Version) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "version-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/version",
		Summary:     "Get version and update information",
		Description: "Returns build metadata (version, commit, build date, components) plus update-check status. GitHub responses are cached for 15 minutes.",
		Tags:        []string{"version"},
	}, h.GetVersion)
}
