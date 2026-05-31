package service

import (
	"context"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// UnmatchedFilesService is the read surface over the matcher inbox — every
// file whose current (non-superseded) match_decision banded to something
// other than confident (auto-matched) or detached (user already said "this
// doesn't belong"). That predicate keeps confident_review / partial_series
// (identity set but flagged) in the inbox. The ranked suggestions and
// decision flows live on the match_decision endpoints (/files/{id}/match,
// /unmatch, /detach, /match-decision).
type UnmatchedFilesService struct {
	repo   *repo.Repository
	logger *logger.Logger
	tmdb   *TmdbService
}

func NewUnmatchedFilesService(r *repo.Repository, l *logger.Logger, tmdb *TmdbService) *UnmatchedFilesService {
	return &UnmatchedFilesService{repo: r, logger: l, tmdb: tmdb}
}

// ListParams contains parameters for listing inbox files. Outcome is an
// optional band filter; nil lists every band.
type ListParams struct {
	LibraryID *uuid.UUID
	Outcome   *string
	Page      int
	PageSize  int
}

// List returns a paginated page of inbox files plus the per-band counts.
// Pagination Total comes from the counts (sum of all bands, or the
// selected band when an outcome filter is set) — a single count query, not
// a second COUNT(*).
func (s *UnmatchedFilesService) List(ctx context.Context, params ListParams) (model.InboxPage, error) {
	if params.PageSize <= 0 {
		params.PageSize = 20
	}
	if params.Page <= 0 {
		params.Page = 1
	}

	offset := int32((params.Page - 1) * params.PageSize)

	items, err := s.repo.ListInboxItems(ctx, repo.InboxQueryParams{
		LibraryID: params.LibraryID,
		Outcome:   params.Outcome,
		PageSize:  int32(params.PageSize),
		Offset:    offset,
	})
	if err != nil {
		return model.InboxPage{}, err
	}

	counts, err := s.repo.CountInboxByOutcome(ctx, params.LibraryID)
	if err != nil {
		return model.InboxPage{}, err
	}

	var total int64
	if params.Outcome != nil {
		total = counts[*params.Outcome]
	} else {
		for _, c := range counts {
			total += c
		}
	}

	totalPages := 0
	if params.PageSize > 0 {
		totalPages = int((total + int64(params.PageSize) - 1) / int64(params.PageSize))
	}

	return model.InboxPage{
		Data: items,
		Pagination: model.Pagination{
			Total:      total,
			Page:       params.Page,
			PageSize:   params.PageSize,
			TotalPages: totalPages,
		},
		CountsByOutcome: counts,
	}, nil
}

// Get returns a single inbox file by ID. The inbox query already excludes
// soft-deleted files and files whose current decision is confident /
// detached, so a file that isn't awaiting review reads as not-found —
// repo's FromPg turns the no-rows result into a 404.
func (s *UnmatchedFilesService) Get(ctx context.Context, id uuid.UUID) (model.InboxItem, error) {
	item, err := s.repo.GetInboxItem(ctx, id)
	if apperrors.IsNotFound(err) {
		return model.InboxItem{}, apperrors.Wrap(err, "file %s is not awaiting review", id)
	}
	return item, err
}
