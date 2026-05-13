package importer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var videoExts = map[string]bool{
	".mkv":  true,
	".mp4":  true,
	".avi":  true,
	".m2ts": true,
}

func IsVideoPath(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	return videoExts[ext]
}

func LooksLikeSample(p string) bool {
	lp := strings.ToLower(p)
	return strings.Contains(lp, "sample")
}

// HardlinkOrCopy tries to hardlink src->dst. If hardlink fails, it falls back to a byte-for-byte copy.
// Returns method: "hardlink" or "copy".
func HardlinkOrCopy(src, dst string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", fmt.Errorf("mkdir dest dir: %w", err)
	}

	if err := os.Link(src, dst); err == nil {
		return "hardlink", nil
	}

	// Copy fallback
	in, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("open src: %w", err)
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return "", fmt.Errorf("create tmp: %w", err)
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("copy: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("close tmp: %w", closeErr)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("rename tmp: %w", err)
	}
	return "copy", nil
}
