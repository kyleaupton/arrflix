package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

type TitleStatus struct{ svc *service.Services }

func NewTitleStatus(s *service.Services) *TitleStatus { return &TitleStatus{svc: s} }

// ----- Get -----

type TitleStatusGetInput struct {
	MediaType string `path:"mediaType" enum:"movie,series" doc:"Media type"`
	TmdbID    int64  `path:"tmdbId" doc:"TMDB id"`
}

type TitleStatusGetOutput struct{ Body model.TitleStatus }

// Get returns the acquisition state of one title as seen by the caller. It is
// defined for every title, including ones nothing local knows about — an
// unrequested title is not_requested, not a 404.
func (h *TitleStatus) Get(ctx context.Context, input *TitleStatusGetInput) (*TitleStatusGetOutput, error) {
	userID, err := userIDFromContext(ctx, "TitleStatusHandler.Get")
	if err != nil {
		return nil, err
	}
	status, err := h.svc.TitleStatus.Get(ctx, userID, model.MediaType(input.MediaType), input.TmdbID)
	if err != nil {
		return nil, err
	}
	return &TitleStatusGetOutput{Body: status}, nil
}

// ----- Register -----

func (h *TitleStatus) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "title-status-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/titles/{mediaType}/{tmdbId}/status",
		Summary:     "Get a title's acquisition status",
		Description: "The acquisition read model for one title, computed for the calling user: headline state, what the library holds, what work is in flight, and per-episode state for a series.",
		Tags:        []string{"titles"},
	}, h.Get)
}
