package service

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
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

// SuggestedMatch represents a potential match for an unmatched file
type SuggestedMatch struct {
	TmdbID int64  `json:"tmdbId"`
	Title  string `json:"title"`
	Year   int    `json:"year,omitempty"`
	Type   string `json:"type"` // movie or series
	Score  int    `json:"score"`
}

// UnmatchedFileResponse is the API response for an unmatched file
type UnmatchedFileResponse struct {
	ID               string           `json:"id"`
	LibraryID        string           `json:"libraryId"`
	Path             string           `json:"path"`
	FileSize         *int64           `json:"fileSize,omitempty"`
	DiscoveredAt     string           `json:"discoveredAt"`
	SuggestedMatches []SuggestedMatch `json:"suggestedMatches,omitempty"`
}

// ListParams contains parameters for listing unmatched files
type ListParams struct {
	LibraryID *pgtype.UUID
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
func (s *UnmatchedFilesService) Get(ctx context.Context, id pgtype.UUID) (UnmatchedFileResponse, error) {
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
func (s *UnmatchedFilesService) Match(ctx context.Context, id pgtype.UUID, req MatchRequest) (dbgen.MediaFile, error) {
	// Get the unmatched file
	unmatched, err := s.repo.GetUnmatchedFile(ctx, id)
	if err != nil {
		return dbgen.MediaFile{}, err
	}

	// Check if already resolved
	if unmatched.ResolvedAt.Valid {
		return dbgen.MediaFile{}, apperrors.Conflictf("unmatched file %s already resolved", id).
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

	mediaItem, err := s.repo.UpsertMediaItem(ctx, req.Type, title, year, &req.TmdbID)
	if err != nil {
		return dbgen.MediaFile{}, err
	}

	// For series, upsert season and episode
	var episodeID *pgtype.UUID
	if req.Type == "series" && req.Season != nil {
		season, err := s.repo.UpsertSeason(ctx, repo.UpsertSeasonParams{
			MediaItemID:  uuid.UUID(mediaItem.ID.Bytes),
			SeasonNumber: int32(*req.Season),
		})
		if err != nil {
			return dbgen.MediaFile{}, err
		}

		if req.Episode != nil {
			episode, err := s.repo.UpsertEpisode(ctx, repo.UpsertEpisodeParams{
				SeasonID:      season.ID,
				EpisodeNumber: int32(*req.Episode),
			})
			if err != nil {
				return dbgen.MediaFile{}, err
			}
			epIDPg := pgtype.UUID{Bytes: episode.ID, Valid: true}
			episodeID = &epIDPg
		}
	}

	// Create media file
	mediaFile, err := s.repo.CreateMediaFile(ctx, unmatched.LibraryID, mediaItem.ID, episodeID, unmatched.Path)
	if err != nil {
		return dbgen.MediaFile{}, err
	}

	// Create file state
	if _, err := s.repo.UpsertMediaFileState(ctx, mediaFile.ID, true, unmatched.FileSize); err != nil {
		s.logger.Warn().Err(err).Msg("Failed to create media file state")
	}

	// Record import history
	if _, err := s.repo.CreateMediaFileImport(ctx, dbgen.CreateMediaFileImportParams{
		MediaFileID: mediaFile.ID,
		Method:      "manual_match",
		DestPath:    unmatched.Path,
		Success:     true,
	}); err != nil {
		s.logger.Warn().Err(err).Msg("Failed to record import history")
	}

	// Mark unmatched file as resolved
	if _, err := s.repo.ResolveUnmatchedFile(ctx, id, mediaFile.ID); err != nil {
		s.logger.Warn().Err(err).Msg("Failed to mark unmatched file as resolved")
	}

	return mediaFile, nil
}

// Dismiss marks an unmatched file as dismissed (resolved without matching)
func (s *UnmatchedFilesService) Dismiss(ctx context.Context, id pgtype.UUID) error {
	if _, err := s.repo.DismissUnmatchedFile(ctx, id); err != nil {
		return err
	}
	return nil
}

// RefreshSuggestions regenerates match suggestions for an unmatched file
func (s *UnmatchedFilesService) RefreshSuggestions(ctx context.Context, id pgtype.UUID) (UnmatchedFileResponse, error) {
	file, err := s.repo.GetUnmatchedFile(ctx, id)
	if err != nil {
		return UnmatchedFileResponse{}, err
	}

	// Get library to determine type
	lib, err := s.repo.GetLibrary(ctx, uuid.UUID(file.LibraryID.Bytes))
	if err != nil {
		return UnmatchedFileResponse{}, err
	}

	// Generate suggestions based on filename
	suggestions := s.generateSuggestions(ctx, file.Path, lib.Type)

	// Update suggestions in database
	suggestionsJSON, _ := json.Marshal(suggestions)
	file, err = s.repo.UpdateUnmatchedFileSuggestions(ctx, id, suggestionsJSON)
	if err != nil {
		return UnmatchedFileResponse{}, err
	}

	return s.toResponse(file), nil
}

func (s *UnmatchedFilesService) toResponse(f dbgen.UnmatchedFile) UnmatchedFileResponse {
	var suggestions []SuggestedMatch
	if f.SuggestedMatches != nil {
		_ = json.Unmarshal(f.SuggestedMatches, &suggestions)
	}

	return UnmatchedFileResponse{
		ID:               f.ID.String(),
		LibraryID:        f.LibraryID.String(),
		Path:             f.Path,
		FileSize:         f.FileSize,
		DiscoveredAt:     f.DiscoveredAt.Format("2006-01-02T15:04:05Z"),
		SuggestedMatches: suggestions,
	}
}

func (s *UnmatchedFilesService) generateSuggestions(ctx context.Context, path string, mediaType string) []SuggestedMatch {
	// TODO: Implement suggestion generation by parsing filename and searching TMDB
	// For now, return empty suggestions
	return []SuggestedMatch{}
}
