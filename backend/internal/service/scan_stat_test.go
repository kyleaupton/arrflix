//go:build unix

package service

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// failingDirEntry is a walked entry whose stat fails, as when the file is
// removed between the directory read and the lstat.
type failingDirEntry struct{}

func (failingDirEntry) Name() string               { return "gone.mkv" }
func (failingDirEntry) IsDir() bool                { return false }
func (failingDirEntry) Type() fs.FileMode          { return 0 }
func (failingDirEntry) Info() (fs.FileInfo, error) { return nil, fs.ErrNotExist }

// TestStatWalkEntry_FailedStatYieldsNoFacts pins that a failed stat surfaces
// as an error with no facts, so the scan skips the file rather than writing
// NULL size/link facts into its file_state.
func TestStatWalkEntry_FailedStatYieldsNoFacts(t *testing.T) {
	t.Parallel()
	size, links, err := statWalkEntry(failingDirEntry{})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
	if size != nil || links != nil {
		t.Fatalf("got size=%v links=%v, want nil facts on a failed stat", size, links)
	}
}

// TestStatWalkEntry_ReadsSizeAndLinkFacts pins that a successful stat yields
// the file's size and its link facts.
func TestStatWalkEntry_ReadsSizeAndLinkFacts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "a.mkv")
	if err := os.WriteFile(p, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(p, filepath.Join(dir, "b.mkv")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	size, links, err := statWalkEntry(entries[0])
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if size == nil || *size != 5 {
		t.Fatalf("size = %v, want 5", size)
	}
	if links == nil || links.LinkCount != 2 {
		t.Fatalf("links = %+v, want link count 2", links)
	}
}
