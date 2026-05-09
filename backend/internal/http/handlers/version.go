// version.go is the humachi-shaped version handler. The single GET /version
// endpoint is public (bypasses JWT) and returns build metadata + update
// status. The service does the GitHub-API fetch and 15-minute caching;
// the handler is a thin pass-through.
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

// VersionGetInput is empty — no parameters.
type VersionGetInput struct{}

// VersionGetOutput wraps service.VersionInfo as-is so the wire shape matches
// the pre-migration Echo response byte-for-byte.
type VersionGetOutput struct {
	Body service.VersionInfo
}

// GetVersion returns build metadata plus update-check status. Errors flow
// through from the service; in practice the GitHub fallback path returns
// `status: unknown` rather than erroring, so this op's only meaningful
// failure mode is a generic 500 (universal — omitted from Errors).
func (h *Version) GetVersion(ctx context.Context, _ *VersionGetInput) (*VersionGetOutput, error) {
	info, err := h.svc.Version.GetVersionInfo(ctx)
	if err != nil {
		return nil, err
	}
	return &VersionGetOutput{Body: info}, nil
}

// ----- Register -----

// RegisterHumachi wires the single version operation. The path is in the
// publicPathSet allowlist (middlewares/chi.go) so JWT is bypassed.
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
