package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/http/middlewares"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

type Invites struct{ svc *service.Services }

func NewInvites(s *service.Services) *Invites { return &Invites{svc: s} }

// ----- List -----

type InvitesListInput struct{}

type InvitesListOutput struct {
	Body []model.Invite
}

func (h *Invites) List(ctx context.Context, _ *InvitesListInput) (*InvitesListOutput, error) {
	out, err := h.svc.Invites.List(ctx)
	if err != nil {
		return nil, err
	}
	return &InvitesListOutput{Body: out}, nil
}

// ----- Create -----

type InvitesCreateBody struct {
	Email string `json:"email" required:"true" format:"email" minLength:"1" doc:"Invitee email"`
	Role  string `json:"role,omitempty" enum:"requester,viewer,co_admin,admin" doc:"Target role for the invited user (defaults to requester)"`
}

type InvitesCreateInput struct {
	// Origin is the browser's origin on the create POST — the public URL the admin
	// reached Arrflix at. It's the fallback base for the emailed magic link when the
	// site.base_url setting is unset (fetch sends Origin on every POST).
	Origin string `header:"Origin"`
	Body   InvitesCreateBody
}

// InvitesCreateResponse returns the invite, the raw token, and whether the magic
// link was also emailed. The token is shown once, here: the admin builds the accept
// link (`<app-origin>/accept?token=<token>`) to copy — always the source of truth,
// whether or not it was emailed. It is never persisted in plaintext nor returned by
// any other op. Emailed is true only when SMTP is configured and a base URL was
// known; false means "copy the link" (unconfigured SMTP or no base URL).
type InvitesCreateResponse struct {
	Invite  model.Invite `json:"invite"`
	Token   string       `json:"token" doc:"Raw invite token; build the accept link as <app-origin>/accept?token=<token>"`
	Emailed bool         `json:"emailed" doc:"Whether the invite magic link was also emailed to the invitee"`
}

type InvitesCreateOutput struct {
	Body InvitesCreateResponse
}

func (h *Invites) Create(ctx context.Context, input *InvitesCreateInput) (*InvitesCreateOutput, error) {
	claims, ok := middlewares.ClaimsFromContext(ctx)
	if !ok {
		return nil, apperrors.Unauthenticatedf("missing credentials").Op("InvitesHandler.Create")
	}
	userIDStr, ok := claims["sub"].(string)
	if !ok {
		return nil, apperrors.Unauthenticatedf("invalid token subject").Op("InvitesHandler.Create")
	}
	invitedBy, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, apperrors.Unauthenticatedf("invalid token").Op("InvitesHandler.Create")
	}

	invite, rawToken, emailed, err := h.svc.Invites.Create(ctx, input.Body.Email, input.Body.Role, invitedBy, input.Origin)
	if err != nil {
		return nil, err
	}
	return &InvitesCreateOutput{Body: InvitesCreateResponse{Invite: invite, Token: rawToken, Emailed: emailed}}, nil
}

// ----- Delete -----

type InvitesDeleteInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Invite ID"`
}

type InvitesDeleteOutput struct{}

func (h *Invites) Delete(ctx context.Context, input *InvitesDeleteInput) (*InvitesDeleteOutput, error) {
	if err := h.svc.Invites.Delete(ctx, input.ID); err != nil {
		return nil, err
	}
	return &InvitesDeleteOutput{}, nil
}

// ----- Register -----

func (h *Invites) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "invites-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/invites",
		Summary:     "List invites",
		Tags:        []string{"invites"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID:   "invites-create",
		Method:        http.MethodPost,
		Path:          "/api/v1/invites",
		Summary:       "Create invite",
		Tags:          []string{"invites"},
		DefaultStatus: http.StatusCreated,
		Errors:        errsWrite,
	}, h.Create)

	huma.Register(api, huma.Operation{
		OperationID:   "invites-delete",
		Method:        http.MethodDelete,
		Path:          "/api/v1/invites/{id}",
		Summary:       "Delete invite",
		Tags:          []string{"invites"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errsRead,
	}, h.Delete)
}
