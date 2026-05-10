package service

import (
	"context"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

type UnmatchedFilesService struct {
	repo   *repo.Repository
	logger *logger.Logger
	tmdb   *TmdbService
}

func NewUnmatchedFilesService(r *repo.Repository, l *logger.Logger, tmdb *TmdbService) *UnmatchedFilesService {
	return &UnmatchedFilesService{repo: r, logger: l, tmdb: tmdb}
}

// UnmatchedFileResponse is the API response for an unmatched file
type UnmatchedFileResponse struct {
	ID               string                 `json:"id"`
	LibraryID        string                 `json:"libraryId"`
	Path             string                 `json:"path"`
	FileSize         *int64                 `json:"fileSize,omitempty"`
	DiscoveredAt     string                 `json:"discoveredAt"`
	SuggestedMatches []model.SuggestedMatch `json:"suggestedMatches,omitempty"`
}

// ListParams contains parameters for listing unmatched files
type ListParams struct {
	LibraryID *uuid.UUID
	Page      int
	PageSize  int
}

// ListResult contains a paginated list of unmatched files
type ListResult struct {
	Items      []UnmatchedFileResponse `json:"items"`
	TotalCount int64                   `json:"totalCount"`
	Page       int                     `json:"page"`
	PageSize   int                     `json:"pageSize"`
}

// List returns a paginated list of unresolved unmatched files
func (s *UnmatchedFilesService) List(ctx context.Context, params ListParams) (ListResult, error) {
	if params.PageSize <= 0 {
		params.PageSize = 20
	}
	if params.Page <= 0 {
		params.Page = 1
	}

	offset := int32((params.Page - 1) * params.PageSize)

	files, err := s.repo.ListUnmatchedFilesPaginated(ctx, repo.UnmatchedFilesQueryParams{
		LibraryID: params.LibraryID,
		PageSize:  int32(params.PageSize),
		Offset:    offset,
	})
	if err != nil {
		return ListResult{}, err
	}

	count, err := s.repo.CountUnmatchedFiles(ctx, params.LibraryID)
	if err != nil {
		return ListResult{}, err
	}

	items := make([]UnmatchedFileResponse, 0, len(files))
	for _, f := range files {
		items = append(items, s.toResponse(f))
	}

	return ListResult{
		Items:      items,
		TotalCount: count,
		Page:       params.Page,
		PageSize:   params.PageSize,
	}, nil
}

// Get returns a single unmatched file by ID
func (s *UnmatchedFilesService) Get(ctx context.Context, id uuid.UUID) (UnmatchedFileResponse, error) {
	file, err := s.repo.GetUnmatchedFile(ctx, id)
	if err != nil {
		return UnmatchedFileResponse{}, err
	}
	return s.toResponse(file), nil
}

// MatchRequest contains the parameters for matching an unmatched file
type MatchRequest struct {
	TmdbID  int64  `json:"tmdbId"`
	Type    string `json:"type"`    // movie or series
	Season  *int   `json:"season"`  // for series
	Episode *int   `json:"episode"` // for series
}

// Match manually matches an unmatched file to a media item
func (s *UnmatchedFilesService) Match(ctx context.Context, id uuid.UUID, req MatchRequest) (model.MediaFile, error) {
	// Get the unmatched file
	unmatched, err := s.repo.GetUnmatchedFile(ctx, id)
	if err != nil {
		return model.MediaFile{}, err
	}

	// Check if already resolved
	if unmatched.ResolvedAt != nil {
		return model.MediaFile{}, apperrors.Conflictf("unmatched file %s already resolved", id).
			Op("UnmatchedFilesService.Match")
	}

	// Upsert media item
	var year *int32
	var title string
	if req.Type == "movie" {
		// Try to get movie details for year and title
		movie, err := s.tmdb.GetMovieDetails(ctx, req.TmdbID)
		if err == nil {
			title = movie.Title
			if movie.ReleaseDate != "" {
				year = parseYear(movie.ReleaseDate)
			}
		}
	} else {
		// Try to get series details for year and title
		series, err := s.tmdb.GetSeriesDetails(ctx, req.TmdbID)
		if err == nil {
			title = series.Name
			if series.FirstAirDate != "" {
				year = parseYear(series.FirstAirDate)
			}
		}
	}

	mediaItem, err := s.repo.UpsertMediaItem(ctx, repo.UpsertMediaItemParams{
		Type:   req.Type,
		Title:  title,
		Year:   year,
		TmdbID: &req.TmdbID,
	})
	if err != nil {
		return model.MediaFile{}, err
	}

	// For series, upsert season and episode
	var episodeID *uuid.UUID
	if req.Type == "series" && req.Season != nil {
		season, err := s.repo.UpsertSeason(ctx, repo.UpsertSeasonParams{
			MediaItemID:  mediaItem.ID,
			SeasonNumber: int32(*req.Season),
		})
		if err != nil {
			return model.MediaFile{}, err
		}

		if req.Episode != nil {
			episode, err := s.repo.UpsertEpisode(ctx, repo.UpsertEpisodeParams{
				SeasonID:      season.ID,
				EpisodeNumber: int32(*req.Episode),
			})
			if err != nil {
				return model.MediaFile{}, err
			}
			epID := episode.ID
			episodeID = &epID
		}
	}

	mediaFile, err := s.repo.CreateMediaFile(ctx, repo.CreateMediaFileParams{
		LibraryID:   unmatched.LibraryID,
		MediaItemID: mediaItem.ID,
		EpisodeID:   episodeID,
		Path:        unmatched.Path,
	})
	if err != nil {
		return model.MediaFile{}, err
	}

	// Create file state
	if _, err := s.repo.UpsertMediaFileState(ctx, repo.UpsertMediaFileStateParams{
		MediaFileID: mediaFile.ID,
		FileExists:  true,
		FileSize:    unmatched.FileSize,
	}); err != nil {
		s.logger.Warn().Err(err).Msg("Failed to create media file state")
	}

	// Record import history.
	if _, err := s.repo.CreateMediaFileImport(ctx, repo.CreateMediaFileImportParams{
		MediaFileID: mediaFile.ID,
		Method:      "manual_match",
		DestPath:    unmatched.Path,
		Success:     true,
	}); err != nil {
		s.logger.Warn().Err(err).Msg("Failed to record import history")
	}

	// Mark unmatched file as resolved
	if _, err := s.repo.ResolveUnmatchedFile(ctx, repo.ResolveUnmatchedFileParams{
		ID:                  id,
		ResolvedMediaFileID: mediaFile.ID,
	}); err != nil {
		s.logger.Warn().Err(err).Msg("Failed to mark unmatched file as resolved")
	}

	return mediaFile, nil
}

// Dismiss marks an unmatched file as dismissed (resolved without matching)
func (s *UnmatchedFilesService) Dismiss(ctx context.Context, id uuid.UUID) error {
	if _, err := s.repo.DismissUnmatchedFile(ctx, id); err != nil {
		return err
	}
	return nil
}

// RefreshSuggestions regenerates match suggestions for an unmatched file
func (s *UnmatchedFilesService) RefreshSuggestions(ctx context.Context, id uuid.UUID) (UnmatchedFileResponse, error) {
	file, err := s.repo.GetUnmatchedFile(ctx, id)
	if err != nil {
		return UnmatchedFileResponse{}, err
	}

	// Get library to determine type
	lib, err := s.repo.GetLibrary(ctx, file.LibraryID)
	if err != nil {
		return UnmatchedFileResponse{}, err
	}

	// Generate suggestions based on filename
	suggestions := s.generateSuggestions(ctx, file.Path, lib.Type)

	// Update suggestions in database
	file, err = s.repo.UpdateUnmatchedFileSuggestions(ctx, repo.UpdateUnmatchedFileSuggestionsParams{
		ID:               id,
		SuggestedMatches: suggestions,
	})
	if err != nil {
		return UnmatchedFileResponse{}, err
	}

	return s.toResponse(file), nil
}

func (s *UnmatchedFilesService) toResponse(f model.UnmatchedFile) UnmatchedFileResponse {
	return UnmatchedFileResponse{
		ID:               f.ID.String(),
		LibraryID:        f.LibraryID.String(),
		Path:             f.Path,
		FileSize:         f.FileSize,
		DiscoveredAt:     f.DiscoveredAt.Format("2006-01-02T15:04:05Z"),
		SuggestedMatches: f.SuggestedMatches,
	}
}

func (s *UnmatchedFilesService) generateSuggestions(ctx context.Context, path string, mediaType string) []model.SuggestedMatch {
	// TODO: Implement suggestion generation by parsing filename and searching TMDB
	// For now, return empty suggestions
	return []model.SuggestedMatch{}
}
