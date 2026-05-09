// media.go is the humachi-shaped media handler. The route shape is the
// pre-migration one: paginated /library, /search via TMDB, and
// {movie,series,person}/{tmdbId} detail endpoints. There are no slug or UUID
// addressed routes today — TMDB ids (int64) are the only addressable form.
//
// All endpoints are protected by the global ChiJWT middleware; reads of the
// authenticated principal aren't required by any handler here, so we don't
// touch the claims context.
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

// MediaListInput carries pagination + filtering for GET /library.
type MediaListInput struct {
	Page     int    `query:"page" minimum:"1" doc:"1-based page index"`
	PageSize int    `query:"pageSize" minimum:"1" maximum:"100" doc:"Items per page (1-100)"`
	Type     string `query:"type" enum:",movie,series" doc:"Filter by media type"`
	Search   string `query:"search" doc:"Substring match on title"`
	SortBy   string `query:"sortBy" enum:",title,year,createdAt" doc:"Sort field"`
	SortDir  string `query:"sortDir" enum:",asc,desc" doc:"Sort direction"`
}

// MediaListOutput wraps the paginated library page. The Page generic was
// aliased to PaginatedLibraryResponse upstream for stable schema naming.
type MediaListOutput struct {
	Body model.PaginatedLibraryResponse
}

// List returns a paginated, optionally filtered slice of media items in the
// library. Defaults / clamps are applied in the service layer.
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

// MediaSearchInput carries the TMDB multi-search query.
type MediaSearchInput struct {
	Q     string `query:"q" required:"true" minLength:"1" doc:"Search query"`
	Limit int    `query:"limit" minimum:"1" maximum:"100" doc:"Max results (default 20)"`
	Page  int    `query:"page" minimum:"1" doc:"1-based page index"`
}

// MediaSearchOutput wraps the TMDB search response shape.
type MediaSearchOutput struct {
	Body model.SearchResponse
}

// Search runs a TMDB multi-search and enriches the results with library
// status. Upstream failures surface as 502 BadGateway via the service.
func (h *Media) Search(ctx context.Context, input *MediaSearchInput) (*MediaSearchOutput, error) {
	res, err := h.svc.Media.Search(ctx, input.Q, input.Limit, input.Page)
	if err != nil {
		return nil, err
	}
	return &MediaSearchOutput{Body: res}, nil
}

// ----- Get movie -----

// MediaGetMovieInput carries the TMDB movie id.
type MediaGetMovieInput struct {
	ID int64 `path:"id" minimum:"1" doc:"TMDB movie id"`
}

// MediaGetMovieOutput wraps the movie-detail shape.
type MediaGetMovieOutput struct {
	Body model.MovieDetail
}

// GetMovie returns the full movie detail (TMDB-enriched, with local files
// and active download jobs spliced in by the service).
func (h *Media) GetMovie(ctx context.Context, input *MediaGetMovieInput) (*MediaGetMovieOutput, error) {
	res, err := h.svc.Media.GetMovieDetail(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &MediaGetMovieOutput{Body: res}, nil
}

// ----- Get series -----

// MediaGetSeriesInput carries the TMDB series id.
type MediaGetSeriesInput struct {
	ID int64 `path:"id" minimum:"1" doc:"TMDB series id"`
}

// MediaGetSeriesOutput wraps the series-detail shape.
type MediaGetSeriesOutput struct {
	Body model.SeriesDetail
}

// GetSeries returns the full series detail.
func (h *Media) GetSeries(ctx context.Context, input *MediaGetSeriesInput) (*MediaGetSeriesOutput, error) {
	res, err := h.svc.Media.GetSeriesDetail(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &MediaGetSeriesOutput{Body: res}, nil
}

// ----- Get person -----

// MediaGetPersonInput carries the TMDB person id.
type MediaGetPersonInput struct {
	ID int64 `path:"id" minimum:"1" doc:"TMDB person id"`
}

// MediaGetPersonOutput wraps the person-detail shape.
type MediaGetPersonOutput struct {
	Body model.PersonDetail
}

// GetPerson returns the full person detail.
func (h *Media) GetPerson(ctx context.Context, input *MediaGetPersonInput) (*MediaGetPersonOutput, error) {
	res, err := h.svc.Media.GetPersonDetail(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &MediaGetPersonOutput{Body: res}, nil
}

// ----- Register -----

// RegisterHumachi wires the media operations onto the humachi API. All
// routes are protected (no public-path entries).
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
