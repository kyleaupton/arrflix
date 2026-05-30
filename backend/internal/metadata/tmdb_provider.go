package metadata

import (
	"context"
	"errors"
	"strconv"
	"strings"

	tmdb "github.com/cyruzin/golang-tmdb"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

// TmdbClient is the minimum slice of TMDB capability the provider adapter
// needs. The production implementation is `*service.TmdbService` (which
// adds disk-backed caching on top of the raw `*tmdb.Client`); tests can
// satisfy the interface with a hand-rolled fake to avoid the network. The
// seam is local to this package — exporting it here, rather than reaching
// into `service`, keeps the dependency direction one-way (matcher →
// metadata → TMDB).
type TmdbClient interface {
	FindByID(ctx context.Context, id, source string) (tmdb.FindByID, error)
	GetMovieDetails(ctx context.Context, id int64) (tmdb.MovieDetails, error)
	GetSeriesDetails(ctx context.Context, id int64) (tmdb.TVDetails, error)
	MultiSearch(ctx context.Context, query string, page int) (tmdb.SearchMulti, error)
}

// TmdbProvider adapts the existing TMDB integration to MetadataProvider.
// It's the only implementation today; additional providers slot in
// alongside it once the metadata module proper lands.
type TmdbProvider struct {
	client TmdbClient
}

// NewTmdbProvider wires a MetadataProvider around the TMDB client. The
// caller owns the underlying client lifecycle — InitClient calls on a
// shared *TmdbService still take effect through this adapter because the
// adapter holds an interface reference, not a snapshot.
func NewTmdbProvider(client TmdbClient) *TmdbProvider {
	return &TmdbProvider{client: client}
}

// LookupByID validates a (source, id) pair against TMDB. For tmdb-source
// IDs the call routes directly to the right details endpoint; for
// imdb/tvdb IDs it resolves the TMDB ID via FindByID first, then loops
// back. Returns (nil, nil) when the upstream record is missing — callers
// downgrade those candidates to 0 confidence rather than failing.
func (p *TmdbProvider) LookupByID(ctx context.Context, source ExternalSource, id string) (*Item, error) {
	if p.client == nil {
		return nil, ErrProviderUnavailable
	}

	switch source {
	case SourceTMDB:
		return p.lookupTmdb(ctx, id)
	case SourceIMDB, SourceTVDB:
		tmdbID, err := p.ResolveCrossProviderID(ctx, source, id)
		if err != nil {
			return nil, err
		}
		if tmdbID == "" {
			return nil, nil
		}
		item, err := p.lookupTmdb(ctx, tmdbID)
		if err != nil || item == nil {
			return item, err
		}
		// Cross-provider lookups never count as a "redirect" — the input
		// ID was in a different namespace, so the TMDB ID returned is
		// the correct mapping, not a merge target.
		item.Redirected = false
		return item, nil
	default:
		return nil, ErrProviderUnavailable
	}
}

// lookupTmdb attempts movie details first and falls back to series
// details. TMDB doesn't expose a typeless lookup, so we probe both
// endpoints — the first 4xx skips to the next. The redirect detection
// fires when TMDB returns a record under an ID that differs from the
// requested one (rare but real — TMDB silently follows merges).
func (p *TmdbProvider) lookupTmdb(ctx context.Context, id string) (*Item, error) {
	idInt, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, apperrors.Validation("invalid tmdb id",
			apperrors.Field("id", "must be numeric"),
		).Op("TmdbProvider.LookupByID")
	}

	if movie, err := p.client.GetMovieDetails(ctx, idInt); err == nil && movie.ID != 0 {
		yr, _ := splitYear(movie.ReleaseDate)
		returnedID := strconv.FormatInt(movie.ID, 10)
		return &Item{
			Source:     SourceTMDB,
			ExternalID: returnedID,
			Type:       EntityMovie,
			Title:      movie.Title,
			Year:       yr,
			Redirected: returnedID != id,
		}, nil
	} else if err != nil && !isNotFound(err) {
		return nil, classifyUpstream(err)
	}

	if series, err := p.client.GetSeriesDetails(ctx, idInt); err == nil && series.ID != 0 {
		yr, _ := splitYear(series.FirstAirDate)
		returnedID := strconv.FormatInt(series.ID, 10)
		return &Item{
			Source:     SourceTMDB,
			ExternalID: returnedID,
			Type:       EntitySeries,
			Title:      series.Name,
			Year:       yr,
			Redirected: returnedID != id,
		}, nil
	} else if err != nil && !isNotFound(err) {
		return nil, classifyUpstream(err)
	}

	return nil, nil
}

// Search wraps TMDB's multi-search. Page 1 only in v1; the matcher's
// paged-stop-on-confidence behavior (parsing spec) lives one layer up
// in the aggregator. Year and Type biases reorder the result, they
// never filter.
func (p *TmdbProvider) Search(ctx context.Context, query string, opts SearchOptions) ([]Candidate, error) {
	if p.client == nil {
		return nil, ErrProviderUnavailable
	}
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}

	res, err := p.client.MultiSearch(ctx, query, 1)
	if err != nil {
		return nil, classifyUpstream(err)
	}
	if res.SearchMultiResults == nil {
		return nil, nil
	}

	cands := make([]Candidate, 0, len(res.Results))
	for _, r := range res.Results {
		switch r.MediaType {
		case "movie":
			yr, _ := splitYear(r.ReleaseDate)
			cands = append(cands, Candidate{
				Source:     SourceTMDB,
				ExternalID: strconv.FormatInt(r.ID, 10),
				Type:       EntityMovie,
				Title:      r.Title,
				Year:       yr,
				Popularity: float64(r.Popularity),
			})
		case "tv":
			yr, _ := splitYear(r.FirstAirDate)
			cands = append(cands, Candidate{
				Source:     SourceTMDB,
				ExternalID: strconv.FormatInt(r.ID, 10),
				Type:       EntitySeries,
				Title:      r.Name,
				Year:       yr,
				Popularity: float64(r.Popularity),
			})
		default:
			// person results have no media identity — drop silently.
		}
	}

	biasCandidates(cands, opts)
	return cands, nil
}

// ResolveCrossProviderID maps an IMDb/TVDB ID to a TMDB ID via TMDB's
// /find endpoint. Returns "" when no mapping exists; the caller decides
// whether absence is fatal or just a fall-through.
func (p *TmdbProvider) ResolveCrossProviderID(ctx context.Context, fromSource ExternalSource, id string) (string, error) {
	if p.client == nil {
		return "", ErrProviderUnavailable
	}

	// TMDB's external_source parameter takes the same lowercase tokens
	// our ExternalSource enum uses, plus a "_id" suffix.
	var src string
	switch fromSource {
	case SourceIMDB:
		src = "imdb_id"
	case SourceTVDB:
		src = "tvdb_id"
	case SourceTMDB:
		return id, nil
	default:
		return "", nil
	}

	res, err := p.client.FindByID(ctx, id, src)
	if err != nil {
		if isNotFound(err) {
			return "", nil
		}
		return "", classifyUpstream(err)
	}

	// FindByID returns movie_results / tv_results / tv_episode_results;
	// the first hit in either of the first two wins for matcher purposes.
	if len(res.MovieResults) > 0 {
		return strconv.FormatInt(res.MovieResults[0].ID, 10), nil
	}
	if len(res.TvResults) > 0 {
		return strconv.FormatInt(res.TvResults[0].ID, 10), nil
	}
	return "", nil
}

// biasCandidates reorders candidates in place: matching-type first, then
// matching-year first, then popularity-desc. The bias is soft — nothing
// is dropped — matching the spec's "year mismatch is a penalty not a
// filter" stance. We use a stable in-place sort to preserve TMDB's
// original ranking as the tiebreaker.
func biasCandidates(cands []Candidate, opts SearchOptions) {
	if len(cands) <= 1 {
		return
	}
	// Selection-sort by composite score, descending. The n's are small
	// (TMDB page-1 = 20) so an O(n^2) pass keeps things simple and
	// stable.
	score := func(c Candidate) int {
		s := 0
		if opts.Type != nil && c.Type == *opts.Type {
			s += 2
		}
		if opts.Year != nil && c.Year != 0 && c.Year == *opts.Year {
			s += 1
		}
		return s
	}
	for i := 0; i < len(cands)-1; i++ {
		best := i
		for j := i + 1; j < len(cands); j++ {
			if score(cands[j]) > score(cands[best]) {
				best = j
			}
		}
		if best != i {
			cands[i], cands[best] = cands[best], cands[i]
		}
	}
}

// splitYear pulls the four-digit year off the front of a YYYY-MM-DD
// string. TMDB date fields are empty for unreleased items; returning 0
// in that case is the contract Item.Year documents.
func splitYear(date string) (int, bool) {
	if len(date) < 4 {
		return 0, false
	}
	yr, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0, false
	}
	return yr, true
}

// isNotFound recognizes upstream-404 shapes from the TMDB client. The
// underlying client doesn't expose typed errors — we sniff the text. The
// service-layer wrapper turns 404s into KindNotFound via apperrors, but
// the path through getOrFetchFromCache (in service/tmdb.go) wraps even
// 404s as BadGateway, so we also check that shape.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	if apperrors.IsNotFound(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "404") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "could not be found") ||
		// TMDB's "resource not found" status_code surfaces as either
		// "code 34" (bare) or "code: 34" (key:value form) depending on
		// which layer formats the upstream payload.
		strings.Contains(msg, "code 34") ||
		strings.Contains(msg, "code: 34")
}

// classifyUpstream converts an arbitrary TMDB-layer error into the sentinel
// the seam exports. Callers do errors.Is(err, ErrUpstreamUnavailable); we
// preserve the chain so logs still see the original.
func classifyUpstream(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(ErrUpstreamUnavailable, err)
}
