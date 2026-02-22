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

type ScannerService struct {
	repo    *repo.Repository
	logger  *logger.Logger
	tmdb    *TmdbService
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

func (s *ScannerService) executeScan(ctx context.Context, library dbgen.Library, scanID string) (scanStats, error) {
	stats := scanStats{}
	start := time.Now()
	libKey := library.ID.String()

	s.logger.Info().Str("library_name", library.Name).Str("library_path", library.RootPath).Msg("Starting Scan")

	err := filepath.WalkDir(library.RootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err // propagate permission/IO errors
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
		identity, err := identity.Resolve(library, path)
		if err != nil {
			s.logger.Error().Str("path", path).Err(err).Msg("Error resolving identity")
			return nil
		}

		if identity.TmdbID == nil {
			// If we got an identity, but no tmdb id, we'll need to convert to one.
			// This is a best effort to get a tmdb id. If we don't get one, we'll skip the file.
			var id string
			var provider string

			if identity.TvdbID != nil {
				id = strconv.FormatInt(*identity.TvdbID, 10)
				provider = "tvdb_id"
			} else if identity.ImdbID != nil {
				id = *identity.ImdbID
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

			identity.TmdbID = tmdbId
		}

		if identity.TmdbID == nil {
			// We can't do anything without a tmdb id, so we'll skip the file.
			// TODO: keep track of files that we couldn't do anything with so we can alert the user.
			s.logger.Error().Str("path", path).Msg("No tmdb id found")
			return nil
		}

		// See if the tmdb id exists within media_item
		mediaItem, err := s.repo.GetMediaItemByTmdbID(ctx, *identity.TmdbID)
		if err != nil && err != pgx.ErrNoRows {
			s.logger.Error().Str("path", path).Err(err).Msg("Error getting media item by tmdb id")
			return err
		}

		var mediaItemId pgtype.UUID
		var episodeId *pgtype.UUID

		if err == pgx.ErrNoRows {
			// If we get here then mediaItem doesn't exists, so we need to make it.
			s.logger.Debug().Str("path", path).Msg("Media item not found, grabbing things")

			var title string
			var year int32

			switch library.Type {
			case "movie":
				movie, err := s.tmdb.GetMovieDetails(ctx, *identity.TmdbID)
				if err != nil {
					s.logger.Error().Str("path", path).Err(err).Msg("Error getting movie details")
					return nil
				}

				title = movie.Title
				// yyyy-mm-dd
				yearStr := strings.Split(movie.ReleaseDate, "-")[0]
				// cast to int
				year64, err := strconv.ParseInt(yearStr, 10, 32)
				if err != nil {
					s.logger.Error().Str("path", path).Err(err).Msg("Error getting movie details")
					return nil
				}

				year = int32(year64)
			case "series":
				tv, err := s.tmdb.GetSeriesDetails(ctx, *identity.TmdbID)
				if err != nil {
					s.logger.Error().Str("path", path).Err(err).Msg("Error getting series details")
					return nil
				}

				title = tv.Name
				// yyyy-mm-dd
				yearStr := strings.Split(tv.FirstAirDate, "-")[0]
				// cast to int
				year64, err := strconv.ParseInt(yearStr, 10, 32)
				if err != nil {
					s.logger.Error().Str("path", path).Err(err).Msg("Error getting episode details")
					return nil
				}

				year = int32(year64)
			}

			s.logger.Debug().Str("title", title).Int32("year", year).Int64("tmdb_id", *identity.TmdbID).Msg("Creating media_item")

			// create media_item if it doesn't exist
			createdMediaItem, err := s.repo.CreateMediaItem(
				ctx,
				library.Type,
				title,
				&year,
				identity.TmdbID,
			)
			if err != nil {
				s.logger.Error().Str("path", path).Err(err).Msg("Error creating media_item")
				return nil
			}

			s.logger.Debug().Str("path", path).Str("media_item_id", createdMediaItem.ID.String()).Msg("Media item created")
			mediaItemId = createdMediaItem.ID
			stats.MediaItemsCreated++

			if identity.Season != nil {
				// create media_season if it doesn't exist
				seasonRow, err := s.repo.UpsertSeason(ctx, mediaItemId, *identity.Season, pgtype.Date{})
				if err != nil {
					return nil
				}

				if identity.Episode != nil {
					// grab the episode details from tmdb
					episode, err := s.tmdb.GetEpisodeDetails(ctx, *identity.TmdbID, int64(*identity.Season), int64(*identity.Episode))
					if err != nil {
						return err
					}

					// create media_episode if it doesn't exist
					episodeRow, err := s.repo.UpsertEpisode(ctx, seasonRow.ID, *identity.Episode, &episode.Name, pgtype.Date{}, nil, nil)
					if err != nil {
						return nil
					}
					episodeId = &episodeRow.ID
				}
			}

		} else {
			mediaItemId = mediaItem.ID
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
	extensions := []string{".mkv", ".mp4", ".avi", ".mov", ".wmv", ".flv", ".m4v", ".webm"}
	for _, ext := range extensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}
