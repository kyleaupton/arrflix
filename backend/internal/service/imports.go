package service

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	"github.com/kyleaupton/arrflix/internal/downloader"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/importer"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/mediainfo"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/release"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/template"
)

type ImportService struct {
	repo      *repo.Repository
	log       *logger.Logger
	mediaInfo *mediainfo.Analyzer
}

func NewImportService(r *repo.Repository, l *logger.Logger) *ImportService {
	return &ImportService{
		repo:      r,
		log:       l,
		mediaInfo: mediainfo.NewAnalyzer(*l),
	}
}

type ImportResult struct {
	SourcePath  string
	DestPath    string
	DestRelPath string
	Method      string // hardlink|copy
	MediaItem   dbgen.MediaItem
	MediaFile   dbgen.MediaFile
}

func (s *ImportService) ImportMovieFile(ctx context.Context, job dbgen.DownloadJob, sourcePath string) (ImportResult, error) {
	if !job.MediaItemID.Valid {
		return ImportResult{}, apperrors.Internalf("download job %s missing media_item_id", job.ID).
			Op("ImportService.ImportMovieFile").
			NotRetryable()
	}

	mediaItem, err := s.repo.GetMediaItem(ctx, job.MediaItemID)
	if err != nil {
		return ImportResult{}, err
	}

	lib, err := s.repo.GetLibrary(ctx, job.LibraryID)
	if err != nil {
		return ImportResult{}, err
	}
	nt, err := s.repo.GetNameTemplate(ctx, job.NameTemplateID)
	if err != nil {
		return ImportResult{}, err
	}

	// Build evaluation context for template rendering
	evalCtx := s.buildMovieEvalContext(mediaItem, job.CandidateTitle)

	// Extract mediainfo from source file BEFORE rendering template
	if mi := s.mediaInfo.Analyze(sourcePath); mi != nil {
		evalCtx = evalCtx.WithMediaInfo(mi)
	} else {
		s.log.Warn().Str("path", sourcePath).Msg("Failed to extract mediainfo, continuing without it")
	}

	templateData := evalCtx.ToTemplateData()

	var rel string
	ext := filepath.Ext(sourcePath)

	{
		// Fallback to calculating from template (existing logic)
		if nt.Type == "series" {
			showPart, err := template.Render(coalesce(nt.SeriesShowTemplate, ""), templateData)
			if err != nil {
				return ImportResult{}, apperrors.Internalf("render show template: %v", err).
					Op("ImportService.ImportMovieFile").
					NotRetryable()
			}
			seasonPart, err := template.Render(coalesce(nt.SeriesSeasonTemplate, ""), templateData)
			if err != nil {
				return ImportResult{}, apperrors.Internalf("render season template: %v", err).
					Op("ImportService.ImportMovieFile").
					NotRetryable()
			}
			filePart, err := template.Render(nt.Template, templateData)
			if err != nil {
				return ImportResult{}, apperrors.Internalf("render file template: %v", err).
					Op("ImportService.ImportMovieFile").
					NotRetryable()
			}
			rel = filepath.Join(showPart, seasonPart, filePart)
		} else {
			// Movie type - render directory and file templates
			var dirPart string
			if nt.MovieDirTemplate != nil && *nt.MovieDirTemplate != "" {
				dirPart, err = template.Render(*nt.MovieDirTemplate, templateData)
				if err != nil {
					return ImportResult{}, apperrors.Internalf("render movie dir template: %v", err).
						Op("ImportService.ImportMovieFile").
						NotRetryable()
				}
			}
			filePart, err := template.Render(nt.Template, templateData)
			if err != nil {
				return ImportResult{}, apperrors.Internalf("render file template: %v", err).
					Op("ImportService.ImportMovieFile").
					NotRetryable()
			}
			if dirPart != "" {
				rel = filepath.Join(dirPart, filePart)
			} else {
				rel = filePart
			}
		}
		rel = importer.EnsureExt(rel, ext)
	}

	dest := filepath.Join(lib.RootPath, rel)

	destRel, err := filepath.Rel(lib.RootPath, dest)
	if err != nil || strings.HasPrefix(destRel, "..") {
		return ImportResult{}, apperrors.Internalf("compute relative dest %q: %v", dest, err).
			Op("ImportService.ImportMovieFile").
			NotRetryable()
	}

	method, err := importer.HardlinkOrCopy(sourcePath, dest)
	if err != nil {
		return ImportResult{}, apperrors.Internalf("hardlink or copy %q -> %q: %v", sourcePath, dest, err).
			Op("ImportService.ImportMovieFile")
	}

	// Upsert-ish media_file by path
	mf, err := s.repo.GetMediaFileByLibraryAndPath(ctx, lib.ID, destRel)
	if err != nil {
		if apperrors.IsNotFound(err) {
			mf, err = s.repo.CreateMediaFile(ctx, lib.ID, mediaItem.ID, nil, destRel)
			if err != nil {
				return ImportResult{}, err
			}
			// Create file state for the new file
			if _, err := s.repo.UpsertMediaFileState(ctx, mf.ID, true, nil); err != nil {
				s.log.Warn().Err(err).Msg("Failed to create media file state")
			}
			// Record import history (note: no import_task_id when called from manual import)
			if _, err := s.repo.CreateMediaFileImport(ctx, dbgen.CreateMediaFileImportParams{
				MediaFileID:  mf.ID,
				ImportTaskID: pgtype.UUID{Valid: false},
				Method:       "hardlink",
				SourcePath:   &sourcePath,
				DestPath:     dest,
				Success:      true,
			}); err != nil {
				s.log.Warn().Err(err).Msg("Failed to record import history")
			}
		} else {
			return ImportResult{}, err
		}
	}

	return ImportResult{
		SourcePath:  sourcePath,
		DestPath:    dest,
		DestRelPath: destRel,
		Method:      method,
		MediaItem:   mediaItem,
		MediaFile:   mf,
	}, nil
}

func (s *ImportService) ImportSeriesJob(ctx context.Context, job dbgen.DownloadJob, downloaderClient downloader.Client) ([]ImportResult, error) {
	if !job.MediaItemID.Valid {
		return nil, apperrors.Internalf("download job %s missing media_item_id", job.ID).
			Op("ImportService.ImportSeriesJob").
			NotRetryable()
	}

	mediaItem, err := s.repo.GetMediaItem(ctx, job.MediaItemID)
	if err != nil {
		return nil, err
	}

	lib, err := s.repo.GetLibrary(ctx, job.LibraryID)
	if err != nil {
		return nil, err
	}
	nt, err := s.repo.GetNameTemplate(ctx, job.NameTemplateID)
	if err != nil {
		return nil, err
	}

	if job.DownloaderExternalID == nil {
		return nil, apperrors.Internalf("download job %s missing downloader_external_id", job.ID).
			Op("ImportService.ImportSeriesJob").
			NotRetryable()
	}

	files, err := downloaderClient.ListFiles(ctx, *job.DownloaderExternalID)
	if err != nil {
		return nil, apperrors.BadGatewayf("downloader list files for job %s: %v", job.ID, err).
			Op("ImportService.ImportSeriesJob")
	}

	// Derive target season and episode from episode_id (season is derived from episode)
	var targetSeason *int
	var targetEpisode *int
	if job.EpisodeID.Valid {
		episode, err := s.repo.GetEpisode(ctx, job.EpisodeID)
		if err == nil {
			eNum := int(episode.EpisodeNumber)
			targetEpisode = &eNum
			// Derive season from episode
			season, err := s.repo.GetSeason(ctx, episode.SeasonID)
			if err == nil {
				sNum := int(season.SeasonNumber)
				targetSeason = &sNum
			}
		}
	}

	matchedFiles := importer.MatchFilesToEpisodes(files, targetSeason, targetEpisode)
	s.log.Debug().
		Int("matched_count", len(matchedFiles)).
		Interface("matched_files", matchedFiles).
		Msg("Matched files to episodes")

	if len(matchedFiles) == 0 {
		return nil, apperrors.Conflictf("download job %s: no files matched target episodes", job.ID).
			Op("ImportService.ImportSeriesJob")
	}

	// Fetch downloader details to get SavePath
	dlItem, err := downloaderClient.Get(ctx, *job.DownloaderExternalID)
	if err != nil {
		return nil, apperrors.BadGatewayf("downloader get for job %s: %v", job.ID, err).
			Op("ImportService.ImportSeriesJob")
	}

	var results []ImportResult

	for epNum, f := range matchedFiles {
		s.log.Debug().Int("episode", epNum).Str("file", f.Path).Msg("Processing matched file")
		// Ensure we have a season for this episode
		var season dbgen.MediaSeason
		if targetSeason != nil && epNum >= 0 {
			season, err = s.repo.UpsertSeason(ctx, mediaItem.ID, int32(*targetSeason), pgtype.Date{Valid: false})
			if err != nil {
				s.log.Error().Err(err).Int("season", *targetSeason).Msg("Failed to upsert season")
				continue
			}
		} else {
			// If we don't know the season, we might need to parse it from the file
			info, ok := importer.ParseSeriesInfo(filepath.Base(f.Path))
			if !ok {
				s.log.Warn().Str("file", f.Path).Msg("Failed to parse series info from filename")
				continue
			}
			season, err = s.repo.UpsertSeason(ctx, mediaItem.ID, int32(info.Season), pgtype.Date{Valid: false})
			if err != nil {
				s.log.Error().Err(err).Int("season", info.Season).Msg("Failed to upsert parsed season")
				continue
			}
		}

		// Ensure we have an episode record
		episode, err := s.repo.UpsertEpisode(ctx, season.ID, int32(epNum), nil, pgtype.Date{Valid: false}, nil, nil)
		if err != nil {
			s.log.Error().Err(err).Int("episode", epNum).Msg("Failed to upsert episode")
			continue
		}

		// Build evaluation context for this episode
		seasonNum := int(season.SeasonNumber)
		episodeNum := int(episode.EpisodeNumber)
		evalCtx := s.buildSeriesEvalContext(mediaItem, job.CandidateTitle, &seasonNum, &episodeNum, episode.Title)

		// Extract mediainfo from source file BEFORE rendering template
		sourcePath := f.Path
		if !filepath.IsAbs(sourcePath) && dlItem.SavePath != "" {
			sourcePath = filepath.Join(dlItem.SavePath, f.Path)
		}
		if mi := s.mediaInfo.Analyze(sourcePath); mi != nil {
			evalCtx = evalCtx.WithMediaInfo(mi)
		} else {
			s.log.Warn().Str("path", sourcePath).Msg("Failed to extract mediainfo, continuing without it")
		}

		templateData := evalCtx.ToTemplateData()

		ext := filepath.Ext(f.Path)
		var rel string
		if nt.Type == "series" {
			showPart, err := template.Render(coalesce(nt.SeriesShowTemplate, ""), templateData)
			if err != nil {
				s.log.Error().Err(err).Interface("templateData", templateData).Msg("Failed to render show template")
				continue
			}
			seasonPart, err := template.Render(coalesce(nt.SeriesSeasonTemplate, ""), templateData)
			if err != nil {
				s.log.Error().Err(err).Interface("templateData", templateData).Msg("Failed to render season template")
				continue
			}
			filePart, err := template.Render(nt.Template, templateData)
			if err != nil {
				s.log.Error().Err(err).Interface("templateData", templateData).Msg("Failed to render file template")
				continue
			}
			rel = filepath.Join(showPart, seasonPart, filePart)
		} else {
			rel, err = template.Render(nt.Template, templateData)
			if err != nil {
				s.log.Error().Err(err).Interface("templateData", templateData).Msg("Failed to render template")
				continue
			}
		}
		rel = importer.EnsureExt(rel, ext)

		dest := filepath.Join(lib.RootPath, rel)
		destRel, err := filepath.Rel(lib.RootPath, dest)
		if err != nil {
			s.log.Error().Err(err).Str("dest", dest).Msg("Failed to compute relative path")
			continue
		}

		s.log.Debug().Str("source", sourcePath).Str("dest", dest).Msg("Attempting import")
		method, err := importer.HardlinkOrCopy(sourcePath, dest)
		if err != nil {
			s.log.Error().Err(err).Str("source", sourcePath).Str("dest", dest).Msg("Failed to hardlink or copy file")
			continue
		}

		mf, err := s.repo.CreateMediaFile(ctx, lib.ID, mediaItem.ID, &episode.ID, destRel)
		if err != nil {
			s.log.Error().Err(err).Str("path", destRel).Msg("Failed to create media file record")
			continue
		}

		// Create file state for the new file
		if _, err := s.repo.UpsertMediaFileState(ctx, mf.ID, true, nil); err != nil {
			s.log.Warn().Err(err).Msg("Failed to create media file state")
		}

		// Record import history (note: no import_task_id when called from manual import)
		if _, err := s.repo.CreateMediaFileImport(ctx, dbgen.CreateMediaFileImportParams{
			MediaFileID:  mf.ID,
			ImportTaskID: pgtype.UUID{Valid: false},
			Method:       method,
			SourcePath:   &sourcePath,
			DestPath:     dest,
			Success:      true,
		}); err != nil {
			s.log.Warn().Err(err).Msg("Failed to record import history")
		}

		s.log.Info().Str("path", destRel).Str("method", method).Msg("Successfully imported episode")
		results = append(results, ImportResult{
			SourcePath:  sourcePath,
			DestPath:    dest,
			DestRelPath: destRel,
			Method:      method,
			MediaItem:   mediaItem,
			MediaFile:   mf,
		})
	}

	return results, nil
}

// buildMovieEvalContext creates an EvaluationContext for movie imports
func (s *ImportService) buildMovieEvalContext(mediaItem dbgen.MediaItem, candidateTitle string) model.EvaluationContext {
	q := release.Parse(candidateTitle)

	// Create a minimal candidate from job info (we don't have full candidate data at import time)
	candidate := model.DownloadCandidate{
		Title: candidateTitle,
	}

	evalCtx := model.NewEvaluationContext(candidate, q)

	// Add media metadata
	year := 0
	if mediaItem.Year != nil {
		year = int(*mediaItem.Year)
	}
	tmdbID := int64(0)
	if mediaItem.TmdbID != nil {
		tmdbID = *mediaItem.TmdbID
	}
	evalCtx = evalCtx.WithMedia(model.MediaTypeMovie, mediaItem.Title, year, tmdbID)

	return evalCtx
}

// buildSeriesEvalContext creates an EvaluationContext for series imports
func (s *ImportService) buildSeriesEvalContext(mediaItem dbgen.MediaItem, candidateTitle string, season, episode *int, episodeTitle *string) model.EvaluationContext {
	q := release.Parse(candidateTitle)

	// Create a minimal candidate from job info
	candidate := model.DownloadCandidate{
		Title: candidateTitle,
	}

	evalCtx := model.NewEvaluationContext(candidate, q)

	// Add media metadata
	year := 0
	if mediaItem.Year != nil {
		year = int(*mediaItem.Year)
	}
	tmdbID := int64(0)
	if mediaItem.TmdbID != nil {
		tmdbID = *mediaItem.TmdbID
	}
	evalCtx = evalCtx.WithMedia(model.MediaTypeSeries, mediaItem.Title, year, tmdbID)
	evalCtx = evalCtx.WithSeriesInfo(season, episode, episodeTitle)

	return evalCtx
}
