// health.go is the humachi-shaped health handler. The single GET /health
// endpoint is public (bypasses both JWT and the setup-mode gate) — k8s
// liveness probes and uptime monitors hit it before anything else is up.
//
// Wire shape: 200 with `text/plain` body "ok\n". Pre-migration was an Echo
// `c.String(200, "ok")`; huma adds a trailing newline because it writes via
// the http.ResponseWriter (echo also added one). The `Body []byte` form
// bypasses huma's JSON serializer.
package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// ----- Handler -----

type Health struct{}

func NewHealth() *Health { return &Health{} }

// ----- Health -----

// HealthInput is empty — no parameters.
type HealthInput struct{}

// HealthOutput emits a `text/plain` body. The `[]byte` body bypasses huma's
// JSON serialization; the explicit Content-Type header documents the wire
// shape in the OpenAPI spec.
type HealthOutput struct {
	ContentType string `header:"Content-Type"`
	Body        []byte
}

// Health returns a fixed "ok" body. Liveness probe.
func (h *Health) Health(_ context.Context, _ *HealthInput) (*HealthOutput, error) {
	return &HealthOutput{
		ContentType: "text/plain",
		Body:        []byte("ok"),
	}, nil
}

// ----- Register -----

// RegisterHumachi wires the single health operation. The path is in the
// publicPathSet allowlist (middlewares/chi.go) so JWT is bypassed; the
// chi setup-mode middleware also explicitly allowlists /health.
func (h *Health) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "health-check",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Health check",
		Description: "Liveness probe; returns 200 with body \"ok\". Bypasses auth and setup-mode.",
		Tags:        []string{"health"},
	}, h.Health)
}
