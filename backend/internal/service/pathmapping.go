package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/pathmapping"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// PathMappingService owns volumes and their downloader aliases, and resolves
// downloader-reported paths into Arrflix's frame through them.
type PathMappingService struct {
	repo *repo.Repository
}

func NewPathMappingService(r *repo.Repository) *PathMappingService {
	return &PathMappingService{repo: r}
}

// VolumeInput is a volume as a caller asks to save it.
type VolumeInput struct {
	Name        string
	ArrflixBase string
	Aliases     []VolumeAliasInput
}

// VolumeAliasInput is one downloader's view of a volume being saved.
type VolumeAliasInput struct {
	DownloaderID uuid.UUID
	Base         string
}

func (s *PathMappingService) List(ctx context.Context) ([]model.Volume, error) {
	return s.repo.ListVolumes(ctx)
}

func (s *PathMappingService) Get(ctx context.Context, id uuid.UUID) (model.Volume, error) {
	return s.repo.GetVolume(ctx, id)
}

func (s *PathMappingService) Create(ctx context.Context, in VolumeInput) (model.Volume, error) {
	params, err := s.validate(ctx, uuid.Nil, in)
	if err != nil {
		return model.Volume{}, apperrors.WithOp(err, "PathMappingService.Create")
	}
	return s.repo.CreateVolume(ctx, params)
}

func (s *PathMappingService) Update(ctx context.Context, id uuid.UUID, in VolumeInput) (model.Volume, error) {
	if _, err := s.repo.GetVolume(ctx, id); err != nil {
		return model.Volume{}, err
	}
	params, err := s.validate(ctx, id, in)
	if err != nil {
		return model.Volume{}, apperrors.WithOp(err, "PathMappingService.Update")
	}
	return s.repo.UpdateVolume(ctx, id, params)
}

func (s *PathMappingService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteVolume(ctx, id)
}

// ToCanonical resolves path, as downloaderID reports it, against the current
// volumes. See pathmapping.Resolver.ToCanonical.
func (s *PathMappingService) ToCanonical(ctx context.Context, downloaderID uuid.UUID, path string) (pathmapping.Resolution, error) {
	volumes, err := s.repo.ListVolumes(ctx)
	if err != nil {
		return pathmapping.Resolution{}, err
	}
	return pathmapping.New(volumes).ToCanonical(downloaderID, path)
}

// validate collects every field problem (422) before checking uniqueness
// against the other volumes (409). It returns the cleaned repo params. self is
// the volume being updated, uuid.Nil on create.
func (s *PathMappingService) validate(ctx context.Context, self uuid.UUID, in VolumeInput) (repo.SaveVolumeParams, error) {
	var fields []apperrors.FieldError
	name := strings.TrimSpace(in.Name)
	if name == "" {
		fields = append(fields, apperrors.Field("body.name", "required"))
	}

	base := in.ArrflixBase
	switch {
	case !filepath.IsAbs(base):
		fields = append(fields, apperrors.Field("body.arrflixBase", "must be an absolute path"))
	default:
		base = filepath.Clean(base)
		info, err := os.Stat(base)
		switch {
		case err != nil:
			fields = append(fields, apperrors.Field("body.arrflixBase", fmt.Sprintf("not accessible: %v", err)))
		case !info.IsDir():
			fields = append(fields, apperrors.Field("body.arrflixBase", "not a directory"))
		}
	}

	aliases := make([]repo.VolumeAliasParams, 0, len(in.Aliases))
	seen := make(map[uuid.UUID]bool, len(in.Aliases))
	for i, a := range in.Aliases {
		loc := fmt.Sprintf("body.aliases[%d]", i)
		if seen[a.DownloaderID] {
			fields = append(fields, apperrors.Field(loc+".downloaderId", "a downloader can have only one alias per volume"))
		}
		seen[a.DownloaderID] = true
		if _, err := s.repo.GetDownloader(ctx, a.DownloaderID); err != nil {
			if !apperrors.IsNotFound(err) {
				return repo.SaveVolumeParams{}, err
			}
			fields = append(fields, apperrors.Field(loc+".downloaderId", "no such downloader"))
		}
		if !filepath.IsAbs(a.Base) {
			fields = append(fields, apperrors.Field(loc+".base", "must be an absolute path"))
			continue
		}
		aliases = append(aliases, repo.VolumeAliasParams{DownloaderID: a.DownloaderID, Base: filepath.Clean(a.Base)})
	}
	if len(fields) > 0 {
		return repo.SaveVolumeParams{}, apperrors.Validation("invalid volume", fields...)
	}

	existing, err := s.repo.ListVolumes(ctx)
	if err != nil {
		return repo.SaveVolumeParams{}, err
	}
	for _, v := range existing {
		if v.ID == self {
			continue
		}
		if strings.EqualFold(v.Name, name) {
			return repo.SaveVolumeParams{}, apperrors.Conflictf("a volume named %q already exists", v.Name)
		}
		if filepath.Clean(v.ArrflixBase) == base {
			return repo.SaveVolumeParams{}, apperrors.Conflictf("volume %q already has the Arrflix base %q", v.Name, v.ArrflixBase)
		}
	}
	return repo.SaveVolumeParams{Name: name, ArrflixBase: base, Aliases: aliases}, nil
}
