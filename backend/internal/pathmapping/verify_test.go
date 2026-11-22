package pathmapping

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/model"
)

func library(name, root string) model.Library {
	return model.Library{ID: uuid.New(), Name: name, RootPath: root}
}

// REQ-PATH-004 — An operator must be able to verify a mapping before relying
// on it, including whether imports will hardlink or copy.
//
// A downloader path mapped through a volume is tested against two libraries.
// Where the path and the library root share a device the result says imports
// hardlink; where they don't it says imports copy. A second real filesystem
// isn't available in a test, so the "other" library's device comes from an
// injected device function. A path that doesn't exist is reported as missing
// with no library verdicts.
func TestReqPath004_TestReportsHardlinkOrCopy(t *testing.T) {
	t.Parallel()
	qbit := uuid.New()
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "torrents"), 0o755); err != nil {
		t.Fatal(err)
	}
	same := library("Movies", t.TempDir())
	other := library("Series", t.TempDir())
	vol := volume("media", base, alias(qbit, "/downloads"))
	r := New([]model.Volume{vol})

	device := func(path string) (uint64, error) {
		if path == other.RootPath {
			return 0xdead, nil
		}
		return Device(path)
	}

	got, err := r.TestPath(qbit, "/downloads/torrents", []model.Library{same, other}, device)
	if err != nil {
		t.Fatalf("TestPath: %v", err)
	}
	if got.ResolvedPath != filepath.Join(base, "torrents") || !got.Exists {
		t.Fatalf("resolved = %q exists=%v, want %q existing", got.ResolvedPath, got.Exists, filepath.Join(base, "torrents"))
	}
	if got.VolumeID == nil || *got.VolumeID != vol.ID || got.VolumeName != "media" {
		t.Errorf("volume = %v %q, want media", got.VolumeID, got.VolumeName)
	}
	verdicts := map[uuid.UUID]string{}
	for _, l := range got.Libraries {
		verdicts[l.LibraryID] = l.Verdict
	}
	if verdicts[same.ID] != string(LinkHardlink) {
		t.Errorf("same-device library verdict = %q, want hardlink", verdicts[same.ID])
	}
	if verdicts[other.ID] != string(LinkCopy) {
		t.Errorf("different-device library verdict = %q, want copy", verdicts[other.ID])
	}

	missing, err := r.TestPath(qbit, "/downloads/nope", []model.Library{same, other}, device)
	if err != nil {
		t.Fatalf("TestPath(missing): %v", err)
	}
	if missing.Exists || len(missing.Libraries) != 0 {
		t.Errorf("missing path = %+v, want not found with no library rows", missing)
	}
}

func TestVerdictUnknownWhenLibraryRootIsMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	v, err := Verdict(dir, filepath.Join(dir, "gone"), Device)
	if v != LinkUnknown || err == nil {
		t.Errorf("Verdict = %q, %v; want unknown with an error", v, err)
	}
}

// REQ-PATH-003 — An install where every actor sees the same paths must need
// no path configuration.
//
// With no volumes, a downloader whose default save path exists in Arrflix as
// a directory is reported as needing no mapping.
func TestReqPath003_SharedSavePathNeedsNoMapping(t *testing.T) {
	t.Parallel()
	dl := model.Downloader{ID: uuid.New(), Name: "qbit"}
	savePath := t.TempDir()

	got := New(nil).CheckSavePath(dl, savePath)
	if got.Status != model.PathCheckNoMappingNeeded {
		t.Errorf("status = %q, want no_mapping_needed", got.Status)
	}
	if got.ResolvedPath != savePath || got.VolumeID != nil {
		t.Errorf("check = %+v, want the save path unchanged with no volume", got)
	}
}

func TestCheckSavePath(t *testing.T) {
	t.Parallel()
	dl := model.Downloader{ID: uuid.New(), Name: "qbit"}
	base := t.TempDir()
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r := New([]model.Volume{volume("media", base, alias(dl.ID, "/downloads"))})

	cases := []struct {
		name, savePath string
		want           model.PathCheckStatus
	}{
		{"a volume maps it to a directory", "/downloads", model.PathCheckMapped},
		{"the mapped path is missing", "/downloads/missing", model.PathCheckNotVisible},
		{"the mapped path is a file", "/downloads/file", model.PathCheckNotVisible},
		{"no volume and the path is missing", "/nowhere/" + strings.Repeat("x", 8), model.PathCheckNotVisible},
		{"a relative save path can't be resolved", "downloads", model.PathCheckUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := r.CheckSavePath(dl, tc.savePath); got.Status != tc.want {
				t.Errorf("CheckSavePath(%q) = %+v, want %q", tc.savePath, got, tc.want)
			}
		})
	}
}
