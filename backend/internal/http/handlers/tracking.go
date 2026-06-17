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

// Tracking is the observability surface over the tracking primitive and its
// wants and requesters, plus the cancel (stop-tracking) write. The spawn write
// path lives on the Requests handler; pause/resume/archive remain out of scope.
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
	TmdbID int64  `path:"tmdbId" minimum:"1" doc:"TMDB id of the media item"`
	Type   string `query:"type" enum:"movie,series" default:"movie" doc:"Media domain"`
}

// TrackingByTmdb is the acquisition state for a media item keyed by TMDB id: the
// tracking plus its wants. A movie has one want; a series has one want per
// in-scope episode.
type TrackingByTmdb struct {
	Tracking model.Tracking `json:"tracking"`
	Wants    []model.Want   `json:"wants"`
}

type TrackingByTmdbOutput struct {
	Body TrackingByTmdb
}

func (h *Tracking) GetByTmdb(ctx context.Context, input *TrackingByTmdbInput) (*TrackingByTmdbOutput, error) {
	tracking, wants, err := h.svc.Tracking.GetByTmdbID(ctx, input.TmdbID, input.Type)
	if err != nil {
		return nil, err
	}
	return &TrackingByTmdbOutput{Body: TrackingByTmdb{Tracking: tracking, Wants: wants}}, nil
}

// ----- Cancel -----

type TrackingCancelInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Tracking ID"`
}

func (h *Tracking) Cancel(ctx context.Context, input *TrackingCancelInput) (*TrackingOutput, error) {
	out, err := h.svc.Tracking.Cancel(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &TrackingOutput{Body: out}, nil
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
		Description: "Resolves a media item's acquisition state (tracking + wants) from its TMDB id and type. Returns 404 when the item is not tracked.",
		Tags:        []string{"tracking"},
		Errors:      errsRead,
	}, h.GetByTmdb)

	huma.Register(api, huma.Operation{
		OperationID: "tracking-cancel",
		Method:      http.MethodPost,
		Path:        "/api/v1/tracking/{id}/cancel",
		Summary:     "Stop tracking",
		Description: "Cancels in-flight wants (and their download jobs) and marks the tracking canceled. Already-available wants and their files are left intact.",
		Tags:        []string{"tracking"},
		Errors:      errsRead,
	}, h.Cancel)

	huma.Register(api, huma.Operation{
		OperationID: "tracking-requesters",
		Method:      http.MethodGet,
		Path:        "/api/v1/tracking/{id}/requesters",
		Summary:     "List a tracking's requesters",
		Tags:        []string{"tracking"},
		Errors:      errsRead,
	}, h.ListRequesters)
}
