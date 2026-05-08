package repo

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

type DownloaderRepo interface {
	ListDownloaders(ctx context.Context) ([]dbgen.Downloader, error)
	GetDownloader(ctx context.Context, id pgtype.UUID) (dbgen.Downloader, error)
	GetDefaultDownloader(ctx context.Context, protocol string) (dbgen.Downloader, error)
	CreateDownloader(ctx context.Context, name, downloaderType, protocol, url string, username, password *string, configJSON []byte, enabled, isDefault bool) (dbgen.Downloader, error)
	UpdateDownloader(ctx context.Context, id pgtype.UUID, name, downloaderType, protocol, url string, username, password *string, configJSON []byte, enabled, isDefault bool) (dbgen.Downloader, error)
	DeleteDownloader(ctx context.Context, id pgtype.UUID) error
}

func (r *Repository) ListDownloaders(ctx context.Context) ([]dbgen.Downloader, error) {
	dls, err := r.Q.ListDownloaders(ctx)
	return dls, apperrors.FromPg(err, "list downloaders")
}

func (r *Repository) GetDownloader(ctx context.Context, id pgtype.UUID) (dbgen.Downloader, error) {
	dl, err := r.Q.GetDownloader(ctx, id)
	return dl, apperrors.FromPg(err, "downloader %s not found", id)
}

func (r *Repository) GetDefaultDownloader(ctx context.Context, protocol string) (dbgen.Downloader, error) {
	dl, err := r.Q.GetDefaultDownloader(ctx, protocol)
	return dl, apperrors.FromPg(err, "default %s downloader not found", protocol)
}

func (r *Repository) CreateDownloader(ctx context.Context, name, downloaderType, protocol, url string, username, password *string, configJSON []byte, enabled, isDefault bool) (dbgen.Downloader, error) {
	var configJSONVal []byte
	if configJSON != nil && len(configJSON) > 0 {
		configJSONVal = configJSON
	}

	dl, err := r.Q.CreateDownloader(ctx, dbgen.CreateDownloaderParams{
		Name:           name,
		DownloaderType: downloaderType,
		Protocol:       protocol,
		Url:            url,
		Username:       username,
		Password:       password,
		ConfigJson:     configJSONVal,
		Enabled:        enabled,
		IsDefault:      isDefault,
	})
	return dl, apperrors.FromPg(err, "create downloader %q", name)
}

func (r *Repository) UpdateDownloader(ctx context.Context, id pgtype.UUID, name, downloaderType, protocol, url string, username, password *string, configJSON []byte, enabled, isDefault bool) (dbgen.Downloader, error) {
	var configJSONVal []byte
	if configJSON != nil && len(configJSON) > 0 {
		configJSONVal = configJSON
	}

	dl, err := r.Q.UpdateDownloader(ctx, dbgen.UpdateDownloaderParams{
		ID:             id,
		Name:           name,
		DownloaderType: downloaderType,
		Protocol:       protocol,
		Url:            url,
		Username:       username,
		Password:       password,
		ConfigJson:     configJSONVal,
		Enabled:        enabled,
		IsDefault:      isDefault,
	})
	return dl, apperrors.FromPg(err, "update downloader %s", id)
}

func (r *Repository) DeleteDownloader(ctx context.Context, id pgtype.UUID) error {
	return apperrors.FromPg(r.Q.DeleteDownloader(ctx, id), "delete downloader %s", id)
}
