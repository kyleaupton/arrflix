package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/guessit"
	"github.com/kyleaupton/arrflix/internal/identity"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/sse"
)

// scanRepo is the subset of repo.Repository used by the scanner.
type scanRepo interface {
	GetLibrary(ctx context.Context, id uuid.UUID) (model.Library, error)
	GetMediaFileByLibraryAndPath(ctx context.Context, params repo.GetMediaFileByLibraryAndPathParams) (model.MediaFile, error)
	GetMediaItemByTmdbIDAndType(ctx context.Context, tmdbID int64, typ string) (model.MediaItem, error)
	CreateMediaItem(ctx context.Context, params repo.CreateMediaItemParams) (model.MediaItem, error)
	UpsertSeason(ctx context.Context, params repo.UpsertSeasonParams) (model.MediaSeason, error)
	UpsertEpisode(ctx context.Context, params repo.UpsertEpisodeParams) (model.MediaEpisode, error)
	CreateMediaFile(ctx context.Context, params repo.CreateMediaFileParams) (model.MediaFile, error)
	UpsertMediaFileState(ctx context.Context, params repo.UpsertMediaFileStateParams) (model.MediaFileState, error)
	CreateMediaFileImport(ctx context.Context, params repo.CreateMediaFileImportParams) (model.MediaFileImport, error)
	ListMediaFilePathsForLibrary(ctx context.Context, libraryID uuid.UUID) ([]string, error)
	ListUnmatchedFilePathsForLibrary(ctx context.Context, libraryID uuid.UUID) ([]string, error)
	UpsertUnmatchedFile(ctx context.Context, params repo.UpsertUnmatchedFileParams) (model.UnmatchedFile, error)
}

// scanTmdb is the subset of TmdbService used by the scanner.
type scanTmdb interface {
	FindByID(ctx context.Context, id, source string) (tmdb.FindByID, error)
	GetMovieDetails(ctx context.Context, id int64) (tmdb.MovieDetails, error)
	GetSeriesDetails(ctx context.Context, id int64) (tmdb.TVDetails, error)
	GetEpisodeDetails(ctx context.Context, id int64, season int64, episode int64) (tmdb.TVEpisodeDetails, error)
	MultiSearch(ctx context.Context, query string, page int) (tmdb.SearchMulti, error)
}

type ScannerService struct {
	repo       scanRepo
	logger     *logger.Logger
	tmdb       scanTmdb
	broker     *sse.Broker
	guessit    *guessit.Client
	enrichment *EnrichmentService
	ctx        context.Context // background context for goroutines; cancelled on shutdown
	running    sync.Map        // libraryID string -> scanID string
}

func NewScannerService(r *repo.Repository, l *logger.Logger, tmdb *TmdbService, broker *sse.Broker, gc *guessit.Client, enrichment *EnrichmentService) *ScannerService {
	return &ScannerService{repo: r, logger: l, tmdb: tmdb, broker: broker, guessit: gc, enrichment: enrichment, ctx: context.Background()}
}

// SetContext sets the background context used by scan goroutines.
// Pass a cancellable context to allow graceful shutdown of in-flight scans.
func (s *ScannerService) SetContext(ctx context.Context) {
	s.ctx = ctx
}

type scanStats struct {
	FilesSeen           int `json:"filesSeen"`
	FilesSkipped        int `json:"filesSkipped"`
	IdentifiedByEmbed   int `json:"identifiedByEmbed"`
	IdentifiedByGuessit int `json:"identifiedByGuessit"`
	MediaItemsCreated   int `json:"mediaItemsCreated"`
	UnmatchedCount      int `json:"unmatchedCount"`
	SkippedGuessitDown  int `json:"skippedGuessitDown"`
	EpisodeLookupFailed int `json:"episodeLookupFailed"`
	Duration            int `json:"duration"`
}

// Internal pipeline types

type collectedFile struct {
	RelPath  string
	AbsPath  string
	FileSize *int64
}

type identifiedFile struct {
	collectedFile
	TmdbID  int64
	Season  *int32
	Episode *int32
}

type unmatchedFileEntry struct {
	collectedFile
	Suggestions []model.SuggestedMatch
}

// tmdbSearchKey is used to deduplicate TMDB searches for files that likely belong to the same title.
type tmdbSearchKey struct {
	Query string
	Year  *int
	Type  string // "movie" or "series"
}

func (k tmdbSearchKey) String() string {
	if k.Year != nil {
		return fmt.Sprintf("%s|%d|%s", k.Query, *k.Year, k.Type)
	}
	return fmt.Sprintf("%s||%s", k.Query, k.Type)
}

// StartScan kicks off an async library scan. It returns the scan ID immediately.
// If a scan is already running for the given library, it returns a Conflict error.
func (s *ScannerService) StartScan(ctx context.Context, libraryID uuid.UUID) (string, error) {
	libKey := libraryID.String()

	// Concurrency guard: only one scan per library at a time.
	scanID := uuid.NewString()
	if _, loaded := s.running.LoadOrStore(libKey, scanID); loaded {
		return "", apperrors.Conflictf("scan already running for library %s", libraryID).
			Op("ScannerService.StartScan")
	}

	// Validate the library exists before launching the goroutine.
	library, err := s.repo.GetLibrary(ctx, libraryID)
	if err != nil {
		s.running.Delete(libKey)
		return "", err
	}

	go func() {
		defer s.running.Delete(libKey)

		s.publishEvent("scan_started", scanID, libKey, nil)

		stats, err := s.executeScan(s.ctx, library, scanID)
		if err != nil {
			s.publishEvent("scan_failed", scanID, libKey, map[string]any{
				"error": err.Error(),
			})
			return
		}

		// Final progress event so clients see the exact totals before completion.
		s.publishEvent("scan_progress", scanID, libKey, map[string]any{
			"filesSeen":         stats.FilesSeen,
			"mediaItemsCreated": stats.MediaItemsCreated,
		})

		s.publishEvent("scan_completed", scanID, libKey, map[string]any{
			"filesSeen":            stats.FilesSeen,
			"identifiedByEmbed":    stats.IdentifiedByEmbed,
			"identifiedByGuessit":  stats.IdentifiedByGuessit,
			"mediaItemsCreated":    stats.MediaItemsCreated,
			"unmatchedCount":       stats.UnmatchedCount,
			"episodeLookupFailed":  stats.EpisodeLookupFailed,
			"duration":             stats.Duration,
		})
	}()

	return scanID, nil
}

func (s *ScannerService) publishEvent(eventType, scanID, libraryID string, extra map[string]any) {
	if s.broker == nil {
		return
	}
	payload := map[string]any{
		"scanId":    scanID,
		"libraryId": libraryID,
	}
	for k, v := range extra {
		payload[k] = v
	}
	data, _ := json.Marshal(payload)
	s.broker.Publish(sse.Event{
		Type: eventType,
		Data: data,
		ID:   scanID,
	})
}

// ensureSeasonAndEpisode upserts the season and episode for a series media file.
// Returns the episode ID if one was created, or nil for movies / missing season info.
func (s *ScannerService) ensureSeasonAndEpisode(ctx context.Context, mediaItemID uuid.UUID, tmdbID int64, season, episode *int32, path string) (*uuid.UUID, error) {
	if season == nil {
		return nil, nil
	}

	seasonRow, err := s.repo.UpsertSeason(ctx, repo.UpsertSeasonParams{
		MediaItemID:  mediaItemID,
		SeasonNumber: *season,
	})
	if err != nil {
		s.logger.Error().Err(err).Str("path", path).Msg("Error upserting season")
		return nil, err
	}

	if episode == nil {
		return nil, nil
	}

	ep, err := s.tmdb.GetEpisodeDetails(ctx, tmdbID, int64(*season), int64(*episode))
	if err != nil {
		s.logger.Error().Err(err).Str("path", path).Msg("Error getting episode details from TMDB")
		return nil, err
	}

	episodeRow, err := s.repo.UpsertEpisode(ctx, repo.UpsertEpisodeParams{
		SeasonID:      seasonRow.ID,
		EpisodeNumber: *episode,
		Title:         &ep.Name,
	})
	if err != nil {
		s.logger.Error().Err(err).Str("path", path).Msg("Error upserting episode")
		return nil, err
	}

	episodeID := episodeRow.ID
	return &episodeID, nil
}

// ---------------------------------------------------------------------------
// Pipeline: executeScan
// ---------------------------------------------------------------------------

func (s *ScannerService) executeScan(ctx context.Context, library model.Library, scanID string) (scanStats, error) {
	stats := scanStats{}
	start := time.Now()
	libKey := library.ID.String()

	s.logger.Info().Str("library_name", library.Name).Str("library_path", library.RootPath).Msg("Starting Scan")

	// Phase 1: Walk & Collect
	knownPaths, err := s.loadKnownPaths(ctx, library.ID)
	if err != nil {
		s.logger.Warn().Err(err).Msg("Failed to bulk-load known paths, falling back to per-file checks")
		knownPaths = nil // will fall back to per-file DB lookups
	}

	var collected []collectedFile

	err = filepath.WalkDir(library.RootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || !isMediaFile(path) {
			return nil
		}
		if isExtraFile(path) {
			return nil
		}

		stats.FilesSeen++

		if stats.FilesSeen%50 == 0 {
			s.publishEvent("scan_progress", scanID, libKey, map[string]any{
				"filesSeen":         stats.FilesSeen,
				"mediaItemsCreated": stats.MediaItemsCreated,
			})
		}

		relPath, err := filepath.Rel(library.RootPath, path)
		if err != nil || strings.HasPrefix(relPath, "..") {
			s.logger.Error().Str("path", path).Err(err).Msg("Path outside library root")
			return nil
		}

		// Dedup against known paths
		if knownPaths != nil {
			if _, exists := knownPaths[relPath]; exists {
				stats.FilesSkipped++
				return nil
			}
		} else {
			// Fallback: per-file DB check
			_, dbErr := s.repo.GetMediaFileByLibraryAndPath(ctx, repo.GetMediaFileByLibraryAndPathParams{
				LibraryID: library.ID,
				Path:      relPath,
			})
			if dbErr == nil {
				stats.FilesSkipped++
				return nil
			}
			if !apperrors.IsNotFound(dbErr) {
				return dbErr
			}
		}

		var fileSize *int64
		if info, infoErr := d.Info(); infoErr == nil && info != nil {
			size := info.Size()
			fileSize = &size
		}

		collected = append(collected, collectedFile{
			RelPath:  relPath,
			AbsPath:  path,
			FileSize: fileSize,
		})

		return nil
	})
	if err != nil {
		return scanStats{}, err
	}

	s.logger.Info().Int("collected", len(collected)).Int("seen", stats.FilesSeen).Msg("Phase 1 complete: Walk & Collect")

	// Phase 2: Embedded ID Resolution
	var identified []identifiedFile
	var needsParsing []collectedFile

	for _, f := range collected {
		ident, resolveErr := identity.Resolve(library, f.AbsPath)
		if resolveErr != nil {
			needsParsing = append(needsParsing, f)
			continue
		}

		tmdbID, convertErr := s.resolveTmdbID(ctx, library, ident)
		if convertErr != nil {
			s.logger.Debug().Err(convertErr).Str("path", f.RelPath).Msg("Could not resolve TMDB ID from embedded identity")
			needsParsing = append(needsParsing, f)
			continue
		}

		identified = append(identified, identifiedFile{
			collectedFile: f,
			TmdbID:        tmdbID,
			Season:        ident.Season,
			Episode:       ident.Episode,
		})
		stats.IdentifiedByEmbed++
	}

	s.logger.Info().Int("identified", len(identified)).Int("needsParsing", len(needsParsing)).Msg("Phase 2 complete: Embedded ID Resolution")

	// Phase 3: Guessit + TMDB Search
	if len(needsParsing) > 0 {
		guessitIdentified, unmatched, guessitErr := s.processWithGuessit(ctx, library, needsParsing)
		if guessitErr != nil {
			s.logger.Warn().Err(guessitErr).Msg("Guessit processing failed")
			stats.SkippedGuessitDown = len(needsParsing)
		} else {
			identified = append(identified, guessitIdentified...)
			stats.IdentifiedByGuessit = len(guessitIdentified)

			// Create unmatched entries
			for _, uf := range unmatched {
				if _, upsertErr := s.repo.UpsertUnmatchedFile(ctx, repo.UpsertUnmatchedFileParams{
					LibraryID:        library.ID,
					Path:             uf.RelPath,
					FileSize:         uf.FileSize,
					SuggestedMatches: uf.Suggestions,
				}); upsertErr != nil {
					s.logger.Warn().Err(upsertErr).Str("path", uf.RelPath).Msg("Failed to upsert unmatched file")
				}
				stats.UnmatchedCount++
			}
		}
	}

	s.logger.Info().Int("totalIdentified", len(identified)).Msg("Phase 3 complete: Guessit + TMDB Search")

	// Phase 4: Process Identified Files
	for i, f := range identified {
		if ctx.Err() != nil {
			return scanStats{}, ctx.Err()
		}

		created, epFailed, processErr := s.processIdentifiedFile(ctx, library, f)
		if processErr != nil {
			s.logger.Warn().Err(processErr).Str("path", f.RelPath).Msg("Error processing identified file")
			continue
		}
		if created {
			stats.MediaItemsCreated++
		}
		if epFailed {
			stats.EpisodeLookupFailed++
		}

		if (i+1)%50 == 0 {
			s.publishEvent("scan_progress", scanID, libKey, map[string]any{
				"filesSeen":         stats.FilesSeen,
				"mediaItemsCreated": stats.MediaItemsCreated,
			})
		}
	}

	s.logger.Info().Str("library_id", libKey).Msg("Scan Complete")
	stats.Duration = int(time.Since(start).Seconds())

	return stats, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// loadKnownPaths bulk-loads all media_file and unresolved unmatched_file paths
// for the given library into a set for O(1) dedup during the walk phase.
func (s *ScannerService) loadKnownPaths(ctx context.Context, libraryID uuid.UUID) (map[string]struct{}, error) {
	mediaPaths, err := s.repo.ListMediaFilePathsForLibrary(ctx, libraryID)
	if err != nil {
		return nil, err
	}

	unmatchedPaths, err := s.repo.ListUnmatchedFilePathsForLibrary(ctx, libraryID)
	if err != nil {
		return nil, err
	}

	known := make(map[string]struct{}, len(mediaPaths)+len(unmatchedPaths))
	for _, p := range mediaPaths {
		known[p] = struct{}{}
	}
	for _, p := range unmatchedPaths {
		known[p] = struct{}{}
	}
	return known, nil
}

// resolveTmdbID extracts a TMDB ID from an identity, converting from TVDB/IMDB if needed.
func (s *ScannerService) resolveTmdbID(ctx context.Context, library model.Library, ident identity.Identity) (int64, error) {
	if ident.TmdbID != nil {
		return *ident.TmdbID, nil
	}

	var id string
	var provider string

	if ident.TvdbID != nil {
		id = strconv.FormatInt(*ident.TvdbID, 10)
		provider = "tvdb_id"
	} else if ident.ImdbID != nil {
		id = *ident.ImdbID
		provider = "imdb_id"
	} else {
		return 0, apperrors.Internalf("embedded identity has no provider IDs").
			Op("ScannerService.resolveTmdbID").
			NotRetryable()
	}

	res, err := s.tmdb.FindByID(ctx, id, provider)
	if err != nil {
		return 0, err
	}

	switch library.Type {
	case "movie":
		if len(res.MovieResults) > 0 {
			return res.MovieResults[0].ID, nil
		}
	case "series":
		if len(res.TvResults) > 0 {
			return res.TvResults[0].ID, nil
		}
	}

	return 0, apperrors.NotFoundf("no TMDB results for %s=%q", provider, id).
		Op("ScannerService.resolveTmdbID")
}

// ensureMediaItem looks up or creates a media item for the given TMDB ID.
// Returns the media item ID and whether a new item was created.
func (s *ScannerService) ensureMediaItem(ctx context.Context, library model.Library, tmdbID int64) (uuid.UUID, bool, error) {
	existing, err := s.repo.GetMediaItemByTmdbIDAndType(ctx, tmdbID, library.Type)
	if err == nil {
		return existing.ID, false, nil
	}
	if !apperrors.IsNotFound(err) {
		return uuid.Nil, false, err
	}

	// Fetch details from TMDB
	var title string
	var year int32

	switch library.Type {
	case "movie":
		movie, detErr := s.tmdb.GetMovieDetails(ctx, tmdbID)
		if detErr != nil {
			return uuid.Nil, false, detErr
		}
		title = movie.Title
		if movie.ReleaseDate == "" {
			return uuid.Nil, false, apperrors.BadGatewayf("tmdb movie %d has empty release date", tmdbID).
				Op("ScannerService.ensureMediaItem")
		}
		yearStr := strings.Split(movie.ReleaseDate, "-")[0]
		year64, parseErr := strconv.ParseInt(yearStr, 10, 32)
		if parseErr != nil {
			return uuid.Nil, false, apperrors.BadGatewayf("tmdb movie %d release date %q unparseable: %v", tmdbID, movie.ReleaseDate, parseErr).
				Op("ScannerService.ensureMediaItem")
		}
		year = int32(year64)

	case "series":
		tv, detErr := s.tmdb.GetSeriesDetails(ctx, tmdbID)
		if detErr != nil {
			return uuid.Nil, false, detErr
		}
		title = tv.Name
		if tv.FirstAirDate == "" {
			return uuid.Nil, false, apperrors.BadGatewayf("tmdb series %d has empty first air date", tmdbID).
				Op("ScannerService.ensureMediaItem")
		}
		yearStr := strings.Split(tv.FirstAirDate, "-")[0]
		year64, parseErr := strconv.ParseInt(yearStr, 10, 32)
		if parseErr != nil {
			return uuid.Nil, false, apperrors.BadGatewayf("tmdb series %d first air date %q unparseable: %v", tmdbID, tv.FirstAirDate, parseErr).
				Op("ScannerService.ensureMediaItem")
		}
		year = int32(year64)
	}

	item, err := s.repo.CreateMediaItem(ctx, repo.CreateMediaItemParams{
		Type:   library.Type,
		Title:  title,
		Year:   &year,
		TmdbID: &tmdbID,
	})
	if err != nil {
		return uuid.Nil, false, err
	}

	// Fire async enrichment so metadata is available quickly
	if s.enrichment != nil {
		go func() {
			if err := s.enrichment.EnrichMediaItem(s.ctx, item); err != nil {
				s.logger.Warn().Err(err).Str("title", item.Title).Msg("scan-time enrichment failed, worker will retry")
			}
		}()
	}

	return item.ID, true, nil
}

// processIdentifiedFile creates all DB records for a single identified file.
// Returns (mediaItemCreated, episodeLookupFailed, error).
func (s *ScannerService) processIdentifiedFile(ctx context.Context, library model.Library, f identifiedFile) (bool, bool, error) {
	mediaItemID, created, err := s.ensureMediaItem(ctx, library, f.TmdbID)
	if err != nil {
		return false, false, err
	}

	episodeID, err := s.ensureSeasonAndEpisode(ctx, mediaItemID, f.TmdbID, f.Season, f.Episode, f.AbsPath)
	if err != nil {
		return created, true, nil // skip file, continue
	}

	mf, err := s.repo.CreateMediaFile(ctx, repo.CreateMediaFileParams{
		LibraryID:   library.ID,
		MediaItemID: mediaItemID,
		EpisodeID:   episodeID,
		Path:        f.RelPath,
	})
	if err != nil {
		return created, false, err
	}

	if _, err := s.repo.UpsertMediaFileState(ctx, repo.UpsertMediaFileStateParams{
		MediaFileID: mf.ID,
		FileExists:  true,
		FileSize:    f.FileSize,
	}); err != nil {
		s.logger.Warn().Err(err).Msg("Failed to create media file state")
	}

	if _, err := s.repo.CreateMediaFileImport(ctx, repo.CreateMediaFileImportParams{
		MediaFileID: mf.ID,
		Method:      "scan",
		DestPath:    f.AbsPath,
		Success:     true,
	}); err != nil {
		s.logger.Warn().Err(err).Msg("Failed to record import history")
	}

	return created, false, nil
}

// processWithGuessit handles files that couldn't be identified by embedded IDs.
// It batch-parses filenames via guessit, groups by title, searches TMDB, and
// either auto-matches or creates unmatched entries with suggestions.
func (s *ScannerService) processWithGuessit(ctx context.Context, library model.Library, files []collectedFile) ([]identifiedFile, []unmatchedFileEntry, error) {
	if s.guessit == nil || !s.guessit.Healthy(ctx) {
		return nil, nil, apperrors.BadGatewayf("guessit sidecar unavailable").
			Op("ScannerService.processWithGuessit")
	}

	// Build guessit input strings
	inputs := make([]string, len(files))
	for i, f := range files {
		inputs[i] = guessitInput(library.Type, f.RelPath)
	}

	// Batch parse in chunks of 200
	const chunkSize = 200
	allResults := make([]guessit.ParseResult, 0, len(inputs))
	for i := 0; i < len(inputs); i += chunkSize {
		end := i + chunkSize
		if end > len(inputs) {
			end = len(inputs)
		}
		chunk, err := s.guessit.ParseBatch(ctx, inputs[i:end])
		if err != nil {
			return nil, nil, apperrors.BadGatewayf("guessit batch parse: %v", err).
				Op("ScannerService.processWithGuessit")
		}
		allResults = append(allResults, chunk...)
	}

	// Group files by search key
	type groupEntry struct {
		file   collectedFile
		result guessit.ParseResult
	}
	groups := make(map[string][]groupEntry)
	keyMap := make(map[string]tmdbSearchKey) // string key -> search key

	for i, f := range files {
		result := allResults[i]
		key := buildSearchKey(library.Type, f.RelPath, result)
		keyStr := key.String()
		groups[keyStr] = append(groups[keyStr], groupEntry{file: f, result: result})
		keyMap[keyStr] = key
	}

	s.logger.Info().Int("groups", len(groups)).Int("files", len(files)).Msg("Guessit grouped files for TMDB search")

	var identified []identifiedFile
	var unmatched []unmatchedFileEntry

	// One TMDB search per unique group key
	for keyStr, entries := range groups {
		key := keyMap[keyStr]
		if key.Query == "" {
			// Guessit couldn't parse a title — mark all as unmatched with no suggestions
			for _, e := range entries {
				unmatched = append(unmatched, unmatchedFileEntry{
					collectedFile: e.file,
					Suggestions:   []model.SuggestedMatch{},
				})
			}
			continue
		}

		searchResult, err := s.tmdb.MultiSearch(ctx, key.Query, 1)
		if err != nil {
			s.logger.Warn().Err(err).Str("query", key.Query).Msg("TMDB multi search failed")
			for _, e := range entries {
				unmatched = append(unmatched, unmatchedFileEntry{
					collectedFile: e.file,
					Suggestions:   []model.SuggestedMatch{},
				})
			}
			continue
		}

		match, suggestions := evaluateSearchResults(library.Type, key, searchResult)

		if match != nil {
			// Auto-matched: assign TMDB ID to all files in this group
			for _, e := range entries {
				idf := identifiedFile{
					collectedFile: e.file,
					TmdbID:        match.tmdbID,
				}
				// For series, extract season/episode from guessit result
				if library.Type == "series" && e.result.Season != nil {
					s32 := int32(*e.result.Season)
					idf.Season = &s32
				}
				if library.Type == "series" && e.result.Episode != nil {
					e32 := int32(*e.result.Episode)
					idf.Episode = &e32
				}
				identified = append(identified, idf)
			}
		} else {
			for _, e := range entries {
				unmatched = append(unmatched, unmatchedFileEntry{
					collectedFile: e.file,
					Suggestions:   suggestions,
				})
			}
		}
	}

	return identified, unmatched, nil
}

// guessitInput picks the best path segment to send to guessit.
// For series: use the filename (e.g., "South Park S01E01.mkv")
// For movies: prefer parent directory name (e.g., "The Matrix (1999)"), fall back to filename
func guessitInput(libraryType, relPath string) string {
	if libraryType == "series" {
		return filepath.Base(relPath)
	}

	// Movie: prefer parent directory
	dir := filepath.Dir(relPath)
	if dir != "" && dir != "." {
		return filepath.Base(dir)
	}
	return filepath.Base(relPath)
}

// buildSearchKey creates a dedup key for grouping files that should share one TMDB search.
// Series: group by top-level directory name (all episodes of the same show)
// Movies: group by (title, year) from guessit
func buildSearchKey(libraryType, relPath string, result guessit.ParseResult) tmdbSearchKey {
	if libraryType == "series" {
		// Use the top-level directory name as the search query
		parts := strings.SplitN(relPath, string(filepath.Separator), 2)
		topDir := parts[0]
		// Clean common patterns from directory names for better search
		query := cleanDirName(topDir)
		return tmdbSearchKey{Query: query, Type: "series"}
	}

	// Movie: use title and year from guessit
	title := ""
	if result.Title != nil {
		title = *result.Title
	}
	return tmdbSearchKey{Query: title, Year: result.Year, Type: "movie"}
}

// cleanDirName removes common non-title artifacts from directory names.
func cleanDirName(name string) string {
	// Remove year in parentheses or brackets like "(2024)" or "[2024]"
	// but keep the rest. This gives better TMDB search results.
	// For series dirs like "South Park", this is already clean.
	// For "South Park (1997)", we want to search "South Park".
	name = strings.TrimSpace(name)

	// Remove trailing year patterns
	for _, sep := range []string{"(", "["} {
		if idx := strings.LastIndex(name, sep); idx > 0 {
			candidate := strings.TrimSpace(name[:idx])
			if candidate != "" {
				name = candidate
			}
		}
	}

	return name
}

type tmdbMatch struct {
	tmdbID int64
}

// evaluateSearchResults matches TMDB search results against a search key.
// First filters by media type, then narrows by year if available. Returns a
// match if exactly 1 candidate remains, otherwise returns up to 5 suggestions.
func evaluateSearchResults(libraryType string, key tmdbSearchKey, searchResult tmdb.SearchMulti) (*tmdbMatch, []model.SuggestedMatch) {
	if searchResult.SearchMultiResults == nil {
		return nil, []model.SuggestedMatch{}
	}

	// Map library type to TMDB media type
	wantMediaType := "movie"
	if libraryType == "series" {
		wantMediaType = "tv"
	}

	// Filter to matching media type
	type candidate struct {
		ID    int64
		Title string
		Year  int
	}
	var candidates []candidate

	for _, r := range searchResult.Results {
		if r.MediaType != wantMediaType {
			continue
		}

		title := r.Title
		dateStr := r.ReleaseDate
		if wantMediaType == "tv" {
			title = r.Name
			dateStr = r.FirstAirDate
		}

		year := 0
		if len(dateStr) >= 4 {
			if y, err := strconv.Atoi(dateStr[:4]); err == nil {
				year = y
			}
		}

		candidates = append(candidates, candidate{ID: r.ID, Title: title, Year: year})
	}

	// If we have a year from guessit, narrow candidates to exact year matches only.
	// This lets us auto-match even when TMDB returns multiple results (e.g. sequels/remakes),
	// as long as only one result has the right year. Results with no year (year==0) are
	// excluded because they are typically obscure entries that would prevent disambiguation.
	if key.Year != nil {
		var yearFiltered []candidate
		for _, c := range candidates {
			if c.Year == *key.Year {
				yearFiltered = append(yearFiltered, c)
			}
		}
		if len(yearFiltered) > 0 {
			candidates = yearFiltered
		}
	}

	// Auto-match: exactly 1 candidate remaining (after year filtering if applicable)
	if len(candidates) == 1 {
		return &tmdbMatch{tmdbID: candidates[0].ID}, nil
	}

	// Build suggestions (top 5)
	limit := 5
	if len(candidates) < limit {
		limit = len(candidates)
	}

	suggestions := make([]model.SuggestedMatch, 0, limit)
	for i := 0; i < limit; i++ {
		c := candidates[i]
		score := 100 - (i * 15) // rank-based score: 100, 85, 70, 55, 40
		if score < 10 {
			score = 10
		}
		suggestions = append(suggestions, model.SuggestedMatch{
			TmdbID: c.ID,
			Title:  c.Title,
			Year:   c.Year,
			Type:   libraryType,
			Score:  score,
		})
	}

	return nil, suggestions
}

func isMediaFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mkv", ".mp4", ".avi", ".mov", ".wmv", ".flv", ".m4v", ".webm":
		return true
	}
	return false
}

// isExtraFile returns true for files in directories that contain bonus content
// (featurettes, samples, extras, etc.) rather than the main movie/episode.
func isExtraFile(path string) bool {
	for _, part := range strings.Split(filepath.Dir(path), string(filepath.Separator)) {
		switch strings.ToLower(part) {
		case "featurettes", "featurette", "sample", "samples",
			"extras", "extra", "bonus", "behind the scenes",
			"deleted scenes", "interviews", "trailers", "shorts":
			return true
		}
	}

	// Also catch "sample.mkv" or files starting with "sample" at the filename level
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if base == "sample" || strings.HasPrefix(base, "sample.") || strings.HasPrefix(base, "sample-") {
		return true
	}

	return false
}

// Compile-time assertion: *repo.Repository must satisfy scanRepo. Catches
// drift if the repo signature changes without updating the interface.
var _ scanRepo = (*repo.Repository)(nil)
