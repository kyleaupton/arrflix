package service

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	"github.com/kyleaupton/arrflix/internal/identity"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/sse"
)

// ErrScanAlreadyRunning is returned when a scan is already in progress for a library.
var ErrScanAlreadyRunning = errors.New("scan already running for this library")

// scanRepo is the subset of repo.Repository used by the scanner.
type scanRepo interface {
	GetLibrary(ctx context.Context, id pgtype.UUID) (dbgen.Library, error)
	GetMediaFileByLibraryAndPath(ctx context.Context, libraryID pgtype.UUID, path string) (dbgen.MediaFile, error)
	GetMediaItemByTmdbIDAndType(ctx context.Context, tmdbID int64, typ string) (dbgen.MediaItem, error)
	CreateMediaItem(ctx context.Context, typ, title string, year *int32, tmdbID *int64) (dbgen.MediaItem, error)
	UpsertSeason(ctx context.Context, mediaItemID pgtype.UUID, seasonNumber int32, airDate pgtype.Date) (dbgen.MediaSeason, error)
	UpsertEpisode(ctx context.Context, seasonID pgtype.UUID, episodeNumber int32, title *string, airDate pgtype.Date, tmdbID *int64, tvdbID *int64) (dbgen.MediaEpisode, error)
	CreateMediaFile(ctx context.Context, libraryID, mediaItemID pgtype.UUID, episodeID *pgtype.UUID, path string) (dbgen.MediaFile, error)
	UpsertMediaFileState(ctx context.Context, mediaFileID pgtype.UUID, fileExists bool, fileSize *int64) (dbgen.MediaFileState, error)
	CreateMediaFileImport(ctx context.Context, arg dbgen.CreateMediaFileImportParams) (dbgen.MediaFileImport, error)
}

// scanTmdb is the subset of TmdbService used by the scanner.
type scanTmdb interface {
	FindByID(ctx context.Context, id, source string) (tmdb.FindByID, error)
	GetMovieDetails(ctx context.Context, id int64) (tmdb.MovieDetails, error)
	GetSeriesDetails(ctx context.Context, id int64) (tmdb.TVDetails, error)
	GetEpisodeDetails(ctx context.Context, id int64, season int64, episode int64) (tmdb.TVEpisodeDetails, error)
}

type ScannerService struct {
	repo    scanRepo
	logger  *logger.Logger
	tmdb    scanTmdb
	broker  *sse.Broker
	ctx     context.Context // background context for goroutines; cancelled on shutdown
	running sync.Map        // libraryID string -> scanID string
}

func NewScannerService(r *repo.Repository, l *logger.Logger, tmdb *TmdbService, broker *sse.Broker) *ScannerService {
	return &ScannerService{repo: r, logger: l, tmdb: tmdb, broker: broker, ctx: context.Background()}
}

// SetContext sets the background context used by scan goroutines.
// Pass a cancellable context to allow graceful shutdown of in-flight scans.
func (s *ScannerService) SetContext(ctx context.Context) {
	s.ctx = ctx
}

type scanStats struct {
	FilesSeen         int `json:"filesSeen"`
	MediaItemsCreated int `json:"mediaItemsCreated"`
	Duration          int `json:"duration"`
}

// StartScan kicks off an async library scan. It returns the scan ID immediately.
// If a scan is already running for the given library, it returns ErrScanAlreadyRunning.
func (s *ScannerService) StartScan(ctx context.Context, libraryID pgtype.UUID) (string, error) {
	libKey := libraryID.String()

	// Concurrency guard: only one scan per library at a time.
	scanID := uuid.NewString()
	if _, loaded := s.running.LoadOrStore(libKey, scanID); loaded {
		return "", ErrScanAlreadyRunning
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
			"filesSeen":         stats.FilesSeen,
			"mediaItemsCreated": stats.MediaItemsCreated,
			"duration":          stats.Duration,
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
func (s *ScannerService) ensureSeasonAndEpisode(ctx context.Context, mediaItemID pgtype.UUID, ident identity.Identity, path string) (*pgtype.UUID, error) {
	if ident.Season == nil {
		return nil, nil
	}

	seasonRow, err := s.repo.UpsertSeason(ctx, mediaItemID, *ident.Season, pgtype.Date{})
	if err != nil {
		s.logger.Error().Err(err).Str("path", path).Msg("Error upserting season")
		return nil, err
	}

	if ident.Episode == nil {
		return nil, nil
	}

	episode, err := s.tmdb.GetEpisodeDetails(ctx, *ident.TmdbID, int64(*ident.Season), int64(*ident.Episode))
	if err != nil {
		s.logger.Error().Err(err).Str("path", path).Msg("Error getting episode details from TMDB")
		return nil, err
	}

	episodeRow, err := s.repo.UpsertEpisode(ctx, seasonRow.ID, *ident.Episode, &episode.Name, pgtype.Date{}, nil, nil)
	if err != nil {
		s.logger.Error().Err(err).Str("path", path).Msg("Error upserting episode")
		return nil, err
	}

	return &episodeRow.ID, nil
}

func (s *ScannerService) executeScan(ctx context.Context, library dbgen.Library, scanID string) (scanStats, error) {
	stats := scanStats{}
	start := time.Now()
	libKey := library.ID.String()

	s.logger.Info().Str("library_name", library.Name).Str("library_path", library.RootPath).Msg("Starting Scan")

	err := filepath.WalkDir(library.RootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err // propagate permission/IO errors
		}

		// Stop walking if the context has been cancelled.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if d.IsDir() || !isMediaFile(path) {
			s.logger.Debug().Str("path", path).Msg("Skipping Directory or Non-Media File")
			return nil
		}

		s.logger.Debug().Str("path", path).Msg("Processing Media File")

		stats.FilesSeen++

		// Publish progress every 50 files
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

		// see if path exists in media_file
		// if it does, skip
		_, err = s.repo.GetMediaFileByLibraryAndPath(ctx, library.ID, relPath)
		if err != nil && err != pgx.ErrNoRows {
			return err
		}
		if err == nil {
			s.logger.Debug().Str("path", path).Msg("Media file already exists")
			return nil
		}

		// attempt to get identification
		ident, err := identity.Resolve(library, path)
		if err != nil {
			s.logger.Error().Str("path", path).Err(err).Msg("Error resolving identity")
			return nil
		}

		if ident.TmdbID == nil {
			// If we got an identity, but no tmdb id, we'll need to convert to one.
			// This is a best effort to get a tmdb id. If we don't get one, we'll skip the file.
			var id string
			var provider string

			if ident.TvdbID != nil {
				id = strconv.FormatInt(*ident.TvdbID, 10)
				provider = "tvdb_id"
			} else if ident.ImdbID != nil {
				id = *ident.ImdbID
				provider = "imdb_id"
			}

			s.logger.Debug().Str("path", path).Str("provider", provider).Str("id", id).Msg("Identity has no tmdb id, converting to tmdb id")

			// This is like a general "search for this external id" type of thing.
			// It will return a list of results categorized by the type of media it is.
			// For now, we'll just grab the first item from the right category and use that.
			// TODO: We should probably do something more intelligent here.
			res, err := s.tmdb.FindByID(ctx, id, provider)
			if err != nil {
				s.logger.Error().Str("path", path).Err(err).Msg("Error getting find by id")
				return nil
			}

			var tmdbId *int64
			switch library.Type {
			case "movie":
				if len(res.MovieResults) == 0 {
					s.logger.Error().Str("path", path).Msg("No movie results found")
					return nil
				}

				tmdbId = &res.MovieResults[0].ID
			case "series":
				if len(res.TvResults) == 0 {
					s.logger.Error().Str("path", path).Msg("No series results found")
					return nil
				}

				tmdbId = &res.TvResults[0].ID
			}

			ident.TmdbID = tmdbId
		}

		if ident.TmdbID == nil {
			// We can't do anything without a tmdb id, so we'll skip the file.
			// TODO: keep track of files that we couldn't do anything with so we can alert the user.
			s.logger.Error().Str("path", path).Msg("No tmdb id found")
			return nil
		}

		// See if the tmdb id exists within media_item, scoped to the library type
		// to avoid collisions between movies and series sharing the same TMDB ID.
		mediaItem, err := s.repo.GetMediaItemByTmdbIDAndType(ctx, *ident.TmdbID, library.Type)
		if err != nil && err != pgx.ErrNoRows {
			s.logger.Error().Str("path", path).Err(err).Msg("Error getting media item by tmdb id")
			return err
		}

		var mediaItemId pgtype.UUID

		if err == pgx.ErrNoRows {
			// If we get here then mediaItem doesn't exists, so we need to make it.
			s.logger.Debug().Str("path", path).Msg("Media item not found, grabbing things")

			var title string
			var year int32

			switch library.Type {
			case "movie":
				movie, err := s.tmdb.GetMovieDetails(ctx, *ident.TmdbID)
				if err != nil {
					s.logger.Error().Str("path", path).Err(err).Msg("Error getting movie details")
					return nil
				}

				title = movie.Title

				if movie.ReleaseDate == "" {
					s.logger.Error().Str("path", path).Msg("Movie has empty release date")
					return nil
				}

				yearStr := strings.Split(movie.ReleaseDate, "-")[0]
				year64, err := strconv.ParseInt(yearStr, 10, 32)
				if err != nil {
					s.logger.Error().Str("path", path).Err(err).Msg("Error parsing movie release year")
					return nil
				}

				year = int32(year64)
			case "series":
				tv, err := s.tmdb.GetSeriesDetails(ctx, *ident.TmdbID)
				if err != nil {
					s.logger.Error().Str("path", path).Err(err).Msg("Error getting series details")
					return nil
				}

				title = tv.Name

				if tv.FirstAirDate == "" {
					s.logger.Error().Str("path", path).Msg("Series has empty first air date")
					return nil
				}

				yearStr := strings.Split(tv.FirstAirDate, "-")[0]
				year64, err := strconv.ParseInt(yearStr, 10, 32)
				if err != nil {
					s.logger.Error().Str("path", path).Err(err).Msg("Error parsing series first air date year")
					return nil
				}

				year = int32(year64)
			}

			s.logger.Debug().Str("title", title).Int32("year", year).Int64("tmdb_id", *ident.TmdbID).Msg("Creating media_item")

			// create media_item if it doesn't exist
			createdMediaItem, err := s.repo.CreateMediaItem(
				ctx,
				library.Type,
				title,
				&year,
				ident.TmdbID,
			)
			if err != nil {
				s.logger.Error().Str("path", path).Err(err).Msg("Error creating media_item")
				return nil
			}

			s.logger.Debug().Str("path", path).Str("media_item_id", createdMediaItem.ID.String()).Msg("Media item created")
			mediaItemId = createdMediaItem.ID
			stats.MediaItemsCreated++
		} else {
			mediaItemId = mediaItem.ID
		}

		// Ensure season/episode records for series files.
		episodeId, err := s.ensureSeasonAndEpisode(ctx, mediaItemId, ident, path)
		if err != nil {
			return nil // skip file, continue walking
		}

		// create media_file (removed seasonId - derived from episode)
		mf, err := s.repo.CreateMediaFile(ctx, library.ID, mediaItemId, episodeId, relPath)
		if err != nil {
			s.logger.Error().Err(err).Str("path", relPath).Msg("Failed to create media file")
			return nil
		}

		// Create file state for the new file
		info, _ := d.Info()
		var fileSize *int64
		if info != nil {
			size := info.Size()
			fileSize = &size
		}
		if _, err := s.repo.UpsertMediaFileState(ctx, mf.ID, true, fileSize); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to create media file state")
		}

		// Record import history (method: scan)
		if _, err := s.repo.CreateMediaFileImport(ctx, dbgen.CreateMediaFileImportParams{
			MediaFileID: mf.ID,
			Method:      "scan",
			DestPath:    path,
			Success:     true,
		}); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to record import history")
		}

		return nil
	})

	if err != nil {
		s.logger.Error().Err(err).Msg("Error walking directory")
		return scanStats{}, err
	}

	s.logger.Info().Str("library_id", libKey).Msg("Scan Complete")
	stats.Duration = int(time.Since(start).Seconds())

	return stats, nil
}

func isMediaFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mkv", ".mp4", ".avi", ".mov", ".wmv", ".flv", ".m4v", ".webm":
		return true
	}
	return false
}
