//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
)

// TestFileOrigin_InsertOnce proves CreateFileOriginIfAbsent is write-once: a
// second call on the same file_id with different values is a no-op (ON CONFLICT
// DO NOTHING), so the first writer's provenance survives. This is what keeps a
// re-scan after an arrflix rename from overwriting the original source_title
// with the rendered on-disk path.
func TestFileOrigin_InsertOnce(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	ctx := context.Background()

	lib, err := r.CreateLibrary(ctx, repo.CreateLibraryParams{
		Name:     "Movies",
		Type:     "movie",
		RootPath: "/movies",
	})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	fileID := uuid.New()
	if _, err := r.CreateFile(ctx, repo.CreateFileParams{
		ID:        fileID,
		LibraryID: lib.ID,
		Path:      "The Matrix (1999)/The Matrix (1999) Bluray-1080p.mkv",
	}); err != nil {
		t.Fatalf("create file: %v", err)
	}

	first := "The.Matrix.1999.1080p.BluRay.x264-GROUP"
	firstRes := "1080p"
	if err := r.CreateFileOriginIfAbsent(ctx, repo.CreateFileOriginParams{
		FileID:        fileID,
		Origin:        "grab",
		SourceTitle:   &first,
		BinResolution: &firstRes,
	}); err != nil {
		t.Fatalf("first CreateFileOriginIfAbsent: %v", err)
	}

	// A second write with different values — e.g. a re-scan presenting the
	// rendered on-disk path at a wrong resolution — must be a silent no-op.
	second := "The Matrix (1999) Bluray-1080p.mkv"
	secondRes := "720p"
	if err := r.CreateFileOriginIfAbsent(ctx, repo.CreateFileOriginParams{
		FileID:        fileID,
		Origin:        "scan",
		SourceTitle:   &second,
		BinResolution: &secondRes,
	}); err != nil {
		t.Fatalf("second CreateFileOriginIfAbsent: %v", err)
	}

	got, err := r.GetFileOrigin(ctx, fileID)
	if err != nil {
		t.Fatalf("GetFileOrigin: %v", err)
	}
	if got.Origin != "grab" {
		t.Errorf("origin: got %q, want %q (first writer)", got.Origin, "grab")
	}
	if got.SourceTitle == nil || *got.SourceTitle != first {
		t.Errorf("source_title: got %v, want %q (first writer)", got.SourceTitle, first)
	}
	if got.BinResolution == nil || *got.BinResolution != firstRes {
		t.Errorf("bin_resolution: got %v, want %q (first writer)", got.BinResolution, firstRes)
	}
}
