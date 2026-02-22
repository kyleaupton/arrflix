package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	"github.com/kyleaupton/arrflix/internal/identity"
	"github.com/rs/zerolog"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testUUID(n byte) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte{n}, Valid: true}
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

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

type fakeRepo struct {
	getLibraryFn                   func(ctx context.Context, id pgtype.UUID) (dbgen.Library, error)
	getMediaFileByLibraryAndPathFn func(ctx context.Context, libraryID pgtype.UUID, path string) (dbgen.MediaFile, error)
	getMediaItemByTmdbIDAndTypeFn  func(ctx context.Context, tmdbID int64, typ string) (dbgen.MediaItem, error)
	createMediaItemFn              func(ctx context.Context, typ, title string, year *int32, tmdbID *int64) (dbgen.MediaItem, error)
	upsertSeasonFn                 func(ctx context.Context, mediaItemID pgtype.UUID, seasonNumber int32, airDate pgtype.Date) (dbgen.MediaSeason, error)
	upsertEpisodeFn                func(ctx context.Context, seasonID pgtype.UUID, episodeNumber int32, title *string, airDate pgtype.Date, tmdbID *int64, tvdbID *int64) (dbgen.MediaEpisode, error)
	createMediaFileFn              func(ctx context.Context, libraryID, mediaItemID pgtype.UUID, episodeID *pgtype.UUID, path string) (dbgen.MediaFile, error)
	upsertMediaFileStateFn         func(ctx context.Context, mediaFileID pgtype.UUID, fileExists bool, fileSize *int64) (dbgen.MediaFileState, error)
	createMediaFileImportFn        func(ctx context.Context, arg dbgen.CreateMediaFileImportParams) (dbgen.MediaFileImport, error)

	mu                   sync.Mutex
	createMediaItemCalls int
	createMediaFileCalls int
	upsertSeasonCalls    int
	upsertEpisodeCalls   int
}

func (f *fakeRepo) GetLibrary(ctx context.Context, id pgtype.UUID) (dbgen.Library, error) {
	if f.getLibraryFn != nil {
		return f.getLibraryFn(ctx, id)
	}
	return dbgen.Library{}, nil
}

func (f *fakeRepo) GetMediaFileByLibraryAndPath(ctx context.Context, libraryID pgtype.UUID, path string) (dbgen.MediaFile, error) {
	if f.getMediaFileByLibraryAndPathFn != nil {
		return f.getMediaFileByLibraryAndPathFn(ctx, libraryID, path)
	}
	return dbgen.MediaFile{}, pgx.ErrNoRows
}

func (f *fakeRepo) GetMediaItemByTmdbIDAndType(ctx context.Context, tmdbID int64, typ string) (dbgen.MediaItem, error) {
	if f.getMediaItemByTmdbIDAndTypeFn != nil {
		return f.getMediaItemByTmdbIDAndTypeFn(ctx, tmdbID, typ)
	}
	return dbgen.MediaItem{}, pgx.ErrNoRows
}

func (f *fakeRepo) CreateMediaItem(ctx context.Context, typ, title string, year *int32, tmdbID *int64) (dbgen.MediaItem, error) {
	f.mu.Lock()
	f.createMediaItemCalls++
	f.mu.Unlock()
	if f.createMediaItemFn != nil {
		return f.createMediaItemFn(ctx, typ, title, year, tmdbID)
	}
	return dbgen.MediaItem{}, nil
}

func (f *fakeRepo) UpsertSeason(ctx context.Context, mediaItemID pgtype.UUID, seasonNumber int32, airDate pgtype.Date) (dbgen.MediaSeason, error) {
	f.mu.Lock()
	f.upsertSeasonCalls++
	f.mu.Unlock()
	if f.upsertSeasonFn != nil {
		return f.upsertSeasonFn(ctx, mediaItemID, seasonNumber, airDate)
	}
	return dbgen.MediaSeason{}, nil
}

func (f *fakeRepo) UpsertEpisode(ctx context.Context, seasonID pgtype.UUID, episodeNumber int32, title *string, airDate pgtype.Date, tmdbID *int64, tvdbID *int64) (dbgen.MediaEpisode, error) {
	f.mu.Lock()
	f.upsertEpisodeCalls++
	f.mu.Unlock()
	if f.upsertEpisodeFn != nil {
		return f.upsertEpisodeFn(ctx, seasonID, episodeNumber, title, airDate, tmdbID, tvdbID)
	}
	return dbgen.MediaEpisode{}, nil
}

func (f *fakeRepo) CreateMediaFile(ctx context.Context, libraryID, mediaItemID pgtype.UUID, episodeID *pgtype.UUID, path string) (dbgen.MediaFile, error) {
	f.mu.Lock()
	f.createMediaFileCalls++
	f.mu.Unlock()
	if f.createMediaFileFn != nil {
		return f.createMediaFileFn(ctx, libraryID, mediaItemID, episodeID, path)
	}
	return dbgen.MediaFile{}, nil
}

func (f *fakeRepo) UpsertMediaFileState(ctx context.Context, mediaFileID pgtype.UUID, fileExists bool, fileSize *int64) (dbgen.MediaFileState, error) {
	if f.upsertMediaFileStateFn != nil {
		return f.upsertMediaFileStateFn(ctx, mediaFileID, fileExists, fileSize)
	}
	return dbgen.MediaFileState{}, nil
}

func (f *fakeRepo) CreateMediaFileImport(ctx context.Context, arg dbgen.CreateMediaFileImportParams) (dbgen.MediaFileImport, error) {
	if f.createMediaFileImportFn != nil {
		return f.createMediaFileImportFn(ctx, arg)
	}
	return dbgen.MediaFileImport{}, nil
}

type fakeTmdb struct {
	findByIDFn          func(ctx context.Context, id, source string) (tmdb.FindByID, error)
	getMovieDetailsFn   func(ctx context.Context, id int64) (tmdb.MovieDetails, error)
	getSeriesDetailsFn  func(ctx context.Context, id int64) (tmdb.TVDetails, error)
	getEpisodeDetailsFn func(ctx context.Context, id int64, season int64, episode int64) (tmdb.TVEpisodeDetails, error)
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
	mediaItemID := testUUID(1)
	seasonID := testUUID(2)
	episodeID := testUUID(3)

	var tmdbID int64 = 12345
	var seasonNum int32 = 1
	var episodeNum int32 = 5

	t.Run("nil season returns nil", func(t *testing.T) {
		s := newTestScanner(&fakeRepo{}, &fakeTmdb{})
		ident := identity.Identity{TmdbID: &tmdbID}
		got, err := s.ensureSeasonAndEpisode(context.Background(), mediaItemID, ident, "/some/path")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Fatal("expected nil episode ID")
		}
	})

	t.Run("nil episode upserts season only", func(t *testing.T) {
		fr := &fakeRepo{
			upsertSeasonFn: func(ctx context.Context, mid pgtype.UUID, sn int32, ad pgtype.Date) (dbgen.MediaSeason, error) {
				return dbgen.MediaSeason{ID: seasonID}, nil
			},
		}
		s := newTestScanner(fr, &fakeTmdb{})
		ident := identity.Identity{TmdbID: &tmdbID, Season: &seasonNum}
		got, err := s.ensureSeasonAndEpisode(context.Background(), mediaItemID, ident, "/some/path")
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
			upsertSeasonFn: func(ctx context.Context, mid pgtype.UUID, sn int32, ad pgtype.Date) (dbgen.MediaSeason, error) {
				return dbgen.MediaSeason{ID: seasonID}, nil
			},
			upsertEpisodeFn: func(ctx context.Context, sid pgtype.UUID, en int32, title *string, ad pgtype.Date, tid *int64, tvid *int64) (dbgen.MediaEpisode, error) {
				if title == nil || *title != epName {
					t.Errorf("expected episode title %q, got %v", epName, title)
				}
				return dbgen.MediaEpisode{ID: episodeID}, nil
			},
		}
		ft := &fakeTmdb{
			getEpisodeDetailsFn: func(ctx context.Context, id int64, season int64, episode int64) (tmdb.TVEpisodeDetails, error) {
				return tmdb.TVEpisodeDetails{Name: epName}, nil
			},
		}
		s := newTestScanner(fr, ft)
		ident := identity.Identity{TmdbID: &tmdbID, Season: &seasonNum, Episode: &episodeNum}
		got, err := s.ensureSeasonAndEpisode(context.Background(), mediaItemID, ident, "/some/path")
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
			upsertSeasonFn: func(ctx context.Context, mid pgtype.UUID, sn int32, ad pgtype.Date) (dbgen.MediaSeason, error) {
				return dbgen.MediaSeason{}, errors.New("db error")
			},
		}
		s := newTestScanner(fr, &fakeTmdb{})
		ident := identity.Identity{TmdbID: &tmdbID, Season: &seasonNum, Episode: &episodeNum}
		_, err := s.ensureSeasonAndEpisode(context.Background(), mediaItemID, ident, "/some/path")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("GetEpisodeDetails error", func(t *testing.T) {
		fr := &fakeRepo{
			upsertSeasonFn: func(ctx context.Context, mid pgtype.UUID, sn int32, ad pgtype.Date) (dbgen.MediaSeason, error) {
				return dbgen.MediaSeason{ID: seasonID}, nil
			},
		}
		ft := &fakeTmdb{
			getEpisodeDetailsFn: func(ctx context.Context, id int64, season int64, episode int64) (tmdb.TVEpisodeDetails, error) {
				return tmdb.TVEpisodeDetails{}, errors.New("tmdb error")
			},
		}
		s := newTestScanner(fr, ft)
		ident := identity.Identity{TmdbID: &tmdbID, Season: &seasonNum, Episode: &episodeNum}
		_, err := s.ensureSeasonAndEpisode(context.Background(), mediaItemID, ident, "/some/path")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("UpsertEpisode error", func(t *testing.T) {
		fr := &fakeRepo{
			upsertSeasonFn: func(ctx context.Context, mid pgtype.UUID, sn int32, ad pgtype.Date) (dbgen.MediaSeason, error) {
				return dbgen.MediaSeason{ID: seasonID}, nil
			},
			upsertEpisodeFn: func(ctx context.Context, sid pgtype.UUID, en int32, title *string, ad pgtype.Date, tid *int64, tvid *int64) (dbgen.MediaEpisode, error) {
				return dbgen.MediaEpisode{}, errors.New("db error")
			},
		}
		ft := &fakeTmdb{
			getEpisodeDetailsFn: func(ctx context.Context, id int64, season int64, episode int64) (tmdb.TVEpisodeDetails, error) {
				return tmdb.TVEpisodeDetails{Name: "ep"}, nil
			},
		}
		s := newTestScanner(fr, ft)
		ident := identity.Identity{TmdbID: &tmdbID, Season: &seasonNum, Episode: &episodeNum}
		_, err := s.ensureSeasonAndEpisode(context.Background(), mediaItemID, ident, "/some/path")
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
		getLibraryFn: func(ctx context.Context, id pgtype.UUID) (dbgen.Library, error) {
			return dbgen.Library{ID: id, RootPath: dir, Type: "movie", Name: "test"}, nil
		},
		getMediaFileByLibraryAndPathFn: func(ctx context.Context, libraryID pgtype.UUID, path string) (dbgen.MediaFile, error) {
			<-block
			return dbgen.MediaFile{}, pgx.ErrNoRows
		},
	}

	s := newTestScanner(fr, &fakeTmdb{})

	libID := testUUID(1)
	_, err := s.StartScan(context.Background(), libID)
	if err != nil {
		t.Fatalf("first scan should succeed: %v", err)
	}

	// The running key was stored before the goroutine launched,
	// so a second scan for the same library must fail.
	_, err = s.StartScan(context.Background(), libID)
	if !errors.Is(err, ErrScanAlreadyRunning) {
		t.Fatalf("expected ErrScanAlreadyRunning, got %v", err)
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
		getMediaFileByLibraryAndPathFn: func(ctx context.Context, libraryID pgtype.UUID, path string) (dbgen.MediaFile, error) {
			return dbgen.MediaFile{}, nil // file already exists
		},
	}

	s := newTestScanner(fr, &fakeTmdb{})
	library := dbgen.Library{ID: testUUID(1), RootPath: dir, Type: "movie", Name: "test"}

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
	library := dbgen.Library{ID: testUUID(1), RootPath: dir, Type: "movie", Name: "test"}

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

	mediaItemID := testUUID(10)
	mediaFileID := testUUID(11)

	fr := &fakeRepo{
		getMediaFileByLibraryAndPathFn: func(ctx context.Context, libraryID pgtype.UUID, path string) (dbgen.MediaFile, error) {
			return dbgen.MediaFile{}, pgx.ErrNoRows
		},
		getMediaItemByTmdbIDAndTypeFn: func(ctx context.Context, tmdbID int64, typ string) (dbgen.MediaItem, error) {
			return dbgen.MediaItem{}, pgx.ErrNoRows
		},
		createMediaItemFn: func(ctx context.Context, typ, title string, year *int32, tmdbID *int64) (dbgen.MediaItem, error) {
			if typ != "movie" {
				t.Errorf("expected type 'movie', got %q", typ)
			}
			if title != "Movie Title" {
				t.Errorf("expected title 'Movie Title', got %q", title)
			}
			return dbgen.MediaItem{ID: mediaItemID}, nil
		},
		createMediaFileFn: func(ctx context.Context, libraryID, mid pgtype.UUID, episodeID *pgtype.UUID, path string) (dbgen.MediaFile, error) {
			if episodeID != nil {
				t.Error("expected nil episodeID for movie")
			}
			return dbgen.MediaFile{ID: mediaFileID}, nil
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
	library := dbgen.Library{ID: testUUID(1), RootPath: dir, Type: "movie", Name: "test"}

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

	mediaItemID := testUUID(10)
	seasonID := testUUID(11)
	episodeID := testUUID(12)
	mediaFileID := testUUID(13)

	fr := &fakeRepo{
		getMediaFileByLibraryAndPathFn: func(ctx context.Context, libraryID pgtype.UUID, path string) (dbgen.MediaFile, error) {
			return dbgen.MediaFile{}, pgx.ErrNoRows
		},
		getMediaItemByTmdbIDAndTypeFn: func(ctx context.Context, tmdbID int64, typ string) (dbgen.MediaItem, error) {
			return dbgen.MediaItem{}, pgx.ErrNoRows
		},
		createMediaItemFn: func(ctx context.Context, typ, title string, year *int32, tmdbID *int64) (dbgen.MediaItem, error) {
			return dbgen.MediaItem{ID: mediaItemID}, nil
		},
		upsertSeasonFn: func(ctx context.Context, mid pgtype.UUID, sn int32, ad pgtype.Date) (dbgen.MediaSeason, error) {
			if sn != 1 {
				t.Errorf("expected season 1, got %d", sn)
			}
			return dbgen.MediaSeason{ID: seasonID}, nil
		},
		upsertEpisodeFn: func(ctx context.Context, sid pgtype.UUID, en int32, title *string, ad pgtype.Date, tid *int64, tvid *int64) (dbgen.MediaEpisode, error) {
			if en != 1 {
				t.Errorf("expected episode 1, got %d", en)
			}
			return dbgen.MediaEpisode{ID: episodeID}, nil
		},
		createMediaFileFn: func(ctx context.Context, libraryID, mid pgtype.UUID, epID *pgtype.UUID, path string) (dbgen.MediaFile, error) {
			if epID == nil {
				t.Error("expected non-nil episodeID for series")
			}
			return dbgen.MediaFile{ID: mediaFileID}, nil
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
	library := dbgen.Library{ID: testUUID(1), RootPath: dir, Type: "series", Name: "test"}

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

	mediaItemID := testUUID(10)
	seasonID := testUUID(11)
	episodeID := testUUID(12)
	mediaFileID := testUUID(13)

	fr := &fakeRepo{
		getMediaFileByLibraryAndPathFn: func(ctx context.Context, libraryID pgtype.UUID, path string) (dbgen.MediaFile, error) {
			return dbgen.MediaFile{}, pgx.ErrNoRows // file doesn't exist yet
		},
		getMediaItemByTmdbIDAndTypeFn: func(ctx context.Context, tmdbID int64, typ string) (dbgen.MediaItem, error) {
			return dbgen.MediaItem{ID: mediaItemID}, nil // media item already exists
		},
		upsertSeasonFn: func(ctx context.Context, mid pgtype.UUID, sn int32, ad pgtype.Date) (dbgen.MediaSeason, error) {
			return dbgen.MediaSeason{ID: seasonID}, nil
		},
		upsertEpisodeFn: func(ctx context.Context, sid pgtype.UUID, en int32, title *string, ad pgtype.Date, tid *int64, tvid *int64) (dbgen.MediaEpisode, error) {
			if en != 2 {
				t.Errorf("expected episode 2, got %d", en)
			}
			return dbgen.MediaEpisode{ID: episodeID}, nil
		},
		createMediaFileFn: func(ctx context.Context, libraryID, mid pgtype.UUID, epID *pgtype.UUID, path string) (dbgen.MediaFile, error) {
			if epID == nil {
				t.Error("expected non-nil episodeID")
			}
			return dbgen.MediaFile{ID: mediaFileID}, nil
		},
	}

	ft := &fakeTmdb{
		getEpisodeDetailsFn: func(ctx context.Context, id int64, season int64, episode int64) (tmdb.TVEpisodeDetails, error) {
			return tmdb.TVEpisodeDetails{Name: "Episode 2"}, nil
		},
	}

	s := newTestScanner(fr, ft)
	library := dbgen.Library{ID: testUUID(1), RootPath: dir, Type: "series", Name: "test"}

	stats, err := s.executeScan(context.Background(), library, "scan-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.FilesSeen != 1 {
		t.Fatalf("expected 1 file seen, got %d", stats.FilesSeen)
	}
	// Media item already existed, so shouldn't be counted as created.
	if stats.MediaItemsCreated != 0 {
		t.Fatalf("expected 0 media items created, got %d", stats.MediaItemsCreated)
	}
	// Should still create season and episode for the new file.
	if fr.upsertSeasonCalls != 1 {
		t.Fatalf("expected 1 UpsertSeason call, got %d", fr.upsertSeasonCalls)
	}
	if fr.upsertEpisodeCalls != 1 {
		t.Fatalf("expected 1 UpsertEpisode call, got %d", fr.upsertEpisodeCalls)
	}
	// CreateMediaItem should NOT have been called.
	if fr.createMediaItemCalls != 0 {
		t.Fatalf("expected 0 CreateMediaItem calls, got %d", fr.createMediaItemCalls)
	}
	// CreateMediaFile should have been called.
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
		getMediaFileByLibraryAndPathFn: func(ctx context.Context, libraryID pgtype.UUID, path string) (dbgen.MediaFile, error) {
			return dbgen.MediaFile{}, pgx.ErrNoRows
		},
		getMediaItemByTmdbIDAndTypeFn: func(ctx context.Context, tmdbID int64, typ string) (dbgen.MediaItem, error) {
			return dbgen.MediaItem{}, pgx.ErrNoRows
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
	library := dbgen.Library{ID: testUUID(1), RootPath: dir, Type: "movie", Name: "test"}

	stats, err := s.executeScan(context.Background(), library, "scan-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.FilesSeen != 1 {
		t.Fatalf("expected 1 file seen, got %d", stats.FilesSeen)
	}
	// File should have been skipped gracefully (no media item created).
	if stats.MediaItemsCreated != 0 {
		t.Fatalf("expected 0 media items created, got %d", stats.MediaItemsCreated)
	}
	if fr.createMediaItemCalls != 0 {
		t.Fatalf("expected 0 CreateMediaItem calls, got %d", fr.createMediaItemCalls)
	}
}
