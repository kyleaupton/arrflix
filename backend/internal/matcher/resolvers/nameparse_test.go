package resolvers

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/matcher"
	"github.com/kyleaupton/arrflix/internal/metadata"
	"github.com/kyleaupton/arrflix/internal/parsing"
)

// fakeMetadataProvider returns canned Search/LookupByID responses. Only
// Search is exercised by the name-parse tests; the other methods are
// stubs so the type still satisfies the interface.
type fakeMetadataProvider struct {
	searchResults []metadata.Candidate
	searchErr     error
	searchCalls   int

	lastQuery string
	lastOpts  metadata.SearchOptions
}

func (f *fakeMetadataProvider) LookupByID(_ context.Context, _ metadata.ExternalSource, _ string) (*metadata.Item, error) {
	return nil, nil
}

func (f *fakeMetadataProvider) Search(_ context.Context, query string, opts metadata.SearchOptions) ([]metadata.Candidate, error) {
	f.searchCalls++
	f.lastQuery = query
	f.lastOpts = opts
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.searchResults, nil
}

func (f *fakeMetadataProvider) ResolveCrossProviderID(_ context.Context, _ metadata.ExternalSource, _ string) (string, error) {
	return "", nil
}

func parsedWithIdentity(title string, year int, typeHint string, titleConf float64) *parsing.ParsedRelease {
	return &parsing.ParsedRelease{
		Identity: parsing.IdentityAttrs{
			Title:    parsing.Field[string]{Value: title, Confidence: titleConf},
			Year:     parsing.Field[int]{Value: year, Confidence: 1.0},
			TypeHint: parsing.Field[string]{Value: typeHint, Confidence: 1.0},
		},
	}
}

func parsedSeries(title string, season int, episodes []int, titleConf float64) *parsing.ParsedRelease {
	return &parsing.ParsedRelease{
		Identity: parsing.IdentityAttrs{
			Title:    parsing.Field[string]{Value: title, Confidence: titleConf},
			TypeHint: parsing.Field[string]{Value: "series", Confidence: 1.0},
			Numbering: parsing.Numbering{
				Kind:           parsing.NumberingSeasonEpisode,
				Season:         parsing.Field[int]{Value: season, Confidence: 1.0},
				EpisodeNumbers: parsing.Field[[]int]{Value: episodes, Confidence: 1.0},
			},
		},
	}
}

func approxEq(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestNameParse_UnavailableWhenNoProvider(t *testing.T) {
	t.Parallel()
	np := NewNameParse(nil)
	if np.Available(context.Background()) {
		t.Fatal("expected Available()=false with nil provider")
	}
}

func TestNameParse_NoParse_ZeroCandidates(t *testing.T) {
	t.Parallel()
	prov := &fakeMetadataProvider{}
	np := NewNameParse(prov)
	res, err := np.Resolve(context.Background(), matcher.FileRef{ID: uuid.New()})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Candidates) != 0 {
		t.Fatalf("expected 0 candidates when Parsed is nil, got %d", len(res.Candidates))
	}
	if prov.searchCalls != 0 {
		t.Fatalf("expected no provider Search calls, got %d", prov.searchCalls)
	}
}

func TestNameParse_EmptyTitle_ZeroCandidates(t *testing.T) {
	t.Parallel()
	prov := &fakeMetadataProvider{}
	np := NewNameParse(prov)
	res, err := np.Resolve(context.Background(), matcher.FileRef{
		ID:     uuid.New(),
		Parsed: parsedWithIdentity("   ", 2020, "movie", 1.0),
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Candidates) != 0 {
		t.Fatalf("expected 0 candidates on empty title, got %d", len(res.Candidates))
	}
}

func TestNameParse_MovieWithYear_UniqueResult_TopScore(t *testing.T) {
	t.Parallel()
	prov := &fakeMetadataProvider{
		searchResults: []metadata.Candidate{
			{Source: metadata.SourceTMDB, ExternalID: "27205", Type: metadata.EntityMovie, Title: "Inception", Year: 2010},
		},
	}
	np := NewNameParse(prov)
	res, err := np.Resolve(context.Background(), matcher.FileRef{
		ID:     uuid.New(),
		Parsed: parsedWithIdentity("Inception", 2010, "movie", 1.0),
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(res.Candidates))
	}
	c := res.Candidates[0]
	// base 0.50 + unique 0.10 + year 0.15 + exact-title 0.10 - 0*rank = 0.85, capped at 0.85
	if !approxEq(c.Confidence, 0.85) {
		t.Fatalf("expected confidence 0.85 (capped), got %v", c.Confidence)
	}
	if c.Ref.ExternalID != "27205" {
		t.Fatalf("expected tmdb:27205, got %s:%s", c.Ref.Source, c.Ref.ExternalID)
	}
	// year is propagated as a soft search bias
	if prov.lastOpts.Year == nil || *prov.lastOpts.Year != 2010 {
		t.Fatalf("expected Year=2010 search bias, got %+v", prov.lastOpts.Year)
	}
	if prov.lastOpts.Type == nil || *prov.lastOpts.Type != metadata.EntityMovie {
		t.Fatalf("expected Type=movie search bias, got %+v", prov.lastOpts.Type)
	}
}

func TestNameParse_MovieWithoutYear_MultipleResults_DecayThroughList(t *testing.T) {
	t.Parallel()
	prov := &fakeMetadataProvider{
		searchResults: []metadata.Candidate{
			{Source: metadata.SourceTMDB, ExternalID: "1", Type: metadata.EntityMovie, Title: "Heat", Year: 1995},
			{Source: metadata.SourceTMDB, ExternalID: "2", Type: metadata.EntityMovie, Title: "Heat", Year: 1986},
			{Source: metadata.SourceTMDB, ExternalID: "3", Type: metadata.EntityMovie, Title: "Heat", Year: 2019},
		},
	}
	np := NewNameParse(prov)
	res, err := np.Resolve(context.Background(), matcher.FileRef{
		ID:     uuid.New(),
		Parsed: parsedWithIdentity("Heat", 0, "movie", 1.0),
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(res.Candidates))
	}
	// rank-0: base 0.50 + title-exact 0.10 = 0.60 (no unique, no year)
	if !approxEq(res.Candidates[0].Confidence, 0.60) {
		t.Fatalf("expected top confidence 0.60, got %v", res.Candidates[0].Confidence)
	}
	// rank-1: 0.60 - 0.10 = 0.50
	if !approxEq(res.Candidates[1].Confidence, 0.50) {
		t.Fatalf("expected rank-1 confidence 0.50, got %v", res.Candidates[1].Confidence)
	}
	// rank-2: 0.60 - 0.20 = 0.40
	if !approxEq(res.Candidates[2].Confidence, 0.40) {
		t.Fatalf("expected rank-2 confidence 0.40, got %v", res.Candidates[2].Confidence)
	}
}

func TestNameParse_SeriesEpisodeAttached(t *testing.T) {
	t.Parallel()
	prov := &fakeMetadataProvider{
		searchResults: []metadata.Candidate{
			{Source: metadata.SourceTMDB, ExternalID: "1396", Type: metadata.EntitySeries, Title: "Breaking Bad", Year: 2008},
		},
	}
	np := NewNameParse(prov)
	res, err := np.Resolve(context.Background(), matcher.FileRef{
		ID:     uuid.New(),
		Parsed: parsedSeries("Breaking Bad", 1, []int{3}, 1.0),
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(res.Candidates))
	}
	ep := res.Candidates[0].Episode
	if ep == nil || ep.Season != 1 || ep.Episode != 3 {
		t.Fatalf("expected S01E03, got %+v", ep)
	}
}

func TestNameParse_YearHintMismatch_DoesNotPreFilter(t *testing.T) {
	t.Parallel()
	// Parser saw year 2010; provider returns a 2019 record. The
	// aggregator's soft penalty is what handles the disagreement —
	// name-parse just surfaces the candidate.
	prov := &fakeMetadataProvider{
		searchResults: []metadata.Candidate{
			{Source: metadata.SourceTMDB, ExternalID: "9", Type: metadata.EntityMovie, Title: "X", Year: 2019},
		},
	}
	np := NewNameParse(prov)
	res, err := np.Resolve(context.Background(), matcher.FileRef{
		ID:     uuid.New(),
		Parsed: parsedWithIdentity("X", 2010, "movie", 1.0),
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("expected 1 candidate (no pre-filter), got %d", len(res.Candidates))
	}
	// base 0.50 + unique 0.10 + (no year match) + title-exact 0.10 = 0.70
	if !approxEq(res.Candidates[0].Confidence, 0.70) {
		t.Fatalf("expected confidence 0.70 (no year bonus), got %v", res.Candidates[0].Confidence)
	}
}

func TestNameParse_TitleConfidencePropagates(t *testing.T) {
	t.Parallel()
	// Parser was only 0.5-confident in the title. Score should halve.
	prov := &fakeMetadataProvider{
		searchResults: []metadata.Candidate{
			{Source: metadata.SourceTMDB, ExternalID: "1", Type: metadata.EntityMovie, Title: "X", Year: 2020},
		},
	}
	np := NewNameParse(prov)
	res, err := np.Resolve(context.Background(), matcher.FileRef{
		ID:     uuid.New(),
		Parsed: parsedWithIdentity("X", 2020, "movie", 0.5),
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// (0.50 + 0.10 + 0.15 + 0.10) * 0.5 = 0.425, well below cap
	if !approxEq(res.Candidates[0].Confidence, 0.425) {
		t.Fatalf("expected 0.425, got %v", res.Candidates[0].Confidence)
	}
}

func TestNameParse_TitleEquivalence_IgnoresPunctuation(t *testing.T) {
	t.Parallel()
	// "Wall-E" vs "WALL E" — punctuation/case differ, alphanumeric core matches.
	prov := &fakeMetadataProvider{
		searchResults: []metadata.Candidate{
			{Source: metadata.SourceTMDB, ExternalID: "10681", Type: metadata.EntityMovie, Title: "WALL-E", Year: 2008},
		},
	}
	np := NewNameParse(prov)
	res, err := np.Resolve(context.Background(), matcher.FileRef{
		ID:     uuid.New(),
		Parsed: parsedWithIdentity("Wall E", 2008, "movie", 1.0),
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// 0.50 + 0.10 unique + 0.15 year + 0.10 title-exact = 0.85 (cap)
	if !approxEq(res.Candidates[0].Confidence, 0.85) {
		t.Fatalf("expected 0.85, got %v", res.Candidates[0].Confidence)
	}
}

func TestNameParse_ProviderError_Propagates(t *testing.T) {
	t.Parallel()
	prov := &fakeMetadataProvider{searchErr: errors.New("upstream boom")}
	np := NewNameParse(prov)
	_, err := np.Resolve(context.Background(), matcher.FileRef{
		ID:     uuid.New(),
		Parsed: parsedWithIdentity("X", 0, "movie", 1.0),
	})
	if err == nil {
		t.Fatal("expected error from provider, got nil")
	}
}

// Compile-time check.
var _ matcher.Resolver = (*NameParse)(nil)
