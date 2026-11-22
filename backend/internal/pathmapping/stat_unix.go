//go:build unix

package pathmapping

import (
	"os"
	"syscall"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

// Device returns the st_dev of path: two paths on the same device can be
// hardlinked to each other. Nothing derived from it is persisted.
func Device(path string) (uint64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return 0, apperrors.Internalf("no device information for %q", path).Op("pathmapping.Device")
	}
	return uint64(st.Dev), nil //nolint:gosec,unconvert // width/sign varies by platform
}
