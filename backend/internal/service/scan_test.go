package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/guessit"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/rs/zerolog"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testUUID(n byte) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte{n}, Valid: true}
}

func testLibraryUUID(n byte) uuid.UUID {
	return uuid.UUID([16]byte{n})
}

// testUUIDDomain returns a uuid.UUID seeded the same way testUUID seeds a
// pgtype.UUID, so domain-shaped test fixtures stay aligned with pgtype-shaped
// fixtures used for not-yet-migrated entities.
func testUUIDDomain(n byte) uuid.UUID {
	return uuid.UUID([16]byte{n})
}

func newTestScanner(r scanRepo, t scanTmdb) *ScannerService {
	nop := zerolog.Nop()
	return &ScannerService{
		repo:   r,
		tmdb:   t,
		logger: &nop,
		ctx:    context.Background(),
	}
}

func newTestScannerWithGuessit(r scanRepo, t scanTmdb, gc *guessit.Client) *ScannerService {
	s := newTestScanner(r, t)
	s.guessit = gc
	return s
}

// makeSearchMulti builds a tmdb.SearchMulti via JSON roundtrip to avoid
// dealing with the anonymous struct and embedded VoteMetrics types.
func makeSearchMulti(t *testing.T, entries []map[string]any) tmdb.SearchMulti {
	t.Helper()
	payload := map[string]any{
		"page":          1,
		"total_pages":   1,
		"total_results": len(entries),
		"results":       entries,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal search multi: %v", err)
	}
	var result tmdb.SearchMulti
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal search multi: %v", err)
	}
	return result
}

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

type fakeRepo struct {
	getLibraryFn                   func(ctx context.Context, id uuid.UUID) (model.Library, error)
	getMediaFileByLibraryAndPathFn func(ctx context.Context, params repo.GetMediaFileByLibraryAndPathParams) (model.MediaFile, error)
	getMediaItemByTmdbIDAndTypeFn  func(ctx context.Context, tmdbID int64, typ string) (model.MediaItem, error)
	createMediaItemFn              func(ctx context.Context, params repo.CreateMediaItemParams) (model.MediaItem, error)
	upsertSeasonFn                 func(ctx context.Context, params repo.UpsertSeasonParams) (model.MediaSeason, error)
	upsertEpisodeFn                func(ctx context.Context, params repo.UpsertEpisodeParams) (model.MediaEpisode, error)
	createMediaFileFn              func(ctx context.Context, params repo.CreateMediaFileParams) (model.MediaFile, error)
	upsertMediaFileStateFn         func(ctx context.Context, params repo.UpsertMediaFileStateParams) (model.MediaFileState, error)
	createMediaFileImportFn        func(ctx context.Context, arg dbgen.CreateMediaFileImportParams) (dbgen.MediaFileImport, error)

	listMediaFilePathsForLibraryFn     func(ctx context.Context, libraryID uuid.UUID) ([]string, error)
	listUnmatchedFilePathsForLibraryFn func(ctx context.Context, libraryID pgtype.UUID) ([]string, error)
	upsertUnmatchedFileFn              func(ctx context.Context, libraryID pgtype.UUID, path string, fileSize *int64, suggestedMatches []byte) (dbgen.UnmatchedFile, error)

	mu                   sync.Mutex
	createMediaItemCalls int
	createMediaFileCalls int
	upsertSeasonCalls    int
	upsertEpisodeCalls   int
	upsertUnmatchedCalls int
}

func (f *fakeRepo) GetLibrary(ctx context.Context, id uuid.UUID) (model.Library, error) {
	if f.getLibraryFn != nil {
		return f.getLibraryFn(ctx, id)
	}
	return model.Library{}, nil
}

func (f *fakeRepo) GetMediaFileByLibraryAndPath(ctx context.Context, params repo.GetMediaFileByLibraryAndPathParams) (model.MediaFile, error) {
	if f.getMediaFileByLibraryAndPathFn != nil {
		return f.getMediaFileByLibraryAndPathFn(ctx, params)
	}
	return model.MediaFile{}, apperrors.NotFoundf("media file not found")
}

func (f *fakeRepo) GetMediaItemByTmdbIDAndType(ctx context.Context, tmdbID int64, typ string) (model.MediaItem, error) {
	if f.getMediaItemByTmdbIDAndTypeFn != nil {
		return f.getMediaItemByTmdbIDAndTypeFn(ctx, tmdbID, typ)
	}
	return model.MediaItem{}, apperrors.NotFoundf("media item not found")
}

func (f *fakeRepo) CreateMediaItem(ctx context.Context, params repo.CreateMediaItemParams) (model.MediaItem, error) {
	f.mu.Lock()
	f.createMediaItemCalls++
	f.mu.Unlock()
	if f.createMediaItemFn != nil {
		return f.createMediaItemFn(ctx, params)
	}
	return model.MediaItem{}, nil
}

func (f *fakeRepo) UpsertSeason(ctx context.Context, params repo.UpsertSeasonParams) (model.MediaSeason, error) {
	f.mu.Lock()
	f.upsertSeasonCalls++
	f.mu.Unlock()
	if f.upsertSeasonFn != nil {
		return f.upsertSeasonFn(ctx, params)
	}
	return model.MediaSeason{}, nil
}

func (f *fakeRepo) UpsertEpisode(ctx context.Context, params repo.UpsertEpisodeParams) (model.MediaEpisode, error) {
	f.mu.Lock()
	f.upsertEpisodeCalls++
	f.mu.Unlock()
	if f.upsertEpisodeFn != nil {
		return f.upsertEpisodeFn(ctx, params)
	}
	return model.MediaEpisode{}, nil
}

func (f *fakeRepo) CreateMediaFile(ctx context.Context, params repo.CreateMediaFileParams) (model.MediaFile, error) {
	f.mu.Lock()
	f.createMediaFileCalls++
	f.mu.Unlock()
	if f.createMediaFileFn != nil {
		return f.createMediaFileFn(ctx, params)
	}
	return model.MediaFile{}, nil
}

func (f *fakeRepo) UpsertMediaFileState(ctx context.Context, params repo.UpsertMediaFileStateParams) (model.MediaFileState, error) {
	if f.upsertMediaFileStateFn != nil {
		return f.upsertMediaFileStateFn(ctx, params)
	}
	return model.MediaFileState{}, nil
}

func (f *fakeRepo) CreateMediaFileImport(ctx context.Context, arg dbgen.CreateMediaFileImportParams) (dbgen.MediaFileImport, error) {
	if f.createMediaFileImportFn != nil {
		return f.createMediaFileImportFn(ctx, arg)
	}
	return dbgen.MediaFileImport{}, nil
}

func (f *fakeRepo) ListMediaFilePathsForLibrary(ctx context.Context, libraryID uuid.UUID) ([]string, error) {
	if f.listMediaFilePathsForLibraryFn != nil {
		return f.listMediaFilePathsForLibraryFn(ctx, libraryID)
	}
	return nil, nil
}

func (f *fakeRepo) ListUnmatchedFilePathsForLibrary(ctx context.Context, libraryID pgtype.UUID) ([]string, error) {
	if f.listUnmatchedFilePathsForLibraryFn != nil {
		return f.listUnmatchedFilePathsForLibraryFn(ctx, libraryID)
	}
	return nil, nil
}

func (f *fakeRepo) UpsertUnmatchedFile(ctx context.Context, libraryID pgtype.UUID, path string, fileSize *int64, suggestedMatches []byte) (dbgen.UnmatchedFile, error) {
	f.mu.Lock()
	f.upsertUnmatchedCalls++
	f.mu.Unlock()
	if f.upsertUnmatchedFileFn != nil {
		return f.upsertUnmatchedFileFn(ctx, libraryID, path, fileSize, suggestedMatches)
	}
	return dbgen.UnmatchedFile{}, nil
}

type fakeTmdb struct {
	findByIDFn          func(ctx context.Context, id, source string) (tmdb.FindByID, error)
	getMovieDetailsFn   func(ctx context.Context, id int64) (tmdb.MovieDetails, error)
	getSeriesDetailsFn  func(ctx context.Context, id int64) (tmdb.TVDetails, error)
	getEpisodeDetailsFn func(ctx context.Context, id int64, season int64, episode int64) (tmdb.TVEpisodeDetails, error)
	multiSearchFn       func(ctx context.Context, query string, page int) (tmdb.SearchMulti, error)
}

func (f *fakeTmdb) FindByID(ctx context.Context, id, source string) (tmdb.FindByID, error) {
	if f.findByIDFn != nil {
		return f.findByIDFn(ctx, id, source)
	}
	return tmdb.FindByID{}, nil
}

func (f *fakeTmdb) GetMovieDetails(ctx context.Context, id int64) (tmdb.MovieDetails, error) {
	if f.getMovieDetailsFn != nil {
		return f.getMovieDetailsFn(ctx, id)
	}
	return tmdb.MovieDetails{}, nil
}

func (f *fakeTmdb) GetSeriesDetails(ctx context.Context, id int64) (tmdb.TVDetails, error) {
	if f.getSeriesDetailsFn != nil {
		return f.getSeriesDetailsFn(ctx, id)
	}
	return tmdb.TVDetails{}, nil
}

func (f *fakeTmdb) GetEpisodeDetails(ctx context.Context, id int64, season int64, episode int64) (tmdb.TVEpisodeDetails, error) {
	if f.getEpisodeDetailsFn != nil {
		return f.getEpisodeDetailsFn(ctx, id, season, episode)
	}
	return tmdb.TVEpisodeDetails{}, nil
}

func (f *fakeTmdb) MultiSearch(ctx context.Context, query string, page int) (tmdb.SearchMulti, error) {
	if f.multiSearchFn != nil {
		return f.multiSearchFn(ctx, query, page)
	}
	return tmdb.SearchMulti{}, nil
}

// ---------------------------------------------------------------------------
// Tests: isMediaFile
// ---------------------------------------------------------------------------

func TestIsMediaFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"movie.mkv", true},
		{"movie.mp4", true},
		{"movie.avi", true},
		{"movie.mov", true},
		{"movie.wmv", true},
		{"movie.flv", true},
		{"movie.m4v", true},
		{"movie.webm", true},
		{"movie.MKV", true},
		{"movie.Mp4", true},
		{"movie.AVI", true},
		{"movie.txt", false},
		{"movie.srt", false},
		{"movie.nfo", false},
		{"", false},
		{"no_extension", false},
		{".mkv", true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isMediaFile(tt.path); got != tt.want {
				t.Errorf("isMediaFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: ensureSeasonAndEpisode
// ---------------------------------------------------------------------------

func TestEnsureSeasonAndEpisode(t *testing.T) {
	mediaItemID := testUUIDDomain(1)
	seasonID := testUUIDDomain(2)
	episodeID := testUUIDDomain(3)

	var tmdbID int64 = 12345
	var seasonNum int32 = 1
	var episodeNum int32 = 5

	t.Run("nil season returns nil", func(t *testing.T) {
		s := newTestScanner(&fakeRepo{}, &fakeTmdb{})
		got, err := s.ensureSeasonAndEpisode(context.Background(), mediaItemID, tmdbID, nil, nil, "/some/path")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Fatal("expected nil episode ID")
		}
	})

	t.Run("nil episode upserts season only", func(t *testing.T) {
		fr := &fakeRepo{
			upsertSeasonFn: func(ctx context.Context, params repo.UpsertSeasonParams) (model.MediaSeason, error) {
				return model.MediaSeason{ID: seasonID}, nil
			},
		}
		s := newTestScanner(fr, &fakeTmdb{})
		got, err := s.ensureSeasonAndEpisode(context.Background(), mediaItemID, tmdbID, &seasonNum, nil, "/some/path")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Fatal("expected nil episode ID when episode is nil")
		}
		if fr.upsertSeasonCalls != 1 {
			t.Fatalf("expected 1 UpsertSeason call, got %d", fr.upsertSeasonCalls)
		}
	})

	t.Run("happy path", func(t *testing.T) {
		epName := "Test Episode"
		fr := &fakeRepo{
			upsertSeasonFn: func(ctx context.Context, params repo.UpsertSeasonParams) (model.MediaSeason, error) {
				return model.MediaSeason{ID: seasonID}, nil
			},
			upsertEpisodeFn: func(ctx context.Context, params repo.UpsertEpisodeParams) (model.MediaEpisode, error) {
				if params.Title == nil || *params.Title != epName {
					t.Errorf("expected episode title %q, got %v", epName, params.Title)
				}
				return model.MediaEpisode{ID: episodeID}, nil
			},
		}
		ft := &fakeTmdb{
			getEpisodeDetailsFn: func(ctx context.Context, id int64, season int64, episode int64) (tmdb.TVEpisodeDetails, error) {
				return tmdb.TVEpisodeDetails{Name: epName}, nil
			},
		}
		s := newTestScanner(fr, ft)
		got, err := s.ensureSeasonAndEpisode(context.Background(), mediaItemID, tmdbID, &seasonNum, &episodeNum, "/some/path")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil {
			t.Fatal("expected non-nil episode ID")
		}
		if *got != episodeID {
			t.Fatalf("expected episode ID %v, got %v", episodeID, *got)
		}
	})

	t.Run("UpsertSeason error", func(t *testing.T) {
		fr := &fakeRepo{
			upsertSeasonFn: func(ctx context.Context, params repo.UpsertSeasonParams) (model.MediaSeason, error) {
				return model.MediaSeason{}, errors.New("db error")
			},
		}
		s := newTestScanner(fr, &fakeTmdb{})
		_, err := s.ensureSeasonAndEpisode(context.Background(), mediaItemID, tmdbID, &seasonNum, &episodeNum, "/some/path")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("GetEpisodeDetails error", func(t *testing.T) {
		fr := &fakeRepo{
			upsertSeasonFn: func(ctx context.Context, params repo.UpsertSeasonParams) (model.MediaSeason, error) {
				return model.MediaSeason{ID: seasonID}, nil
			},
		}
		ft := &fakeTmdb{
			getEpisodeDetailsFn: func(ctx context.Context, id int64, season int64, episode int64) (tmdb.TVEpisodeDetails, error) {
				return tmdb.TVEpisodeDetails{}, errors.New("tmdb error")
			},
		}
		s := newTestScanner(fr, ft)
		_, err := s.ensureSeasonAndEpisode(context.Background(), mediaItemID, tmdbID, &seasonNum, &episodeNum, "/some/path")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("UpsertEpisode error", func(t *testing.T) {
		fr := &fakeRepo{
			upsertSeasonFn: func(ctx context.Context, params repo.UpsertSeasonParams) (model.MediaSeason, error) {
				return model.MediaSeason{ID: seasonID}, nil
			},
			upsertEpisodeFn: func(ctx context.Context, params repo.UpsertEpisodeParams) (model.MediaEpisode, error) {
				return model.MediaEpisode{}, errors.New("db error")
			},
		}
		ft := &fakeTmdb{
			getEpisodeDetailsFn: func(ctx context.Context, id int64, season int64, episode int64) (tmdb.TVEpisodeDetails, error) {
				return tmdb.TVEpisodeDetails{Name: "ep"}, nil
			},
		}
		s := newTestScanner(fr, ft)
		_, err := s.ensureSeasonAndEpisode(context.Background(), mediaItemID, tmdbID, &seasonNum, &episodeNum, "/some/path")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: StartScan concurrency guard
// ---------------------------------------------------------------------------

func TestStartScan_ConcurrencyGuard(t *testing.T) {
	dir := t.TempDir()
	movieDir := filepath.Join(dir, "Movie {tmdb-100}")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(movieDir, "movie.mkv"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// Block the scan goroutine so the running key stays in the map.
	block := make(chan struct{})

	fr := &fakeRepo{
		getLibraryFn: func(ctx context.Context, id uuid.UUID) (model.Library, error) {
			return model.Library{ID: id, RootPath: dir, Type: "movie", Name: "test"}, nil
		},
		getMediaFileByLibraryAndPathFn: func(ctx context.Context, params repo.GetMediaFileByLibraryAndPathParams) (model.MediaFile, error) {
			<-block
			return model.MediaFile{}, apperrors.NotFoundf("media file not found")
		},
		// loadKnownPaths will fail, so scanner falls back to per-file check (which blocks)
		listMediaFilePathsForLibraryFn: func(ctx context.Context, libraryID uuid.UUID) ([]string, error) {
			return nil, errors.New("block via fallback")
		},
	}

	s := newTestScanner(fr, &fakeTmdb{})

	libID := testLibraryUUID(1)
	_, err := s.StartScan(context.Background(), libID)
	if err != nil {
		t.Fatalf("first scan should succeed: %v", err)
	}

	// The running key was stored before the goroutine launched,
	// so a second scan for the same library must fail.
	_, err = s.StartScan(context.Background(), libID)
	if !apperrors.IsConflict(err) {
		t.Fatalf("expected Conflict (scan already running), got %v", err)
	}

	close(block)
}

// ---------------------------------------------------------------------------
// Tests: executeScan
// ---------------------------------------------------------------------------

func TestExecuteScan_SkipsExistingFiles(t *testing.T) {
	dir := t.TempDir()
	movieDir := filepath.Join(dir, "Movie {tmdb-100}")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(movieDir, "movie.mkv"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	fr := &fakeRepo{
		// Return the file path as already known
		listMediaFilePathsForLibraryFn: func(ctx context.Context, libraryID uuid.UUID) ([]string, error) {
			return []string{filepath.Join("Movie {tmdb-100}", "movie.mkv")}, nil
		},
	}

	s := newTestScanner(fr, &fakeTmdb{})
	library := model.Library{ID: testLibraryUUID(1), RootPath: dir, Type: "movie", Name: "test"}

	stats, err := s.executeScan(context.Background(), library, "scan-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.FilesSeen != 1 {
		t.Fatalf("expected 1 file seen, got %d", stats.FilesSeen)
	}
	if fr.createMediaFileCalls != 0 {
		t.Fatalf("expected 0 CreateMediaFile calls, got %d", fr.createMediaFileCalls)
	}
}

func TestExecuteScan_ContextCancelled(t *testing.T) {
	dir := t.TempDir()
	movieDir := filepath.Join(dir, "Movie {tmdb-100}")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(movieDir, "movie.mkv"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	s := newTestScanner(&fakeRepo{}, &fakeTmdb{})
	library := model.Library{ID: testLibraryUUID(1), RootPath: dir, Type: "movie", Name: "test"}

	_, err := s.executeScan(ctx, library, "scan-1")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestExecuteScan_MovieHappyPath(t *testing.T) {
	dir := t.TempDir()
	movieDir := filepath.Join(dir, "Movie Title {tmdb-100}")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(movieDir, "movie.mkv"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	mediaItemID := testUUIDDomain(10)
	mediaFileID := testUUIDDomain(11)

	fr := &fakeRepo{
		getMediaItemByTmdbIDAndTypeFn: func(ctx context.Context, tmdbID int64, typ string) (model.MediaItem, error) {
			return model.MediaItem{}, apperrors.NotFoundf("media item not found")
		},
		createMediaItemFn: func(ctx context.Context, params repo.CreateMediaItemParams) (model.MediaItem, error) {
			if params.Type != "movie" {
				t.Errorf("expected type 'movie', got %q", params.Type)
			}
			if params.Title != "Movie Title" {
				t.Errorf("expected title 'Movie Title', got %q", params.Title)
			}
			return model.MediaItem{ID: mediaItemID}, nil
		},
		createMediaFileFn: func(ctx context.Context, params repo.CreateMediaFileParams) (model.MediaFile, error) {
			if params.EpisodeID != nil {
				t.Error("expected nil episodeID for movie")
			}
			return model.MediaFile{ID: mediaFileID}, nil
		},
	}

	ft := &fakeTmdb{
		getMovieDetailsFn: func(ctx context.Context, id int64) (tmdb.MovieDetails, error) {
			return tmdb.MovieDetails{
				Title:       "Movie Title",
				ReleaseDate: "2024-01-15",
			}, nil
		},
	}

	s := newTestScanner(fr, ft)
	library := model.Library{ID: testLibraryUUID(1), RootPath: dir, Type: "movie", Name: "test"}

	stats, err := s.executeScan(context.Background(), library, "scan-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.FilesSeen != 1 {
		t.Fatalf("expected 1 file seen, got %d", stats.FilesSeen)
	}
	if stats.MediaItemsCreated != 1 {
		t.Fatalf("expected 1 media item created, got %d", stats.MediaItemsCreated)
	}
	if fr.createMediaItemCalls != 1 {
		t.Fatalf("expected 1 CreateMediaItem call, got %d", fr.createMediaItemCalls)
	}
	if fr.createMediaFileCalls != 1 {
		t.Fatalf("expected 1 CreateMediaFile call, got %d", fr.createMediaFileCalls)
	}
	if stats.IdentifiedByEmbed != 1 {
		t.Fatalf("expected 1 identified by embed, got %d", stats.IdentifiedByEmbed)
	}
}

func TestExecuteScan_SeriesHappyPath(t *testing.T) {
	dir := t.TempDir()
	seriesDir := filepath.Join(dir, "Series Title {tmdb-200}", "Season 01")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seriesDir, "Series Title s01e01.mkv"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	mediaItemID := testUUIDDomain(10)
	seasonID := testUUIDDomain(11)
	episodeID := testUUIDDomain(12)
	mediaFileID := testUUIDDomain(13)

	fr := &fakeRepo{
		getMediaItemByTmdbIDAndTypeFn: func(ctx context.Context, tmdbID int64, typ string) (model.MediaItem, error) {
			return model.MediaItem{}, apperrors.NotFoundf("media item not found")
		},
		createMediaItemFn: func(ctx context.Context, params repo.CreateMediaItemParams) (model.MediaItem, error) {
			return model.MediaItem{ID: mediaItemID}, nil
		},
		upsertSeasonFn: func(ctx context.Context, params repo.UpsertSeasonParams) (model.MediaSeason, error) {
			if params.SeasonNumber != 1 {
				t.Errorf("expected season 1, got %d", params.SeasonNumber)
			}
			return model.MediaSeason{ID: seasonID}, nil
		},
		upsertEpisodeFn: func(ctx context.Context, params repo.UpsertEpisodeParams) (model.MediaEpisode, error) {
			if params.EpisodeNumber != 1 {
				t.Errorf("expected episode 1, got %d", params.EpisodeNumber)
			}
			return model.MediaEpisode{ID: episodeID}, nil
		},
		createMediaFileFn: func(ctx context.Context, params repo.CreateMediaFileParams) (model.MediaFile, error) {
			if params.EpisodeID == nil {
				t.Error("expected non-nil episodeID for series")
			}
			return model.MediaFile{ID: mediaFileID}, nil
		},
	}

	ft := &fakeTmdb{
		getSeriesDetailsFn: func(ctx context.Context, id int64) (tmdb.TVDetails, error) {
			return tmdb.TVDetails{
				Name:         "Series Title",
				FirstAirDate: "2024-03-01",
			}, nil
		},
		getEpisodeDetailsFn: func(ctx context.Context, id int64, season int64, episode int64) (tmdb.TVEpisodeDetails, error) {
			return tmdb.TVEpisodeDetails{Name: "Pilot"}, nil
		},
	}

	s := newTestScanner(fr, ft)
	library := model.Library{ID: testLibraryUUID(1), RootPath: dir, Type: "series", Name: "test"}

	stats, err := s.executeScan(context.Background(), library, "scan-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.FilesSeen != 1 {
		t.Fatalf("expected 1 file seen, got %d", stats.FilesSeen)
	}
	if stats.MediaItemsCreated != 1 {
		t.Fatalf("expected 1 media item created, got %d", stats.MediaItemsCreated)
	}
	if fr.upsertSeasonCalls != 1 {
		t.Fatalf("expected 1 UpsertSeason call, got %d", fr.upsertSeasonCalls)
	}
	if fr.upsertEpisodeCalls != 1 {
		t.Fatalf("expected 1 UpsertEpisode call, got %d", fr.upsertEpisodeCalls)
	}
}

func TestExecuteScan_ExistingSeriesNewEpisode(t *testing.T) {
	dir := t.TempDir()
	seriesDir := filepath.Join(dir, "Series Title {tmdb-200}", "Season 01")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seriesDir, "Series Title s01e02.mkv"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	mediaItemID := testUUIDDomain(10)
	seasonID := testUUIDDomain(11)
	episodeID := testUUIDDomain(12)
	mediaFileID := testUUIDDomain(13)

	fr := &fakeRepo{
		getMediaItemByTmdbIDAndTypeFn: func(ctx context.Context, tmdbID int64, typ string) (model.MediaItem, error) {
			return model.MediaItem{ID: mediaItemID}, nil // media item already exists
		},
		upsertSeasonFn: func(ctx context.Context, params repo.UpsertSeasonParams) (model.MediaSeason, error) {
			return model.MediaSeason{ID: seasonID}, nil
		},
		upsertEpisodeFn: func(ctx context.Context, params repo.UpsertEpisodeParams) (model.MediaEpisode, error) {
			if params.EpisodeNumber != 2 {
				t.Errorf("expected episode 2, got %d", params.EpisodeNumber)
			}
			return model.MediaEpisode{ID: episodeID}, nil
		},
		createMediaFileFn: func(ctx context.Context, params repo.CreateMediaFileParams) (model.MediaFile, error) {
			if params.EpisodeID == nil {
				t.Error("expected non-nil episodeID")
			}
			return model.MediaFile{ID: mediaFileID}, nil
		},
	}

	ft := &fakeTmdb{
		getEpisodeDetailsFn: func(ctx context.Context, id int64, season int64, episode int64) (tmdb.TVEpisodeDetails, error) {
			return tmdb.TVEpisodeDetails{Name: "Episode 2"}, nil
		},
	}

	s := newTestScanner(fr, ft)
	library := model.Library{ID: testLibraryUUID(1), RootPath: dir, Type: "series", Name: "test"}

	stats, err := s.executeScan(context.Background(), library, "scan-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.FilesSeen != 1 {
		t.Fatalf("expected 1 file seen, got %d", stats.FilesSeen)
	}
	if stats.MediaItemsCreated != 0 {
		t.Fatalf("expected 0 media items created, got %d", stats.MediaItemsCreated)
	}
	if fr.upsertSeasonCalls != 1 {
		t.Fatalf("expected 1 UpsertSeason call, got %d", fr.upsertSeasonCalls)
	}
	if fr.upsertEpisodeCalls != 1 {
		t.Fatalf("expected 1 UpsertEpisode call, got %d", fr.upsertEpisodeCalls)
	}
	if fr.createMediaItemCalls != 0 {
		t.Fatalf("expected 0 CreateMediaItem calls, got %d", fr.createMediaItemCalls)
	}
	if fr.createMediaFileCalls != 1 {
		t.Fatalf("expected 1 CreateMediaFile call, got %d", fr.createMediaFileCalls)
	}
}

func TestExecuteScan_EmptyReleaseDate(t *testing.T) {
	dir := t.TempDir()
	movieDir := filepath.Join(dir, "Movie {tmdb-300}")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(movieDir, "movie.mkv"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	fr := &fakeRepo{
		getMediaItemByTmdbIDAndTypeFn: func(ctx context.Context, tmdbID int64, typ string) (model.MediaItem, error) {
			return model.MediaItem{}, apperrors.NotFoundf("media item not found")
		},
	}

	ft := &fakeTmdb{
		getMovieDetailsFn: func(ctx context.Context, id int64) (tmdb.MovieDetails, error) {
			return tmdb.MovieDetails{
				Title:       "Movie",
				ReleaseDate: "", // empty release date
			}, nil
		},
	}

	s := newTestScanner(fr, ft)
	library := model.Library{ID: testLibraryUUID(1), RootPath: dir, Type: "movie", Name: "test"}

	stats, err := s.executeScan(context.Background(), library, "scan-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.FilesSeen != 1 {
		t.Fatalf("expected 1 file seen, got %d", stats.FilesSeen)
	}
	if stats.MediaItemsCreated != 0 {
		t.Fatalf("expected 0 media items created, got %d", stats.MediaItemsCreated)
	}
	if fr.createMediaItemCalls != 0 {
		t.Fatalf("expected 0 CreateMediaItem calls, got %d", fr.createMediaItemCalls)
	}
}

// ---------------------------------------------------------------------------
// Tests: Guessit integration
// ---------------------------------------------------------------------------

// newFakeGuessitServer creates an httptest server that mimics the guessit sidecar.
func newFakeGuessitServer(t *testing.T, handler func(filenames []string) []guessit.ParseResult) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
			return
		}
		if r.URL.Path == "/parse/batch" {
			var filenames []string
			if err := json.NewDecoder(r.Body).Decode(&filenames); err != nil {
				t.Errorf("decode request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			results := handler(filenames)
			json.NewEncoder(w).Encode(results)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestExecuteScan_GuessitMovieHappyPath(t *testing.T) {
	dir := t.TempDir()
	movieDir := filepath.Join(dir, "The Matrix (1999)")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(movieDir, "The Matrix.mkv"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	mediaItemID := testUUIDDomain(10)
	mediaFileID := testUUIDDomain(11)

	fr := &fakeRepo{
		getMediaItemByTmdbIDAndTypeFn: func(ctx context.Context, tmdbID int64, typ string) (model.MediaItem, error) {
			return model.MediaItem{}, apperrors.NotFoundf("media item not found")
		},
		createMediaItemFn: func(ctx context.Context, params repo.CreateMediaItemParams) (model.MediaItem, error) {
			if params.TmdbID == nil || *params.TmdbID != 603 {
				t.Errorf("expected TMDB ID 603, got %v", params.TmdbID)
			}
			return model.MediaItem{ID: mediaItemID}, nil
		},
		createMediaFileFn: func(ctx context.Context, params repo.CreateMediaFileParams) (model.MediaFile, error) {
			return model.MediaFile{ID: mediaFileID}, nil
		},
	}

	ft := &fakeTmdb{
		multiSearchFn: func(ctx context.Context, query string, page int) (tmdb.SearchMulti, error) {
			return makeSearchMulti(t, []map[string]any{
				{"id": 603, "title": "The Matrix", "media_type": "movie", "release_date": "1999-03-31"},
			}), nil
		},
		getMovieDetailsFn: func(ctx context.Context, id int64) (tmdb.MovieDetails, error) {
			return tmdb.MovieDetails{Title: "The Matrix", ReleaseDate: "1999-03-31"}, nil
		},
	}

	title := "The Matrix"
	year := 1999
	srv := newFakeGuessitServer(t, func(filenames []string) []guessit.ParseResult {
		results := make([]guessit.ParseResult, len(filenames))
		for i := range filenames {
			results[i] = guessit.ParseResult{Title: &title, Year: &year}
		}
		return results
	})
	defer srv.Close()

	gc := guessit.NewClient(srv.URL)
	s := newTestScannerWithGuessit(fr, ft, gc)
	library := model.Library{ID: testLibraryUUID(1), RootPath: dir, Type: "movie", Name: "test"}

	stats, err := s.executeScan(context.Background(), library, "scan-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.IdentifiedByGuessit != 1 {
		t.Fatalf("expected 1 identified by guessit, got %d", stats.IdentifiedByGuessit)
	}
	if stats.MediaItemsCreated != 1 {
		t.Fatalf("expected 1 media item created, got %d", stats.MediaItemsCreated)
	}
	if fr.createMediaFileCalls != 1 {
		t.Fatalf("expected 1 CreateMediaFile call, got %d", fr.createMediaFileCalls)
	}
}

func TestExecuteScan_GuessitSeriesDedup(t *testing.T) {
	dir := t.TempDir()
	seriesDir := filepath.Join(dir, "South Park", "Season 01")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		fname := filepath.Join(seriesDir, fmt.Sprintf("South Park S01E%02d.mkv", i))
		if err := os.WriteFile(fname, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mediaItemID := testUUIDDomain(10)
	seasonID := testUUIDDomain(11)
	mediaFileID := testUUIDDomain(13)

	fr := &fakeRepo{
		getMediaItemByTmdbIDAndTypeFn: func(ctx context.Context, tmdbID int64, typ string) (model.MediaItem, error) {
			if tmdbID == 2190 {
				return model.MediaItem{ID: mediaItemID}, nil
			}
			return model.MediaItem{}, apperrors.NotFoundf("media item not found")
		},
		createMediaItemFn: func(ctx context.Context, params repo.CreateMediaItemParams) (model.MediaItem, error) {
			return model.MediaItem{ID: mediaItemID}, nil
		},
		upsertSeasonFn: func(ctx context.Context, params repo.UpsertSeasonParams) (model.MediaSeason, error) {
			return model.MediaSeason{ID: seasonID}, nil
		},
		upsertEpisodeFn: func(ctx context.Context, params repo.UpsertEpisodeParams) (model.MediaEpisode, error) {
			return model.MediaEpisode{ID: testUUIDDomain(byte(20 + params.EpisodeNumber))}, nil
		},
		createMediaFileFn: func(ctx context.Context, params repo.CreateMediaFileParams) (model.MediaFile, error) {
			return model.MediaFile{ID: mediaFileID}, nil
		},
	}

	searchCalls := 0
	ft := &fakeTmdb{
		multiSearchFn: func(ctx context.Context, query string, page int) (tmdb.SearchMulti, error) {
			searchCalls++
			return makeSearchMulti(t, []map[string]any{
				{"id": 2190, "name": "South Park", "media_type": "tv", "first_air_date": "1997-08-13"},
			}), nil
		},
		getSeriesDetailsFn: func(ctx context.Context, id int64) (tmdb.TVDetails, error) {
			return tmdb.TVDetails{Name: "South Park", FirstAirDate: "1997-08-13"}, nil
		},
		getEpisodeDetailsFn: func(ctx context.Context, id int64, season int64, episode int64) (tmdb.TVEpisodeDetails, error) {
			return tmdb.TVEpisodeDetails{Name: fmt.Sprintf("Episode %d", episode)}, nil
		},
	}

	title := "South Park"
	srv := newFakeGuessitServer(t, func(filenames []string) []guessit.ParseResult {
		results := make([]guessit.ParseResult, len(filenames))
		for i, f := range filenames {
			s, e := parseTestSE(f)
			results[i] = guessit.ParseResult{Title: &title, Season: &s, Episode: &e}
		}
		return results
	})
	defer srv.Close()

	gc := guessit.NewClient(srv.URL)
	s := newTestScannerWithGuessit(fr, ft, gc)
	library := model.Library{ID: testLibraryUUID(1), RootPath: dir, Type: "series", Name: "test"}

	stats, err := s.executeScan(context.Background(), library, "scan-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.IdentifiedByGuessit != 5 {
		t.Fatalf("expected 5 identified by guessit, got %d", stats.IdentifiedByGuessit)
	}
	// All 5 episodes should share a single TMDB search
	if searchCalls != 1 {
		t.Fatalf("expected 1 TMDB search call (dedup), got %d", searchCalls)
	}
	if fr.createMediaFileCalls != 5 {
		t.Fatalf("expected 5 CreateMediaFile calls, got %d", fr.createMediaFileCalls)
	}
}

func TestExecuteScan_GuessitAmbiguous(t *testing.T) {
	dir := t.TempDir()
	movieDir := filepath.Join(dir, "Avatar")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(movieDir, "Avatar.mkv"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	fr := &fakeRepo{}

	ft := &fakeTmdb{
		multiSearchFn: func(ctx context.Context, query string, page int) (tmdb.SearchMulti, error) {
			return makeSearchMulti(t, []map[string]any{
				{"id": 19995, "title": "Avatar", "media_type": "movie", "release_date": "2009-12-18"},
				{"id": 76600, "title": "Avatar: The Way of Water", "media_type": "movie", "release_date": "2022-12-16"},
			}), nil
		},
	}

	title := "Avatar"
	srv := newFakeGuessitServer(t, func(filenames []string) []guessit.ParseResult {
		results := make([]guessit.ParseResult, len(filenames))
		for i := range filenames {
			results[i] = guessit.ParseResult{Title: &title}
		}
		return results
	})
	defer srv.Close()

	gc := guessit.NewClient(srv.URL)
	s := newTestScannerWithGuessit(fr, ft, gc)
	library := model.Library{ID: testLibraryUUID(1), RootPath: dir, Type: "movie", Name: "test"}

	stats, err := s.executeScan(context.Background(), library, "scan-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.UnmatchedCount != 1 {
		t.Fatalf("expected 1 unmatched, got %d", stats.UnmatchedCount)
	}
	if stats.IdentifiedByGuessit != 0 {
		t.Fatalf("expected 0 identified by guessit, got %d", stats.IdentifiedByGuessit)
	}
	if fr.upsertUnmatchedCalls != 1 {
		t.Fatalf("expected 1 UpsertUnmatchedFile call, got %d", fr.upsertUnmatchedCalls)
	}
}

func TestExecuteScan_GuessitDown(t *testing.T) {
	dir := t.TempDir()
	movieDir := filepath.Join(dir, "Some Movie")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(movieDir, "movie.mkv"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	fr := &fakeRepo{}

	gc := guessit.NewClient("http://127.0.0.1:0")
	s := newTestScannerWithGuessit(fr, &fakeTmdb{}, gc)
	library := model.Library{ID: testLibraryUUID(1), RootPath: dir, Type: "movie", Name: "test"}

	stats, err := s.executeScan(context.Background(), library, "scan-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.SkippedGuessitDown != 1 {
		t.Fatalf("expected 1 skipped guessit down, got %d", stats.SkippedGuessitDown)
	}
	if stats.UnmatchedCount != 0 {
		t.Fatalf("expected 0 unmatched, got %d", stats.UnmatchedCount)
	}
	if fr.upsertUnmatchedCalls != 0 {
		t.Fatalf("expected 0 UpsertUnmatchedFile calls, got %d", fr.upsertUnmatchedCalls)
	}
}

func TestExecuteScan_Mixed(t *testing.T) {
	dir := t.TempDir()

	// File with embedded ID
	embedDir := filepath.Join(dir, "Movie A {tmdb-100}")
	if err := os.MkdirAll(embedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(embedDir, "movie.mkv"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// File without embedded ID (needs guessit)
	noIdDir := filepath.Join(dir, "Movie B (2020)")
	if err := os.MkdirAll(noIdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(noIdDir, "Movie B.mkv"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	mediaItemID := testUUIDDomain(10)
	mediaFileID := testUUIDDomain(11)

	fr := &fakeRepo{
		getMediaItemByTmdbIDAndTypeFn: func(ctx context.Context, tmdbID int64, typ string) (model.MediaItem, error) {
			return model.MediaItem{}, apperrors.NotFoundf("media item not found")
		},
		createMediaItemFn: func(ctx context.Context, params repo.CreateMediaItemParams) (model.MediaItem, error) {
			return model.MediaItem{ID: mediaItemID}, nil
		},
		createMediaFileFn: func(ctx context.Context, params repo.CreateMediaFileParams) (model.MediaFile, error) {
			return model.MediaFile{ID: mediaFileID}, nil
		},
	}

	ft := &fakeTmdb{
		getMovieDetailsFn: func(ctx context.Context, id int64) (tmdb.MovieDetails, error) {
			if id == 100 {
				return tmdb.MovieDetails{Title: "Movie A", ReleaseDate: "2020-01-01"}, nil
			}
			return tmdb.MovieDetails{Title: "Movie B", ReleaseDate: "2020-06-15"}, nil
		},
		multiSearchFn: func(ctx context.Context, query string, page int) (tmdb.SearchMulti, error) {
			return makeSearchMulti(t, []map[string]any{
				{"id": 200, "title": "Movie B", "media_type": "movie", "release_date": "2020-06-15"},
			}), nil
		},
	}

	title := "Movie B"
	year := 2020
	srv := newFakeGuessitServer(t, func(filenames []string) []guessit.ParseResult {
		results := make([]guessit.ParseResult, len(filenames))
		for i := range filenames {
			results[i] = guessit.ParseResult{Title: &title, Year: &year}
		}
		return results
	})
	defer srv.Close()

	gc := guessit.NewClient(srv.URL)
	s := newTestScannerWithGuessit(fr, ft, gc)
	library := model.Library{ID: testLibraryUUID(1), RootPath: dir, Type: "movie", Name: "test"}

	stats, err := s.executeScan(context.Background(), library, "scan-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.FilesSeen != 2 {
		t.Fatalf("expected 2 files seen, got %d", stats.FilesSeen)
	}
	if stats.IdentifiedByEmbed != 1 {
		t.Fatalf("expected 1 identified by embed, got %d", stats.IdentifiedByEmbed)
	}
	if stats.IdentifiedByGuessit != 1 {
		t.Fatalf("expected 1 identified by guessit, got %d", stats.IdentifiedByGuessit)
	}
	if stats.MediaItemsCreated != 2 {
		t.Fatalf("expected 2 media items created, got %d", stats.MediaItemsCreated)
	}
	if fr.createMediaFileCalls != 2 {
		t.Fatalf("expected 2 CreateMediaFile calls, got %d", fr.createMediaFileCalls)
	}
}

// ---------------------------------------------------------------------------
// Tests: helper functions
// ---------------------------------------------------------------------------

func TestGuessitInput(t *testing.T) {
	tests := []struct {
		libType string
		relPath string
		want    string
	}{
		{"series", filepath.Join("South Park", "Season 01", "South Park S01E01.mkv"), "South Park S01E01.mkv"},
		{"movie", filepath.Join("The Matrix (1999)", "The Matrix.mkv"), "The Matrix (1999)"},
		{"movie", "movie.mkv", "movie.mkv"},
	}
	for _, tt := range tests {
		t.Run(tt.relPath, func(t *testing.T) {
			got := guessitInput(tt.libType, tt.relPath)
			if got != tt.want {
				t.Errorf("guessitInput(%q, %q) = %q, want %q", tt.libType, tt.relPath, got, tt.want)
			}
		})
	}
}

func TestBuildSearchKey(t *testing.T) {
	title := "South Park"
	year := 1999
	movieTitle := "The Matrix"

	t.Run("series groups by top dir", func(t *testing.T) {
		r1 := guessit.ParseResult{Title: &title, Season: intPtr(1), Episode: intPtr(1)}
		r2 := guessit.ParseResult{Title: &title, Season: intPtr(1), Episode: intPtr(2)}
		k1 := buildSearchKey("series", filepath.Join("South Park", "Season 01", "S01E01.mkv"), r1)
		k2 := buildSearchKey("series", filepath.Join("South Park", "Season 01", "S01E02.mkv"), r2)
		if k1.String() != k2.String() {
			t.Errorf("expected same key, got %q and %q", k1.String(), k2.String())
		}
	})

	t.Run("movie groups by title+year", func(t *testing.T) {
		r := guessit.ParseResult{Title: &movieTitle, Year: &year}
		k := buildSearchKey("movie", filepath.Join("The Matrix (1999)", "The Matrix.mkv"), r)
		if k.Query != "The Matrix" {
			t.Errorf("expected query 'The Matrix', got %q", k.Query)
		}
		if k.Year == nil || *k.Year != 1999 {
			t.Errorf("expected year 1999, got %v", k.Year)
		}
	})
}

func TestEvaluateSearchResults(t *testing.T) {
	t.Run("single match auto-matches", func(t *testing.T) {
		sr := makeSearchMulti(t, []map[string]any{
			{"id": 603, "title": "The Matrix", "media_type": "movie", "release_date": "1999-03-31"},
		})
		year := 1999
		key := tmdbSearchKey{Query: "The Matrix", Year: &year, Type: "movie"}
		match, suggestions := evaluateSearchResults("movie", key, sr)
		if match == nil {
			t.Fatal("expected auto-match")
		}
		if match.tmdbID != 603 {
			t.Errorf("expected tmdbID 603, got %d", match.tmdbID)
		}
		if len(suggestions) != 0 {
			t.Errorf("expected 0 suggestions, got %d", len(suggestions))
		}
	})

	t.Run("multiple results returns suggestions", func(t *testing.T) {
		sr := makeSearchMulti(t, []map[string]any{
			{"id": 19995, "title": "Avatar", "media_type": "movie", "release_date": "2009-12-18"},
			{"id": 76600, "title": "Avatar 2", "media_type": "movie", "release_date": "2022-12-16"},
		})
		key := tmdbSearchKey{Query: "Avatar", Type: "movie"}
		match, suggestions := evaluateSearchResults("movie", key, sr)
		if match != nil {
			t.Fatal("expected no auto-match for ambiguous results")
		}
		if len(suggestions) != 2 {
			t.Fatalf("expected 2 suggestions, got %d", len(suggestions))
		}
	})

	t.Run("filters by media type", func(t *testing.T) {
		sr := makeSearchMulti(t, []map[string]any{
			{"id": 123, "name": "Test Show", "media_type": "tv", "first_air_date": "2020-01-01"},
			{"id": 456, "name": "Test Person", "media_type": "person"},
		})
		key := tmdbSearchKey{Query: "Test Show", Type: "series"}
		match, _ := evaluateSearchResults("series", key, sr)
		if match == nil {
			t.Fatal("expected auto-match (only 1 tv result after filtering)")
		}
		if match.tmdbID != 123 {
			t.Errorf("expected tmdbID 123, got %d", match.tmdbID)
		}
	})
}

// ---------------------------------------------------------------------------
// Test utilities
// ---------------------------------------------------------------------------

func intPtr(n int) *int { return &n }

// parseTestSE extracts S##E## from a filename for test fixtures
func parseTestSE(filename string) (int, int) {
	for i := 0; i < len(filename)-5; i++ {
		if (filename[i] == 'S' || filename[i] == 's') &&
			filename[i+1] >= '0' && filename[i+1] <= '9' &&
			filename[i+2] >= '0' && filename[i+2] <= '9' &&
			(filename[i+3] == 'E' || filename[i+3] == 'e') &&
			filename[i+4] >= '0' && filename[i+4] <= '9' &&
			filename[i+5] >= '0' && filename[i+5] <= '9' {
			s := int(filename[i+1]-'0')*10 + int(filename[i+2]-'0')
			e := int(filename[i+4]-'0')*10 + int(filename[i+5]-'0')
			return s, e
		}
	}
	return 0, 0
}
