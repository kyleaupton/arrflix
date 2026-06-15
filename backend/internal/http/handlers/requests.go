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

type Requests struct{ svc *service.Services }

func NewRequests(s *service.Services) *Requests { return &Requests{svc: s} }

// ----- List -----

type RequestsListInput struct{}

type RequestsListOutput struct {
	Body []model.Request
}

func (h *Requests) List(ctx context.Context, _ *RequestsListInput) (*RequestsListOutput, error) {
	out, err := h.svc.Requests.List(ctx)
	if err != nil {
		return nil, err
	}
	return &RequestsListOutput{Body: out}, nil
}

// ----- Get -----

type RequestsGetInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Request ID"`
}

type RequestOutput struct {
	Body model.Request
}

func (h *Requests) Get(ctx context.Context, input *RequestsGetInput) (*RequestOutput, error) {
	out, err := h.svc.Requests.Get(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &RequestOutput{Body: out}, nil
}

// ----- Create -----

type requestCreateBody struct {
	TmdbID int64  `json:"tmdbId" required:"true" minimum:"1" doc:"TMDB id of the requested title"`
	Type   string `json:"type" required:"true" enum:"movie,series" doc:"Media domain"`
	Tier   string `json:"tier" required:"true" enum:"HD,4K" doc:"Requested quality tier"`
}

type RequestsCreateInput struct {
	Body requestCreateBody
}

func (h *Requests) Create(ctx context.Context, input *RequestsCreateInput) (*RequestOutput, error) {
	userID, err := userIDFromCtx(ctx, "RequestsHandler.Create")
	if err != nil {
		return nil, err
	}
	out, err := h.svc.Requests.Create(ctx, service.CreateRequestInput{
		RequestedBy: userID,
		TmdbID:      input.Body.TmdbID,
		Type:        input.Body.Type,
		Tier:        input.Body.Tier,
	})
	if err != nil {
		return nil, err
	}
	return &RequestOutput{Body: out}, nil
}

// ----- Register -----

func (h *Requests) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "requests-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/requests",
		Summary:     "List requests",
		Tags:        []string{"requests"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "requests-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/requests/{id}",
		Summary:     "Get request",
		Tags:        []string{"requests"},
		Errors:      errsRead,
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID:   "requests-create",
		Method:        http.MethodPost,
		Path:          "/api/v1/requests",
		Summary:       "Create a request",
		Description:   "Validates the body and resolves the title against TMDB, then runs the spawn: an approved movie request produces a tracking and a pending want; an unapproved one is persisted pending with no spawn.",
		Tags:          []string{"requests"},
		DefaultStatus: http.StatusCreated,
		Errors:        errs(errsWrite, errsUpstream),
	}, h.Create)
}
