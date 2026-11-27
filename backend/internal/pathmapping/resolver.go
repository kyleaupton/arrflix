// Package pathmapping translates paths between a downloader's view of storage
// and Arrflix's. It is pure: callers hand it the configured volumes, and it
// never reads persistence.
//
// A path no alias matches is used unchanged, so an install where every actor
// mounts storage at the same path needs no configuration at all.
package pathmapping

import (
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

// Resolver maps paths through a fixed set of volumes.
type Resolver struct {
	volumes []model.Volume
}

// New returns a Resolver over volumes.
func New(volumes []model.Volume) *Resolver {
	return &Resolver{volumes: volumes}
}

// Resolution is the outcome of mapping one path into Arrflix's frame. Volume
// is nil when no alias matched and Path is the input, cleaned.
type Resolution struct {
	Path   string
	Volume *model.Volume
	// Alias is the downloader base that matched; empty when Volume is nil.
	Alias string
}

// ToCanonical maps path, as the downloader downloaderID reports it, into
// Arrflix's frame. The alias with the longest base that contains path on whole
// path segments wins, so nested volumes resolve to the innermost one. A
// relative path is an error: it is never resolved against the working
// directory.
func (r *Resolver) ToCanonical(downloaderID uuid.UUID, path string) (Resolution, error) {
	clean, err := cleanAbs(path)
	if err != nil {
		return Resolution{}, err
	}
	best := -1
	var bestVolume *model.Volume
	var bestBase, bestRel string
	for i := range r.volumes {
		v := &r.volumes[i]
		for _, a := range v.Aliases {
			if a.DownloaderID != downloaderID {
				continue
			}
			base := filepath.Clean(a.Base)
			rel, ok := under(base, clean)
			if ok && len(base) > best {
				best, bestVolume, bestBase, bestRel = len(base), v, base, rel
			}
		}
	}
	if bestVolume == nil {
		return Resolution{Path: clean}, nil
	}
	return Resolution{
		Path:   filepath.Join(filepath.Clean(bestVolume.ArrflixBase), bestRel),
		Volume: bestVolume,
		Alias:  bestBase,
	}, nil
}

// FromCanonical maps arrflixPath, inside volume, into the frame of the
// downloader downloaderID. When the volume has no alias for that downloader
// the downloader sees the same path and it is returned unchanged.
func FromCanonical(volume model.Volume, arrflixPath string, downloaderID uuid.UUID) (string, error) {
	clean, err := cleanAbs(arrflixPath)
	if err != nil {
		return "", err
	}
	rel, ok := under(filepath.Clean(volume.ArrflixBase), clean)
	if !ok {
		return "", apperrors.Internalf("path %q is outside volume %q (%q)", clean, volume.Name, volume.ArrflixBase).
			Op("pathmapping.FromCanonical").
			NotRetryable()
	}
	for _, a := range volume.Aliases {
		if a.DownloaderID == downloaderID {
			return filepath.Join(filepath.Clean(a.Base), rel), nil
		}
	}
	return clean, nil
}

// ToDownloader maps arrflixPath into the frame of the downloader downloaderID
// through the volume whose Arrflix base contains it; the innermost volume wins
// when they nest. A path inside no volume is returned cleaned and unchanged,
// with a nil volume: the downloader is taken to share Arrflix's mount.
func (r *Resolver) ToDownloader(downloaderID uuid.UUID, arrflixPath string) (string, *model.Volume, error) {
	clean, err := cleanAbs(arrflixPath)
	if err != nil {
		return "", nil, err
	}
	var best *model.Volume
	for i := range r.volumes {
		v := &r.volumes[i]
		base := filepath.Clean(v.ArrflixBase)
		if _, ok := under(base, clean); ok && (best == nil || len(base) > len(filepath.Clean(best.ArrflixBase))) {
			best = v
		}
	}
	if best == nil {
		return clean, nil, nil
	}
	out, err := FromCanonical(*best, clean, downloaderID)
	if err != nil {
		return "", nil, err
	}
	return out, best, nil
}

// cleanAbs cleans path and rejects a relative one.
func cleanAbs(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", apperrors.Internalf("path %q is relative; only absolute paths can be resolved", path).
			Op("pathmapping.cleanAbs").
			NotRetryable()
	}
	return filepath.Clean(path), nil
}

// under reports whether path is base or lies beneath it on whole segments
// ("/data" contains "/data/x" but not "/database"), and returns the remainder.
// Both arguments must be clean and absolute.
func under(base, path string) (string, bool) {
	if path == base {
		return "", true
	}
	prefix := base
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	rest, ok := strings.CutPrefix(path, prefix)
	return rest, ok
}
