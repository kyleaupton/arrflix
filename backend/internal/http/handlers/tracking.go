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

// Tracking is the read-only observability surface over the tracking primitive
// and its wants and requesters. The spawn write path lives on the Requests
// handler; state transitions (pause/resume/cancel) are out of PoC scope.
type Tracking struct{ svc *service.Services }

func NewTracking(s *service.Services) *Tracking { return &Tracking{svc: s} }

// ----- List -----

type TrackingListInput struct{}

type TrackingListOutput struct {
	Body []model.Tracking
}

func (h *Tracking) List(ctx context.Context, _ *TrackingListInput) (*TrackingListOutput, error) {
	out, err := h.svc.Tracking.List(ctx)
	if err != nil {
		return nil, err
	}
	return &TrackingListOutput{Body: out}, nil
}

// ----- Get -----

type TrackingGetInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Tracking ID"`
}

type TrackingOutput struct {
	Body model.Tracking
}

func (h *Tracking) Get(ctx context.Context, input *TrackingGetInput) (*TrackingOutput, error) {
	out, err := h.svc.Tracking.Get(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &TrackingOutput{Body: out}, nil
}

// ----- List wants -----

type TrackingWantsInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Tracking ID"`
}

type TrackingWantsOutput struct {
	Body []model.Want
}

func (h *Tracking) ListWants(ctx context.Context, input *TrackingWantsInput) (*TrackingWantsOutput, error) {
	out, err := h.svc.Tracking.ListWants(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &TrackingWantsOutput{Body: out}, nil
}

// ----- Get by TMDB id -----

type TrackingByTmdbInput struct {
	TmdbID int64 `path:"tmdbId" minimum:"1" doc:"TMDB id of the movie"`
}

// TrackingByTmdb is the acquisition state for a movie keyed by TMDB id: the
// tracking plus its wants. Movie-only in the PoC, so there is one want.
type TrackingByTmdb struct {
	Tracking model.Tracking `json:"tracking"`
	Wants    []model.Want   `json:"wants"`
}

type TrackingByTmdbOutput struct {
	Body TrackingByTmdb
}

func (h *Tracking) GetByTmdb(ctx context.Context, input *TrackingByTmdbInput) (*TrackingByTmdbOutput, error) {
	tracking, wants, err := h.svc.Tracking.GetByTmdbID(ctx, input.TmdbID)
	if err != nil {
		return nil, err
	}
	return &TrackingByTmdbOutput{Body: TrackingByTmdb{Tracking: tracking, Wants: wants}}, nil
}

// ----- List requesters -----

type TrackingRequestersInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Tracking ID"`
}

type TrackingRequestersOutput struct {
	Body []model.TrackingRequester
}

func (h *Tracking) ListRequesters(ctx context.Context, input *TrackingRequestersInput) (*TrackingRequestersOutput, error) {
	out, err := h.svc.Tracking.ListRequesters(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &TrackingRequestersOutput{Body: out}, nil
}

// ----- Register -----

func (h *Tracking) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "tracking-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/tracking",
		Summary:     "List trackings",
		Tags:        []string{"tracking"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "tracking-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/tracking/{id}",
		Summary:     "Get tracking",
		Tags:        []string{"tracking"},
		Errors:      errsRead,
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "tracking-wants",
		Method:      http.MethodGet,
		Path:        "/api/v1/tracking/{id}/wants",
		Summary:     "List a tracking's wants",
		Tags:        []string{"tracking"},
		Errors:      errsRead,
	}, h.ListWants)

	huma.Register(api, huma.Operation{
		OperationID: "tracking-by-tmdb",
		Method:      http.MethodGet,
		Path:        "/api/v1/tracking/by-tmdb/{tmdbId}",
		Summary:     "Get tracking by TMDB id",
		Description: "Resolves a movie's acquisition state (tracking + wants) from its TMDB id. Returns 404 when the movie is not tracked.",
		Tags:        []string{"tracking"},
		Errors:      errsRead,
	}, h.GetByTmdb)

	huma.Register(api, huma.Operation{
		OperationID: "tracking-requesters",
		Method:      http.MethodGet,
		Path:        "/api/v1/tracking/{id}/requesters",
		Summary:     "List a tracking's requesters",
		Tags:        []string{"tracking"},
		Errors:      errsRead,
	}, h.ListRequesters)
}
