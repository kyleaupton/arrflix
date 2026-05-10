package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

type Settings struct{ svc *service.Services }

func NewSettings(s *service.Services) *Settings { return &Settings{svc: s} }

// ----- List -----

type SettingsListResponse map[string]any

type SettingsListInput struct{}

type SettingsListOutput struct {
	Body SettingsListResponse
}

func (h *Settings) List(ctx context.Context, _ *SettingsListInput) (*SettingsListOutput, error) {
	out, err := h.svc.Settings.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	return &SettingsListOutput{Body: out}, nil
}

// ----- Patch -----

type SettingsPatchBody struct {
	Key   string `json:"key" required:"true" minLength:"1" doc:"Registered setting key"`
	Value any    `json:"value" doc:"Setting value; type must match the registry entry for the key"`
}

type SettingsPatchInput struct {
	Body SettingsPatchBody
}

type SettingsPatchOutput struct{}

func (h *Settings) Patch(ctx context.Context, input *SettingsPatchInput) (*SettingsPatchOutput, error) {
	if err := h.svc.Settings.Set(ctx, input.Body.Key, input.Body.Value); err != nil {
		return nil, err
	}
	return &SettingsPatchOutput{}, nil
}

// ----- Register -----

func (h *Settings) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "settings-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/settings",
		Summary:     "List settings",
		Tags:        []string{"settings"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID:   "settings-patch",
		Method:        http.MethodPatch,
		Path:          "/api/v1/settings",
		Summary:       "Update a setting",
		Tags:          []string{"settings"},
		DefaultStatus: http.StatusNoContent,
		// 404 = unknown setting key; 422 = value type mismatch with registry.
		// No conflict surface here — a setting overwrite cannot 409.
		Errors: []int{http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity},
	}, h.Patch)
}
