package service

import (
	"context"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// UnmatchedFilesService is the read surface over "files needing review" —
// files whose identity hasn't been resolved (file.media_item_id IS NULL).
// "Unmatched" is now a query over the unified file table, not a separate
// entity; the ranked suggestions and decision flows live on the
// match_decision endpoints (/files/{id}/match, /unmatch, /detach,
// /match-decision).
type UnmatchedFilesService struct {
	repo   *repo.Repository
	logger *logger.Logger
	tmdb   *TmdbService
}

func NewUnmatchedFilesService(r *repo.Repository, l *logger.Logger, tmdb *TmdbService) *UnmatchedFilesService {
	return &UnmatchedFilesService{repo: r, logger: l, tmdb: tmdb}
}

// UnmatchedFileResponse is the API response for a file needing review.
// partial_series is derived: media_item_id set with episode_id NULL on a
// series file. Ranked suggestions are read from the file's current
// match_decision (the /files/{id}/match-decision endpoint), not here.
type UnmatchedFileResponse struct {
	ID            string `json:"id"`
	LibraryID     string `json:"libraryId"`
	Path          string `json:"path"`
	FileSize      *int64 `json:"fileSize,omitempty"`
	DiscoveredAt  string `json:"discoveredAt"`
	PartialSeries bool   `json:"partialSeries,omitempty"`
}

// ListParams contains parameters for listing files needing review.
type ListParams struct {
	LibraryID *uuid.UUID
	Page      int
	PageSize  int
}

// ListResult contains a paginated list of files needing review.
type ListResult struct {
	Items      []UnmatchedFileResponse `json:"items"`
	TotalCount int64                   `json:"totalCount"`
	Page       int                     `json:"page"`
	PageSize   int                     `json:"pageSize"`
}

// List returns a paginated list of files needing review (identity NULL).
func (s *UnmatchedFilesService) List(ctx context.Context, params ListParams) (ListResult, error) {
	if params.PageSize <= 0 {
		params.PageSize = 20
	}
	if params.Page <= 0 {
		params.Page = 1
	}

	offset := int32((params.Page - 1) * params.PageSize)

	files, err := s.repo.ListFilesNeedingReview(ctx, repo.FilesReviewQueryParams{
		LibraryID: params.LibraryID,
		PageSize:  int32(params.PageSize),
		Offset:    offset,
	})
	if err != nil {
		return ListResult{}, err
	}

	count, err := s.repo.CountFilesNeedingReview(ctx, params.LibraryID)
	if err != nil {
		return ListResult{}, err
	}

	items := make([]UnmatchedFileResponse, 0, len(files))
	for _, f := range files {
		resp, rerr := s.toResponse(ctx, f)
		if rerr != nil {
			return ListResult{}, rerr
		}
		items = append(items, resp)
	}

	return ListResult{
		Items:      items,
		TotalCount: count,
		Page:       params.Page,
		PageSize:   params.PageSize,
	}, nil
}

// Get returns a single file needing review by ID. Returns NotFound when
// the file is identified (not in the review set) or doesn't exist.
func (s *UnmatchedFilesService) Get(ctx context.Context, id uuid.UUID) (UnmatchedFileResponse, error) {
	file, err := s.repo.GetFile(ctx, id)
	if err != nil {
		return UnmatchedFileResponse{}, err
	}
	if file.MediaItemID != nil {
		return UnmatchedFileResponse{}, apperrors.NotFoundf("file %s is not awaiting review", id).
			Op("UnmatchedFilesService.Get")
	}
	return s.toResponse(ctx, file)
}

// toResponse maps a file row to the wire shape. partial_series is derived
// from the series-with-no-episode shape; here, review files have NULL
// media_item_id so partial_series is always false (a partial-series file
// carries the series id and isn't in this set).
func (s *UnmatchedFilesService) toResponse(ctx context.Context, f model.File) (UnmatchedFileResponse, error) {
	var size *int64
	if state, err := s.repo.GetFileState(ctx, f.ID); err == nil {
		size = state.SizeBytes
	}
	return UnmatchedFileResponse{
		ID:           f.ID.String(),
		LibraryID:    f.LibraryID.String(),
		Path:         f.Path,
		FileSize:     size,
		DiscoveredAt: f.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}, nil
}
