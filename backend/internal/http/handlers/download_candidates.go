// download_candidates.go is the humachi-shaped download-candidates handler.
// Six operations across movies and series:
//
//   - List candidates                 (GET  /movie/{id}/candidates,
//     GET  /series/{id}/candidates)
//   - Preview policy evaluation       (POST /movie/{id}/candidate/preview,
//     POST /series/{id}/candidate/preview)
//   - Enqueue download                (POST /movie/{id}/candidate/download,
//     POST /series/{id}/candidate/download)
//
// Movie and series share the same request body shape (`enqueueCandidateBody`)
// for the preview/download verbs. The series variants accept extra Season /
// Episode hints in the body.
//
// All three verbs hit Prowlarr (search) and the policy engine (evaluation),
// so every op enumerates errsUpstream.
//
// Path ids are TMDB ids (int64), not UUIDs — keep the `path:"id"` typed as
// int64 so huma binds it directly and emits a path.id 422 on a bad value.
package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/kyleaupton/arrflix/internal/model"
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
// ignore them. Required field validation runs at the tag level.
type enqueueCandidateBody struct {
	IndexerID int64  `json:"indexerId" required:"true" minimum:"1" doc:"Indexer ID the candidate came from"`
	GUID      string `json:"guid" required:"true" minLength:"1" doc:"Indexer-specific candidate identifier"`
	Season    *int   `json:"season,omitempty" doc:"Season number (series only)"`
	Episode   *int   `json:"episode,omitempty" doc:"Episode number (series only)"`
}

// DownloadCandidateResponse is the shared response shape for
// movie / series download enqueue operations: the policy evaluation trace
// plus the created download job.
type DownloadCandidateResponse struct {
	Trace model.EvaluationTrace `json:"trace"`
	Job   model.DownloadJob     `json:"job"`
}

// ----- ListMovieCandidates -----

// DownloadCandidatesListMovieInput is the path-id envelope for
// GET /movie/{id}/candidates.
type DownloadCandidatesListMovieInput struct {
	ID int64 `path:"id" doc:"TMDB movie ID"`
}

// DownloadCandidatesListMovieOutput is the flat list of candidates.
type DownloadCandidatesListMovieOutput struct {
	Body []model.DownloadCandidate
}

// ListMovie searches every configured indexer for download candidates
// matching the movie. The result is the post-filter (policy-applicable)
// list; final policy decisions happen at preview / download time.
func (h *DownloadCandidates) ListMovie(ctx context.Context, input *DownloadCandidatesListMovieInput) (*DownloadCandidatesListMovieOutput, error) {
	out, err := h.svc.DownloadCandidates.SearchDownloadCandidates(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &DownloadCandidatesListMovieOutput{Body: out}, nil
}

// ----- ListSeriesCandidates -----

// DownloadCandidatesListSeriesInput holds the path id plus optional season /
// episode query params (mirrors the Echo query-param shape exactly).
//
// Huma doesn't accept pointer types for query params; we use 0 as the
// "absent" sentinel since season/episode numbers are always positive in the
// pre-migration Echo handler (`strconv.Atoi` failures left the *int nil).
// The handler converts to *int at the service boundary.
type DownloadCandidatesListSeriesInput struct {
	ID      int64 `path:"id" doc:"TMDB series ID"`
	Season  int   `query:"season" doc:"Optional season number — 0 / omitted means all seasons"`
	Episode int   `query:"episode" doc:"Optional episode number — 0 / omitted means all episodes"`
}

// DownloadCandidatesListSeriesOutput is the flat list of candidates.
type DownloadCandidatesListSeriesOutput struct {
	Body []model.DownloadCandidate
}

// ListSeries searches every configured indexer for download candidates
// matching the series, optionally narrowed to a season / episode.
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

// DownloadCandidatesPreviewMovieInput combines the path id and the shared
// enqueue body for POST /movie/{id}/candidate/preview.
type DownloadCandidatesPreviewMovieInput struct {
	ID   int64 `path:"id" doc:"TMDB movie ID"`
	Body enqueueCandidateBody
}

// DownloadCandidatesPreviewMovieOutput wraps the policy evaluation trace.
type DownloadCandidatesPreviewMovieOutput struct {
	Body model.EvaluationTrace
}

// PreviewMovie evaluates a movie candidate against the policy engine
// without enqueueing. Used by the FE to show a candidate's
// would-pass / would-reject decision before the user commits.
func (h *DownloadCandidates) PreviewMovie(ctx context.Context, input *DownloadCandidatesPreviewMovieInput) (*DownloadCandidatesPreviewMovieOutput, error) {
	trace, err := h.svc.DownloadCandidates.EvaluateCandidate(ctx, input.ID, input.Body.IndexerID, input.Body.GUID)
	if err != nil {
		return nil, err
	}
	return &DownloadCandidatesPreviewMovieOutput{Body: trace}, nil
}

// ----- PreviewSeriesCandidate -----

// DownloadCandidatesPreviewSeriesInput is the path id + shared enqueue body
// for POST /series/{id}/candidate/preview.
type DownloadCandidatesPreviewSeriesInput struct {
	ID   int64 `path:"id" doc:"TMDB series ID"`
	Body enqueueCandidateBody
}

// DownloadCandidatesPreviewSeriesOutput wraps the policy evaluation trace.
type DownloadCandidatesPreviewSeriesOutput struct {
	Body model.EvaluationTrace
}

// PreviewSeries evaluates a series candidate against the policy engine
// without enqueueing. The Season / Episode hints in the body are part of
// the candidate identity but the underlying evaluation call is the same
// signature as the movie version.
func (h *DownloadCandidates) PreviewSeries(ctx context.Context, input *DownloadCandidatesPreviewSeriesInput) (*DownloadCandidatesPreviewSeriesOutput, error) {
	trace, err := h.svc.DownloadCandidates.EvaluateCandidate(ctx, input.ID, input.Body.IndexerID, input.Body.GUID)
	if err != nil {
		return nil, err
	}
	return &DownloadCandidatesPreviewSeriesOutput{Body: trace}, nil
}

// ----- DownloadMovieCandidate -----

// DownloadCandidatesDownloadMovieInput is the path id + shared enqueue body
// for POST /movie/{id}/candidate/download.
type DownloadCandidatesDownloadMovieInput struct {
	ID   int64 `path:"id" doc:"TMDB movie ID"`
	Body enqueueCandidateBody
}

// DownloadCandidatesDownloadMovieOutput wraps DownloadCandidateResponse.
type DownloadCandidatesDownloadMovieOutput struct {
	Body DownloadCandidateResponse
}

// DownloadMovie commits the candidate: runs the policy engine, creates a
// download job, and returns the trace alongside the job. The service
// produces typed errors (NotFound when the movie is unknown, BadGateway when
// Prowlarr fails, etc.).
func (h *DownloadCandidates) DownloadMovie(ctx context.Context, input *DownloadCandidatesDownloadMovieInput) (*DownloadCandidatesDownloadMovieOutput, error) {
	trace, job, err := h.svc.DownloadCandidates.EnqueueCandidate(ctx, input.ID, input.Body.IndexerID, input.Body.GUID)
	if err != nil {
		return nil, err
	}
	return &DownloadCandidatesDownloadMovieOutput{Body: DownloadCandidateResponse{Trace: trace, Job: job}}, nil
}

// ----- DownloadSeriesCandidate -----

// DownloadCandidatesDownloadSeriesInput is the path id + shared enqueue body
// for POST /series/{id}/candidate/download.
type DownloadCandidatesDownloadSeriesInput struct {
	ID   int64 `path:"id" doc:"TMDB series ID"`
	Body enqueueCandidateBody
}

// DownloadCandidatesDownloadSeriesOutput wraps DownloadCandidateResponse.
type DownloadCandidatesDownloadSeriesOutput struct {
	Body DownloadCandidateResponse
}

// DownloadSeries commits a series candidate. Season and Episode in the body
// disambiguate which slice of the series the candidate covers.
func (h *DownloadCandidates) DownloadSeries(ctx context.Context, input *DownloadCandidatesDownloadSeriesInput) (*DownloadCandidatesDownloadSeriesOutput, error) {
	trace, job, err := h.svc.DownloadCandidates.EnqueueSeriesCandidate(ctx, input.ID, input.Body.IndexerID, input.Body.GUID, input.Body.Season, input.Body.Episode)
	if err != nil {
		return nil, err
	}
	return &DownloadCandidatesDownloadSeriesOutput{Body: DownloadCandidateResponse{Trace: trace, Job: job}}, nil
}

// ----- Register -----

// RegisterHumachi wires every download-candidates operation onto the
// humachi API. List ops compose errsRead+errsUpstream; preview/download
// compose errsWrite+errsUpstream because they accept a body subject to
// per-field validation.
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
		Summary:     "Preview policy evaluation for a movie candidate",
		Tags:        []string{"download-candidates"},
		Errors:      errs(errsUpsert, errsUpstream),
	}, h.PreviewMovie)

	huma.Register(api, huma.Operation{
		OperationID: "download-candidates-preview-series",
		Method:      http.MethodPost,
		Path:        "/api/v1/series/{id}/candidate/preview",
		Summary:     "Preview policy evaluation for a series candidate",
		Tags:        []string{"download-candidates"},
		Errors:      errs(errsUpsert, errsUpstream),
	}, h.PreviewSeries)

	huma.Register(api, huma.Operation{
		OperationID: "download-candidates-download-movie",
		Method:      http.MethodPost,
		Path:        "/api/v1/movie/{id}/candidate/download",
		Summary:     "Enqueue a movie download candidate",
		Description: "Runs the policy engine, creates a download job, and returns the trace + job.",
		Tags:        []string{"download-candidates"},
		Errors:      errs(errsUpsert, errsUpstream),
	}, h.DownloadMovie)

	huma.Register(api, huma.Operation{
		OperationID: "download-candidates-download-series",
		Method:      http.MethodPost,
		Path:        "/api/v1/series/{id}/candidate/download",
		Summary:     "Enqueue a series download candidate",
		Description: "Runs the policy engine, creates a download job (with season/episode if supplied), and returns the trace + job.",
		Tags:        []string{"download-candidates"},
		Errors:      errs(errsUpsert, errsUpstream),
	}, h.DownloadSeries)
}
