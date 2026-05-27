package parsing

// Tier-1 parity: hermetic, fast, runs in `just check`. It diffs Parse against
// the committed goldens that the Tier-2 reference (internal/test/parity)
// captured from live pinned Sonarr/Radarr. No containers, no network — the
// goldens are embedded.
//
// Enforced fields (a mismatch fails the test, modulo the allowlist): the
// stabilized identity + group fields — Sonarr title/year/season/episodes/group
// and Radarr title/year/edition/group. Reported-only fields (compat measured but
// not enforced while the matching half stabilizes): quality bin / version /
// isRepack (P6 quality re-port), languages (P5.5), and Sonarr absolute (anime,
// out of the v1 claim). The codec/audio/HDR/dual-audio fields have no
// parse-oracle and are not compared here at all.
//
// The test reports per-field/per-tool compat % and fails on any enforced-field
// mismatch that is not in the intentional-divergence allowlist.

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// parityMiss is one field disagreement between Parse and the oracle.
type parityMiss struct{ input, field, want, got string }

//go:embed testdata/sonarr.golden.json
var sonarrGolden []byte

//go:embed testdata/radarr.golden.json
var radarrGolden []byte

// goldenEntry mirrors what the Tier-2 regen writes: the input and the raw
// oracle parse output.
type goldenEntry struct {
	Input  string          `json:"input"`
	Output json.RawMessage `json:"output"`
}

// oracleQuality is the shared shape of Sonarr/Radarr's nested quality object.
type oracleQuality struct {
	Quality struct {
		Name string `json:"name"`
	} `json:"quality"`
	Revision struct {
		Version  int  `json:"version"`
		Real     int  `json:"real"`
		IsRepack bool `json:"isRepack"`
	} `json:"revision"`
}

// oracleFields is the tool-agnostic projection we compare against.
type oracleFields struct {
	bin      string
	group    string
	version  int
	isRepack bool
	edition  string // movies only
	// identity
	title     string
	year      int
	season    int
	episodes  []int
	absolute  []int
	languages []string
}

// langName is the {id,name} shape of an oracle language entry.
type langName struct {
	Name string `json:"name"`
}

// langNames extracts the names from an oracle language list.
func langNames(in []langName) []string {
	out := make([]string, 0, len(in))
	for _, l := range in {
		out = append(out, l.Name)
	}
	return out
}

func (q *oracleQuality) bin() string {
	if q == nil || q.Quality.Name == "" {
		return "Unknown"
	}
	return q.Quality.Name
}

// allowlistKey identifies one intentional divergence: a specific field on a
// specific input for a specific tool.
type allowlistKey struct {
	tool  string
	input string
	field string
}

// allowlist holds divergences we accept on purpose. compat % = matches /
// (total − allowlisted); the test fails only on un-allowlisted mismatches of an
// enforced field. Each entry carries the reason it's expected.
//
// Phase 5 rebuild: the entire prior allowlist is now INERT and was removed. The
// faithful Sonarr/Radarr group re-port (group.go) and the clean.go CJK rune-bug
// fix brought every previously-divergent ENFORCED field (Sonarr title/year/
// season/episodes/group; Radarr title/year/edition/group) to 100% parity against
// the goldens with NO masking required. Verified by an exhaustive liveness probe:
// not one of the old title/season/episodes entries was still being hit. The only
// remaining live divergences sit on still-REPORTED fields — bin (Sonarr couples
// quality to a successful episode parse; Radarr v6 uses a different quality
// vocabulary) and languages — both deliberately deferred (bin → P6 quality
// re-port, languages → P5.5). Reported fields are not gated, so they need no
// allowlist; their divergences surface honestly in the reported compat metric.
//
// Net: there is zero genuine two-sided disagreement left on any enforced field,
// so the allowlist is intentionally empty.
var allowlist = map[allowlistKey]string{}

// fieldSpec names a compared field and whether a mismatch fails the test.
type fieldSpec struct {
	name     string
	enforced bool
}

// Promotion status (Phase 5). After the faithful Sonarr/Radarr group re-port
// (group.go) and the clean.go CJK rune-slicing fix, the stabilized identity +
// group fields hit 100% golden parity and are now ENFORCED:
//
//   - Sonarr: title, year, season, episodes, group
//   - Radarr: title, year, edition, group
//
// Still REPORTED-only (deliberately deferred, NOT enforced):
//   - bin / version / isRepack — the quality engine is re-ported in P6 (Radarr
//     bin is only ~66% against the v6 oracle vocabulary).
//   - languages — re-ported in P5.5.
//   - absolute (Sonarr) — anime absolute numbering, ~99.8% but out of the v1
//     enforced claim.
//
// The allowlist is empty: no enforced field has any genuine divergence to mask
// (see the allowlist comment). Do NOT add allowlist masks or tweak the parser to
// chase the remaining reported-field numbers — those belong to P5.5 / P6.

func TestParitySonarr(t *testing.T) {
	runParity(t, "sonarr", sonarrGolden, decodeSonarr, []fieldSpec{
		{"bin", false}, {"version", false}, {"isRepack", false}, {"group", true},
		{"title", true}, {"year", true}, {"season", true}, {"episodes", true}, {"languages", false},
		{"absolute", false},
	})
}

func TestParityRadarr(t *testing.T) {
	runParity(t, "radarr", radarrGolden, decodeRadarr, []fieldSpec{
		{"bin", false}, {"version", false}, {"isRepack", false}, {"edition", true}, {"group", true},
		{"title", true}, {"year", true}, {"languages", false},
	})
}

func decodeSonarr(raw json.RawMessage) oracleFields {
	var out struct {
		ParsedEpisodeInfo *struct {
			Quality                *oracleQuality `json:"quality"`
			ReleaseGroup           string         `json:"releaseGroup"`
			SeasonNumber           int            `json:"seasonNumber"`
			EpisodeNumbers         []int          `json:"episodeNumbers"`
			AbsoluteEpisodeNumbers []int          `json:"absoluteEpisodeNumbers"`
			Languages              []langName     `json:"languages"`
			SeriesTitleInfo        *struct {
				TitleWithoutYear string `json:"titleWithoutYear"`
				Year             int    `json:"year"`
			} `json:"seriesTitleInfo"`
		} `json:"parsedEpisodeInfo"`
	}
	_ = json.Unmarshal(raw, &out)
	pei := out.ParsedEpisodeInfo
	if pei == nil {
		return oracleFields{bin: "Unknown"}
	}
	f := oracleFields{
		bin:       pei.Quality.bin(),
		group:     pei.ReleaseGroup,
		version:   pei.Quality.Revision.Version,
		isRepack:  pei.Quality.Revision.IsRepack,
		season:    pei.SeasonNumber,
		episodes:  pei.EpisodeNumbers,
		absolute:  pei.AbsoluteEpisodeNumbers,
		languages: langNames(pei.Languages),
	}
	if pei.SeriesTitleInfo != nil {
		f.title = pei.SeriesTitleInfo.TitleWithoutYear
		f.year = pei.SeriesTitleInfo.Year
	}
	return f
}

func decodeRadarr(raw json.RawMessage) oracleFields {
	var out struct {
		ParsedMovieInfo *struct {
			Quality      *oracleQuality `json:"quality"`
			ReleaseGroup string         `json:"releaseGroup"`
			Edition      string         `json:"edition"`
			MovieTitle   string         `json:"movieTitle"`
			Year         int            `json:"year"`
			Languages    []langName     `json:"languages"`
		} `json:"parsedMovieInfo"`
	}
	_ = json.Unmarshal(raw, &out)
	pmi := out.ParsedMovieInfo
	if pmi == nil {
		return oracleFields{bin: "Unknown"}
	}
	return oracleFields{
		bin:       pmi.Quality.bin(),
		group:     pmi.ReleaseGroup,
		version:   pmi.Quality.Revision.Version,
		isRepack:  pmi.Quality.Revision.IsRepack,
		edition:   pmi.Edition,
		title:     pmi.MovieTitle,
		year:      pmi.Year,
		languages: langNames(pmi.Languages),
	}
}

// fieldStat tracks compat for one field.
type fieldStat struct {
	matches     int
	compared    int
	allowlisted int
}

func runParity(t *testing.T, tool string, golden []byte, decode func(json.RawMessage) oracleFields, fields []fieldSpec) {
	t.Helper()

	var entries []goldenEntry
	if err := json.Unmarshal(golden, &entries); err != nil {
		t.Fatalf("decode %s golden: %v", tool, err)
	}
	if len(entries) == 0 {
		t.Fatalf("%s golden is empty", tool)
	}

	stats := map[string]*fieldStat{}
	enforced := map[string]bool{}
	for _, f := range fields {
		stats[f.name] = &fieldStat{}
		enforced[f.name] = f.enforced
	}

	var failMisses, reportMisses []parityMiss

	// Parse in the tool's domain: series inputs → Sonarr patterns, movie → Radarr.
	domainOpt := AsSeries()
	if tool == "radarr" {
		domainOpt = AsMovie()
	}

	for _, e := range entries {
		want := decode(e.Output)
		got := Parse(e.Input, domainOpt).Values()

		for _, f := range fields {
			expected, actual := compareField(f.name, want, got)
			st := stats[f.name]
			st.compared++
			if expected == actual {
				st.matches++
				continue
			}
			if _, ok := allowlist[allowlistKey{tool, e.Input, f.name}]; ok {
				st.allowlisted++
				continue
			}
			m := parityMiss{e.Input, f.name, expected, actual}
			if f.enforced {
				failMisses = append(failMisses, m)
			} else {
				reportMisses = append(reportMisses, m)
			}
		}
	}

	// Report per-field compat %.
	t.Logf("=== %s parity over %d inputs ===", tool, len(entries))
	for _, f := range fields {
		st := stats[f.name]
		denom := st.compared - st.allowlisted
		pct := 100.0
		if denom > 0 {
			pct = float64(st.matches) / float64(denom) * 100
		}
		tag := "enforced"
		if !f.enforced {
			tag = "reported"
		}
		t.Logf("  %-9s %6.2f%%  (%d/%d matched, %d allowlisted) [%s]", f.name, pct, st.matches, denom, st.allowlisted, tag)
	}

	if len(reportMisses) > 0 {
		t.Logf("%d reported-only %s divergences (not enforced — triage pending):%s", len(reportMisses), tool, formatMisses(reportMisses))
	}
	if len(failMisses) > 0 {
		t.Errorf("%d un-allowlisted enforced %s mismatches:%s", len(failMisses), tool, formatMisses(failMisses))
	}
}

// formatMisses renders misses deterministically (sorted by field then input).
func formatMisses(misses []parityMiss) string {
	sort.Slice(misses, func(i, j int) bool {
		if misses[i].field != misses[j].field {
			return misses[i].field < misses[j].field
		}
		return misses[i].input < misses[j].input
	})
	var b strings.Builder
	for _, m := range misses {
		fmt.Fprintf(&b, "\n  [%s] %q\n      want %q  got %q", m.field, m.input, m.want, m.got)
	}
	return b.String()
}

// compareField returns the (expected, actual) string pair for a field, with the
// oracle→ours normalizations applied.
func compareField(field string, want oracleFields, got Values) (string, string) {
	switch field {
	case "bin":
		return want.bin, got.Quality.Full
	case "group":
		return want.group, got.Release.ReleaseGroup
	case "version":
		// Compare the proper level, not the raw counter: the oracle's baseline
		// is 1 (and 0 when it didn't parse), while our engine uses 0 for the
		// original. properLevel collapses both to "how many propers deep".
		return fmt.Sprintf("%d", properLevel(want.version)), fmt.Sprintf("%d", properLevel(got.Quality.Version))
	case "isRepack":
		return fmt.Sprintf("%t", want.isRepack), fmt.Sprintf("%t", got.Quality.IsRepack)
	case "edition":
		return want.edition, got.Identity.Edition
	case "title":
		return want.title, got.Identity.Title
	case "year":
		return fmt.Sprintf("%d", want.year), fmt.Sprintf("%d", got.Identity.Year)
	case "season":
		return fmt.Sprintf("%d", want.season), fmt.Sprintf("%d", got.Identity.Numbering.Season)
	case "episodes":
		return joinInts(want.episodes), joinInts(got.Identity.Numbering.EpisodeNumbers)
	case "absolute":
		return joinInts(want.absolute), joinInts(got.Identity.Numbering.AbsoluteNumbers)
	case "languages":
		return normalizeLangs(want.languages), normalizeLangs(got.Release.Languages)
	default:
		return "", ""
	}
}

// normalizeLangs renders a language set order-independently and treats the
// oracle's "Unknown" as the empty set (which is how we represent no detection).
func normalizeLangs(langs []string) string {
	kept := make([]string, 0, len(langs))
	for _, l := range langs {
		if l == "Unknown" {
			continue
		}
		kept = append(kept, l)
	}
	sort.Strings(kept)
	return strings.Join(kept, ",")
}

// joinInts renders an int slice as a stable comma-joined string for comparison.
func joinInts(xs []int) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = strconv.Itoa(x)
	}
	return strings.Join(parts, ",")
}

// properLevel maps a revision counter to "propers deep": 0/1 → 0 (original),
// 2 → 1, etc. Symmetric across the oracle's 1-based and our 0-based baselines.
func properLevel(v int) int {
	if v <= 1 {
		return 0
	}
	return v - 1
}
