package service

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/repo"
)

type DownloadersService struct {
	repo *repo.Repository
}

func NewDownloadersService(r *repo.Repository) *DownloadersService {
	return &DownloadersService{repo: r}
}

func (s *DownloadersService) List(ctx context.Context) ([]dbgen.Downloader, error) {
	return s.repo.ListDownloaders(ctx)
}

func (s *DownloadersService) Get(ctx context.Context, id pgtype.UUID) (dbgen.Downloader, error) {
	return s.repo.GetDownloader(ctx, id)
}

func (s *DownloadersService) GetDefault(ctx context.Context, protocol string) (dbgen.Downloader, error) {
	return s.repo.GetDefaultDownloader(ctx, protocol)
}

// validateDownloaderInput collects every field-level problem with the supplied
// downloader payload before returning. Returns nil when the input is valid.
func validateDownloaderInput(name, downloaderType, protocol, downloaderURL string) *apperrors.Error {
	var fields []apperrors.FieldError
	if name == "" {
		fields = append(fields, apperrors.Field("body.name", "required"))
	}
	if downloaderType != "qbittorrent" {
		fields = append(fields, apperrors.Field("body.type", "must be 'qbittorrent'"))
	}
	if protocol != "torrent" && protocol != "usenet" {
		fields = append(fields, apperrors.Field("body.protocol", "must be 'torrent' or 'usenet'"))
	}
	if downloaderURL == "" {
		fields = append(fields, apperrors.Field("body.url", "required"))
	} else if _, err := url.Parse(downloaderURL); err != nil {
		fields = append(fields, apperrors.Field("body.url", "invalid url format"))
	}
	if len(fields) > 0 {
		return apperrors.Validation("invalid downloader", fields...)
	}
	return nil
}

func (s *DownloadersService) Create(ctx context.Context, name, downloaderType, protocol, downloaderURL string, username, password *string, configJSON map[string]interface{}, enabled, isDefault bool) (dbgen.Downloader, error) {
	if err := validateDownloaderInput(name, downloaderType, protocol, downloaderURL); err != nil {
		return dbgen.Downloader{}, err.Op("DownloadersService.Create")
	}

	// If setting as default, unset other defaults of same protocol
	if isDefault {
		existingDefaults, err := s.repo.ListDownloaders(ctx)
		if err == nil {
			for _, d := range existingDefaults {
				if d.Protocol == protocol && d.Default {
					// Unset this default
					_, _ = s.repo.UpdateDownloader(ctx, d.ID, d.Name, d.Type, d.Protocol, d.Url, d.Username, d.Password, d.ConfigJson, d.Enabled, false)
				}
			}
		}
	}

	var configJSONBytes []byte
	if configJSON != nil {
		var err error
		configJSONBytes, err = json.Marshal(configJSON)
		if err != nil {
			return dbgen.Downloader{}, apperrors.Validation("invalid downloader",
				apperrors.Field("body.config_json", "must be valid JSON"),
			).Op("DownloadersService.Create")
		}
	}

	return s.repo.CreateDownloader(ctx, name, downloaderType, protocol, downloaderURL, username, password, configJSONBytes, enabled, isDefault)
}

func (s *DownloadersService) Update(ctx context.Context, id pgtype.UUID, name, downloaderType, protocol, downloaderURL string, username, password *string, configJSON map[string]interface{}, enabled, isDefault bool) (dbgen.Downloader, error) {
	if err := validateDownloaderInput(name, downloaderType, protocol, downloaderURL); err != nil {
		return dbgen.Downloader{}, err.Op("DownloadersService.Update")
	}

	// If setting as default, unset other defaults of same protocol
	if isDefault {
		existingDefaults, err := s.repo.ListDownloaders(ctx)
		if err == nil {
			for _, d := range existingDefaults {
				if d.Protocol == protocol && d.Default && d.ID != id {
					// Unset this default
					_, _ = s.repo.UpdateDownloader(ctx, d.ID, d.Name, d.Type, d.Protocol, d.Url, d.Username, d.Password, d.ConfigJson, d.Enabled, false)
				}
			}
		}
	}

	var configJSONBytes []byte
	if configJSON != nil {
		var err error
		configJSONBytes, err = json.Marshal(configJSON)
		if err != nil {
			return dbgen.Downloader{}, apperrors.Validation("invalid downloader",
				apperrors.Field("body.config_json", "must be valid JSON"),
			).Op("DownloadersService.Update")
		}
	}

	// If password is nil, fetch existing downloader to preserve its password
	var passwordToUse *string = password
	if password == nil {
		existing, err := s.repo.GetDownloader(ctx, id)
		if err != nil {
			return dbgen.Downloader{}, err
		}
		passwordToUse = existing.Password
	}

	return s.repo.UpdateDownloader(ctx, id, name, downloaderType, protocol, downloaderURL, username, passwordToUse, configJSONBytes, enabled, isDefault)
}

func (s *DownloadersService) Delete(ctx context.Context, id pgtype.UUID) error {
	return s.repo.DeleteDownloader(ctx, id)
}
