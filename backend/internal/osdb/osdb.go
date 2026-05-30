// Package osdb computes the OpenSubtitles "OSDB" file hash — a fast content
// fingerprint (filesize + a 64-bit checksum of the first and last 64 KiB),
// used for subtitle lookup and content-identity (drop-in re-identification,
// duplicate detection). Frozen algorithm; see the reference vector in the test.
package osdb

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const chunkSize = 65536 // 64 KiB read from each end; 128 KiB total regardless of file size.

// Hash returns the 16-char lowercase-hex OSDB hash of the file at path.
// Files smaller than chunkSize cannot be hashed and yield an error.
func Hash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := info.Size()
	if size < chunkSize {
		return "", fmt.Errorf("osdb: file %q too small to hash: %d bytes (need >= %d)", path, size, chunkSize)
	}

	hash := uint64(size)

	buf := make([]byte, chunkSize)

	// First 64 KiB.
	if _, err := io.ReadFull(f, buf); err != nil {
		return "", err
	}
	hash += sumChunk(buf)

	// Last 64 KiB. For files between 64 and 128 KiB this overlaps the first
	// chunk — that overlap is part of the canonical spec, not a bug.
	if _, err := f.Seek(size-chunkSize, io.SeekStart); err != nil {
		return "", err
	}
	if _, err := io.ReadFull(f, buf); err != nil {
		return "", err
	}
	hash += sumChunk(buf)

	return fmt.Sprintf("%016x", hash), nil
}

// sumChunk adds each little-endian uint64 word of buf into an accumulator,
// relying on natural uint64 wraparound (the algorithm's implicit mod 2^64).
func sumChunk(buf []byte) uint64 {
	var sum uint64
	for i := 0; i < len(buf); i += 8 {
		sum += binary.LittleEndian.Uint64(buf[i : i+8])
	}
	return sum
}
