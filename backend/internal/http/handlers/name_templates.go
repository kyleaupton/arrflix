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

type NameTemplates struct{ svc *service.Services }

func NewNameTemplates(s *service.Services) *NameTemplates { return &NameTemplates{svc: s} }

// ----- Shared body shape -----

type nameTemplateWriteBody struct {
	Name                 string  `json:"name" required:"true" minLength:"1" doc:"Display name"`
	Type                 string  `json:"type" required:"true" enum:"movie,series" doc:"Template type"`
	Template             string  `json:"template" required:"true" minLength:"1" doc:"File-name template"`
	SeriesShowTemplate   *string `json:"seriesShowTemplate,omitempty" doc:"Optional series show-folder template"`
	SeriesSeasonTemplate *string `json:"seriesSeasonTemplate,omitempty" doc:"Optional series season-folder template"`
	MovieDirTemplate     *string `json:"movieDirTemplate,omitempty" doc:"Optional movie-folder template"`
	Default              bool    `json:"default" doc:"Whether this is the default template for its type"`
}

// ----- List -----

type NameTemplatesListInput struct{}

type NameTemplatesListOutput struct {
	Body []model.NameTemplate
}

func (h *NameTemplates) List(ctx context.Context, _ *NameTemplatesListInput) (*NameTemplatesListOutput, error) {
	out, err := h.svc.NameTemplates.List(ctx)
	if err != nil {
		return nil, err
	}
	return &NameTemplatesListOutput{Body: out}, nil
}

// ----- Get -----

type NameTemplatesGetInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Name template ID"`
}

type NameTemplatesGetOutput struct {
	Body model.NameTemplate
}

func (h *NameTemplates) Get(ctx context.Context, input *NameTemplatesGetInput) (*NameTemplatesGetOutput, error) {
	out, err := h.svc.NameTemplates.Get(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &NameTemplatesGetOutput{Body: out}, nil
}

// ----- Create -----

type NameTemplatesCreateInput struct {
	Body nameTemplateWriteBody
}

type NameTemplatesCreateOutput struct {
	Body model.NameTemplate
}

func (h *NameTemplates) Create(ctx context.Context, input *NameTemplatesCreateInput) (*NameTemplatesCreateOutput, error) {
	out, err := h.svc.NameTemplates.Create(
		ctx,
		input.Body.Name,
		input.Body.Type,
		input.Body.Template,
		input.Body.SeriesShowTemplate,
		input.Body.SeriesSeasonTemplate,
		input.Body.MovieDirTemplate,
		input.Body.Default,
	)
	if err != nil {
		return nil, err
	}
	return &NameTemplatesCreateOutput{Body: out}, nil
}

// ----- Update -----

type NameTemplatesUpdateInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"Name template ID"`
	Body nameTemplateWriteBody
}

type NameTemplatesUpdateOutput struct {
	Body model.NameTemplate
}

func (h *NameTemplates) Update(ctx context.Context, input *NameTemplatesUpdateInput) (*NameTemplatesUpdateOutput, error) {
	out, err := h.svc.NameTemplates.Update(
		ctx,
		input.ID,
		input.Body.Name,
		input.Body.Type,
		input.Body.Template,
		input.Body.SeriesShowTemplate,
		input.Body.SeriesSeasonTemplate,
		input.Body.MovieDirTemplate,
		input.Body.Default,
	)
	if err != nil {
		return nil, err
	}
	return &NameTemplatesUpdateOutput{Body: out}, nil
}

// ----- Delete -----

type NameTemplatesDeleteInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Name template ID"`
}

type NameTemplatesDeleteOutput struct{}

func (h *NameTemplates) Delete(ctx context.Context, input *NameTemplatesDeleteInput) (*NameTemplatesDeleteOutput, error) {
	if err := h.svc.NameTemplates.Delete(ctx, input.ID); err != nil {
		return nil, err
	}
	return &NameTemplatesDeleteOutput{}, nil
}

// ----- GetDefault -----

type NameTemplatesGetDefaultInput struct {
	Type string `path:"type" enum:"movie,series" doc:"Template type"`
}

type NameTemplatesGetDefaultOutput struct {
	Body model.NameTemplate
}

func (h *NameTemplates) GetDefault(ctx context.Context, input *NameTemplatesGetDefaultInput) (*NameTemplatesGetDefaultOutput, error) {
	out, err := h.svc.NameTemplates.GetDefault(ctx, input.Type)
	if err != nil {
		return nil, err
	}
	return &NameTemplatesGetDefaultOutput{Body: out}, nil
}

// ----- Register -----

func (h *NameTemplates) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "name-templates-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/name-templates",
		Summary:     "List name templates",
		Tags:        []string{"name-templates"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "name-templates-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/name-templates/{id}",
		Summary:     "Get name template",
		Tags:        []string{"name-templates"},
		Errors:      errsRead,
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID:   "name-templates-create",
		Method:        http.MethodPost,
		Path:          "/api/v1/name-templates",
		Summary:       "Create name template",
		Tags:          []string{"name-templates"},
		DefaultStatus: http.StatusCreated,
		Errors:        errsWrite,
	}, h.Create)

	huma.Register(api, huma.Operation{
		OperationID: "name-templates-update",
		Method:      http.MethodPut,
		Path:        "/api/v1/name-templates/{id}",
		Summary:     "Update name template",
		Tags:        []string{"name-templates"},
		Errors:      errsUpsert,
	}, h.Update)

	huma.Register(api, huma.Operation{
		OperationID:   "name-templates-delete",
		Method:        http.MethodDelete,
		Path:          "/api/v1/name-templates/{id}",
		Summary:       "Delete name template",
		Tags:          []string{"name-templates"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errsDelete,
	}, h.Delete)

	huma.Register(api, huma.Operation{
		OperationID: "name-templates-get-default",
		Method:      http.MethodGet,
		Path:        "/api/v1/name-templates/default/{type}",
		Summary:     "Get default name template by type",
		Tags:        []string{"name-templates"},
		Errors:      errsRead,
	}, h.GetDefault)
}
