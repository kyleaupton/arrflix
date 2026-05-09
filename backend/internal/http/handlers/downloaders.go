package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/downloader"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

type Downloaders struct {
	svc               *service.Services
	downloaderManager *downloader.Manager
}

func NewDownloaders(s *service.Services, manager *downloader.Manager) *Downloaders {
	return &Downloaders{
		svc:               s,
		downloaderManager: manager,
	}
}

// ----- Shared body shape -----

type downloaderWriteBody struct {
	Name       string                 `json:"name" required:"true" minLength:"1" doc:"Display name"`
	Type       string                 `json:"type" required:"true" minLength:"1" doc:"Downloader implementation type (e.g. qbittorrent, sabnzbd)"`
	Protocol   string                 `json:"protocol" required:"true" enum:"torrent,usenet" doc:"Transport protocol"`
	URL        string                 `json:"url" required:"true" minLength:"1" doc:"Base URL of the downloader"`
	Username   *string                `json:"username,omitempty" doc:"Optional username"`
	Password   *string                `json:"password,omitempty" doc:"Optional password; on Update, empty string preserves existing"`
	ConfigJSON map[string]interface{} `json:"configJson,omitempty" doc:"Implementation-specific config blob"`
	Enabled    bool                   `json:"enabled" doc:"Whether the downloader is active"`
	Default    bool                   `json:"default" doc:"Whether this is the default for its protocol"`
}

// ----- List -----

type DownloadersListInput struct{}

type DownloadersListOutput struct {
	Body []model.DownloaderWithStatus
}

func (h *Downloaders) List(ctx context.Context, _ *DownloadersListInput) (*DownloadersListOutput, error) {
	downloaders, err := h.svc.Downloaders.List(ctx)
	if err != nil {
		return nil, err
	}

	initializedClients := h.downloaderManager.ListClients(ctx)
	initializedIDs := make(map[string]bool)
	for _, client := range initializedClients {
		initializedIDs[string(client.InstanceID())] = true
	}

	result := make([]model.DownloaderWithStatus, 0, len(downloaders))
	for _, dl := range downloaders {
		isInitialized := initializedIDs[dl.ID.String()] && dl.Enabled
		result = append(result, model.DownloaderWithStatus{
			Downloader:  dl,
			Initialized: isInitialized,
		})
	}

	return &DownloadersListOutput{Body: result}, nil
}

// ----- Get -----

type DownloadersGetInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Downloader ID"`
}

type DownloadersGetOutput struct {
	Body model.Downloader
}

func (h *Downloaders) Get(ctx context.Context, input *DownloadersGetInput) (*DownloadersGetOutput, error) {
	dl, err := h.svc.Downloaders.Get(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &DownloadersGetOutput{Body: dl}, nil
}

// ----- GetDefault -----

type DownloadersGetDefaultInput struct {
	Protocol string `path:"protocol" enum:"torrent,usenet" doc:"Protocol"`
}

type DownloadersGetDefaultOutput struct {
	Body model.Downloader
}

func (h *Downloaders) GetDefault(ctx context.Context, input *DownloadersGetDefaultInput) (*DownloadersGetDefaultOutput, error) {
	dl, err := h.svc.Downloaders.GetDefault(ctx, input.Protocol)
	if err != nil {
		return nil, err
	}
	return &DownloadersGetDefaultOutput{Body: dl}, nil
}

// ----- Create -----

type DownloadersCreateInput struct {
	Body downloaderWriteBody
}

type DownloadersCreateOutput struct {
	Body model.Downloader
}

func (h *Downloaders) Create(ctx context.Context, input *DownloadersCreateInput) (*DownloadersCreateOutput, error) {
	dl, err := h.svc.Downloaders.Create(
		ctx,
		input.Body.Name,
		input.Body.Type,
		input.Body.Protocol,
		input.Body.URL,
		input.Body.Username,
		input.Body.Password,
		input.Body.ConfigJSON,
		input.Body.Enabled,
		input.Body.Default,
	)
	if err != nil {
		return nil, err
	}

	if dl.Enabled {
		// Best-effort initialization; persistence already succeeded so the
		// caller can retry if this fails.
		_ = h.downloaderManager.InitializeDownloader(ctx, dl.ID.String())
	}

	return &DownloadersCreateOutput{Body: dl}, nil
}

// ----- Update -----

type DownloadersUpdateInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"Downloader ID"`
	Body downloaderWriteBody
}

type DownloadersUpdateOutput struct {
	Body model.Downloader
}

func (h *Downloaders) Update(ctx context.Context, input *DownloadersUpdateInput) (*DownloadersUpdateOutput, error) {
	// Empty-string password is a no-op signal: keep existing.
	var password *string
	if input.Body.Password != nil && *input.Body.Password != "" {
		password = input.Body.Password
	}

	dl, err := h.svc.Downloaders.Update(
		ctx,
		input.ID,
		input.Body.Name,
		input.Body.Type,
		input.Body.Protocol,
		input.Body.URL,
		input.Body.Username,
		password,
		input.Body.ConfigJSON,
		input.Body.Enabled,
		input.Body.Default,
	)
	if err != nil {
		return nil, err
	}

	_ = h.downloaderManager.InitializeDownloader(ctx, dl.ID.String())

	return &DownloadersUpdateOutput{Body: dl}, nil
}

// ----- Delete -----

type DownloadersDeleteInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Downloader ID"`
}

type DownloadersDeleteOutput struct{}

func (h *Downloaders) Delete(ctx context.Context, input *DownloadersDeleteInput) (*DownloadersDeleteOutput, error) {
	h.downloaderManager.RemoveClient(ctx, input.ID.String())
	if err := h.svc.Downloaders.Delete(ctx, input.ID); err != nil {
		return nil, err
	}
	return &DownloadersDeleteOutput{}, nil
}

// ----- Test -----

type DownloadersTestInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Downloader ID"`
}

type DownloadersTestOutput struct {
	Body downloader.TestResult
}

func (h *Downloaders) Test(ctx context.Context, input *DownloadersTestInput) (*DownloadersTestOutput, error) {
	dl, err := h.svc.Downloaders.Get(ctx, input.ID)
	if err != nil {
		return nil, err
	}

	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client, err := h.downloaderManager.BuildTestClient(ctx, dl.ID.String())
	if err != nil {
		return nil, apperrors.Internalf("failed to build test client: %v", err).Op("DownloadersHandler.Test")
	}

	result, err := client.Test(testCtx)
	if err != nil {
		return nil, apperrors.BadGatewayf("%v", err).Op("DownloadersHandler.Test")
	}

	return &DownloadersTestOutput{Body: result}, nil
}

// ----- TestConfig -----

type DownloadersTestConfigBody struct {
	Type       string                 `json:"type" required:"true" minLength:"1" doc:"Downloader implementation type"`
	URL        string                 `json:"url" required:"true" minLength:"1" doc:"Base URL"`
	Username   *string                `json:"username,omitempty" doc:"Optional username"`
	Password   *string                `json:"password,omitempty" doc:"Optional password"`
	ConfigJSON map[string]interface{} `json:"configJson,omitempty" doc:"Implementation-specific config blob"`
}

type DownloadersTestConfigInput struct {
	Body DownloadersTestConfigBody
}

type DownloadersTestConfigOutput struct {
	Body downloader.TestResult
}

func (h *Downloaders) TestConfig(ctx context.Context, input *DownloadersTestConfigInput) (*DownloadersTestConfigOutput, error) {
	var configJSON []byte
	if input.Body.ConfigJSON != nil {
		var err error
		configJSON, err = json.Marshal(input.Body.ConfigJSON)
		if err != nil {
			return nil, apperrors.Validation("invalid configJson",
				apperrors.Field("body.configJson", err.Error()),
			).Op("DownloadersHandler.TestConfig")
		}
	}

	instanceID := downloader.InstanceID("test-" + time.Now().Format("20060102150405"))

	rec := downloader.ConfigRecord{
		ID:       instanceID,
		Type:     downloader.Type(input.Body.Type),
		URL:      input.Body.URL,
		Username: input.Body.Username,
		Password: input.Body.Password,
		Config:   configJSON,
	}

	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client, err := h.downloaderManager.BuildClientFromConfig(ctx, rec)
	if err != nil {
		return nil, apperrors.Internalf("failed to build test client: %v", err).Op("DownloadersHandler.TestConfig")
	}

	result, err := client.Test(testCtx)
	if err != nil {
		return nil, apperrors.BadGatewayf("%v", err).Op("DownloadersHandler.TestConfig")
	}

	return &DownloadersTestConfigOutput{Body: result}, nil
}

// ----- Register -----

func (h *Downloaders) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "downloaders-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/downloaders",
		Summary:     "List downloaders",
		Tags:        []string{"downloaders"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID:   "downloaders-create",
		Method:        http.MethodPost,
		Path:          "/api/v1/downloaders",
		Summary:       "Create downloader",
		Tags:          []string{"downloaders"},
		DefaultStatus: http.StatusCreated,
		Errors:        errsWrite,
	}, h.Create)

	huma.Register(api, huma.Operation{
		OperationID: "downloaders-get-default",
		Method:      http.MethodGet,
		Path:        "/api/v1/downloaders/default/{protocol}",
		Summary:     "Get default downloader by protocol",
		Tags:        []string{"downloaders"},
		Errors:      errsRead,
	}, h.GetDefault)

	huma.Register(api, huma.Operation{
		OperationID: "downloaders-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/downloaders/{id}",
		Summary:     "Get downloader",
		Tags:        []string{"downloaders"},
		Errors:      errsRead,
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "downloaders-update",
		Method:      http.MethodPut,
		Path:        "/api/v1/downloaders/{id}",
		Summary:     "Update downloader",
		Tags:        []string{"downloaders"},
		Errors:      errsUpsert,
	}, h.Update)

	huma.Register(api, huma.Operation{
		OperationID:   "downloaders-delete",
		Method:        http.MethodDelete,
		Path:          "/api/v1/downloaders/{id}",
		Summary:       "Delete downloader",
		Tags:          []string{"downloaders"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errsDelete,
	}, h.Delete)

	huma.Register(api, huma.Operation{
		OperationID: "downloaders-test",
		Method:      http.MethodPost,
		Path:        "/api/v1/downloaders/{id}/test",
		Summary:     "Test downloader connection",
		Tags:        []string{"downloaders"},
		Errors:      errs(errsRead, errsUpstream),
	}, h.Test)

	huma.Register(api, huma.Operation{
		OperationID: "downloaders-test-config",
		Method:      http.MethodPost,
		Path:        "/api/v1/downloaders/test",
		Summary:     "Test downloader connection from form configuration",
		Tags:        []string{"downloaders"},
		Errors:      errs(errsWrite, errsUpstream),
	}, h.TestConfig)
}
