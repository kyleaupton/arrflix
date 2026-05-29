package service

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/metadata"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// commitMatchInput is the unified parameter shape for committing a
// successful match — applies to both scan's auto-match path and the
// manual match-by-ID handler. Identity is always represented as a TMDB
// id by the time we reach here; cross-provider rewrite happens upstream
// (in the matcher's aggregator for the scan path, in the manual handler
// before calling commit for the user-driven path).
type commitMatchInput struct {
	Library model.Library
	// RelPath is the file path relative to the library root. Becomes
	// media_file.path.
	RelPath string
	// AbsPath is the absolute on-disk path. Used by
	// media_file_import.dest_path so the import history matches the
	// scan-time behaviour.
	AbsPath string
	// FileSize is the on-disk file size in bytes. nil leaves the column
	// unset on media_file_state.
	FileSize *int64
	// TmdbID is the validated TMDB identifier the file is being matched
	// to.
	TmdbID int64
	// Item is the pre-fetched metadata.Item the matcher's validation
	// step produced. Used to populate media_item.title/year without a
	// second TMDB call. May be nil — commitMatch falls back to
	// GetMovieDetails / GetSeriesDetails in that case.
	Item *metadata.Item
	// Season / Episode are populated for series matches.
	Season  *int32
	Episode *int32
	// Edition is the movie cut, free-text in v1. Not persisted to
	// media_file directly — lives on match_decision; the column-level
	// edition modeling is a v2 schema change.
	Edition *string
	// Method is the media_file_import.method value — "scan" for the
	// scanner's confident-band path, "manual_match" for the user-driven
	// match-by-ID handler.
	Method string
	// PreservedFileID, when set, becomes the new media_file row's id.
	// Used by the manual match flow to keep the match_decision.file_id
	// join key stable across an unmatched_file → media_file transition.
	// Scan-time calls leave this nil — fresh rows get auto-assigned ids.
	PreservedFileID *uuid.UUID
	// Recommit re-points an existing media_file (identified by
	// PreservedFileID) at the new identity in place, rather than
	// inserting a fresh row. Used by the manual re-match flow (media →
	// media): the UPDATE preserves the row's media_file_state snapshot
	// and media_file_import history, which a delete-and-recreate would
	// destroy. Requires PreservedFileID to be set; the existing
	// media_file_state / media_file_import rows are left untouched.
	Recommit bool
}

// commitMatchResult names what commitMatch produced — the new (or
// existing) media_item / media_file pair, plus whether scan-time
// enrichment should fire on the item, plus whether TMDB episode lookup
// failed (which lets the caller decide between "skip the file" and
// "surface the error").
type commitMatchResult struct {
	MediaItem     model.MediaItem
	MediaFile     model.MediaFile
	ItemCreated   bool
	EpisodeFailed bool
}

// commitMatchDeps bundles what commitMatch needs. Free function rather
// than a method so both ScannerService and MatchDecisionsService can
// call it without having to pick one as "the owner" — both consume the
// same shape.
type commitMatchDeps struct {
	repo *repo.Repository
	log  *logger.Logger
	tmdb *TmdbService
}

// commitMatch is the shared persistence helper for confident-band
// matches. It writes the media_item / media_season / media_episode /
// media_file / media_file_state / media_file_import rows in one
// transaction, taking the same shape regardless of whether the source
// is the scanner's auto-match or the user's match-by-ID handler.
//
// Caller responsibilities (NOT done here):
//   - Writing the match_decision row (the matcher's repo does that).
//   - Deleting any unmatched_file row the file used to live in.
//   - Firing scan-time enrichment on a fresh item (off-thread; both
//     callers do their own fire-and-forget).
//
// Returns EpisodeFailed=true when a TMDB episode-details lookup failed
// — scan treats that as a skip (file stays unmatched). Manual match
// callers can choose to propagate or fall through.
//
// TODO(metadata-seam): GetEpisodeDetails isn't on MetadataProvider yet —
// when it grows LookupEpisode this call moves behind the seam. The
// movie/series fetches already use Item (populated by the aggregator's
// Tier-1 validation) and skip the second TMDB call.
func commitMatch(ctx context.Context, d commitMatchDeps, in commitMatchInput) (commitMatchResult, error) {
	if in.TmdbID <= 0 {
		return commitMatchResult{}, apperrors.Internalf("commitMatch: non-positive tmdb id %d for %q", in.TmdbID, in.RelPath).
			Op("commitMatch").
			NotRetryable()
	}
	if in.Recommit && in.PreservedFileID == nil {
		return commitMatchResult{}, apperrors.Internalf("commitMatch: recommit requires a preserved file id for %q", in.RelPath).
			Op("commitMatch").
			NotRetryable()
	}

	existing, err := d.repo.GetMediaItemByTmdbIDAndType(ctx, in.TmdbID, in.Library.Type)
	needsCreateItem := apperrors.IsNotFound(err)
	if err != nil && !needsCreateItem {
		return commitMatchResult{}, err
	}

	var createItemParams repo.CreateMediaItemParams
	if needsCreateItem {
		title, year, perr := commitMatchTitleAndYear(ctx, d, in.Library, in.TmdbID, in.Item)
		if perr != nil {
			return commitMatchResult{}, perr
		}
		tmdbID := in.TmdbID
		createItemParams = repo.CreateMediaItemParams{
			Type:   in.Library.Type,
			Title:  title,
			Year:   &year,
			TmdbID: &tmdbID,
		}
	}

	// Episode title — populated only for series matches with a season +
	// episode. The lookup is best-effort: a TMDB outage here surfaces
	// EpisodeFailed=true so the caller decides whether to fail or skip.
	var episodeTitle string
	if in.Season != nil && in.Episode != nil {
		ep, eerr := d.tmdb.GetEpisodeDetails(ctx, in.TmdbID, int64(*in.Season), int64(*in.Episode))
		if eerr != nil {
			if d.log != nil {
				d.log.Error().Err(eerr).Str("path", in.AbsPath).Msg("commitMatch: TMDB episode details fetch failed")
			}
			return commitMatchResult{EpisodeFailed: true}, nil
		}
		episodeTitle = ep.Name
	}

	var result commitMatchResult
	err = d.repo.InTx(ctx, func(r *repo.Repository) error {
		var mediaItem model.MediaItem
		if needsCreateItem {
			created, cerr := r.CreateMediaItem(ctx, createItemParams)
			if cerr != nil {
				return cerr
			}
			mediaItem = created
			result.ItemCreated = true
		} else {
			mediaItem = existing
		}

		var episodeIDPtr *uuid.UUID
		if in.Season != nil {
			seasonRow, serr := r.UpsertSeason(ctx, repo.UpsertSeasonParams{
				MediaItemID:  mediaItem.ID,
				SeasonNumber: *in.Season,
			})
			if serr != nil {
				return serr
			}
			if in.Episode != nil {
				epRow, eerr := r.UpsertEpisode(ctx, repo.UpsertEpisodeParams{
					SeasonID:      seasonRow.ID,
					EpisodeNumber: *in.Episode,
					Title:         &episodeTitle,
				})
				if eerr != nil {
					return eerr
				}
				eid := epRow.ID
				episodeIDPtr = &eid
			}
		}

		// Re-match: re-point the existing media_file in place. The
		// media_file_state snapshot and media_file_import history stay
		// attached to the same row — no new state/import write, because
		// nothing moved on disk; only the identity changed. The
		// match_decision row is the audit record for that change.
		if in.Recommit {
			mf, ferr := r.UpdateMediaFileIdentity(ctx, repo.UpdateMediaFileIdentityParams{
				ID:          *in.PreservedFileID,
				MediaItemID: mediaItem.ID,
				EpisodeID:   episodeIDPtr,
			})
			if ferr != nil {
				return ferr
			}
			result.MediaItem = mediaItem
			result.MediaFile = mf
			return nil
		}

		var (
			mf   model.MediaFile
			ferr error
		)
		if in.PreservedFileID != nil {
			mf, ferr = r.CreateMediaFileWithID(ctx, repo.CreateMediaFileWithIDParams{
				ID:          *in.PreservedFileID,
				LibraryID:   in.Library.ID,
				MediaItemID: mediaItem.ID,
				EpisodeID:   episodeIDPtr,
				Path:        in.RelPath,
			})
		} else {
			mf, ferr = r.CreateMediaFile(ctx, repo.CreateMediaFileParams{
				LibraryID:   in.Library.ID,
				MediaItemID: mediaItem.ID,
				EpisodeID:   episodeIDPtr,
				Path:        in.RelPath,
			})
		}
		if ferr != nil {
			return ferr
		}

		if _, serr := r.UpsertMediaFileState(ctx, repo.UpsertMediaFileStateParams{
			MediaFileID: mf.ID,
			FileExists:  true,
			FileSize:    in.FileSize,
		}); serr != nil {
			return serr
		}

		method := in.Method
		if method == "" {
			method = "scan"
		}
		if _, ierr := r.CreateMediaFileImport(ctx, repo.CreateMediaFileImportParams{
			MediaFileID: mf.ID,
			Method:      method,
			DestPath:    in.AbsPath,
			Success:     true,
		}); ierr != nil {
			return ierr
		}

		result.MediaItem = mediaItem
		result.MediaFile = mf
		return nil
	})
	if err != nil {
		return commitMatchResult{}, err
	}

	return result, nil
}

// commitMatchTitleAndYear sources the media_item display fields,
// preferring the pre-fetched Item (from the matcher's validation step
// or the manual handler's LookupByID) and falling back to a fresh TMDB
// lookup. Mirrors what ScannerService.titleAndYear used to do; the
// helper moved here so commitMatch has one home.
func commitMatchTitleAndYear(ctx context.Context, d commitMatchDeps, library model.Library, tmdbID int64, item *metadata.Item) (string, int32, error) {
	if item != nil && item.Title != "" {
		return item.Title, int32(item.Year), nil
	}

	switch library.Type {
	case "movie":
		movie, err := d.tmdb.GetMovieDetails(ctx, tmdbID)
		if err != nil {
			return "", 0, err
		}
		if movie.ReleaseDate == "" {
			return "", 0, apperrors.BadGatewayf("tmdb movie %d has empty release date", tmdbID).
				Op("commitMatchTitleAndYear")
		}
		year64, err := strconv.ParseInt(strings.Split(movie.ReleaseDate, "-")[0], 10, 32)
		if err != nil {
			return "", 0, apperrors.BadGatewayf("tmdb movie %d release date %q unparseable: %v", tmdbID, movie.ReleaseDate, err).
				Op("commitMatchTitleAndYear")
		}
		return movie.Title, int32(year64), nil

	case "series":
		tv, err := d.tmdb.GetSeriesDetails(ctx, tmdbID)
		if err != nil {
			return "", 0, err
		}
		if tv.FirstAirDate == "" {
			return "", 0, apperrors.BadGatewayf("tmdb series %d has empty first air date", tmdbID).
				Op("commitMatchTitleAndYear")
		}
		year64, err := strconv.ParseInt(strings.Split(tv.FirstAirDate, "-")[0], 10, 32)
		if err != nil {
			return "", 0, apperrors.BadGatewayf("tmdb series %d first air date %q unparseable: %v", tmdbID, tv.FirstAirDate, err).
				Op("commitMatchTitleAndYear")
		}
		return tv.Name, int32(year64), nil
	}

	return "", 0, apperrors.Internalf("unknown library type %q", library.Type).
		Op("commitMatchTitleAndYear").
		NotRetryable()
}

// joinLibraryPath builds the absolute on-disk path from a library root +
// relative path. Shared between the scan loop and the manual match
// handler so they speak the same language to commitMatch.
func joinLibraryPath(library model.Library, relPath string) string {
	return filepath.Join(library.RootPath, relPath)
}
