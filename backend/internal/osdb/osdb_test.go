package osdb

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Official OpenSubtitles reference vector (documented, not executed here — the
// fixture is a 12 MB video we don't ship): breakdance.avi, size 12909756 bytes,
// hash 8e245d9679d31e12. https://trac.opensubtitles.org/projects/opensubtitles/wiki/HashSourceCodes

func writeFixture(t *testing.T, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fixture.bin")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return p
}

func TestHash_AllZeros(t *testing.T) {
	t.Parallel()

	// 128 KiB of zero bytes: both 64 KiB chunks sum to 0, so the hash reduces
	// to just the filesize: 131072 == 0x20000.
	p := writeFixture(t, make([]byte, 131072))

	got, err := Hash(p)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if want := "0000000000020000"; got != want {
		t.Fatalf("hash = %q, want %q", got, want)
	}
}

func TestHash_AllOnes(t *testing.T) {
	t.Parallel()

	// 128 KiB of 0xFF. Derivation (all arithmetic mod 2^64):
	//   one 64 KiB chunk = 8192 words of (2^64 - 1) = -8192 ≡ 2^64 - 8192
	//   two chunks       = 2*(2^64 - 8192) ≡ 2^64 - 16384
	//   + filesize 0x20000 (131072) ≡ 2^64 + 114688 ≡ 114688 = 0x1c000
	buf := bytes.Repeat([]byte{0xFF}, 131072)
	p := writeFixture(t, buf)

	got, err := Hash(p)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if want := "000000000001c000"; got != want {
		t.Fatalf("hash = %q, want %q", got, want)
	}
}

func TestHash_TooSmall(t *testing.T) {
	t.Parallel()

	p := writeFixture(t, make([]byte, 65535)) // one byte under chunkSize

	got, err := Hash(p)
	if err == nil {
		t.Fatalf("expected error for undersized file, got hash %q", got)
	}
	if got != "" {
		t.Fatalf("expected empty hash on error, got %q", got)
	}
}
