//go:build unix

package importer

import (
	"os"
	"syscall"

	"github.com/kyleaupton/arrflix/internal/model"
)

// LinkFactsOf reads st_dev, st_ino and st_nlink from a stat result. It
// returns nil when the FileInfo doesn't carry a syscall.Stat_t (a synthetic
// FileInfo, not one from os.Stat/Lstat).
func LinkFactsOf(info os.FileInfo) *model.LinkFacts {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return nil
	}
	return &model.LinkFacts{
		Device:    int64(st.Dev),   //nolint:gosec,unconvert // stored bit-for-bit; width/sign varies by platform
		Inode:     int64(st.Ino),   //nolint:gosec // stored bit-for-bit
		LinkCount: int64(st.Nlink), //nolint:gosec,unconvert // width varies by platform
	}
}
