package pathmapping

import (
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/model"
)

// DeviceFunc returns the device a path lives on. Device is the real one;
// tests substitute their own where a second filesystem isn't available.
type DeviceFunc func(path string) (uint64, error)

// Located is a path resolved into Arrflix's frame together with what Arrflix
// finds there.
type Located struct {
	Resolution
	Exists bool
	IsDir  bool
}

// Locate resolves path, as downloaderID reports it, and stats the result.
// Any stat failure counts as not existing: the caller's question is whether
// Arrflix can use the path, and an unreadable one it can't.
func (r *Resolver) Locate(downloaderID uuid.UUID, path string) (Located, error) {
	res, err := r.ToCanonical(downloaderID, path)
	if err != nil {
		return Located{}, err
	}
	loc := Located{Resolution: res}
	if info, err := os.Stat(res.Path); err == nil {
		loc.Exists = true
		loc.IsDir = info.IsDir()
	}
	return loc, nil
}

// LinkVerdict is whether a file at one path can be hardlinked to another.
type LinkVerdict string

const (
	// LinkHardlink: both paths are on one device, so imports hardlink.
	LinkHardlink LinkVerdict = "hardlink"
	// LinkCopy: the paths are on different devices, so imports copy.
	LinkCopy LinkVerdict = "copy"
	// LinkUnknown: a device couldn't be read, usually because the library
	// root doesn't exist.
	LinkUnknown LinkVerdict = "unknown"
)

// Verdict compares the devices of path and libraryRoot. The error explains
// LinkUnknown and is nil otherwise.
func Verdict(path, libraryRoot string, device DeviceFunc) (LinkVerdict, error) {
	a, err := device(path)
	if err != nil {
		return LinkUnknown, err
	}
	b, err := device(libraryRoot)
	if err != nil {
		return LinkUnknown, err
	}
	if a == b {
		return LinkHardlink, nil
	}
	return LinkCopy, nil
}

// CheckSavePath reports whether Arrflix can see savePath, a downloader's
// default save path in its own frame, as a directory once mapped.
func (r *Resolver) CheckSavePath(dl model.Downloader, savePath string) model.DownloaderPathCheck {
	out := model.DownloaderPathCheck{DownloaderID: dl.ID, DownloaderName: dl.Name, SavePath: savePath}
	loc, err := r.Locate(dl.ID, savePath)
	if err != nil {
		out.Status = model.PathCheckUnavailable
		out.Message = fmt.Sprintf("the downloader reported a save path Arrflix can't resolve: %q", savePath)
		return out
	}
	out.ResolvedPath = loc.Path
	if loc.Volume != nil {
		id := loc.Volume.ID
		out.VolumeID, out.VolumeName = &id, loc.Volume.Name
	}
	switch {
	case !loc.Exists || !loc.IsDir:
		out.Status = model.PathCheckNotVisible
	case loc.Volume != nil:
		out.Status = model.PathCheckMapped
	default:
		out.Status = model.PathCheckNoMappingNeeded
	}
	return out
}

// TestPath resolves path, as downloaderID sees it, and reports for each
// library whether imports from it would hardlink or copy. A path that doesn't
// exist in Arrflix gets no library verdicts.
func (r *Resolver) TestPath(downloaderID uuid.UUID, path string, libraries []model.Library, device DeviceFunc) (model.PathTestResult, error) {
	loc, err := r.Locate(downloaderID, path)
	if err != nil {
		return model.PathTestResult{}, err
	}
	out := model.PathTestResult{
		DownloaderID: downloaderID,
		Path:         path,
		ResolvedPath: loc.Path,
		Exists:       loc.Exists,
		Libraries:    []model.PathTestLibrary{},
	}
	if loc.Volume != nil {
		id := loc.Volume.ID
		out.VolumeID, out.VolumeName = &id, loc.Volume.Name
	}
	if !loc.Exists {
		return out, nil
	}
	for _, lib := range libraries {
		verdict, err := Verdict(loc.Path, lib.RootPath, device)
		row := model.PathTestLibrary{
			LibraryID:   lib.ID,
			LibraryName: lib.Name,
			RootPath:    lib.RootPath,
			Verdict:     string(verdict),
		}
		if err != nil {
			row.Message = err.Error()
		}
		out.Libraries = append(out.Libraries, row)
	}
	return out, nil
}
