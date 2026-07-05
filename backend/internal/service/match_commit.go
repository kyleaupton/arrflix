package service

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/jobs/jobutil"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/metadata"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// commitMatchInput is the unified parameter shape for committing a
// successful match — applies to both scan's auto-match path and the
// manual match-by-ID handler. Identity is always represented as a TMDB
// id by the time we reach here; cross-provider rewrite happens upstream
// (in the matcher's aggregator for the scan path, in the manual handler
// before calling commit for the user-driven path).
//
// The file row is the stable spine: FileID names it, and the row is
// created at discovery. commitMatch writes the media_item / season /
// episode rows, then UPDATEs identity onto the file in place — the row,
// its file_state snapshot, and its file_import history all survive every
// identity transition.
type commitMatchInput struct {
	Library model.Library
	// FileID is the stable file id — the row created at discovery and the
	// match_decision.file_id join key. commitMatch upserts the file row at
	// this id and points its identity at the matched media_item.
	FileID uuid.UUID
	// RelPath is the file path relative to the library root. Becomes
	// file.path.
	RelPath string
	// AbsPath is the absolute on-disk path. Used by file_import.dest_path
	// so the import history matches the scan-time behaviour.
	AbsPath string
	// FileSize is the on-disk file size in bytes. nil leaves the column
	// unset on file_state.
	FileSize *int64
	// OsdbHash is the OSDB content hash. nil leaves the column unset on
	// file_state (too-small file / read error at walk time).
	OsdbHash *string
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
	// Edition is the movie cut, free-text in v1. Stored on file.edition.
	Edition *string
	// Method is the file_import.method value — "scan" for the scanner's
	// confident-band path, "manual_match" for the user-driven
	// match-by-ID handler.
	Method string
}

// commitMatchResult names what commitMatch produced — the new (or
// existing) media_item and the file row, plus whether scan-time
// enrichment should fire on the item, plus whether TMDB episode lookup
// failed (which lets the caller decide between "skip the file" and
// "surface the error").
type commitMatchResult struct {
	MediaItem     model.MediaItem
	File          model.File
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
// matches. It writes the media_item / media_season / media_episode rows,
// upserts the file row at the stable FileID with identity set, and writes
// file_state / file_import in one transaction. Same shape regardless of
// whether the source is the scanner's auto-match or the user's
// match-by-ID handler.
//
// The file row is keyed on the stable FileID: if it already exists (the
// common case — scan discovers it, then matches it), the identity is set
// in place via SetFileIdentity and the file_state / file_import rows are
// rewritten. If it doesn't (a closed-world create), CreateFile mints it
// with identity already set.
//
// Caller responsibilities (NOT done here):
//   - Writing the match_decision row (the matcher's repo does that).
//   - Firing scan-time enrichment on a fresh item (off-thread; both
//     callers do their own fire-and-forget).
//
// Returns EpisodeFailed=true when a TMDB episode-details lookup failed
// — scan treats that as a skip (file stays unidentified). Manual match
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
	if in.FileID == uuid.Nil {
		return commitMatchResult{}, apperrors.Internalf("commitMatch: nil file id for %q", in.RelPath).
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
	// episodeTmdbID is the stable TMDB episode id — the key media_episode is
	// upserted on, so this match converges on the same row series-structure
	// sync creates (rather than minting a NULL-tmdb_id duplicate). Set only
	// when this is an episode match.
	var episodeTmdbID int64
	if in.Season != nil && in.Episode != nil {
		ep, eerr := d.tmdb.GetEpisodeDetails(ctx, in.TmdbID, int64(*in.Season), int64(*in.Episode))
		if eerr != nil {
			if d.log != nil {
				d.log.Error().Err(eerr).Str("path", in.AbsPath).Msg("commitMatch: TMDB episode details fetch failed")
			}
			return commitMatchResult{EpisodeFailed: true}, nil
		}
		episodeTitle = ep.Name
		episodeTmdbID = ep.ID
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
					TmdbID:        &episodeTmdbID,
				})
				if eerr != nil {
					return eerr
				}
				eid := epRow.ID
				episodeIDPtr = &eid
			}
		}

		// Point the file row at the matched identity. The row already
		// exists for scan-discovered files; SetFileIdentity is a no-op-safe
		// in-place UPDATE that preserves the file_state snapshot and import
		// history. If it doesn't exist yet (a closed-world create path),
		// CreateFile mints it at the stable id with identity already set.
		var (
			f    model.File
			ferr error
		)
		// closedWorldCreate is true only when this commit MINTS the file row —
		// the genuine manual/closed-world create. The normal manual-match flow
		// takes the SetFileIdentity branch (the row exists from scan) and stamps
		// nothing, so those files keep origin='scan': provenance tracks how the
		// bytes entered, not when identity was assigned.
		closedWorldCreate := false
		if _, gerr := r.GetFile(ctx, in.FileID); gerr == nil {
			f, ferr = r.SetFileIdentity(ctx, repo.SetFileIdentityParams{
				ID:          in.FileID,
				MediaItemID: mediaItem.ID,
				EpisodeID:   episodeIDPtr,
				Edition:     in.Edition,
			})
		} else if apperrors.IsNotFound(gerr) {
			mediaItemID := mediaItem.ID
			f, ferr = r.CreateFile(ctx, repo.CreateFileParams{
				ID:          in.FileID,
				LibraryID:   in.Library.ID,
				Path:        in.RelPath,
				MediaItemID: &mediaItemID,
				EpisodeID:   episodeIDPtr,
				Edition:     in.Edition,
			})
			closedWorldCreate = true
		} else {
			return gerr
		}
		if ferr != nil {
			return ferr
		}

		if _, serr := r.UpsertFileState(ctx, repo.UpsertFileStateParams{
			FileID:    f.ID,
			Exists:    true,
			SizeBytes: in.FileSize,
			OsdbHash:  in.OsdbHash,
		}); serr != nil {
			return serr
		}

		if closedWorldCreate {
			domain := parsing.DomainSeries
			if in.Library.Type == "movie" {
				domain = parsing.DomainMovie
			}
			parsed := parsing.Parse(in.RelPath, domain, parsing.AsPath())
			if oerr := r.CreateFileOriginIfAbsent(ctx, jobutil.FileOriginParams(
				f.ID, "manual", in.RelPath, parsed, domain, nil, nil, nil,
			)); oerr != nil {
				return oerr
			}
		}

		method := in.Method
		if method == "" {
			method = "scan"
		}
		if _, ierr := r.CreateFileImport(ctx, repo.CreateFileImportParams{
			FileID:   f.ID,
			Method:   method,
			DestPath: in.AbsPath,
			Success:  true,
		}); ierr != nil {
			return ierr
		}

		result.MediaItem = mediaItem
		result.File = f
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
// lookup.
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
