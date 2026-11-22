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

// Cancel stops acquiring a want: it flips the tracking to 'canceled' and
// cancels the want with any job not yet handed to the downloader. A download
// already in the downloader finishes and is imported. Any authenticated user may
// cancel today; restricting to requester-or-admin is the deferred per-action
// authz work.
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
		Description: "Stops acquiring a want and cancels its tracking. A want still searching, or whose download job hasn't been sent to the downloader, is cancelled with that job and its pending import tasks; a download already handed to the downloader is left to finish and be imported, and the want is returned as it stands. Idempotent on an already-canceled want; conflicts when the want is already terminal for another reason.",
		Tags:        []string{"tracking"},
		Errors:      errs(errsRead, errsWrite),
	}, h.Cancel)
}
