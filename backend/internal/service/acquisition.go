package service

import (
	"context"
	"fmt"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/indexer"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/qualityprofile"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// AcquisitionService owns the autonomous front-half of acquisition: turning a
// claimed want into a download_job. It searches indexers, picks a release
// (stubbed in Phase 2), routes it, and creates the job — transitioning the want
// to 'grabbed'. The AcquisitionWorker drives it; the existing DownloadWorker
// then picks up the created job.
//
// It bypasses DownloadCandidatesService's interactive guid-cache: the
// autonomous flow searches and enqueues in one pass, so caching the chosen
// SearchResult between calls buys nothing.
type AcquisitionService struct {
	repo    *repo.Repository
	log     *logger.Logger
	source  indexer.IndexerSource
	routing *RoutingService
	quality *QualityProfileService
}

// NewAcquisitionService creates a new acquisition service. source and routingSvc
// are the same instances DownloadCandidatesService holds; qualitySvc supplies the
// gate→score→pick engine keyed off the want's quality_profile_id.
func NewAcquisitionService(r *repo.Repository, l *logger.Logger, source indexer.IndexerSource, routingSvc *RoutingService, qualitySvc *QualityProfileService) *AcquisitionService {
	return &AcquisitionService{
		repo:    r,
		log:     l,
		source:  source,
		routing: routingSvc,
		quality: qualitySvc,
	}
}

// ProcessWant runs the happy path for one claimed want: search → pick → route →
// enqueue, flipping the want to 'grabbed' in a single transaction. It returns
// grabbed=false (with nil error) when the search yields no eligible release, so
// the worker can reschedule rather than fail.
func (s *AcquisitionService) ProcessWant(ctx context.Context, want model.Want) (grabbed bool, err error) {
	mi, err := s.repo.GetMediaItem(ctx, want.MediaItemID)
	if err != nil {
		return false, err
	}

	results, err := s.source.Search(ctx, s.wantToQuery(mi))
	if err != nil {
		return false, apperrors.BadGatewayf("indexer search %q: %v", mi.Title, err).
			Op("AcquisitionService.ProcessWant")
	}

	picked, err := s.pick(ctx, want, mi, results)
	if err != nil {
		return false, err
	}
	if picked == nil {
		// Every candidate was gated out — behaviorally identical to no results.
		// The worker reschedules with backoff.
		return false, nil
	}
	cand := picked.Subject.Release.Candidate

	// Dispatch at grab: every action slot is resolved (rule-set or default). The
	// picked Subject already carries its Media fields from pick.
	actions, _, err := s.routing.Dispatch(ctx, picked.Subject)
	if err != nil {
		return false, err
	}
	downloaderID := *actions.DownloaderID
	libraryID := *actions.LibraryID
	nameTemplateID := *actions.NameTemplateID

	err = s.repo.InTx(ctx, func(r *repo.Repository) error {
		if _, jerr := r.CreateDownloadJob(ctx, repo.CreateDownloadJobParams{
			Protocol:       cand.Protocol,
			MediaType:      "movie",
			MediaItemID:    want.MediaItemID,
			WantID:         want.ID,
			IndexerID:      cand.IndexerID,
			Guid:           cand.GUID,
			CandidateTitle: cand.Title,
			CandidateLink:  cand.Link,
			DownloaderID:   downloaderID,
			LibraryID:      libraryID,
			NameTemplateID: nameTemplateID,
		}); jerr != nil {
			return jerr
		}
		_, serr := r.SetWantStatus(ctx, want.ID, string(model.WantGrabbed))
		return serr
	})
	if err != nil {
		return false, err
	}

	return true, nil
}

// wantToQuery builds the indexer query from the stored media_item (no TMDB
// call — title+year are already persisted). Movie-only; series adds a branch
// here once it lands.
func (s *AcquisitionService) wantToQuery(mi model.MediaItem) indexer.SearchQuery {
	query := mi.Title
	if mi.Year != nil {
		query = fmt.Sprintf("%s %d", mi.Title, *mi.Year)
	}
	return indexer.SearchQuery{
		Query:     query,
		MediaType: indexer.MediaTypeMovie,
		Limit:     100,
	}
}

// pick runs the real selection: build one Subject per search result, then let the
// want's quality profile gate → score → rank → pick the winner. It returns the
// picked Evaluation (nil when every candidate was gated out) after logging the
// per-release decisions. The picked Subject carries its Media fields, so the
// caller can route it directly.
func (s *AcquisitionService) pick(ctx context.Context, want model.Want, mi model.MediaItem, results []indexer.SearchResult) (*qualityprofile.Evaluation, error) {
	year, tmdbID, runtime := mediaFields(mi)
	subjects := make([]model.Subject, 0, len(results))
	var relevanceRejects []relevanceReject
	for _, res := range results {
		cand := searchResultToCandidate(res)
		parsed := parsing.Parse(cand.Title, parsing.DomainMovie)

		// Relevance gate: drop releases whose parsed identity isn't the wanted
		// movie before quality even looks at them. This is the autonomous flow's
		// only identity check — the download is filed onto the want by linkage
		// with no import-time re-match — so a wrong-title release that slips
		// through here would be filed as the movie.
		if reason, ok := relevanceReason(parsed, mi); !ok {
			relevanceRejects = append(relevanceRejects, relevanceReject{title: cand.Title, reason: reason})
			continue
		}

		subjects = append(subjects, model.NewSubject(cand, parsed).
			WithMedia(model.MediaTypeMovie, mi.Title, year, tmdbID, runtime))
	}

	sel, err := s.quality.Pick(ctx, want.QualityProfileID, subjects)
	if err != nil {
		return nil, err
	}
	s.logDecisions(want, sel, relevanceRejects)
	return sel.Picked, nil
}

// relevanceReject records a release dropped by the identity gate, for logging.
type relevanceReject struct {
	title  string
	reason string
}

// relevanceReason reports whether a parsed release identity refers to the same
// movie as the wanted media item, returning a human reason when it does not. A
// parsed title (primary or AKA) must match the wanted title, and the parsed year
// must agree — an absent parsed year (0) passes, since many release names omit
// it. Mirrors Radarr's ParsingService.TryGetMovieBySearchCriteria + the
// DecisionEngine MovieSpecification "Wrong movie" rejection.
func relevanceReason(parsed parsing.ParsedRelease, mi model.MediaItem) (reason string, ok bool) {
	releaseTitles := append([]string{parsed.Identity.Title.Value}, parsed.Identity.AllTitles.Value...)
	titleOK := false
	for _, rt := range releaseTitles {
		if parsing.TitlesMatch(rt, mi.Title) {
			titleOK = true
			break
		}
	}
	if !titleOK {
		return fmt.Sprintf("title %q does not match %q", parsed.Identity.Title.Value, mi.Title), false
	}

	if py := parsed.Identity.Year.Value; py != 0 && mi.Year != nil && py != int(*mi.Year) {
		return fmt.Sprintf("year %d does not match %d", py, *mi.Year), false
	}

	return "", true
}

// mediaFields pulls the movie metadata pick feeds into the engine: year and
// tmdbID for the Subject's Media identity, and the runtime (minutes) that scales
// size bands. A nil runtime (not yet enriched) self-skips the size gate.
func mediaFields(mi model.MediaItem) (year int, tmdbID int64, runtime *int) {
	if mi.Year != nil {
		year = int(*mi.Year)
	}
	if mi.TmdbID != nil {
		tmdbID = *mi.TmdbID
	}
	if mi.Runtime != nil {
		rt := int(*mi.Runtime)
		runtime = &rt
	}
	return year, tmdbID, runtime
}

// binLabel renders a BinKey as a readable "source/resolution/modifier" triple for
// logs — BinKey carries no display Name (that lives in the vocabulary), so the
// identity axes are the log-friendly form.
func binLabel(b parsing.BinKey) string {
	return fmt.Sprintf("%s/%s/%s", b.Source, b.Resolution, b.Modifier)
}

// logDecisions records the selection outcome: the considered count (including
// releases dropped by the relevance gate before quality saw them), the pick (bin
// + score), and the rejection reason for each gated-out candidate. Decisions are
// logged, not persisted — the audit table is a deferred add.
func (s *AcquisitionService) logDecisions(want model.Want, sel qualityprofile.Selection, relevanceRejects []relevanceReject) {
	considered := len(sel.All) + len(relevanceRejects)
	if sel.Picked != nil {
		s.log.Info().
			Str("want_id", want.ID.String()).
			Int("considered", considered).
			Int("relevance_rejected", len(relevanceRejects)).
			Str("title", sel.Picked.Subject.Release.Candidate.Title).
			Str("bin", binLabel(sel.Picked.Bin)).
			Int("score", sel.Picked.Score).
			Msg("acquisition picked release")
	} else {
		s.log.Info().
			Str("want_id", want.ID.String()).
			Int("considered", considered).
			Int("relevance_rejected", len(relevanceRejects)).
			Msg("acquisition picked no release: all candidates gated out")
	}
	for _, rr := range relevanceRejects {
		s.log.Debug().
			Str("want_id", want.ID.String()).
			Str("title", rr.title).
			Str("gate", "relevance").
			Str("reason", rr.reason).
			Msg("acquisition rejected candidate")
	}
	for _, e := range sel.All {
		if e.Disposition == qualityprofile.DispositionRejected {
			s.log.Debug().
				Str("want_id", want.ID.String()).
				Str("title", e.Subject.Release.Candidate.Title).
				Str("gate", e.RejectReason.Gate).
				Str("reason", e.RejectReason.Detail).
				Msg("acquisition rejected candidate")
		}
	}
}
