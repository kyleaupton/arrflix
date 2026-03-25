package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
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

func (s *DownloadersService) Create(ctx context.Context, name, downloaderType, protocol, downloaderURL string, username, password *string, configJSON map[string]interface{}, enabled, isDefault bool) (dbgen.Downloader, error) {
	if name == "" {
		return dbgen.Downloader{}, errors.New("name required")
	}
	if downloaderType != "qbittorrent" {
		return dbgen.Downloader{}, errors.New("invalid downloader type")
	}
	if protocol != "torrent" && protocol != "usenet" {
		return dbgen.Downloader{}, errors.New("protocol must be 'torrent' or 'usenet'")
	}
	if downloaderURL == "" {
		return dbgen.Downloader{}, errors.New("url required")
	}
	if _, err := url.Parse(downloaderURL); err != nil {
		return dbgen.Downloader{}, errors.New("invalid url format")
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
			return dbgen.Downloader{}, errors.New("invalid config_json")
		}
	}

	return s.repo.CreateDownloader(ctx, name, downloaderType, protocol, downloaderURL, username, password, configJSONBytes, enabled, isDefault)
}

func (s *DownloadersService) Update(ctx context.Context, id pgtype.UUID, name, downloaderType, protocol, downloaderURL string, username, password *string, configJSON map[string]interface{}, enabled, isDefault bool) (dbgen.Downloader, error) {
	if name == "" {
		return dbgen.Downloader{}, errors.New("name required")
	}
	if downloaderType != "qbittorrent" {
		return dbgen.Downloader{}, errors.New("invalid downloader type")
	}
	if protocol != "torrent" && protocol != "usenet" {
		return dbgen.Downloader{}, errors.New("protocol must be 'torrent' or 'usenet'")
	}
	if downloaderURL == "" {
		return dbgen.Downloader{}, errors.New("url required")
	}
	if _, err := url.Parse(downloaderURL); err != nil {
		return dbgen.Downloader{}, errors.New("invalid url format")
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
			return dbgen.Downloader{}, errors.New("invalid config_json")
		}
	}

	// If password is nil, fetch existing downloader to preserve its password
	var passwordToUse *string = password
	if password == nil {
		existing, err := s.repo.GetDownloader(ctx, id)
		if err != nil {
			return dbgen.Downloader{}, errors.New("failed to fetch existing downloader: " + err.Error())
		}
		passwordToUse = existing.Password
	}

	return s.repo.UpdateDownloader(ctx, id, name, downloaderType, protocol, downloaderURL, username, passwordToUse, configJSONBytes, enabled, isDefault)
}

func (s *DownloadersService) Delete(ctx context.Context, id pgtype.UUID) error {
	return s.repo.DeleteDownloader(ctx, id)
}

