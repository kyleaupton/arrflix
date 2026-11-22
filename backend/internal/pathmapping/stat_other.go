//go:build !unix

package pathmapping

import apperrors "github.com/kyleaupton/arrflix/internal/errors"

// Device is unsupported off unix: there is no st_dev to read.
func Device(path string) (uint64, error) {
	return 0, apperrors.Internalf("device lookup unsupported on this platform for %q", path).Op("pathmapping.Device")
}
