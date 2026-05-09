// feed.go is the humachi-shaped feed handler. The single GET /home
// endpoint returns the home-screen feed: rows of media curated for the
// authenticated user.
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

// FeedGetInput is empty — no parameters.
type FeedGetInput struct{}

// FeedGetOutput wraps model.Feed.
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

// RegisterHumachi wires the single feed operation onto the humachi API.
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
