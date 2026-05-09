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
}

type InvitesCreateInput struct {
	Body InvitesCreateBody
}

type InvitesCreateOutput struct {
	Body model.Invite
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

	invite, err := h.svc.Invites.Create(ctx, input.Body.Email, invitedBy)
	if err != nil {
		return nil, err
	}
	return &InvitesCreateOutput{Body: invite}, nil
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
