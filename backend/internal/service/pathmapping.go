package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/downloader"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/pathmapping"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// PathMappingService owns volumes and their downloader aliases, resolves
// downloader-reported paths into Arrflix's frame through them, and verifies
// mappings against the filesystem. Verification persists nothing.
type PathMappingService struct {
	repo   *repo.Repository
	dlm    *downloader.Manager
	device pathmapping.DeviceFunc
}

// NewPathMappingService builds the service. With a nil manager, checks that
// need to ask a downloader for its save path report it unavailable.
func NewPathMappingService(r *repo.Repository, dlm *downloader.Manager) *PathMappingService {
	return &PathMappingService{repo: r, dlm: dlm, device: pathmapping.Device}
}

// savePathTimeout bounds one downloader's answer to "where do you save?", so
// an unreachable downloader doesn't hold up the checks for the others.
const savePathTimeout = 10 * time.Second

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

// Checks asks every downloader for its default save path and reports whether
// Arrflix can see it through the configured volumes.
func (s *PathMappingService) Checks(ctx context.Context) ([]model.DownloaderPathCheck, error) {
	volumes, err := s.repo.ListVolumes(ctx)
	if err != nil {
		return nil, err
	}
	return s.checkAll(ctx, volumes)
}

// VolumeDraft is a volume being edited and not yet saved. ID is the volume it
// replaces, uuid.Nil for a new one.
type VolumeDraft struct {
	ID          uuid.UUID
	ArrflixBase string
	Aliases     []VolumeAliasInput
}

// Preview runs Checks as they would come out with draft saved. A draft whose
// Arrflix base isn't absolute is left out, and so is any alias whose base
// isn't, since neither could be saved.
func (s *PathMappingService) Preview(ctx context.Context, draft VolumeDraft) ([]model.DownloaderPathCheck, error) {
	existing, err := s.repo.ListVolumes(ctx)
	if err != nil {
		return nil, err
	}
	volumes := make([]model.Volume, 0, len(existing)+1)
	for _, v := range existing {
		if v.ID != draft.ID {
			volumes = append(volumes, v)
		}
	}
	if filepath.IsAbs(draft.ArrflixBase) {
		v := model.Volume{ID: draft.ID, Name: "this location", ArrflixBase: filepath.Clean(draft.ArrflixBase)}
		for _, a := range draft.Aliases {
			if filepath.IsAbs(a.Base) {
				v.Aliases = append(v.Aliases, model.VolumeAlias{DownloaderID: a.DownloaderID, Base: filepath.Clean(a.Base), Flavor: "posix"})
			}
		}
		volumes = append(volumes, v)
	}
	return s.checkAll(ctx, volumes)
}

// Test resolves path, as downloaderID sees it, and reports whether it exists in
// Arrflix and, for each library, whether imports from it would hardlink or
// copy.
func (s *PathMappingService) Test(ctx context.Context, downloaderID uuid.UUID, path string) (model.PathTestResult, error) {
	if _, err := s.repo.GetDownloader(ctx, downloaderID); err != nil {
		return model.PathTestResult{}, err
	}
	if !filepath.IsAbs(path) {
		return model.PathTestResult{}, apperrors.Validation("invalid path",
			apperrors.Field("body.path", "must be an absolute path"),
		).Op("PathMappingService.Test")
	}
	volumes, err := s.repo.ListVolumes(ctx)
	if err != nil {
		return model.PathTestResult{}, err
	}
	libraries, err := s.repo.ListLibraries(ctx)
	if err != nil {
		return model.PathTestResult{}, err
	}
	return pathmapping.New(volumes).TestPath(downloaderID, path, libraries, s.device)
}

// PathsResolve reports whether Arrflix can see client's default save path
// through the current volumes, or nil when the check can't run.
func (s *PathMappingService) PathsResolve(ctx context.Context, dl model.Downloader, client downloader.Client) (*bool, error) {
	volumes, err := s.repo.ListVolumes(ctx)
	if err != nil {
		return nil, err
	}
	check := s.checkClient(ctx, pathmapping.New(volumes), dl, client)
	if check.Status == model.PathCheckUnavailable {
		return nil, nil
	}
	ok := check.Status != model.PathCheckNotVisible
	return &ok, nil
}

func (s *PathMappingService) checkAll(ctx context.Context, volumes []model.Volume) ([]model.DownloaderPathCheck, error) {
	downloaders, err := s.repo.ListDownloaders(ctx)
	if err != nil {
		return nil, err
	}
	r := pathmapping.New(volumes)
	out := make([]model.DownloaderPathCheck, len(downloaders))
	var wg sync.WaitGroup
	for i, dl := range downloaders {
		wg.Go(func() { out[i] = s.checkDownloader(ctx, r, dl) })
	}
	wg.Wait()
	return out, nil
}

func (s *PathMappingService) checkDownloader(ctx context.Context, r *pathmapping.Resolver, dl model.Downloader) model.DownloaderPathCheck {
	switch {
	case !dl.Enabled:
		return unavailableCheck(dl, "the downloader is disabled")
	case s.dlm == nil:
		return unavailableCheck(dl, "downloaders aren't available")
	}
	ctx, cancel := context.WithTimeout(ctx, savePathTimeout)
	defer cancel()
	client, err := s.dlm.GetClientByID(ctx, dl.ID.String())
	if err != nil {
		return unavailableCheck(dl, "Arrflix couldn't connect to the downloader")
	}
	return s.checkClient(ctx, r, dl, client)
}

func (s *PathMappingService) checkClient(ctx context.Context, r *pathmapping.Resolver, dl model.Downloader, client downloader.Client) model.DownloaderPathCheck {
	ctx, cancel := context.WithTimeout(ctx, savePathTimeout)
	defer cancel()
	savePath, err := client.DefaultSavePath(ctx)
	if err != nil || savePath == "" {
		return unavailableCheck(dl, "the downloader didn't report its save path")
	}
	return r.CheckSavePath(dl, savePath)
}

func unavailableCheck(dl model.Downloader, message string) model.DownloaderPathCheck {
	return model.DownloaderPathCheck{
		DownloaderID:   dl.ID,
		DownloaderName: dl.Name,
		Status:         model.PathCheckUnavailable,
		Message:        message,
	}
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
