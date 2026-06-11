package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/routing"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

type DownloadCandidates struct{ svc *service.Services }

func NewDownloadCandidates(s *service.Services) *DownloadCandidates {
	return &DownloadCandidates{svc: s}
}

// ----- Shared body shape -----

// enqueueCandidateBody is the shared request body for preview / download
// (movie + series). Season / Episode are series-only; the movie endpoints
// ignore them.
type enqueueCandidateBody struct {
	IndexerID int64  `json:"indexerId" required:"true" minimum:"1" doc:"Indexer ID the candidate came from"`
	GUID      string `json:"guid" required:"true" minLength:"1" doc:"Indexer-specific candidate identifier"`
	Season    *int   `json:"season,omitempty" doc:"Season number (series only)"`
	Episode   *int   `json:"episode,omitempty" doc:"Episode number (series only)"`
}

type DownloadCandidateResponse struct {
	Evaluation routing.Evaluation `json:"evaluation"`
	Job        model.DownloadJob  `json:"job"`
}

// ----- ListMovieCandidates -----

type DownloadCandidatesListMovieInput struct {
	ID int64 `path:"id" doc:"TMDB movie ID"`
}

type DownloadCandidatesListMovieOutput struct {
	Body []model.DownloadCandidate
}

func (h *DownloadCandidates) ListMovie(ctx context.Context, input *DownloadCandidatesListMovieInput) (*DownloadCandidatesListMovieOutput, error) {
	out, err := h.svc.DownloadCandidates.SearchDownloadCandidates(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &DownloadCandidatesListMovieOutput{Body: out}, nil
}

// ----- ListSeriesCandidates -----

// DownloadCandidatesListSeriesInput uses 0 as the "absent" sentinel for
// season/episode because huma doesn't accept pointer types for query params.
// Season/episode are always positive when set; the handler converts to *int
// at the service boundary.
type DownloadCandidatesListSeriesInput struct {
	ID      int64 `path:"id" doc:"TMDB series ID"`
	Season  int   `query:"season" doc:"Optional season number — 0 / omitted means all seasons"`
	Episode int   `query:"episode" doc:"Optional episode number — 0 / omitted means all episodes"`
}

type DownloadCandidatesListSeriesOutput struct {
	Body []model.DownloadCandidate
}

func (h *DownloadCandidates) ListSeries(ctx context.Context, input *DownloadCandidatesListSeriesInput) (*DownloadCandidatesListSeriesOutput, error) {
	var season, episode *int
	if input.Season > 0 {
		s := input.Season
		season = &s
	}
	if input.Episode > 0 {
		e := input.Episode
		episode = &e
	}
	out, err := h.svc.DownloadCandidates.SearchSeriesDownloadCandidates(ctx, input.ID, season, episode)
	if err != nil {
		return nil, err
	}
	return &DownloadCandidatesListSeriesOutput{Body: out}, nil
}

// ----- PreviewMovieCandidate -----

type DownloadCandidatesPreviewMovieInput struct {
	ID   int64 `path:"id" doc:"TMDB movie ID"`
	Body enqueueCandidateBody
}

type DownloadCandidatesPreviewMovieOutput struct {
	Body routing.Evaluation
}

func (h *DownloadCandidates) PreviewMovie(ctx context.Context, input *DownloadCandidatesPreviewMovieInput) (*DownloadCandidatesPreviewMovieOutput, error) {
	trace, err := h.svc.DownloadCandidates.EvaluateCandidate(ctx, input.ID, input.Body.IndexerID, input.Body.GUID)
	if err != nil {
		return nil, err
	}
	return &DownloadCandidatesPreviewMovieOutput{Body: trace}, nil
}

// ----- PreviewSeriesCandidate -----

type DownloadCandidatesPreviewSeriesInput struct {
	ID   int64 `path:"id" doc:"TMDB series ID"`
	Body enqueueCandidateBody
}

type DownloadCandidatesPreviewSeriesOutput struct {
	Body routing.Evaluation
}

func (h *DownloadCandidates) PreviewSeries(ctx context.Context, input *DownloadCandidatesPreviewSeriesInput) (*DownloadCandidatesPreviewSeriesOutput, error) {
	trace, err := h.svc.DownloadCandidates.EvaluateCandidate(ctx, input.ID, input.Body.IndexerID, input.Body.GUID)
	if err != nil {
		return nil, err
	}
	return &DownloadCandidatesPreviewSeriesOutput{Body: trace}, nil
}

// ----- DownloadMovieCandidate -----

type DownloadCandidatesDownloadMovieInput struct {
	ID   int64 `path:"id" doc:"TMDB movie ID"`
	Body enqueueCandidateBody
}

type DownloadCandidatesDownloadMovieOutput struct {
	Body DownloadCandidateResponse
}

func (h *DownloadCandidates) DownloadMovie(ctx context.Context, input *DownloadCandidatesDownloadMovieInput) (*DownloadCandidatesDownloadMovieOutput, error) {
	trace, job, err := h.svc.DownloadCandidates.EnqueueCandidate(ctx, input.ID, input.Body.IndexerID, input.Body.GUID)
	if err != nil {
		return nil, err
	}
	return &DownloadCandidatesDownloadMovieOutput{Body: DownloadCandidateResponse{Evaluation: trace, Job: job}}, nil
}

// ----- DownloadSeriesCandidate -----

type DownloadCandidatesDownloadSeriesInput struct {
	ID   int64 `path:"id" doc:"TMDB series ID"`
	Body enqueueCandidateBody
}

type DownloadCandidatesDownloadSeriesOutput struct {
	Body DownloadCandidateResponse
}

func (h *DownloadCandidates) DownloadSeries(ctx context.Context, input *DownloadCandidatesDownloadSeriesInput) (*DownloadCandidatesDownloadSeriesOutput, error) {
	trace, job, err := h.svc.DownloadCandidates.EnqueueSeriesCandidate(ctx, input.ID, input.Body.IndexerID, input.Body.GUID, input.Body.Season, input.Body.Episode)
	if err != nil {
		return nil, err
	}
	return &DownloadCandidatesDownloadSeriesOutput{Body: DownloadCandidateResponse{Evaluation: trace, Job: job}}, nil
}

// ----- Register -----

func (h *DownloadCandidates) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "download-candidates-list-movie",
		Method:      http.MethodGet,
		Path:        "/api/v1/movie/{id}/candidates",
		Summary:     "List download candidates for a movie",
		Tags:        []string{"download-candidates"},
		Errors:      errs(errsRead, errsUpstream),
	}, h.ListMovie)

	huma.Register(api, huma.Operation{
		OperationID: "download-candidates-list-series",
		Method:      http.MethodGet,
		Path:        "/api/v1/series/{id}/candidates",
		Summary:     "List download candidates for a series",
		Tags:        []string{"download-candidates"},
		Errors:      errs(errsRead, errsUpstream),
	}, h.ListSeries)

	huma.Register(api, huma.Operation{
		OperationID: "download-candidates-preview-movie",
		Method:      http.MethodPost,
		Path:        "/api/v1/movie/{id}/candidate/preview",
		Summary:     "Preview routing dispatch for a movie candidate",
		Tags:        []string{"download-candidates"},
		Errors:      errs(errsUpsert, errsUpstream),
	}, h.PreviewMovie)

	huma.Register(api, huma.Operation{
		OperationID: "download-candidates-preview-series",
		Method:      http.MethodPost,
		Path:        "/api/v1/series/{id}/candidate/preview",
		Summary:     "Preview routing dispatch for a series candidate",
		Tags:        []string{"download-candidates"},
		Errors:      errs(errsUpsert, errsUpstream),
	}, h.PreviewSeries)

	huma.Register(api, huma.Operation{
		OperationID: "download-candidates-download-movie",
		Method:      http.MethodPost,
		Path:        "/api/v1/movie/{id}/candidate/download",
		Summary:     "Enqueue a movie download candidate",
		Description: "Dispatches routing rules, creates a download job, and returns the evaluation + job.",
		Tags:        []string{"download-candidates"},
		Errors:      errs(errsUpsert, errsUpstream),
	}, h.DownloadMovie)

	huma.Register(api, huma.Operation{
		OperationID: "download-candidates-download-series",
		Method:      http.MethodPost,
		Path:        "/api/v1/series/{id}/candidate/download",
		Summary:     "Enqueue a series download candidate",
		Description: "Dispatches routing rules, creates a download job (with season/episode if supplied), and returns the evaluation + job.",
		Tags:        []string{"download-candidates"},
		Errors:      errs(errsUpsert, errsUpstream),
	}, h.DownloadSeries)
}
