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

type HealthInput struct{}

// HealthOutput emits a `text/plain` body. The `[]byte` body bypasses huma's
// JSON serializer; the explicit Content-Type header documents the wire shape
// in the OpenAPI spec.
type HealthOutput struct {
	ContentType string `header:"Content-Type"`
	Body        []byte
}

func (h *Health) Health(_ context.Context, _ *HealthInput) (*HealthOutput, error) {
	return &HealthOutput{
		ContentType: "text/plain",
		Body:        []byte("ok"),
	}, nil
}

// ----- Register -----

// /health is in the publicPathSet allowlist (middlewares/chi.go) so it
// bypasses JWT and the setup-mode gate — k8s liveness probes hit it before
// anything else is up.
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
