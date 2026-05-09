package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

type Media struct{ svc *service.Services }

func NewMedia(s *service.Services) *Media { return &Media{svc: s} }

// ----- List (library) -----

type MediaListInput struct {
	Page     int    `query:"page" minimum:"1" doc:"1-based page index"`
	PageSize int    `query:"pageSize" minimum:"1" maximum:"100" doc:"Items per page (1-100)"`
	Type     string `query:"type" enum:",movie,series" doc:"Filter by media type"`
	Search   string `query:"search" doc:"Substring match on title"`
	SortBy   string `query:"sortBy" enum:",title,year,createdAt" doc:"Sort field"`
	SortDir  string `query:"sortDir" enum:",asc,desc" doc:"Sort direction"`
}

type MediaListOutput struct {
	Body model.PaginatedLibraryResponse
}

func (h *Media) List(ctx context.Context, input *MediaListInput) (*MediaListOutput, error) {
	res, err := h.svc.Media.ListLibraryItemsPaginated(ctx, service.LibraryQueryParams{
		Page:     input.Page,
		PageSize: input.PageSize,
		Type:     input.Type,
		Search:   input.Search,
		SortBy:   input.SortBy,
		SortDir:  input.SortDir,
	})
	if err != nil {
		return nil, err
	}
	return &MediaListOutput{Body: res}, nil
}

// ----- Search -----

type MediaSearchInput struct {
	Q     string `query:"q" required:"true" minLength:"1" doc:"Search query"`
	Limit int    `query:"limit" minimum:"1" maximum:"100" doc:"Max results (default 20)"`
	Page  int    `query:"page" minimum:"1" doc:"1-based page index"`
}

type MediaSearchOutput struct {
	Body model.SearchResponse
}

func (h *Media) Search(ctx context.Context, input *MediaSearchInput) (*MediaSearchOutput, error) {
	res, err := h.svc.Media.Search(ctx, input.Q, input.Limit, input.Page)
	if err != nil {
		return nil, err
	}
	return &MediaSearchOutput{Body: res}, nil
}

// ----- Get movie -----

type MediaGetMovieInput struct {
	ID int64 `path:"id" minimum:"1" doc:"TMDB movie id"`
}

type MediaGetMovieOutput struct {
	Body model.MovieDetail
}

func (h *Media) GetMovie(ctx context.Context, input *MediaGetMovieInput) (*MediaGetMovieOutput, error) {
	res, err := h.svc.Media.GetMovieDetail(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &MediaGetMovieOutput{Body: res}, nil
}

// ----- Get series -----

type MediaGetSeriesInput struct {
	ID int64 `path:"id" minimum:"1" doc:"TMDB series id"`
}

type MediaGetSeriesOutput struct {
	Body model.SeriesDetail
}

func (h *Media) GetSeries(ctx context.Context, input *MediaGetSeriesInput) (*MediaGetSeriesOutput, error) {
	res, err := h.svc.Media.GetSeriesDetail(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &MediaGetSeriesOutput{Body: res}, nil
}

// ----- Get person -----

type MediaGetPersonInput struct {
	ID int64 `path:"id" minimum:"1" doc:"TMDB person id"`
}

type MediaGetPersonOutput struct {
	Body model.PersonDetail
}

func (h *Media) GetPerson(ctx context.Context, input *MediaGetPersonInput) (*MediaGetPersonOutput, error) {
	res, err := h.svc.Media.GetPersonDetail(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &MediaGetPersonOutput{Body: res}, nil
}

// ----- Register -----

func (h *Media) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "library-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/library",
		Summary:     "List library items",
		Tags:        []string{"media"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "media-search",
		Method:      http.MethodGet,
		Path:        "/api/v1/search",
		Summary:     "Search TMDB",
		Description: "Multi-search across movies, series, and people via TMDB.",
		Tags:        []string{"media"},
		Errors:      errsUpstream,
	}, h.Search)

	huma.Register(api, huma.Operation{
		OperationID: "media-get-movie",
		Method:      http.MethodGet,
		Path:        "/api/v1/movie/{id}",
		Summary:     "Get movie",
		Description: "Returns the full movie detail (TMDB + local files).",
		Tags:        []string{"media"},
		Errors:      errs(errsRead, errsUpstream),
	}, h.GetMovie)

	huma.Register(api, huma.Operation{
		OperationID: "media-get-series",
		Method:      http.MethodGet,
		Path:        "/api/v1/series/{id}",
		Summary:     "Get series",
		Description: "Returns the full series detail (TMDB + local files).",
		Tags:        []string{"media"},
		Errors:      errs(errsRead, errsUpstream),
	}, h.GetSeries)

	huma.Register(api, huma.Operation{
		OperationID: "media-get-person",
		Method:      http.MethodGet,
		Path:        "/api/v1/person/{id}",
		Summary:     "Get person",
		Description: "Returns the full person detail.",
		Tags:        []string{"media"},
		Errors:      errs(errsRead, errsUpstream),
	}, h.GetPerson)
}
