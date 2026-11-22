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

type Volumes struct{ svc *service.Services }

func NewVolumes(s *service.Services) *Volumes { return &Volumes{svc: s} }

// ----- Shared body shape -----

type volumeAliasBody struct {
	DownloaderID uuid.UUID `json:"downloaderId" required:"true" format:"uuid" doc:"Downloader that sees this volume at base"`
	Base         string    `json:"base" required:"true" minLength:"1" doc:"Absolute path at which the downloader sees the volume"`
}

type volumeWriteBody struct {
	Name        string            `json:"name" required:"true" minLength:"1" maxLength:"100" doc:"Display name of the volume"`
	ArrflixBase string            `json:"arrflixBase" required:"true" minLength:"1" doc:"Absolute path at which Arrflix sees the volume; must be an existing directory"`
	Aliases     []volumeAliasBody `json:"aliases" required:"false" doc:"Per-downloader paths for the same storage; at most one per downloader"`
}

func (b volumeWriteBody) toInput() service.VolumeInput {
	in := service.VolumeInput{
		Name:        b.Name,
		ArrflixBase: b.ArrflixBase,
		Aliases:     make([]service.VolumeAliasInput, 0, len(b.Aliases)),
	}
	for _, a := range b.Aliases {
		in.Aliases = append(in.Aliases, service.VolumeAliasInput{DownloaderID: a.DownloaderID, Base: a.Base})
	}
	return in
}

// ----- List -----

type VolumesListInput struct{}

type VolumesListOutput struct {
	Body []model.Volume
}

func (h *Volumes) List(ctx context.Context, _ *VolumesListInput) (*VolumesListOutput, error) {
	out, err := h.svc.PathMapping.List(ctx)
	if err != nil {
		return nil, err
	}
	return &VolumesListOutput{Body: out}, nil
}

// ----- Get -----

type VolumesGetInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Volume ID"`
}

type VolumesGetOutput struct {
	Body model.Volume
}

func (h *Volumes) Get(ctx context.Context, input *VolumesGetInput) (*VolumesGetOutput, error) {
	v, err := h.svc.PathMapping.Get(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &VolumesGetOutput{Body: v}, nil
}

// ----- Create -----

type VolumesCreateInput struct {
	Body volumeWriteBody
}

type VolumesCreateOutput struct {
	Body model.Volume
}

func (h *Volumes) Create(ctx context.Context, input *VolumesCreateInput) (*VolumesCreateOutput, error) {
	v, err := h.svc.PathMapping.Create(ctx, input.Body.toInput())
	if err != nil {
		return nil, err
	}
	return &VolumesCreateOutput{Body: v}, nil
}

// ----- Update -----

type VolumesUpdateInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"Volume ID"`
	Body volumeWriteBody
}

type VolumesUpdateOutput struct {
	Body model.Volume
}

func (h *Volumes) Update(ctx context.Context, input *VolumesUpdateInput) (*VolumesUpdateOutput, error) {
	v, err := h.svc.PathMapping.Update(ctx, input.ID, input.Body.toInput())
	if err != nil {
		return nil, err
	}
	return &VolumesUpdateOutput{Body: v}, nil
}

// ----- Delete -----

type VolumesDeleteInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Volume ID"`
}

type VolumesDeleteOutput struct{}

func (h *Volumes) Delete(ctx context.Context, input *VolumesDeleteInput) (*VolumesDeleteOutput, error) {
	if err := h.svc.PathMapping.Delete(ctx, input.ID); err != nil {
		return nil, err
	}
	return &VolumesDeleteOutput{}, nil
}

// ----- Checks -----

type PathMappingChecksInput struct{}

type PathMappingChecksOutput struct {
	Body []model.DownloaderPathCheck
}

func (h *Volumes) Checks(ctx context.Context, _ *PathMappingChecksInput) (*PathMappingChecksOutput, error) {
	out, err := h.svc.PathMapping.Checks(ctx)
	if err != nil {
		return nil, err
	}
	return &PathMappingChecksOutput{Body: out}, nil
}

// ----- Preview -----

type pathMappingPreviewBody struct {
	VolumeID    *uuid.UUID        `json:"volumeId,omitempty" format:"uuid" doc:"Volume being edited; omit for a new one"`
	ArrflixBase string            `json:"arrflixBase" doc:"Draft Arrflix path; ignored unless absolute"`
	Aliases     []volumeAliasBody `json:"aliases" required:"false" doc:"Draft downloader aliases; any whose base isn't absolute is ignored"`
}

type PathMappingPreviewInput struct {
	Body pathMappingPreviewBody
}

type PathMappingPreviewOutput struct {
	Body []model.DownloaderPathCheck
}

func (h *Volumes) Preview(ctx context.Context, input *PathMappingPreviewInput) (*PathMappingPreviewOutput, error) {
	draft := service.VolumeDraft{ArrflixBase: input.Body.ArrflixBase}
	if input.Body.VolumeID != nil {
		draft.ID = *input.Body.VolumeID
	}
	for _, a := range input.Body.Aliases {
		draft.Aliases = append(draft.Aliases, service.VolumeAliasInput{DownloaderID: a.DownloaderID, Base: a.Base})
	}
	out, err := h.svc.PathMapping.Preview(ctx, draft)
	if err != nil {
		return nil, err
	}
	return &PathMappingPreviewOutput{Body: out}, nil
}

// ----- Test -----

type PathMappingTestInput struct {
	Body struct {
		DownloaderID uuid.UUID `json:"downloaderId" required:"true" format:"uuid" doc:"Downloader whose frame path is in"`
		Path         string    `json:"path" required:"true" minLength:"1" doc:"Absolute path as the downloader sees it"`
	}
}

type PathMappingTestOutput struct {
	Body model.PathTestResult
}

func (h *Volumes) Test(ctx context.Context, input *PathMappingTestInput) (*PathMappingTestOutput, error) {
	out, err := h.svc.PathMapping.Test(ctx, input.Body.DownloaderID, input.Body.Path)
	if err != nil {
		return nil, err
	}
	return &PathMappingTestOutput{Body: out}, nil
}

// ----- Register -----

func (h *Volumes) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "volumes-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/volumes",
		Summary:     "List volumes",
		Tags:        []string{"volumes"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "volumes-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/volumes/{id}",
		Summary:     "Get volume",
		Tags:        []string{"volumes"},
		Errors:      errsRead,
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID:   "volumes-create",
		Method:        http.MethodPost,
		Path:          "/api/v1/volumes",
		Summary:       "Create volume",
		Description:   "Create a volume with its downloader aliases. 409 when the name or the Arrflix base is already taken by another volume.",
		Tags:          []string{"volumes"},
		DefaultStatus: http.StatusCreated,
		Errors:        errsWrite,
	}, h.Create)

	huma.Register(api, huma.Operation{
		OperationID: "volumes-update",
		Method:      http.MethodPut,
		Path:        "/api/v1/volumes/{id}",
		Summary:     "Update volume",
		Description: "Replace a volume's fields and its whole alias set.",
		Tags:        []string{"volumes"},
		Errors:      errsUpsert,
	}, h.Update)

	huma.Register(api, huma.Operation{
		OperationID:   "volumes-delete",
		Method:        http.MethodDelete,
		Path:          "/api/v1/volumes/{id}",
		Summary:       "Delete volume",
		Tags:          []string{"volumes"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errsDelete,
	}, h.Delete)

	huma.Register(api, huma.Operation{
		OperationID: "path-mapping-checks",
		Method:      http.MethodGet,
		Path:        "/api/v1/path-mapping/checks",
		Summary:     "Check downloader save paths",
		Description: "Ask each downloader for its default save path, map it through the volumes, and report whether Arrflix sees a directory there. Nothing is persisted.",
		Tags:        []string{"volumes"},
	}, h.Checks)

	huma.Register(api, huma.Operation{
		OperationID: "path-mapping-preview",
		Method:      http.MethodPost,
		Path:        "/api/v1/path-mapping/preview",
		Summary:     "Preview downloader save path checks with a draft volume",
		Description: "Run the save-path checks as they would come out with the draft volume saved. Nothing is persisted.",
		Tags:        []string{"volumes"},
		Errors:      errsWrite,
	}, h.Preview)

	huma.Register(api, huma.Operation{
		OperationID: "path-mapping-test",
		Method:      http.MethodPost,
		Path:        "/api/v1/path-mapping/test",
		Summary:     "Test a downloader path",
		Description: "Map a path as a downloader sees it into Arrflix, report whether it exists, and for each library whether imports from it would hardlink or copy. Nothing is persisted.",
		Tags:        []string{"volumes"},
		Errors:      errsUpsert,
	}, h.Test)
}
