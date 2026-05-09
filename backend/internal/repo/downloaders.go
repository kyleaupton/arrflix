package repo

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

type DownloaderRepo interface {
	ListDownloaders(ctx context.Context) ([]model.Downloader, error)
	GetDownloader(ctx context.Context, id uuid.UUID) (model.Downloader, error)
	GetDefaultDownloader(ctx context.Context, protocol string) (model.Downloader, error)
	CreateDownloader(ctx context.Context, params CreateDownloaderParams) (model.Downloader, error)
	UpdateDownloader(ctx context.Context, params UpdateDownloaderParams) (model.Downloader, error)
	DeleteDownloader(ctx context.Context, id uuid.UUID) error
}

// CreateDownloaderParams is the domain-shaped input for CreateDownloader.
// Mirrors the writeable subset of model.Downloader.
type CreateDownloaderParams struct {
	Name           string
	DownloaderType string
	Protocol       string
	URL            string
	Username       *string
	Password       *string
	ConfigJSON     json.RawMessage
	Enabled        bool
	IsDefault      bool
}

// UpdateDownloaderParams is the domain-shaped input for UpdateDownloader.
// Mirrors the writeable subset of model.Downloader plus the target ID.
type UpdateDownloaderParams struct {
	ID             uuid.UUID
	Name           string
	DownloaderType string
	Protocol       string
	URL            string
	Username       *string
	Password       *string
	ConfigJSON     json.RawMessage
	Enabled        bool
	IsDefault      bool
}

// toModelDownloader translates the persistence-shaped dbgen.Downloader into
// the domain-shaped model.Downloader. ConfigJson (raw []byte) becomes a
// json.RawMessage; an empty/nil column is normalized to "{}" so the wire
// payload matches the prior downloaderToMap output (which always emitted at
// least an empty object).
func toModelDownloader(row dbgen.Downloader) model.Downloader {
	cfg := json.RawMessage(row.ConfigJson)
	if len(cfg) == 0 {
		cfg = json.RawMessage(`{}`)
	}
	return model.Downloader{
		ID:         uuidFromPgtype(row.ID),
		Name:       row.Name,
		Type:       row.Type,
		Protocol:   row.Protocol,
		URL:        row.Url,
		Username:   row.Username,
		Password:   row.Password,
		ConfigJSON: cfg,
		Enabled:    row.Enabled,
		Default:    row.Default,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}
}

func (r *Repository) ListDownloaders(ctx context.Context) ([]model.Downloader, error) {
	rows, err := r.Q.ListDownloaders(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list downloaders")
	}
	out := make([]model.Downloader, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelDownloader(row))
	}
	return out, nil
}

func (r *Repository) GetDownloader(ctx context.Context, id uuid.UUID) (model.Downloader, error) {
	row, err := r.Q.GetDownloader(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.Downloader{}, apperrors.FromPg(err, "downloader %s not found", id)
	}
	return toModelDownloader(row), nil
}

func (r *Repository) GetDefaultDownloader(ctx context.Context, protocol string) (model.Downloader, error) {
	row, err := r.Q.GetDefaultDownloader(ctx, protocol)
	if err != nil {
		return model.Downloader{}, apperrors.FromPg(err, "default %s downloader not found", protocol)
	}
	return toModelDownloader(row), nil
}

func (r *Repository) CreateDownloader(ctx context.Context, params CreateDownloaderParams) (model.Downloader, error) {
	row, err := r.Q.CreateDownloader(ctx, dbgen.CreateDownloaderParams{
		Name:           params.Name,
		DownloaderType: params.DownloaderType,
		Protocol:       params.Protocol,
		Url:            params.URL,
		Username:       params.Username,
		Password:       params.Password,
		ConfigJson:     []byte(params.ConfigJSON),
		Enabled:        params.Enabled,
		IsDefault:      params.IsDefault,
	})
	if err != nil {
		return model.Downloader{}, apperrors.FromPg(err, "create downloader %q", params.Name)
	}
	return toModelDownloader(row), nil
}

func (r *Repository) UpdateDownloader(ctx context.Context, params UpdateDownloaderParams) (model.Downloader, error) {
	row, err := r.Q.UpdateDownloader(ctx, dbgen.UpdateDownloaderParams{
		ID:             pgtypeFromUUID(params.ID),
		Name:           params.Name,
		DownloaderType: params.DownloaderType,
		Protocol:       params.Protocol,
		Url:            params.URL,
		Username:       params.Username,
		Password:       params.Password,
		ConfigJson:     []byte(params.ConfigJSON),
		Enabled:        params.Enabled,
		IsDefault:      params.IsDefault,
	})
	if err != nil {
		return model.Downloader{}, apperrors.FromPg(err, "update downloader %s", params.ID)
	}
	return toModelDownloader(row), nil
}

func (r *Repository) DeleteDownloader(ctx context.Context, id uuid.UUID) error {
	return apperrors.FromPg(r.Q.DeleteDownloader(ctx, pgtypeFromUUID(id)), "delete downloader %s", id)
}
