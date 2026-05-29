//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/matcher"
	"github.com/kyleaupton/arrflix/internal/matcher/resolvers"
	"github.com/kyleaupton/arrflix/internal/metadata"
	"github.com/kyleaupton/arrflix/internal/parsing"
)

// TestScan_ParityCheck_RepresentativeCorpus is the §J sanity pass —
// runs the matcher against ~15 parsing-corpus-shaped synthetic file
// paths and reports the outcome distribution. It's a smoke test (not a
// CI gate), so it tolerates outcomes across bands and only fails if
// the matcher panics or rejects the corpus wholesale. The assertions
// confirm that the cutover hasn't regressed the basics: clean-named
// inputs surface a candidate, gibberish inputs don't, embedded-IDs
// short-circuit.
func TestScan_ParityCheck_RepresentativeCorpus(t *testing.T) {
	t.Parallel()
	prov := &paritySearchProvider{
		items: map[string]*metadata.Item{
			// Path-embed inputs need a validation hit.
			"tmdb:1396":  {Source: metadata.SourceTMDB, ExternalID: "1396", Type: metadata.EntitySeries, Title: "Breaking Bad", Year: 2008},
			"tmdb:1399":  {Source: metadata.SourceTMDB, ExternalID: "1399", Type: metadata.EntitySeries, Title: "Game of Thrones", Year: 2011},
			"tmdb:603":   {Source: metadata.SourceTMDB, ExternalID: "603", Type: metadata.EntityMovie, Title: "The Matrix", Year: 1999},
			"tmdb:27205": {Source: metadata.SourceTMDB, ExternalID: "27205", Type: metadata.EntityMovie, Title: "Inception", Year: 2010},
		},
		// Name-parse search inputs.
		search: map[string][]metadata.Candidate{
			"Breaking Bad":    {{Source: metadata.SourceTMDB, ExternalID: "1396", Type: metadata.EntitySeries, Title: "Breaking Bad", Year: 2008}},
			"Game of Thrones": {{Source: metadata.SourceTMDB, ExternalID: "1399", Type: metadata.EntitySeries, Title: "Game of Thrones", Year: 2011}},
			"The Matrix":      {{Source: metadata.SourceTMDB, ExternalID: "603", Type: metadata.EntityMovie, Title: "The Matrix", Year: 1999}},
			"Inception":       {{Source: metadata.SourceTMDB, ExternalID: "27205", Type: metadata.EntityMovie, Title: "Inception", Year: 2010}},
		},
	}
	svc := matcher.NewMatcherService(
		nil,
		nil, // no repo writes for the parity check
		prov,
		resolvers.DefaultRegistry(prov),
		matcher.DefaultConfig(),
	)

	cases := []struct {
		path   string
		domain parsing.Domain
		hint   string // expected loose band for logging; "" = anything
	}{
		// Path-embed Tier-1 (TRaSH naming) — all confident.
		{"/lib/Breaking Bad {tmdb-1396}/Season 01/Breaking Bad S01E01.mkv", parsing.DomainSeries, "confident"},
		{"/lib/Game of Thrones {tmdb-1399}/Season 02/Game of Thrones S02E01.mkv", parsing.DomainSeries, "confident"},
		{"/lib/Inception (2010) {tmdb-27205}/Inception.mkv", parsing.DomainMovie, "confident"},
		{"/lib/The Matrix (1999) {tmdb-603}/The Matrix.mkv", parsing.DomainMovie, "confident"},

		// Name-parse Tier-3 (clean / scene naming, no embed).
		{"/lib/Breaking Bad/Season 01/Breaking Bad S01E01.mkv", parsing.DomainSeries, ""},
		{"/lib/Game of Thrones/Season 02/Game of Thrones S02E01.mkv", parsing.DomainSeries, ""},
		{"/lib/Inception (2010)/Inception.mkv", parsing.DomainMovie, ""},
		{"/lib/The Matrix (1999)/The Matrix.mkv", parsing.DomainMovie, ""},
		{"/lib/The.Matrix.1999.1080p.BluRay.x264-FGT/The.Matrix.1999.1080p.BluRay.x264-FGT.mkv", parsing.DomainMovie, ""},
		{"/lib/Inception.2010.2160p.WEB-DL.DDP5.1.Atmos.H.265-FLUX/Inception.mkv", parsing.DomainMovie, ""},

		// Stale / unknown embedded ID — provider returns nil, candidate dropped → falls through.
		{"/lib/Phantom {tmdb-999999999}/movie.mkv", parsing.DomainMovie, ""},

		// Scene naming, no clean title (synthetic).
		{"/lib/The.Series.S04E05.HDTV.XviD-LOL/file.mkv", parsing.DomainSeries, ""},
		{"/lib/The.Series.2011.S02E01.WS.PDTV.x264-TLA/file.mkv", parsing.DomainSeries, ""},

		// Gibberish — no match.
		{"/lib/random.mkv", parsing.DomainMovie, "no_match"},
		{"/lib/zzzz999.mkv", parsing.DomainSeries, "no_match"},
	}

	bands := map[matcher.Outcome]int{}
	refs := make([]matcher.FileRef, 0, len(cases))
	for _, c := range cases {
		// Production scan calls Parse on the relative path (library
		// root + the file's rel-path). The /lib/ prefix here stands in
		// for the library root, so we trim it for the parse call.
		rel := strings.TrimPrefix(c.path, "/lib/")
		parsed := parsing.Parse(rel, c.domain, parsing.AsPath())
		refs = append(refs, matcher.FileRef{
			ID:     uuid.New(),
			Path:   c.path,
			Parsed: &parsed,
		})
	}
	outcomes, err := svc.MatchBatch(context.Background(), refs)
	if err != nil {
		t.Fatalf("MatchBatch: %v", err)
	}
	if len(outcomes) != len(cases) {
		t.Fatalf("expected %d outcomes, got %d", len(cases), len(outcomes))
	}
	for i, o := range outcomes {
		bands[o.Outcome]++
		t.Logf("[%2d] %-90s → %s (conf=%.2f)", i, cases[i].path, o.Outcome, o.Confidence)
		if cases[i].hint == "confident" && o.Outcome != matcher.OutcomeConfident {
			t.Errorf("case %d (%q): expected confident, got %s", i, cases[i].path, o.Outcome)
		}
		if cases[i].hint == "no_match" && o.Outcome != matcher.OutcomeNoMatch {
			t.Errorf("case %d (%q): expected no_match, got %s", i, cases[i].path, o.Outcome)
		}
	}
	t.Logf("parity-band distribution: %v", bands)
}

// paritySearchProvider mirrors the unit-test fake but with both
// lookup + search backing maps, keyed for the parity corpus's
// pre-seeded titles.
type paritySearchProvider struct {
	items   map[string]*metadata.Item
	search  map[string][]metadata.Candidate
	resolve map[string]string
}

func (p *paritySearchProvider) LookupByID(_ context.Context, source metadata.ExternalSource, id string) (*metadata.Item, error) {
	if p.items == nil {
		return nil, nil
	}
	return p.items[string(source)+":"+id], nil
}

func (p *paritySearchProvider) Search(_ context.Context, query string, _ metadata.SearchOptions) ([]metadata.Candidate, error) {
	if p.search == nil {
		return nil, nil
	}
	return p.search[query], nil
}

func (p *paritySearchProvider) ResolveCrossProviderID(_ context.Context, src metadata.ExternalSource, id string) (string, error) {
	if p.resolve == nil {
		return "", nil
	}
	return p.resolve[string(src)+":"+id], nil
}
