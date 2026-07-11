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

type RequestsListInput struct {
	// Status narrows the list to one lifecycle state. The approval queue is
	// ?status=pending viewed by a holder of requests.view.any; own/any scoping is
	// applied first, so a requester still sees only their own pending requests. An
	// omitted (empty) status lists every state.
	Status string `query:"status" enum:",pending,approved,spawned,denied,canceled" doc:"Filter by request status"`
}

type RequestsListOutput struct {
	Body []model.Request
}

func (h *Requests) List(ctx context.Context, input *RequestsListInput) (*RequestsListOutput, error) {
	userID, err := userIDFromCtx(ctx, "RequestsHandler.List")
	if err != nil {
		return nil, err
	}
	var status *string
	if input.Status != "" {
		status = &input.Status
	}
	out, err := h.svc.Requests.List(ctx, userID, status)
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
	userID, err := userIDFromCtx(ctx, "RequestsHandler.Get")
	if err != nil {
		return nil, err
	}
	out, err := h.svc.Requests.Get(ctx, userID, input.ID)
	if err != nil {
		return nil, err
	}
	return &RequestOutput{Body: out}, nil
}

// ----- Create -----

type requestCreateBody struct {
	TmdbID int64  `json:"tmdbId" required:"true" minimum:"1" doc:"TMDB id of the requested title"`
	Type   string `json:"type" required:"true" enum:"movie,series" doc:"Media domain"`
	// Everything below is optional intent. Tier and ScopeRule are part of the
	// frozen request (persisted through the pending state); the autonomy pair is
	// operator tracking config, applied only when the request auto-approves and
	// creates a fresh tracking. ScopeRule is series-only and inert for movies; a
	// requester joining an existing tracking never changes autonomy.
	Tier             *string `json:"tier,omitempty" enum:"HD,4K" doc:"Requested quality tier (defaults to 'HD'; operator-picked, requesters omit it)"`
	ScopeRule        *string `json:"scopeRule,omitempty" enum:"all,future_only" doc:"Series scope preset (series only; defaults to 'all')"`
	BackfillAutonomy *string `json:"backfillAutonomy,omitempty" enum:"auto,propose,manual" doc:"Who picks releases for atoms dated before tracking began (defaults to 'auto')"`
	OngoingAutonomy  *string `json:"ongoingAutonomy,omitempty" enum:"auto,propose,manual" doc:"Who picks releases for atoms dated after tracking began (defaults to 'auto')"`
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
		RequestedBy:      userID,
		TmdbID:           input.Body.TmdbID,
		Type:             input.Body.Type,
		Tier:             derefOr(input.Body.Tier, ""),
		ScopeRule:        derefOr(input.Body.ScopeRule, ""),
		BackfillAutonomy: derefOr(input.Body.BackfillAutonomy, ""),
		OngoingAutonomy:  derefOr(input.Body.OngoingAutonomy, ""),
	})
	if err != nil {
		return nil, err
	}
	return &RequestOutput{Body: out}, nil
}

// ----- Approve -----

type RequestsApproveInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Request ID"`
}

func (h *Requests) Approve(ctx context.Context, input *RequestsApproveInput) (*RequestOutput, error) {
	userID, err := userIDFromCtx(ctx, "RequestsHandler.Approve")
	if err != nil {
		return nil, err
	}
	out, err := h.svc.Requests.Approve(ctx, userID, input.ID)
	if err != nil {
		return nil, err
	}
	return &RequestOutput{Body: out}, nil
}

// ----- Deny -----

type RequestsDenyInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"Request ID"`
	Body struct {
		Reason *string `json:"reason,omitempty" doc:"Optional reason shown to the requester"`
	}
}

func (h *Requests) Deny(ctx context.Context, input *RequestsDenyInput) (*RequestOutput, error) {
	userID, err := userIDFromCtx(ctx, "RequestsHandler.Deny")
	if err != nil {
		return nil, err
	}
	out, err := h.svc.Requests.Deny(ctx, userID, input.ID, input.Body.Reason)
	if err != nil {
		return nil, err
	}
	return &RequestOutput{Body: out}, nil
}

// ----- Cancel -----

type RequestsCancelInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Request ID"`
}

func (h *Requests) Cancel(ctx context.Context, input *RequestsCancelInput) (*RequestOutput, error) {
	userID, err := userIDFromCtx(ctx, "RequestsHandler.Cancel")
	if err != nil {
		return nil, err
	}
	out, err := h.svc.Requests.Cancel(ctx, userID, input.ID)
	if err != nil {
		return nil, err
	}
	return &RequestOutput{Body: out}, nil
}

// derefOr returns *p, or def when p is nil — collapsing an omitted optional body
// field to the empty sentinel the service normalizes to its born default.
func derefOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
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
		Description:   "Validates the body and resolves the title against TMDB, then runs the spawn: an auto-approved request produces a tracking and a pending want; everyone else is persisted pending with no spawn, for an operator to approve or deny.",
		Tags:          []string{"requests"},
		DefaultStatus: http.StatusCreated,
		Errors:        errs(errsWrite, errsUpstream),
	}, h.Create)

	huma.Register(api, huma.Operation{
		OperationID: "requests-approve",
		Method:      http.MethodPost,
		Path:        "/api/v1/requests/{id}/approve",
		Summary:     "Approve a pending request",
		Description: "Approves a pending request and runs the spawn (tracking + want). Requires an approve permission for the request's media type. A non-pending request is a 409; an upstream TMDB failure is a 502.",
		Tags:        []string{"requests"},
		Errors:      errs(errsRead, errsForbidden, errsUpstream, []int{http.StatusConflict}),
	}, h.Approve)

	huma.Register(api, huma.Operation{
		OperationID: "requests-deny",
		Method:      http.MethodPost,
		Path:        "/api/v1/requests/{id}/deny",
		Summary:     "Deny a pending request",
		Description: "Denies a pending request with an optional reason. Requires a deny permission for the request's media type. A non-pending request is a 409.",
		Tags:        []string{"requests"},
		Errors:      errs(errsRead, errsForbidden, []int{http.StatusConflict}),
	}, h.Deny)

	huma.Register(api, huma.Operation{
		OperationID: "requests-cancel",
		Method:      http.MethodPost,
		Path:        "/api/v1/requests/{id}/cancel",
		Summary:     "Cancel a request",
		Description: "Withdraws a request (own/any). A spawned request drops the caller from its tracking; only the last requester's withdrawal cancels the tracking's wants and parks it. Already-denied is a 409; already-canceled is idempotent.",
		Tags:        []string{"requests"},
		Errors:      errs(errsRead, errsForbidden, []int{http.StatusConflict}),
	}, h.Cancel)
}
