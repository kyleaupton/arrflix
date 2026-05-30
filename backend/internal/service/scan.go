package service

import (
	"context"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/matcher"
	"github.com/kyleaupton/arrflix/internal/metadata"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/osdb"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/sse"
)

// ScannerService owns filesystem discovery + persistence; the matcher
// owns identification. Phase 3's split (per specs/modules/matching § Code
// structure) lands here: scan's executeScan goes walk → match → enrich →
// persist, with the match step a single MatcherService.MatchBatch call.
type ScannerService struct {
	repo       *repo.Repository
	logger     *logger.Logger
	tmdb       *TmdbService
	broker     *sse.Broker
	matcher    *MatcherService
	enrichment *EnrichmentService
	ctx        context.Context // background context for goroutines; cancelled on shutdown
	running    sync.Map        // libraryID string -> scanID string
}

func NewScannerService(r *repo.Repository, l *logger.Logger, tmdb *TmdbService, broker *sse.Broker, m *MatcherService, enrichment *EnrichmentService) *ScannerService {
	return &ScannerService{repo: r, logger: l, tmdb: tmdb, broker: broker, matcher: m, enrichment: enrichment, ctx: context.Background()}
}

// SetContext sets the background context used by scan goroutines.
// Pass a cancellable context to allow graceful shutdown of in-flight scans.
func (s *ScannerService) SetContext(ctx context.Context) {
	s.ctx = ctx
}

type scanStats struct {
	FilesSeen           int `json:"filesSeen"`
	FilesSkipped        int `json:"filesSkipped"`
	Confident           int `json:"confident"`
	ConfidentReview     int `json:"confidentReview"`
	LowConfidence       int `json:"lowConfidence"`
	Ambiguous           int `json:"ambiguous"`
	NoMatch             int `json:"noMatch"`
	PartialSeries       int `json:"partialSeries"`
	MediaItemsCreated   int `json:"mediaItemsCreated"`
	EpisodeLookupFailed int `json:"episodeLookupFailed"`
	Duration            int `json:"duration"`
}

// collectedFile is the walk phase's view of one file: paths + size +
// OSDB content hash. OsdbHash is nil when the file is too small or
// unreadable for hashing.
type collectedFile struct {
	RelPath  string
	AbsPath  string
	FileSize *int64
	OsdbHash *string
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
			"filesSeen":           stats.FilesSeen,
			"confident":           stats.Confident,
			"confidentReview":     stats.ConfidentReview,
			"lowConfidence":       stats.LowConfidence,
			"ambiguous":           stats.Ambiguous,
			"noMatch":             stats.NoMatch,
			"partialSeries":       stats.PartialSeries,
			"mediaItemsCreated":   stats.MediaItemsCreated,
			"episodeLookupFailed": stats.EpisodeLookupFailed,
			"duration":            stats.Duration,
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

// ---------------------------------------------------------------------------
// Pipeline: executeScan
// ---------------------------------------------------------------------------

// executeScan is the four-phase scan loop. Phase 1 walks the library
// rooted at library.RootPath and collects file metadata, deduping
// against the live file path set. Phase 2 runs the matcher against the
// collection — identification is entirely the matcher's concern from
// here on. Phase 3 reads each MatchOutcomeRecord and persists one file
// row per discovered file: confident / confident_review set identity;
// low_confidence / ambiguous / no_match leave it NULL; partial_series
// sets the series title with the episode left NULL. The ranked
// candidates live on the match_decision row (written by MatchBatch), not
// on the file. Each step publishes SSE progress events for the dashboard.
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
			_, dbErr := s.repo.GetFileByLibraryAndPath(ctx, repo.GetFileByLibraryAndPathParams{
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

		// Best-effort: a NULL hash (too-small file / read error) is tolerated
		// since the column is nullable.
		var osdbHash *string
		if h, herr := osdb.Hash(path); herr == nil {
			osdbHash = &h
		} else {
			s.logger.Debug().Str("path", path).Err(herr).Msg("osdb hash skipped")
		}

		collected = append(collected, collectedFile{
			RelPath:  relPath,
			AbsPath:  path,
			FileSize: fileSize,
			OsdbHash: osdbHash,
		})

		return nil
	})
	if err != nil {
		return scanStats{}, err
	}

	s.logger.Info().Int("collected", len(collected)).Int("seen", stats.FilesSeen).Msg("Phase 1 complete: Walk & Collect")

	if len(collected) == 0 {
		stats.Duration = int(time.Since(start).Seconds())
		return stats, nil
	}

	// Phase 2: Match
	//
	// Each collected file is materialized as a `file` row (identity NULL)
	// before matching, because match_decision.file_id is a real FK — the
	// row must exist before the matcher writes a decision against it. The
	// matcher then owns identification (path-embed Tier 1, name-parse
	// Tier 3, validation, confidence banding); scan hands it a slice of
	// FileRefs and gets back one MatchOutcomeRecord per file in the same
	// order.
	fileRefs, err := s.toFileRefs(ctx, library, collected)
	if err != nil {
		return scanStats{}, err
	}
	outcomes, err := s.matcher.MatchBatch(ctx, fileRefs)
	if err != nil {
		return scanStats{}, err
	}
	if len(outcomes) != len(collected) {
		// MatchBatch should preserve input order 1:1. A mismatch here
		// would be a programming error in the matcher contract.
		return scanStats{}, apperrors.Internalf("matcher returned %d outcomes for %d files", len(outcomes), len(collected)).
			Op("ScannerService.executeScan").
			NotRetryable()
	}

	s.logger.Info().Int("outcomes", len(outcomes)).Msg("Phase 2 complete: Match")

	// Phase 3+4: Enrich & Persist
	for i, f := range collected {
		if ctx.Err() != nil {
			return scanStats{}, ctx.Err()
		}

		rec := outcomes[i]

		switch rec.Outcome {
		case matcher.OutcomeConfident, matcher.OutcomeConfidentReview:
			created, epFailed, perr := s.persistConfident(ctx, library, f, rec)
			if perr != nil {
				s.logger.Warn().Err(perr).Str("path", f.RelPath).Msg("persist confident failed")
				continue
			}
			if created {
				stats.MediaItemsCreated++
			}
			if epFailed {
				stats.EpisodeLookupFailed++
			}
			if rec.Outcome == matcher.OutcomeConfident {
				stats.Confident++
			} else {
				stats.ConfidentReview++
			}
		case matcher.OutcomeLowConfidence, matcher.OutcomeAmbiguous, matcher.OutcomeNoMatch, matcher.OutcomePartialSeries:
			if perr := s.persistUnidentified(ctx, library, f, rec); perr != nil {
				s.logger.Warn().Err(perr).Str("path", f.RelPath).Msg("persist unidentified failed")
				continue
			}
			switch rec.Outcome {
			case matcher.OutcomeLowConfidence:
				stats.LowConfidence++
			case matcher.OutcomeAmbiguous:
				stats.Ambiguous++
			case matcher.OutcomeNoMatch:
				stats.NoMatch++
			case matcher.OutcomePartialSeries:
				stats.PartialSeries++
			}
		default:
			// The aggregator's banded outcomes are all handled above;
			// OutcomeDetached is a user-only action that never comes out
			// of MatchBatch. A new or unexpected band reaching here would
			// otherwise be silently dropped (no file row) while its
			// match_decision row already exists — surface it instead of
			// losing the file.
			s.logger.Warn().Str("path", f.RelPath).Str("outcome", string(rec.Outcome)).Msg("scan: unhandled match outcome; file left unpersisted")
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

// loadKnownPaths bulk-loads all live file paths for the given library
// into a set for O(1) dedup during the walk phase.
func (s *ScannerService) loadKnownPaths(ctx context.Context, libraryID uuid.UUID) (map[string]struct{}, error) {
	paths, err := s.repo.ListFilePathsForLibrary(ctx, libraryID)
	if err != nil {
		return nil, err
	}

	known := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		known[p] = struct{}{}
	}
	return known, nil
}

// toFileRefs translates a slice of walked files into FileRefs the
// matcher consumes. Each ref gets:
//
//   - A stable FileID. We look up the existing live file row by
//     (library_id, relative path) and reuse its id when found; otherwise
//     a fresh UUID is minted. This keeps match_decision.file_id stable
//     across re-scans so the supersede chain points at the same logical
//     file each time, and the id is threaded straight into the file row.
//
//   - A parser hint. parsing.Parse(rel-path, domain, AsPath()) runs
//     domain-typed (DomainSeries for series libraries, DomainMovie for
//     movie libraries). Parse is total — an unparseable path yields
//     zero-confidence fields, not an error — so we pass the result
//     through unconditionally and let NameParse's Available() check
//     short-circuit when the parse has no signal.
//
// The path on the FileRef is absolute, because PathEmbed scans the
// whole path string for TRaSH-style `{tmdb-X}` markers (which often
// live in a parent directory, not the leaf filename).
func (s *ScannerService) toFileRefs(ctx context.Context, library model.Library, collected []collectedFile) ([]matcher.FileRef, error) {
	domain := parsing.DomainSeries
	if library.Type == "movie" {
		domain = parsing.DomainMovie
	}
	refs := make([]matcher.FileRef, 0, len(collected))
	for _, f := range collected {
		id, err := s.ensureFileRow(ctx, library.ID, f)
		if err != nil {
			return nil, err
		}
		parsed := parsing.Parse(f.RelPath, domain, parsing.AsPath())
		refs = append(refs, matcher.FileRef{
			ID:          id,
			Path:        f.AbsPath,
			LibraryID:   library.ID,
			LibraryRoot: library.RootPath,
			Parsed:      &parsed,
		})
	}
	return refs, nil
}

// ensureFileRow returns the stable file id for a collected file, creating
// the row (identity NULL) when it doesn't yet exist. The id is the
// match_decision.file_id join key and must outlive every outcome — a file
// that was no_match on scan #1 and confident on scan #2 keeps the same id
// so the supersede chain points at the same logical file. The row exists
// before MatchBatch runs because the decision's FK to file requires it.
func (s *ScannerService) ensureFileRow(ctx context.Context, libraryID uuid.UUID, f collectedFile) (uuid.UUID, error) {
	existing, err := s.repo.GetFileByLibraryAndPath(ctx, repo.GetFileByLibraryAndPathParams{
		LibraryID: libraryID,
		Path:      f.RelPath,
	})
	if err == nil {
		return existing.ID, nil
	}
	if !apperrors.IsNotFound(err) {
		return uuid.Nil, err
	}

	id := uuid.New()
	if _, err := s.repo.CreateFile(ctx, repo.CreateFileParams{
		ID:        id,
		LibraryID: libraryID,
		Path:      f.RelPath,
	}); err != nil {
		return uuid.Nil, err
	}
	if _, err := s.repo.UpsertFileState(ctx, repo.UpsertFileStateParams{
		FileID:    id,
		Exists:    true,
		SizeBytes: f.FileSize,
		OsdbHash:  f.OsdbHash,
	}); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// persistConfident sets identity on the file (creating the row if scan
// hasn't yet) plus its file_state / file_import for a confident or
// confident_review outcome. The "flagged for review" state for
// confident_review is implicit in match_decision.outcome (already
// written by MatchBatch); we don't store a separate flag on the file.
//
// The actual persistence is delegated to commitMatch, the helper shared
// with the user-driven match-by-ID handler (Phase 4). Scan's wrapper
// owns the matcher-record → commitMatchInput translation (cross-source
// validation, episode unwrap) and the post-commit enrichment kick.
//
// Returns (mediaItemCreated, episodeLookupFailed, error).
//
// TODO(tracking): close any open wants whose identity matches this
// outcome's ChosenRef + ChosenEpisode. Phase 4 of the matcher plan is
// decision flows; want fulfillment hooks in when tracking lands.
// See specs/modules/matching/README.md § Drop-in fulfills wants.
func (s *ScannerService) persistConfident(ctx context.Context, library model.Library, f collectedFile, rec matcher.MatchOutcomeRecord) (bool, bool, error) {
	if rec.ChosenRef == nil {
		return false, false, apperrors.Internalf("confident outcome without chosen ref for %q", f.RelPath).
			Op("ScannerService.persistConfident").
			NotRetryable()
	}
	// The aggregator's cross-provider rewrite ensures any non-TMDB ref
	// landing here has been resolved to TMDB. Defend the invariant.
	if rec.ChosenRef.Source != metadata.SourceTMDB {
		return false, false, apperrors.Internalf("confident outcome carries non-tmdb ref %s:%s for %q", rec.ChosenRef.Source, rec.ChosenRef.ExternalID, f.RelPath).
			Op("ScannerService.persistConfident").
			NotRetryable()
	}
	tmdbID, err := strconv.ParseInt(rec.ChosenRef.ExternalID, 10, 64)
	if err != nil {
		return false, false, apperrors.Internalf("tmdb id %q not numeric: %v", rec.ChosenRef.ExternalID, err).
			Op("ScannerService.persistConfident").
			NotRetryable()
	}

	in := commitMatchInput{
		Library:  library,
		FileID:   rec.FileID,
		RelPath:  f.RelPath,
		AbsPath:  f.AbsPath,
		FileSize: f.FileSize,
		OsdbHash: f.OsdbHash,
		TmdbID:   tmdbID,
		Item:     rec.ChosenItem,
		Edition:  rec.ChosenEdition,
		Method:   "scan",
	}
	if rec.ChosenEpisode != nil {
		sn := int32(rec.ChosenEpisode.Season)
		en := int32(rec.ChosenEpisode.Episode)
		in.Season = &sn
		in.Episode = &en
	}

	result, err := commitMatch(ctx, commitMatchDeps{repo: s.repo, log: s.logger, tmdb: s.tmdb}, in)
	if err != nil {
		return false, false, err
	}
	if result.EpisodeFailed {
		return false, true, nil // skip file, continue
	}

	// Fire enrichment after commit so the goroutine reads a committed row.
	if result.ItemCreated && s.enrichment != nil {
		createdItem := result.MediaItem
		go func() {
			if err := s.enrichment.EnrichMediaItem(s.ctx, createdItem); err != nil {
				s.logger.Warn().Err(err).Str("title", createdItem.Title).Msg("scan-time enrichment failed, worker will retry")
			}
		}()
	}

	return result.ItemCreated, false, nil
}

// persistUnidentified handles the low_confidence / ambiguous / no_match
// bands (and partial_series when the series couldn't be pinned). The file
// row + state already exist from ensureFileRow with identity NULL — the
// inbox surfaces it as needing review — so there's nothing more to write:
// the ranked candidates the matcher surfaces live on the match_decision
// row (written by MatchBatch), not on the file. "Unmatched" is the query
// file.media_item_id IS NULL.
//
// partial_series with a chosen series ref sets the series title on the
// file (media_item_id set, episode_id NULL) so the inbox can offer an
// episode picker; that path reuses commitMatch with no episode.
func (s *ScannerService) persistUnidentified(ctx context.Context, library model.Library, f collectedFile, rec matcher.MatchOutcomeRecord) error {
	if rec.Outcome == matcher.OutcomePartialSeries && rec.ChosenRef != nil && rec.ChosenRef.Source == metadata.SourceTMDB {
		return s.persistPartialSeries(ctx, library, f, rec)
	}
	return nil
}

// persistPartialSeries sets the series title on the file (episode left
// NULL) for a partial_series outcome carrying a validated series ref.
func (s *ScannerService) persistPartialSeries(ctx context.Context, library model.Library, f collectedFile, rec matcher.MatchOutcomeRecord) error {
	tmdbID, err := strconv.ParseInt(rec.ChosenRef.ExternalID, 10, 64)
	if err != nil {
		return apperrors.Internalf("partial_series tmdb id %q not numeric: %v", rec.ChosenRef.ExternalID, err).
			Op("ScannerService.persistPartialSeries").
			NotRetryable()
	}
	_, err = commitMatch(ctx, commitMatchDeps{repo: s.repo, log: s.logger, tmdb: s.tmdb}, commitMatchInput{
		Library:  library,
		FileID:   rec.FileID,
		RelPath:  f.RelPath,
		AbsPath:  f.AbsPath,
		FileSize: f.FileSize,
		OsdbHash: f.OsdbHash,
		TmdbID:   tmdbID,
		Item:     rec.ChosenItem,
		Method:   "scan",
	})
	return err
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
