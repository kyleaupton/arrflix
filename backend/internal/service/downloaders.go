package service

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

type DownloadersService struct {
	repo *repo.Repository
}

func NewDownloadersService(r *repo.Repository) *DownloadersService {
	return &DownloadersService{repo: r}
}

func (s *DownloadersService) List(ctx context.Context) ([]model.Downloader, error) {
	return s.repo.ListDownloaders(ctx)
}

func (s *DownloadersService) Get(ctx context.Context, id uuid.UUID) (model.Downloader, error) {
	return s.repo.GetDownloader(ctx, id)
}

func (s *DownloadersService) GetDefault(ctx context.Context, protocol string) (model.Downloader, error) {
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

// marshalConfig serializes the loose config map to a json.RawMessage. Returns
// a typed Validation error if marshaling fails.
func marshalConfig(configJSON map[string]any, op string) (json.RawMessage, error) {
	if configJSON == nil {
		return nil, nil
	}
	b, err := json.Marshal(configJSON)
	if err != nil {
		return nil, apperrors.Validation("invalid downloader",
			apperrors.Field("body.config_json", "must be valid JSON"),
		).Op(op)
	}
	return json.RawMessage(b), nil
}

func (s *DownloadersService) Create(ctx context.Context, name, downloaderType, protocol, downloaderURL string, username, password *string, configJSON map[string]any, enabled, isDefault bool) (model.Downloader, error) {
	if err := validateDownloaderInput(name, downloaderType, protocol, downloaderURL); err != nil {
		return model.Downloader{}, err.Op("DownloadersService.Create")
	}

	// If setting as default, unset other defaults of same protocol
	if isDefault {
		existingDefaults, err := s.repo.ListDownloaders(ctx)
		if err == nil {
			for _, d := range existingDefaults {
				if d.Protocol == protocol && d.Default {
					// Unset this default
					_, _ = s.repo.UpdateDownloader(ctx, repo.UpdateDownloaderParams{
						ID:             d.ID,
						Name:           d.Name,
						DownloaderType: d.Type,
						Protocol:       d.Protocol,
						URL:            d.URL,
						Username:       d.Username,
						Password:       d.Password,
						ConfigJSON:     d.ConfigJSON,
						Enabled:        d.Enabled,
						IsDefault:      false,
					})
				}
			}
		}
	}

	cfg, err := marshalConfig(configJSON, "DownloadersService.Create")
	if err != nil {
		return model.Downloader{}, err
	}

	return s.repo.CreateDownloader(ctx, repo.CreateDownloaderParams{
		Name:           name,
		DownloaderType: downloaderType,
		Protocol:       protocol,
		URL:            downloaderURL,
		Username:       username,
		Password:       password,
		ConfigJSON:     cfg,
		Enabled:        enabled,
		IsDefault:      isDefault,
	})
}

func (s *DownloadersService) Update(ctx context.Context, id uuid.UUID, name, downloaderType, protocol, downloaderURL string, username, password *string, configJSON map[string]any, enabled, isDefault bool) (model.Downloader, error) {
	if err := validateDownloaderInput(name, downloaderType, protocol, downloaderURL); err != nil {
		return model.Downloader{}, err.Op("DownloadersService.Update")
	}

	// If setting as default, unset other defaults of same protocol
	if isDefault {
		existingDefaults, err := s.repo.ListDownloaders(ctx)
		if err == nil {
			for _, d := range existingDefaults {
				if d.Protocol == protocol && d.Default && d.ID != id {
					// Unset this default
					_, _ = s.repo.UpdateDownloader(ctx, repo.UpdateDownloaderParams{
						ID:             d.ID,
						Name:           d.Name,
						DownloaderType: d.Type,
						Protocol:       d.Protocol,
						URL:            d.URL,
						Username:       d.Username,
						Password:       d.Password,
						ConfigJSON:     d.ConfigJSON,
						Enabled:        d.Enabled,
						IsDefault:      false,
					})
				}
			}
		}
	}

	cfg, err := marshalConfig(configJSON, "DownloadersService.Update")
	if err != nil {
		return model.Downloader{}, err
	}

	// If password is nil, fetch existing downloader to preserve its password
	passwordToUse := password
	if password == nil {
		existing, err := s.repo.GetDownloader(ctx, id)
		if err != nil {
			return model.Downloader{}, err
		}
		passwordToUse = existing.Password
	}

	return s.repo.UpdateDownloader(ctx, repo.UpdateDownloaderParams{
		ID:             id,
		Name:           name,
		DownloaderType: downloaderType,
		Protocol:       protocol,
		URL:            downloaderURL,
		Username:       username,
		Password:       passwordToUse,
		ConfigJSON:     cfg,
		Enabled:        enabled,
		IsDefault:      isDefault,
	})
}

func (s *DownloadersService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteDownloader(ctx, id)
}
