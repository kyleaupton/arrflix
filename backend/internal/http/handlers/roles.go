package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

type Roles struct{ svc *service.Services }

func NewRoles(s *service.Services) *Roles { return &Roles{svc: s} }

// ----- List -----

type RolesListInput struct{}

type RolesListOutput struct {
	Body []model.Role
}

func (h *Roles) List(ctx context.Context, _ *RolesListInput) (*RolesListOutput, error) {
	roles, err := h.svc.Users.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	return &RolesListOutput{Body: roles}, nil
}

// ----- Register -----

func (h *Roles) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "roles-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/roles",
		Summary:     "List roles",
		Tags:        []string{"roles"},
	}, h.List)
}
