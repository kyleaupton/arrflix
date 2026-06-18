package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/indexer"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/metadata"
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
// enqueue, flipping the want to 'grabbed' in a single transaction. On success it
// returns the grabbed want (grabbed=true) so the worker can emit the transition
// without re-reading. It returns grabbed=false (with nil error) when the search
// yields no eligible release, so the worker can reschedule rather than fail.
func (s *AcquisitionService) ProcessWant(ctx context.Context, want model.Want) (grabbedWant model.Want, grabbed bool, err error) {
	// Early skip: a sibling want processed earlier this sequential tick may have
	// grabbed a season pack that already covers this want (GrabWantsForPack flips
	// every covered sibling to 'grabbed' and links it to the one pack job). Re-read
	// it; if it's already grabbed and attached to a live job, the pack has it — no
	// redundant search, no second job.
	if skip, cur, serr := s.alreadyCoveredByPack(ctx, want); serr != nil {
		return model.Want{}, false, serr
	} else if skip {
		return cur, true, nil
	}

	mi, err := s.repo.GetMediaItem(ctx, want.MediaItemID)
	if err != nil {
		return model.Want{}, false, err
	}

	// IMDb id is best-effort: capable indexers (e.g. IPTorrents) accept it for
	// ID-precise search, and the gate verifies it. Absent pre-enrichment, in
	// which case the query and gate fall back to title+year.
	imdbID, err := s.repo.GetMediaItemExternalID(ctx, mi.ID, string(metadata.SourceIMDB))
	if err != nil {
		return model.Want{}, false, err
	}

	// A want for an episode resolves to its season/episode coordinates; movie
	// wants leave epCtx nil and follow the original path unchanged.
	epCtx, err := s.resolveEpisodeCtx(ctx, want)
	if err != nil {
		return model.Want{}, false, err
	}

	// An episode want loads its in-flight season siblings up front: a season pack
	// is chosen and grabbed against the set of wants it covers. Movie wants have no
	// siblings and skip this.
	var siblings []seasonSibling
	if epCtx != nil {
		siblings, err = s.loadSeasonSiblings(ctx, want, epCtx)
		if err != nil {
			return model.Want{}, false, err
		}
	}

	results, err := s.source.Search(ctx, s.wantToQuery(mi, epCtx))
	if err != nil {
		return model.Want{}, false, apperrors.BadGatewayf("indexer search %q: %v", mi.Title, err).
			Op("AcquisitionService.ProcessWant")
	}

	outcome, err := s.pick(ctx, want, mi, imdbID, epCtx, siblings, results)
	if err != nil {
		return model.Want{}, false, err
	}
	if outcome == nil {
		// Every candidate was gated out — behaviorally identical to no results.
		// The worker reschedules with backoff.
		return model.Want{}, false, nil
	}
	cand := outcome.eval.Subject.Release.Candidate

	// Dispatch at grab: every action slot is resolved (rule-set or default). The
	// picked Subject already carries its Media fields from pick.
	actions, _, err := s.routing.Dispatch(ctx, outcome.eval.Subject)
	if err != nil {
		return model.Want{}, false, err
	}
	job := repo.CreateDownloadJobParams{
		Protocol:       cand.Protocol,
		MediaType:      "movie",
		MediaItemID:    want.MediaItemID,
		IndexerID:      cand.IndexerID,
		Guid:           cand.GUID,
		CandidateTitle: cand.Title,
		CandidateLink:  cand.Link,
		DownloaderID:   *actions.DownloaderID,
		LibraryID:      *actions.LibraryID,
		NameTemplateID: *actions.NameTemplateID,
	}
	// A series want carries season linkage onto the job. A single also carries the
	// episode_id (the import worker's series branch files the one file back to it);
	// a pack leaves episode_id nil and links N wants, fanning out at import time.
	if epCtx != nil {
		job.MediaType = "series"
		job.SeasonID = epCtx.seasonID
		if !outcome.isPack {
			job.EpisodeID = *want.EpisodeID
		}
	}

	if outcome.isPack {
		grabbedWant, grabbed, err = s.grabPack(ctx, want, job, outcome.coveredWantIDs)
	} else {
		grabbedWant, grabbed, err = s.grabSingle(ctx, want, job)
	}
	if err != nil {
		return model.Want{}, false, err
	}

	return grabbedWant, grabbed, nil
}

// grabSingle grabs one want and creates the download_job linked to it, in a single
// transaction. CAS first, job second: GrabWant flips 'searching → grabbed' only
// while the worker still owns the want, holding the row lock through commit so
// concurrent grabbers serialize. A 0-row CAS (ok=false) means the want was
// superseded (the reaper reset it and another worker re-claimed, or a concurrent
// grab won) — a benign no-op. grabbed is set only after the job insert, so an
// insert failure rolls back the whole tx (want stays 'searching').
func (s *AcquisitionService) grabSingle(ctx context.Context, want model.Want, jobParams repo.CreateDownloadJobParams) (grabbedWant model.Want, grabbed bool, err error) {
	err = s.repo.InTx(ctx, func(r *repo.Repository) error {
		gw, ok, gerr := r.GrabWant(ctx, want.ID)
		if gerr != nil {
			return gerr
		}
		if !ok {
			return nil
		}
		job, jerr := r.CreateDownloadJob(ctx, jobParams)
		if jerr != nil {
			return jerr
		}
		if lerr := r.LinkDownloadJobWant(ctx, job.ID, want.ID); lerr != nil {
			return lerr
		}
		grabbedWant = gw
		grabbed = true
		return nil
	})
	return grabbedWant, grabbed, err
}

// grabPack grabs a season pack covering several in-flight wants in one transaction:
// find-or-create the job by (indexer_id, guid) over in-flight jobs (so converging
// sibling grabs reuse one job, made idempotent by the partial UNIQUE(indexer_id,
// guid) key), batch-grab the covered wants, and link only the wants the CAS
// actually claimed. The processed want's grabbed row is returned (grabbed=true)
// when the pack claimed it; if a concurrent grab beat us to it, grabbed=false is a
// benign no-op, exactly as in the single path.
func (s *AcquisitionService) grabPack(ctx context.Context, want model.Want, jobParams repo.CreateDownloadJobParams, coveredWantIDs []uuid.UUID) (grabbedWant model.Want, grabbed bool, err error) {
	err = s.repo.InTx(ctx, func(r *repo.Repository) error {
		job, found, ferr := r.FindActiveDownloadJobByGuid(ctx, jobParams.IndexerID, jobParams.Guid)
		if ferr != nil {
			return ferr
		}
		if !found {
			job, ferr = r.CreateDownloadJob(ctx, jobParams)
			if ferr != nil {
				return ferr
			}
		}

		claimed, gerr := r.GrabWantsForPack(ctx, coveredWantIDs)
		if gerr != nil {
			return gerr
		}
		for _, cw := range claimed {
			if lerr := r.LinkDownloadJobWant(ctx, job.ID, cw.ID); lerr != nil {
				return lerr
			}
			if cw.ID == want.ID {
				grabbedWant = cw
				grabbed = true
			}
		}
		return nil
	})
	return grabbedWant, grabbed, err
}

// alreadyCoveredByPack reports whether this want was already grabbed by a sibling's
// season-pack grab earlier in the tick: it re-reads the want and confirms it's
// 'grabbed' with a live linked job. Returns the current want row so the caller can
// emit the (already-)grabbed transition without re-reading. Only episode wants can
// be pack-covered; a movie is single-atom (no sibling pack), so its supersession is
// left to the grab CAS — the dedup contract the movie path relies on.
func (s *AcquisitionService) alreadyCoveredByPack(ctx context.Context, want model.Want) (bool, model.Want, error) {
	if want.EpisodeID == nil {
		return false, model.Want{}, nil
	}
	cur, err := s.repo.GetWant(ctx, want.ID)
	if err != nil {
		return false, model.Want{}, err
	}
	if cur.Status != string(model.WantGrabbed) {
		return false, model.Want{}, nil
	}
	jobs, err := s.repo.ListActiveDownloadJobsByWant(ctx, cur.ID)
	if err != nil {
		return false, model.Want{}, err
	}
	return len(jobs) > 0, cur, nil
}

// loadSeasonSiblings resolves the in-flight episode wants of the processed want's
// tracking+season into (want, episode-number) pairs — the candidate set a season
// pack's coverage is computed against. The processed want is itself one of them.
func (s *AcquisitionService) loadSeasonSiblings(ctx context.Context, want model.Want, epCtx *episodeCtx) ([]seasonSibling, error) {
	wants, err := s.repo.ListInFlightWantsForTrackingSeason(ctx, want.TrackingID, epCtx.seasonID)
	if err != nil {
		return nil, err
	}
	out := make([]seasonSibling, 0, len(wants))
	for _, w := range wants {
		if w.EpisodeID == nil {
			continue
		}
		ep, err := s.repo.GetEpisode(ctx, *w.EpisodeID)
		if err != nil {
			return nil, err
		}
		out = append(out, seasonSibling{want: w, episode: int(ep.EpisodeNumber)})
	}
	return out, nil
}

// episodeCtx carries the coordinates a series want resolves to. It is nil for
// movie wants. season/episode are the numbers that build the search query and
// gate the parsed release; seasonID identifies the season row the download_job
// links to; title/runtime come from the stored episode row — the Phase 3
// structure sync already persisted them, so no per-episode TMDB call is needed
// (unlike the manual path's GetEpisodeDetails) — and feed the Subject's series
// info and the size-band runtime.
type episodeCtx struct {
	seasonID uuid.UUID
	season   int
	episode  int
	title    *string
	runtime  *int
}

// resolveEpisodeCtx loads the season/episode coordinates for an episode want,
// returning nil for movie wants (EpisodeID unset). Repo errors pass through.
func (s *AcquisitionService) resolveEpisodeCtx(ctx context.Context, want model.Want) (*episodeCtx, error) {
	if want.EpisodeID == nil {
		return nil, nil
	}
	ep, err := s.repo.GetEpisode(ctx, *want.EpisodeID)
	if err != nil {
		return nil, err
	}
	season, err := s.repo.GetSeason(ctx, ep.SeasonID)
	if err != nil {
		return nil, err
	}
	var runtime *int
	if ep.Runtime != nil {
		rt := int(*ep.Runtime)
		runtime = &rt
	}
	return &episodeCtx{
		seasonID: season.ID,
		season:   int(season.SeasonNumber),
		episode:  int(ep.EpisodeNumber),
		title:    ep.Title,
		runtime:  runtime,
	}, nil
}

// wantToQuery builds the indexer query from the stored media_item (no TMDB
// call — title+year are already persisted). The search is free-text across all
// indexers; identity precision comes from the relevance gate matching on the
// ids Prowlarr echoes per result, not from embedding ID tokens (which would
// drop text-only indexers — see the prowlarr adapter). An episode want builds a
// "<title> S%02dE%02d" tvsearch query (mirroring SearchSeriesDownloadCandidates);
// the prowlarr adapter drives off the query string + type, so the Season/Episode
// fields ride along unused, exactly as the manual path relies on today.
func (s *AcquisitionService) wantToQuery(mi model.MediaItem, ep *episodeCtx) indexer.SearchQuery {
	if ep != nil {
		season, episode := ep.season, ep.episode
		return indexer.SearchQuery{
			Query:     fmt.Sprintf("%s S%02dE%02d", mi.Title, ep.season, ep.episode),
			MediaType: indexer.MediaTypeSeries,
			Season:    &season,
			Episode:   &episode,
			Limit:     100,
		}
	}

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

// seasonSibling pairs an in-flight episode want with its episode number, the unit
// the coverage computation works over: a pack's covered want set is the subset of
// siblings whose episode the pack carries.
type seasonSibling struct {
	want    model.Want
	episode int
}

// pickOutcome is the front-half's selection result: the chosen Evaluation plus
// whether it's a pack and which in-flight wants its grab claims (just the processed
// want for a single; the covered siblings for a pack).
type pickOutcome struct {
	eval           *qualityprofile.Evaluation
	isPack         bool
	coveredWantIDs []uuid.UUID
}

// pick runs the real selection. It builds one Subject per relevance-passing search
// result, partitions them into singles and packs, lets the want's quality profile
// gate → score → rank → pick a winner in each group, then resolves the two with a
// coverage-first policy (choosePick). Returns nil when every candidate was gated
// out. The picked Subject carries its Media fields, so the caller can route it.
func (s *AcquisitionService) pick(ctx context.Context, want model.Want, mi model.MediaItem, imdbID *string, ep *episodeCtx, siblings []seasonSibling, results []indexer.SearchResult) (*pickOutcome, error) {
	year, tmdbID, runtime := mediaFields(mi)
	domain := parsing.DomainMovie
	if ep != nil {
		// Series releases parse under the Sonarr patterns; the per-episode
		// runtime (not the series-level mi.Runtime) scales the size bands.
		domain = parsing.DomainSeries
		runtime = ep.runtime
	}

	var singleSubjects, packSubjects []model.Subject
	var relevanceRejects []relevanceReject
	for _, res := range results {
		cand := searchResultToCandidate(res)
		parsed := parsing.Parse(cand.Title, domain)

		// Relevance gate: drop releases that aren't the wanted media before
		// quality even looks at them. This is the autonomous flow's only
		// identity check — the download is filed onto the want by linkage with
		// no import-time re-match — so a wrong release that slips through here
		// would be filed as the movie/episode.
		kind, reason := relevanceReason(res, parsed, mi, imdbID, ep)
		if kind == releaseReject {
			relevanceRejects = append(relevanceRejects, relevanceReject{title: cand.Title, reason: reason})
			continue
		}

		subject := model.NewSubject(cand, parsed)
		if ep != nil {
			subject = subject.
				WithMedia(model.MediaTypeSeries, mi.Title, year, tmdbID, runtime).
				WithSeriesInfo(&ep.season, &ep.episode, ep.title)
		} else {
			subject = subject.WithMedia(model.MediaTypeMovie, mi.Title, year, tmdbID, runtime)
		}
		if kind == releasePack {
			packSubjects = append(packSubjects, subject)
		} else {
			singleSubjects = append(singleSubjects, subject)
		}
	}

	bestSingle, singleSel, err := s.pickGroup(ctx, want.QualityProfileID, singleSubjects)
	if err != nil {
		return nil, err
	}
	bestPack, packSel, err := s.pickGroup(ctx, want.QualityProfileID, packSubjects)
	if err != nil {
		return nil, err
	}
	s.logDecisions(want, singleSel, packSel, relevanceRejects)

	var packCovered []uuid.UUID
	if bestPack != nil {
		packCovered = coveredWantIDs(bestPack, siblings)
	}
	return choosePick(want.ID, bestSingle, bestPack, packCovered), nil
}

// pickGroup runs the gate→score→pick engine over one group of subjects (singles or
// packs), returning the winner (nil when the group is empty or fully gated out)
// alongside the full Selection for logging.
func (s *AcquisitionService) pickGroup(ctx context.Context, profileID uuid.UUID, subjects []model.Subject) (*qualityprofile.Evaluation, qualityprofile.Selection, error) {
	if len(subjects) == 0 {
		return nil, qualityprofile.Selection{}, nil
	}
	sel, err := s.quality.Pick(ctx, profileID, subjects)
	if err != nil {
		return nil, qualityprofile.Selection{}, err
	}
	return sel.Picked, sel, nil
}

// coveredWantIDs is the subset of in-flight siblings a pack's grab claims: every
// sibling for a full-season pack, or those whose episode falls in the pack's range.
// The processed want is itself a sibling, so it's included whenever the pack covers
// it.
func coveredWantIDs(pack *qualityprofile.Evaluation, siblings []seasonSibling) []uuid.UUID {
	id := pack.Subject.Release.Identity
	var ids []uuid.UUID
	for _, sib := range siblings {
		if id.FullSeason.Value || containsInt(id.EpisodeNumbers.Value, sib.episode) {
			ids = append(ids, sib.want.ID)
		}
	}
	return ids
}

// choosePick resolves the single and pack winners with the coverage-first, ≥2-guard
// policy: a pack is chosen only when it covers at least two in-flight wants (one
// grab standing in for ≥2 single grabs); otherwise the single wins; a lone-coverage
// pack is the fallback when no single qualifies. Returns nil when neither group
// produced a winner. Pure — exercised directly by the guard unit test.
func choosePick(wantID uuid.UUID, bestSingle, bestPack *qualityprofile.Evaluation, packCovered []uuid.UUID) *pickOutcome {
	if bestPack != nil && len(packCovered) >= 2 {
		return &pickOutcome{eval: bestPack, isPack: true, coveredWantIDs: packCovered}
	}
	if bestSingle != nil {
		return &pickOutcome{eval: bestSingle, isPack: false, coveredWantIDs: []uuid.UUID{wantID}}
	}
	if bestPack != nil {
		return &pickOutcome{eval: bestPack, isPack: true, coveredWantIDs: packCovered}
	}
	return nil
}

// relevanceReject records a release dropped by the identity gate, for logging.
type relevanceReject struct {
	title  string
	reason string
}

// releaseKind classifies a relevance-passing release by how it satisfies an
// episode want: a single covers exactly the wanted episode, a pack (full-season or
// a multi-episode range covering it) covers several in-flight siblings at once.
// releaseReject means the release isn't the want's at all.
type releaseKind int

const (
	releaseReject releaseKind = iota
	releaseSingle
	releasePack
)

// relevanceReason classifies a search result against the want, returning its kind
// and a human reason when rejected. It composes the series/movie identity match
// (always) with the episode classifier (episode wants only): a movie that passes
// identity is always a single; a series result must be the right show and then
// classifies as single/pack/reject by its numbering.
func relevanceReason(res indexer.SearchResult, parsed parsing.ParsedRelease, mi model.MediaItem, imdbID *string, ep *episodeCtx) (kind releaseKind, reason string) {
	if reason, ok := identityReason(res, parsed, mi, imdbID); !ok {
		return releaseReject, reason
	}
	if ep == nil {
		return releaseSingle, ""
	}
	return classifyEpisodeRelease(parsed, ep)
}

// classifyEpisodeRelease decides whether a series release covers the wanted
// episode as a single or as a pack. It accepts full-season packs and
// multi-episode ranges that include the wanted episode (the coverage-first season
// catch-up), and rejects multi-season packs, partial-season packs, non-standard
// numbering (anime absolute / daily), the wrong season, and any single/range that
// doesn't land on the wanted episode. Pure and total — drives the front-half gate
// and is exercised directly by the unit table test.
func classifyEpisodeRelease(parsed parsing.ParsedRelease, ep *episodeCtx) (kind releaseKind, reason string) {
	n := parsed.Identity.Numbering
	if n.Kind != parsing.NumberingSeasonEpisode {
		return releaseReject, fmt.Sprintf("numbering %q is not standard season/episode", n.Kind)
	}
	if n.IsMultiSeason.Value {
		return releaseReject, "multi-season pack"
	}
	if n.IsPartialSeason.Value {
		return releaseReject, "partial-season pack"
	}
	if n.Season.Value != ep.season {
		return releaseReject, fmt.Sprintf("season %d≠%d", n.Season.Value, ep.season)
	}
	eps := n.EpisodeNumbers.Value
	if n.FullSeason.Value {
		return releasePack, ""
	}
	if len(eps) == 1 && eps[0] == ep.episode {
		return releaseSingle, ""
	}
	if len(eps) > 1 && containsInt(eps, ep.episode) {
		return releasePack, ""
	}
	return releaseReject, fmt.Sprintf("release does not cover wanted episode %d", ep.episode)
}

// containsInt reports whether n is in xs.
func containsInt(xs []int, n int) bool {
	for _, x := range xs {
		if x == n {
			return true
		}
	}
	return false
}

// identityReason reports whether a search result refers to the same work as the
// wanted media item, returning a human reason when it does not. It is
// ID-preferred, mirroring Radarr's ParsingService.FindMovie priority: a result
// carrying a stable id is decided by that id alone (never falling through to the
// fuzzier title check); only an id-less result — the text-only-indexer case —
// is judged by parsed title + year.
//
//   - TMDb id present: accept iff it equals the want's tmdb id, else reject.
//   - else IMDb id present and the want has an imdb id: compare numerically
//     (the want id's "tt" prefix stripped), accept on match, reject on mismatch.
//   - else: parsed title (primary or AKA) must match the wanted title and the
//     parsed year must agree — an absent parsed year (0) passes, since many
//     release names omit it (series releases rarely carry a year).
func identityReason(res indexer.SearchResult, parsed parsing.ParsedRelease, mi model.MediaItem, imdbID *string) (reason string, ok bool) {
	// Only decide on tmdb id when the want actually has one to compare against.
	// A want with no tmdb id (wantTmdb would be 0) must fall through to the imdb
	// and title checks, not reject every id-carrying result as "does not match 0".
	if res.TmdbID != 0 && mi.TmdbID != nil {
		if res.TmdbID == *mi.TmdbID {
			return "", true
		}
		return fmt.Sprintf("tmdbId %d does not match %d", res.TmdbID, *mi.TmdbID), false
	}

	if res.ImdbID != 0 && imdbID != nil {
		if want := imdbNumeric(*imdbID); want != 0 {
			if res.ImdbID == want {
				return "", true
			}
			return fmt.Sprintf("imdbId %d does not match %d", res.ImdbID, want), false
		}
	}

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

// imdbNumeric parses the numeric form of an IMDb id ("tt0137523" → 137523) for
// comparison against the integer ids Prowlarr echoes. Returns 0 when the value
// holds no digits, which the caller treats as "no usable id".
func imdbNumeric(id string) int64 {
	n := int64(0)
	for _, c := range id {
		if c >= '0' && c <= '9' {
			n = n*10 + int64(c-'0')
		}
	}
	return n
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

// logDecisions records the selection outcome across both groups: the considered
// count (including releases dropped by the relevance gate before quality saw them),
// each group's pick (bin + score), and the rejection reason for every gated-out
// candidate. Decisions are logged, not persisted — the audit table is a deferred
// add.
func (s *AcquisitionService) logDecisions(want model.Want, singleSel, packSel qualityprofile.Selection, relevanceRejects []relevanceReject) {
	considered := len(singleSel.All) + len(packSel.All) + len(relevanceRejects)
	s.logGroupPick(want, "single", singleSel.Picked, considered, len(relevanceRejects))
	s.logGroupPick(want, "pack", packSel.Picked, considered, len(relevanceRejects))

	for _, rr := range relevanceRejects {
		s.log.Debug().
			Str("want_id", want.ID.String()).
			Str("title", rr.title).
			Str("gate", "relevance").
			Str("reason", rr.reason).
			Msg("acquisition rejected candidate")
	}
	for _, sel := range []qualityprofile.Selection{singleSel, packSel} {
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
}

// logGroupPick logs the winner of one group (single or pack), or nothing when the
// group produced no pick — the chosen-between-groups decision is implicit in the
// grab that follows.
func (s *AcquisitionService) logGroupPick(want model.Want, group string, picked *qualityprofile.Evaluation, considered, relevanceRejected int) {
	if picked == nil {
		return
	}
	s.log.Info().
		Str("want_id", want.ID.String()).
		Str("group", group).
		Int("considered", considered).
		Int("relevance_rejected", relevanceRejected).
		Str("title", picked.Subject.Release.Candidate.Title).
		Str("bin", binLabel(picked.Bin)).
		Int("score", picked.Score).
		Msg("acquisition picked release")
}
