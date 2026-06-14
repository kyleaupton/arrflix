package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

// Wants is the user-driven write surface over a want's lifecycle. Today that's
// cancel; the read surface (a want's state) lives on the Tracking handler.
type Wants struct{ svc *service.Services }

func NewWants(s *service.Services) *Wants { return &Wants{svc: s} }

// ----- Cancel -----

type WantsCancelInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Want ID"`
}

type WantsCancelOutput struct {
	Body model.Want
}

// Cancel flips a want (and its tracking) terminal and cancels its in-flight
// download job + pending import tasks. Any authenticated user may cancel today;
// restricting to requester-or-admin is the deferred per-action authz work.
func (h *Wants) Cancel(ctx context.Context, input *WantsCancelInput) (*WantsCancelOutput, error) {
	want, err := h.svc.Wants.CancelWant(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &WantsCancelOutput{Body: want}, nil
}

// ----- Register -----

func (h *Wants) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "wants-cancel",
		Method:      http.MethodPost,
		Path:        "/api/v1/wants/{id}/cancel",
		Summary:     "Cancel a want",
		Description: "Cancels a want (and its tracking), stopping its in-flight download job and pending import tasks. Idempotent on an already-canceled want; conflicts when the want is already terminal for another reason.",
		Tags:        []string{"tracking"},
		Errors:      errs(errsRead, errsWrite),
	}, h.Cancel)
}
