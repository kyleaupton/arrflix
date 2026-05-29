package matcher

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/metadata"
	"github.com/kyleaupton/arrflix/internal/parsing"
)

// fakeResolver is a test double for Resolver. Tests configure the
// candidates + evidence + tier it returns; the available flag controls
// whether the aggregator skips it.
type fakeResolver struct {
	name       string
	tier       Tier
	available  bool
	candidates []Candidate
	evidence   json.RawMessage
	err        error
}

func (f *fakeResolver) Name() string                     { return f.name }
func (f *fakeResolver) Tier() Tier                       { return f.tier }
func (f *fakeResolver) Available(_ context.Context) bool { return f.available }
func (f *fakeResolver) Resolve(_ context.Context, _ FileRef) (ResolverResult, error) {
	return ResolverResult{Candidates: f.candidates, Evidence: f.evidence}, f.err
}

// fakeProvider implements metadata.MetadataProvider with per-ID
// canned responses. Tests populate items[id] for the validation path;
// missing entries return (nil, nil), modelling a 404.
type fakeProvider struct {
	items map[string]*metadata.Item
}

func (f *fakeProvider) LookupByID(_ context.Context, source metadata.ExternalSource, id string) (*metadata.Item, error) {
	if f.items == nil {
		return nil, nil
	}
	key := string(source) + ":" + id
	return f.items[key], nil
}

func (f *fakeProvider) Search(_ context.Context, _ string, _ metadata.SearchOptions) ([]metadata.Candidate, error) {
	return nil, nil
}

func (f *fakeProvider) ResolveCrossProviderID(_ context.Context, _ metadata.ExternalSource, _ string) (string, error) {
	return "", nil
}

// fakeRepo records writes for assertion. Thread-safe so a parallel test
// running multiple aggregations can read without the race detector
// firing.
type fakeRepo struct {
	mu        sync.Mutex
	records   []MatchOutcomeRecord
	supersede []supersedeCall
	nextID    int64
}

type supersedeCall struct {
	fileID        uuid.UUID
	supersedingID int64
}

func (r *fakeRepo) InsertMatchDecision(_ context.Context, rec MatchOutcomeRecord) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	r.records = append(r.records, rec)
	return r.nextID, nil
}

func (r *fakeRepo) SupersedeMatchDecision(_ context.Context, fileID uuid.UUID, supersedingID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.supersede = append(r.supersede, supersedeCall{fileID: fileID, supersedingID: supersedingID})
	return nil
}

// candidateOf builds a single-candidate slice in the most common test
// shape (TMDB-sourced movie).
func candidateOf(extID string, confidence float64) []Candidate {
	return []Candidate{{
		Ref:        ExternalRef{Source: metadata.SourceTMDB, ExternalID: extID},
		Confidence: confidence,
	}}
}

func registryWith(resolvers ...Resolver) *Registry {
	r := NewRegistry()
	for _, res := range resolvers {
		r.Register(res)
	}
	return r
}

// presetForTest returns the recommended preset — the band thresholds
// the tests assert against (Auto=0.85, ReviewLow=0.7, LowMin=0.5).
func presetForTest() Config {
	return Config{Thresholds: PresetRecommended}
}

func TestAggregator_NoCandidates_NoMatch(t *testing.T) {
	t.Parallel()
	reg := registryWith(&fakeResolver{name: "test", tier: Tier1, available: true})
	agg := NewAggregator(nil, &fakeProvider{}, reg, presetForTest())

	rec := agg.Aggregate(context.Background(), FileRef{ID: uuid.New()})

	if rec.Outcome != OutcomeNoMatch {
		t.Fatalf("expected no_match, got %s", rec.Outcome)
	}
	if rec.ChosenRef != nil {
		t.Fatalf("expected nil chosen ref, got %+v", rec.ChosenRef)
	}
	if rec.Confidence != 0 {
		t.Fatalf("expected confidence 0, got %v", rec.Confidence)
	}
}

func TestAggregator_ConfidentBand(t *testing.T) {
	t.Parallel()
	// Tier-1 candidate at 0.97; after validation (× 0.99) it lands at
	// ~0.96, well above Auto=0.85.
	reg := registryWith(&fakeResolver{
		name: "path-embed", tier: Tier1, available: true,
		candidates: candidateOf("1234", 0.97),
	})
	prov := &fakeProvider{items: map[string]*metadata.Item{
		"tmdb:1234": {Source: metadata.SourceTMDB, ExternalID: "1234", Type: metadata.EntityMovie, Title: "X", Year: 2020},
	}}
	agg := NewAggregator(nil, prov, reg, presetForTest())

	rec := agg.Aggregate(context.Background(), FileRef{ID: uuid.New()})

	if rec.Outcome != OutcomeConfident {
		t.Fatalf("expected confident, got %s (conf=%v)", rec.Outcome, rec.Confidence)
	}
	if rec.ChosenRef == nil || rec.ChosenRef.ExternalID != "1234" {
		t.Fatalf("expected chosen ref tmdb:1234, got %+v", rec.ChosenRef)
	}
}

func TestAggregator_ConfidentReviewBand(t *testing.T) {
	t.Parallel()
	// A 0.78 candidate validates to ~0.77 — between ReviewLow (0.7) and
	// Auto (0.85).
	reg := registryWith(&fakeResolver{
		name: "name-parse", tier: Tier3, available: true,
		candidates: candidateOf("99", 0.78),
	})
	prov := &fakeProvider{items: map[string]*metadata.Item{
		"tmdb:99": {Source: metadata.SourceTMDB, ExternalID: "99", Type: metadata.EntityMovie, Title: "X"},
	}}
	agg := NewAggregator(nil, prov, reg, presetForTest())

	rec := agg.Aggregate(context.Background(), FileRef{ID: uuid.New()})

	if rec.Outcome != OutcomeConfidentReview {
		t.Fatalf("expected confident_review, got %s (conf=%v)", rec.Outcome, rec.Confidence)
	}
}

func TestAggregator_LowConfidenceBand(t *testing.T) {
	t.Parallel()
	// 0.6 candidate — between LowMin (0.5) and ReviewLow (0.7).
	// No provider validation step here (provider nil) so the value
	// passes through.
	reg := registryWith(&fakeResolver{
		name: "guessit", tier: Tier3, available: true,
		candidates: candidateOf("42", 0.6),
	})
	agg := NewAggregator(nil, nil, reg, presetForTest())

	rec := agg.Aggregate(context.Background(), FileRef{ID: uuid.New()})

	if rec.Outcome != OutcomeLowConfidence {
		t.Fatalf("expected low_confidence, got %s (conf=%v)", rec.Outcome, rec.Confidence)
	}
}

func TestAggregator_AmbiguousBand(t *testing.T) {
	t.Parallel()
	// Two candidates at nearly identical confidence — within ε.
	reg := registryWith(
		&fakeResolver{
			name: "guessit", tier: Tier3, available: true,
			candidates: []Candidate{
				{Ref: ExternalRef{Source: metadata.SourceTMDB, ExternalID: "1"}, Confidence: 0.62},
				{Ref: ExternalRef{Source: metadata.SourceTMDB, ExternalID: "2"}, Confidence: 0.61},
			},
		},
	)
	agg := NewAggregator(nil, nil, reg, presetForTest())

	rec := agg.Aggregate(context.Background(), FileRef{ID: uuid.New()})

	if rec.Outcome != OutcomeAmbiguous {
		t.Fatalf("expected ambiguous, got %s", rec.Outcome)
	}
}

func TestAggregator_Tier1_ShortCircuit_SkipsLowerTiers(t *testing.T) {
	t.Parallel()
	// Tier-1 fires confident; Tier-3 must not be consulted (so its
	// resolver's audit doesn't show up).
	t1 := &fakeResolver{
		name: "path-embed", tier: Tier1, available: true,
		candidates: candidateOf("777", 0.98),
	}
	t3 := &fakeResolver{
		name: "name-parse", tier: Tier3, available: true,
		candidates: candidateOf("999", 0.6),
	}
	reg := registryWith(t1, t3)
	prov := &fakeProvider{items: map[string]*metadata.Item{
		"tmdb:777": {Source: metadata.SourceTMDB, ExternalID: "777", Type: metadata.EntityMovie, Title: "X", Year: 2020},
	}}
	agg := NewAggregator(nil, prov, reg, presetForTest())

	rec := agg.Aggregate(context.Background(), FileRef{ID: uuid.New()})

	if rec.Outcome != OutcomeConfident {
		t.Fatalf("expected confident, got %s", rec.Outcome)
	}
	for _, a := range rec.ResolversConsulted {
		if a.Name == "name-parse" {
			t.Fatalf("tier-3 resolver should not have run when tier-1 short-circuited")
		}
	}
}

func TestAggregator_Tier1_404_FallsThroughToLowerTiers(t *testing.T) {
	t.Parallel()
	// Tier-1 hits a dead ID — provider returns nil. Tier-3's signal
	// should still produce a banded outcome.
	t1 := &fakeResolver{
		name: "path-embed", tier: Tier1, available: true,
		candidates: candidateOf("dead", 0.97),
	}
	t3 := &fakeResolver{
		name: "name-parse", tier: Tier3, available: true,
		candidates: candidateOf("alive", 0.6),
	}
	reg := registryWith(t1, t3)
	// "tmdb:dead" not in items map => 404 (nil, nil).
	prov := &fakeProvider{items: map[string]*metadata.Item{}}
	agg := NewAggregator(nil, prov, reg, presetForTest())

	rec := agg.Aggregate(context.Background(), FileRef{ID: uuid.New()})

	if rec.Outcome != OutcomeLowConfidence {
		t.Fatalf("expected low_confidence (from tier-3 alive candidate), got %s (conf=%v)", rec.Outcome, rec.Confidence)
	}
	if rec.ChosenRef == nil || rec.ChosenRef.ExternalID != "alive" {
		t.Fatalf("expected chosen tmdb:alive, got %+v", rec.ChosenRef)
	}
}

func TestAggregator_Redirect_RewritesIDAndScales(t *testing.T) {
	t.Parallel()
	// Tier-1 candidate at 0.97 hits a merge — provider returns the new
	// ID with Redirected=true. Final confidence = 0.97 * 0.95 = 0.9215,
	// still in confident band (≥0.85), but the chosen ref carries the
	// new ID.
	t1 := &fakeResolver{
		name: "path-embed", tier: Tier1, available: true,
		candidates: candidateOf("old", 0.97),
	}
	reg := registryWith(t1)
	prov := &fakeProvider{items: map[string]*metadata.Item{
		"tmdb:old": {Source: metadata.SourceTMDB, ExternalID: "new", Type: metadata.EntityMovie, Redirected: true},
	}}
	agg := NewAggregator(nil, prov, reg, presetForTest())

	rec := agg.Aggregate(context.Background(), FileRef{ID: uuid.New()})

	if rec.Outcome != OutcomeConfident {
		t.Fatalf("expected confident, got %s (conf=%v)", rec.Outcome, rec.Confidence)
	}
	if rec.ChosenRef == nil || rec.ChosenRef.ExternalID != "new" {
		t.Fatalf("expected chosen ref tmdb:new, got %+v", rec.ChosenRef)
	}
	// Evidence should record the redirect.
	var ev map[string]json.RawMessage
	if err := json.Unmarshal(rec.Evidence, &ev); err != nil {
		t.Fatalf("evidence unmarshal: %v", err)
	}
	if _, ok := ev["validation_redirects"]; !ok {
		t.Fatalf("expected validation_redirects in evidence; got keys=%v", keysOf(ev))
	}
}

func TestAggregator_CrossResolverCorroborationCompounds(t *testing.T) {
	t.Parallel()
	// Two resolvers, same ExternalRef, both at 0.6. Combined: max +
	// (N-1)*0.05 = 0.65, capped at 0.99. With nil provider no
	// validation scaling applies.
	a := &fakeResolver{
		name: "name-parse", tier: Tier3, available: true,
		candidates: candidateOf("X", 0.6),
	}
	b := &fakeResolver{
		name: "path-context", tier: Tier3, available: true,
		candidates: candidateOf("X", 0.6),
	}
	reg := registryWith(a, b)
	agg := NewAggregator(nil, nil, reg, presetForTest())

	rec := agg.Aggregate(context.Background(), FileRef{ID: uuid.New()})

	// Single resolver at 0.6 → low_confidence; corroboration pushes it
	// to 0.65 (still low_confidence band [0.5, 0.7)) but verifiably
	// higher than 0.6.
	if rec.Confidence <= 0.6 {
		t.Fatalf("expected corroboration to raise confidence above 0.6, got %v", rec.Confidence)
	}
	if rec.Outcome != OutcomeLowConfidence {
		t.Fatalf("expected low_confidence, got %s", rec.Outcome)
	}
}

func TestAggregator_YearMismatchSoftPenalty(t *testing.T) {
	t.Parallel()
	// FileRef.Parsed.Identity.Year = 2010; provider returns Year=2020
	// on validation. The aggregator's year-penalty multiplier reads
	// candidateYear() — phase 1 returns 0 there, so the test asserts
	// the bare-mechanism shape: a hint-year-bearing file still
	// produces a banded outcome rather than dropping the candidate.
	//
	// Future-proofing note: when phase-2 resolvers populate
	// candidate-side year, this test extends to assert numerical
	// penalty application. For now we assert the don't-drop invariant.
	parsed := &parsing.ParsedRelease{
		Identity: parsing.IdentityAttrs{
			Year: parsing.Field[int]{Value: 2010, Confidence: 1.0},
		},
	}
	r := &fakeResolver{
		name: "name-parse", tier: Tier3, available: true,
		candidates: candidateOf("123", 0.6),
	}
	reg := registryWith(r)
	agg := NewAggregator(nil, nil, reg, presetForTest())

	rec := agg.Aggregate(context.Background(), FileRef{ID: uuid.New(), Parsed: parsed})

	if rec.Outcome == OutcomeNoMatch {
		t.Fatalf("year mismatch should be a soft penalty, not a drop; got no_match")
	}
	if rec.ChosenRef == nil || rec.ChosenRef.ExternalID != "123" {
		t.Fatalf("expected chosen tmdb:123 despite year hint, got %+v", rec.ChosenRef)
	}
}

func TestAggregator_EvidenceTruncationFiresWhenOverCap(t *testing.T) {
	t.Parallel()
	// One resolver produces a single 10KB evidence payload. After
	// truncation only the "_truncated" marker remains.
	big := make([]byte, 10*1024)
	for i := range big {
		big[i] = 'a'
	}
	rawBig, _ := json.Marshal(string(big))
	r := &fakeResolver{
		name: "name-parse", tier: Tier3, available: true,
		candidates: candidateOf("1", 0.6),
		evidence:   rawBig,
	}
	reg := registryWith(r)
	agg := NewAggregator(nil, nil, reg, presetForTest())

	rec := agg.Aggregate(context.Background(), FileRef{ID: uuid.New()})

	if !rec.Truncated {
		t.Fatalf("expected truncated=true with 10KB payload, got false (evidence len=%d)", len(rec.Evidence))
	}
	if len(rec.Evidence) > evidenceCapBytes {
		t.Fatalf("evidence exceeds cap: %d > %d", len(rec.Evidence), evidenceCapBytes)
	}
}

func TestAggregator_PartialSeriesBand(t *testing.T) {
	t.Parallel()
	// File parses as a series (has episode numbering) and resolves to
	// a series identity with no Episode attached. Confidence high
	// enough to bypass low/ambiguous; the partial_series override
	// fires.
	parsed := &parsing.ParsedRelease{
		Identity: parsing.IdentityAttrs{
			Numbering: parsing.Numbering{
				Kind:           parsing.NumberingSeasonEpisode,
				Season:         parsing.Field[int]{Value: 1, Confidence: 1.0},
				EpisodeNumbers: parsing.Field[[]int]{Value: []int{3}, Confidence: 1.0},
			},
		},
	}
	r := &fakeResolver{
		name: "name-parse", tier: Tier3, available: true,
		candidates: []Candidate{{
			Ref:        ExternalRef{Source: metadata.SourceTMDB, ExternalID: "series-1"},
			Confidence: 0.9,
			// Episode deliberately nil — that's what partial means.
		}},
	}
	reg := registryWith(r)
	agg := NewAggregator(nil, nil, reg, presetForTest())

	rec := agg.Aggregate(context.Background(), FileRef{ID: uuid.New(), Parsed: parsed})

	if rec.Outcome != OutcomePartialSeries {
		t.Fatalf("expected partial_series, got %s", rec.Outcome)
	}
}

func TestMatchBatch_WritesRecordsViaRepo(t *testing.T) {
	t.Parallel()
	// End-to-end through MatcherService: a fake repo receives one
	// insert + one supersede call per file.
	r := &fakeResolver{
		name: "name-parse", tier: Tier3, available: true,
		candidates: candidateOf("42", 0.6),
	}
	reg := registryWith(r)
	repo := &fakeRepo{}

	svc := NewMatcherService(nil, repo, nil, reg, presetForTest())

	files := []FileRef{
		{ID: uuid.New(), Path: "/lib/movie.mkv"},
		{ID: uuid.New(), Path: "/lib/other.mkv"},
	}
	out, err := svc.MatchBatch(context.Background(), files)
	if err != nil {
		t.Fatalf("MatchBatch: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 outcomes, got %d", len(out))
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.records) != 2 {
		t.Fatalf("expected 2 records written, got %d", len(repo.records))
	}
	if len(repo.supersede) != 2 {
		t.Fatalf("expected 2 supersede calls, got %d", len(repo.supersede))
	}
	for i, rec := range repo.records {
		if rec.FileID != files[i].ID {
			t.Fatalf("record %d: file id mismatch: got %s, want %s", i, rec.FileID, files[i].ID)
		}
		if rec.DecidedBy != "auto" {
			t.Fatalf("record %d: decided_by = %q, want %q", i, rec.DecidedBy, "auto")
		}
	}
}

func TestMatchBatch_EmptyResolvers_AllNoMatch(t *testing.T) {
	t.Parallel()
	// No resolvers registered (the phase-1 default state). Every file
	// in the batch lands in no_match, and the records still get
	// written.
	repo := &fakeRepo{}
	svc := NewMatcherService(nil, repo, nil, NewRegistry(), presetForTest())

	out, err := svc.MatchBatch(context.Background(), []FileRef{{ID: uuid.New()}, {ID: uuid.New()}})
	if err != nil {
		t.Fatalf("MatchBatch: %v", err)
	}
	for _, rec := range out {
		if rec.Outcome != OutcomeNoMatch {
			t.Fatalf("expected no_match, got %s", rec.Outcome)
		}
	}
}

func TestAggregator_RankedCandidates_AmbiguousReturnsTop5(t *testing.T) {
	t.Parallel()
	// Eight tied candidates at 0.6 — the merge produces ambiguous and
	// the RankedCandidates slice should be truncated to 5, ordered by
	// confidence descending (with a small bonus on the first to
	// guarantee a deterministic ordering at the head).
	cands := []Candidate{
		{Ref: ExternalRef{Source: metadata.SourceTMDB, ExternalID: "1"}, Confidence: 0.62},
		{Ref: ExternalRef{Source: metadata.SourceTMDB, ExternalID: "2"}, Confidence: 0.61},
		{Ref: ExternalRef{Source: metadata.SourceTMDB, ExternalID: "3"}, Confidence: 0.60},
		{Ref: ExternalRef{Source: metadata.SourceTMDB, ExternalID: "4"}, Confidence: 0.59},
		{Ref: ExternalRef{Source: metadata.SourceTMDB, ExternalID: "5"}, Confidence: 0.58},
		{Ref: ExternalRef{Source: metadata.SourceTMDB, ExternalID: "6"}, Confidence: 0.57},
		{Ref: ExternalRef{Source: metadata.SourceTMDB, ExternalID: "7"}, Confidence: 0.56},
		{Ref: ExternalRef{Source: metadata.SourceTMDB, ExternalID: "8"}, Confidence: 0.55},
	}
	r := &fakeResolver{name: "guessit", tier: Tier3, available: true, candidates: cands}
	reg := registryWith(r)
	agg := NewAggregator(nil, nil, reg, presetForTest())

	rec := agg.Aggregate(context.Background(), FileRef{ID: uuid.New()})

	if rec.Outcome != OutcomeAmbiguous {
		t.Fatalf("expected ambiguous, got %s", rec.Outcome)
	}
	if len(rec.RankedCandidates) != RankedCandidatesLimit {
		t.Fatalf("expected %d ranked candidates, got %d", RankedCandidatesLimit, len(rec.RankedCandidates))
	}
	// Sorted descending: 1 (0.62) → 2 (0.61) → ... → 5 (0.58).
	wantOrder := []string{"1", "2", "3", "4", "5"}
	for i, want := range wantOrder {
		if rec.RankedCandidates[i].Ref.ExternalID != want {
			t.Fatalf("rank %d: got %s, want %s", i, rec.RankedCandidates[i].Ref.ExternalID, want)
		}
	}
	// Descending invariant: each entry's confidence ≤ the prior's.
	for i := 1; i < len(rec.RankedCandidates); i++ {
		if rec.RankedCandidates[i].Confidence > rec.RankedCandidates[i-1].Confidence {
			t.Fatalf("rank %d confidence %v > prior %v", i, rec.RankedCandidates[i].Confidence, rec.RankedCandidates[i-1].Confidence)
		}
	}
}

func TestAggregator_RankedCandidates_PerOutcomeContract(t *testing.T) {
	t.Parallel()
	// no_match: empty slice.
	t.Run("no_match", func(t *testing.T) {
		t.Parallel()
		reg := registryWith(&fakeResolver{name: "n", tier: Tier1, available: true})
		agg := NewAggregator(nil, &fakeProvider{}, reg, presetForTest())
		rec := agg.Aggregate(context.Background(), FileRef{ID: uuid.New()})
		if rec.Outcome != OutcomeNoMatch {
			t.Fatalf("expected no_match, got %s", rec.Outcome)
		}
		if len(rec.RankedCandidates) != 0 {
			t.Fatalf("expected 0 ranked candidates for no_match, got %d", len(rec.RankedCandidates))
		}
	})

	// confident: one entry.
	t.Run("confident", func(t *testing.T) {
		t.Parallel()
		reg := registryWith(&fakeResolver{name: "n", tier: Tier1, available: true, candidates: candidateOf("99", 0.97)})
		prov := &fakeProvider{items: map[string]*metadata.Item{
			"tmdb:99": {Source: metadata.SourceTMDB, ExternalID: "99"},
		}}
		agg := NewAggregator(nil, prov, reg, presetForTest())
		rec := agg.Aggregate(context.Background(), FileRef{ID: uuid.New()})
		if rec.Outcome != OutcomeConfident {
			t.Fatalf("expected confident, got %s (conf=%v)", rec.Outcome, rec.Confidence)
		}
		if len(rec.RankedCandidates) != 1 {
			t.Fatalf("expected 1 ranked candidate for confident, got %d", len(rec.RankedCandidates))
		}
		if rec.RankedCandidates[0].Ref.ExternalID != "99" {
			t.Fatalf("ranked chosen mismatch: got %s, want 99", rec.RankedCandidates[0].Ref.ExternalID)
		}
	})

	// low_confidence: one entry.
	t.Run("low_confidence", func(t *testing.T) {
		t.Parallel()
		reg := registryWith(&fakeResolver{name: "n", tier: Tier3, available: true, candidates: candidateOf("42", 0.6)})
		agg := NewAggregator(nil, nil, reg, presetForTest())
		rec := agg.Aggregate(context.Background(), FileRef{ID: uuid.New()})
		if rec.Outcome != OutcomeLowConfidence {
			t.Fatalf("expected low_confidence, got %s", rec.Outcome)
		}
		if len(rec.RankedCandidates) != 1 {
			t.Fatalf("expected 1 ranked candidate for low_confidence, got %d", len(rec.RankedCandidates))
		}
	})

	// ambiguous: ≤5 entries.
	t.Run("ambiguous_two_tied", func(t *testing.T) {
		t.Parallel()
		reg := registryWith(&fakeResolver{
			name: "n", tier: Tier3, available: true,
			candidates: []Candidate{
				{Ref: ExternalRef{Source: metadata.SourceTMDB, ExternalID: "1"}, Confidence: 0.62},
				{Ref: ExternalRef{Source: metadata.SourceTMDB, ExternalID: "2"}, Confidence: 0.61},
			},
		})
		agg := NewAggregator(nil, nil, reg, presetForTest())
		rec := agg.Aggregate(context.Background(), FileRef{ID: uuid.New()})
		if rec.Outcome != OutcomeAmbiguous {
			t.Fatalf("expected ambiguous, got %s", rec.Outcome)
		}
		if len(rec.RankedCandidates) != 2 {
			t.Fatalf("expected 2 ranked candidates, got %d", len(rec.RankedCandidates))
		}
	})
}

func TestAggregator_RankedCandidates_ChosenItemPropagates(t *testing.T) {
	t.Parallel()
	// Tier-1 candidate validated by the provider — the validated item
	// should appear on the corresponding RankedCandidate so persistence
	// can write title/year without a re-lookup.
	reg := registryWith(&fakeResolver{
		name: "path-embed", tier: Tier1, available: true,
		candidates: candidateOf("100", 0.97),
	})
	prov := &fakeProvider{items: map[string]*metadata.Item{
		"tmdb:100": {Source: metadata.SourceTMDB, ExternalID: "100", Title: "Hundred", Year: 2020},
	}}
	agg := NewAggregator(nil, prov, reg, presetForTest())

	rec := agg.Aggregate(context.Background(), FileRef{ID: uuid.New()})

	if len(rec.RankedCandidates) != 1 {
		t.Fatalf("expected 1 ranked candidate, got %d", len(rec.RankedCandidates))
	}
	rc := rec.RankedCandidates[0]
	if rc.Item == nil {
		t.Fatalf("expected ranked candidate to carry validated item; got nil")
	}
	if rc.Item.Title != "Hundred" || rc.Item.Year != 2020 {
		t.Fatalf("ranked item mismatch: got %+v", rc.Item)
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Compile-time check: fakeResolver satisfies Resolver.
var _ Resolver = (*fakeResolver)(nil)

// Compile-time check: fakeProvider satisfies metadata.MetadataProvider.
var _ metadata.MetadataProvider = (*fakeProvider)(nil)

// Compile-time check: fakeRepo satisfies MatcherRepo.
var _ MatcherRepo = (*fakeRepo)(nil)

// Sanity: when we strip the package version comment, evidence keys
// remain lowercase. (A reminder for future readers — the audit JSON is
// downstream-consumed, the keys are part of the implicit contract.)
func TestResolverAudit_JSONKeysAreLowerCamel(t *testing.T) {
	t.Parallel()
	a := ResolverAudit{Name: "x", Tier: Tier1, CandidateCount: 1, TopConfidence: 0.5}
	raw, _ := json.Marshal(a)
	for _, want := range []string{"name", "tier", "candidateCount", "topConfidence"} {
		if !strings.Contains(string(raw), `"`+want+`"`) {
			t.Fatalf("expected key %q in audit JSON %s", want, raw)
		}
	}
}
