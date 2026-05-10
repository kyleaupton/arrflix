package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

type Feed struct{ svc *service.Services }

func NewFeed(s *service.Services) *Feed { return &Feed{svc: s} }

// ----- Get -----

type FeedGetInput struct{}

type FeedGetOutput struct {
	Body model.Feed
}

// GetFeed returns the home-screen feed. Errors from the service flow through
// unchanged; there is no semantic-error surface to enumerate (the only
// failure modes are 500-class).
func (h *Feed) GetFeed(ctx context.Context, _ *FeedGetInput) (*FeedGetOutput, error) {
	feed, err := h.svc.Feed.GetFeed(ctx)
	if err != nil {
		return nil, err
	}
	return &FeedGetOutput{Body: *feed}, nil
}

// ----- Register -----

func (h *Feed) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "feed-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/home",
		Summary:     "Get home feed",
		Description: "Returns the curated home-screen feed (hero items + rows).",
		Tags:        []string{"feed"},
	}, h.GetFeed)
}
