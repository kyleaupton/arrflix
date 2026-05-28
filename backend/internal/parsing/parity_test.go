package parsing_test

// Tier-1 parity: hermetic, fast, runs in `just check`. It diffs Parse against
// the committed goldens that the Tier-2 reference (internal/test/parity)
// captured from live pinned Sonarr/Radarr. No containers, no network — the
// goldens are embedded.
//
// Enforced fields (a mismatch fails the test, modulo the allowlist): the
// identity + group + language fields plus the quality bin — Sonarr
// title/year/season/episodes/group/languages/bin and Radarr title/year/edition/
// group/languages/bin. Reported-only fields (compat measured but not enforced):
// quality version / isRepack and Sonarr absolute (anime, out of the v1 claim).
// The codec/audio/HDR/dual-audio fields have no parse-oracle and are not
// compared here at all.
//
// Lives in the external test package (parsing_test) so it can import the
// internal/quality projection without forming a cycle: quality imports parsing
// at the package level, and the parity harness layers on top to render the
// parsed core into each tool's bin vocabulary.
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

	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/quality"
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

// allowlistPredicate is a programmatic intentional-divergence rule that matches
// a CLASS of mismatches (e.g. "Sonarr/Radarr return Unknown when identity parse
// fails"). The static `allowlist` map is the right home for one-off enumerated
// divergences; predicates carry the class-shaped ones where enumeration would
// be brittle to corpus changes and obscure the intent. Each predicate's Reason
// surfaces in the harness log alongside any counted-as-allowlisted entry.
type allowlistPredicate struct {
	Match  func(tool, input, field, expected, actual string) bool
	Reason string
}

// allowlistPredicates encodes the documented class-shaped divergences. Keep
// these narrow and named — a loose predicate masks future regressions. New
// entries need a corresponding spec citation (parsing OQ or
// quality-profiles OQ) explaining why the divergence is intentional.
var allowlistPredicates = []allowlistPredicate{
	{
		// Parsing spec OQ#13: arrflix extracts quality independently of
		// identity, while Sonarr/Radarr's oracle returns "Unknown" for the bin
		// whenever its episode/title parse fails (no parsedEpisodeInfo /
		// parsedMovieInfo at all). Documented intentional divergence — we keep
		// the strictly-more-information path because matching is a separate
		// layer downstream; the quality engine should not be gated on a
		// successful identity parse.
		Match: func(tool, input, field, expected, actual string) bool {
			return field == "bin" && expected == "Unknown" && actual != "Unknown"
		},
		Reason: "identity-independent quality (parsing spec OQ#13): arrflix extracts quality even when identity parse fails; *arrs return Unknown",
	},
}

// allowlist holds one-off intentional divergences keyed on (tool, input,
// field). compat % = matches / (total − allowlisted); the test fails only on
// un-allowlisted mismatches of an enforced field. Each entry carries the reason
// it's expected. For divergences with a stable shape across many inputs, prefer
// allowlistPredicates above.
//
// Today the entries fall into one bucket: the engine carries Sonarr's
// MediaFileExtensions table (bare `.mkv` → `HDTV-720p`, bare `.avi` → `SDTV`)
// because porting Radarr's table (`.mkv` → `WEBDL-720p`) regressed the Sonarr
// `bin` compat from ~97% to 91% — the shared table favors Sonarr by deliberate
// trade. A per-domain extension lookup is the eventual fix and is deferred to a
// future quality-engine tuning step (see PORT_NOTES.md "extension table is
// Sonarr-keyed").
var allowlist = map[allowlistKey]string{
	// Radarr — extension-table defaults. Each input below has no source/
	// resolution token in the release name; the oracle's QualityParser falls
	// back to Radarr's extension default (`.mkv` → WEBDL-720p), ours falls back
	// to Sonarr's (`.mkv` → HDTV-720p). Reason is shared.
	{"radarr", "2021 A Movie (1968) Director's Cut .mkv", "bin"}:                    extDefaultsReason,
	{"radarr", "A Fake Movie 2035 2012 Directors.mkv", "bin"}:                       extDefaultsReason,
	{"radarr", "A Fake Movie 2035 Directors 2012.mkv", "bin"}:                       extDefaultsReason,
	{"radarr", "Movie 2012 2in1.mkv", "bin"}:                                        extDefaultsReason,
	{"radarr", "Movie 2012 IMAX.mkv", "bin"}:                                        extDefaultsReason,
	{"radarr", "Movie 2012 Restored.mkv", "bin"}:                                    extDefaultsReason,
	{"radarr", "Movie 2049 Director's Cut.mkv", "bin"}:                              extDefaultsReason,
	{"radarr", "Movie 2in1 2012.mkv", "bin"}:                                        extDefaultsReason,
	{"radarr", "Movie Director's Cut (1968).mkv", "bin"}:                            extDefaultsReason,
	{"radarr", "Movie Director's Cut 2049.mkv", "bin"}:                              extDefaultsReason,
	{"radarr", "Movie IMAX 2012.mkv", "bin"}:                                        extDefaultsReason,
	{"radarr", "Movie Title (Despecialized) 1999.mkv", "bin"}:                       extDefaultsReason,
	{"radarr", "Movie Title 1999 (Despecialized).mkv", "bin"}:                       extDefaultsReason,
	{"radarr", "Movie Title 2012 50th Anniversary Edition.mkv", "bin"}:              extDefaultsReason,
	{"radarr", "Movie Title 50th Anniversary Edition 2012.mkv", "bin"}:              extDefaultsReason,
	{"radarr", "We Are the Movie!.2013.720p.H264.mkv", "bin"}:                       extDefaultsReason,
	{"radarr", "[Arid] Cowboy Bebop - Knockin' on Heaven's Door v2 [00F4CDA0].mkv", "bin"}: extDefaultsReason,
	{"radarr", "[MTBB] Kimi no Na wa. (2016) v2 [97681524].mkv", "bin"}:             extDefaultsReason,

	// Radarr — pre-release source (parsing OQ#12). Oracle renders DVDSCR as a
	// distinct bin via Modifier.SCREENER; our engine recognizes the SCR/DVDSCR
	// token and emits source=DVD/res=480p but leaves the SCREENER modifier
	// unset (the v0 parser's CAM/Telesync/Telecine/Screener consts are
	// scaffolded but never wired), so the bin renders as plain "DVD". Wiring
	// the SCREENER modifier through is deferred with the rest of the pre-
	// release source detection.
	{"radarr", "Movie Title (2018) Telugu DVDScr X264 AAC 700 MB", "bin"}: prereleaseSourceReason,

	// Sonarr — extension-table defaults. The two reversed-path corpus inputs
	// flow through Sonarr's "if the parsed token is in the last folder, try
	// the reverse" recovery, picking up WEB-DL on the reversed string. Our
	// engine doesn't reverse, so it falls back to the `.mkv` extension default
	// (HDTV-720p). Same class as the Radarr extension-default cases.
	{"sonarr", `C:\Test\Fake.Dir.S01E01-Test\yrucreM-462.H.0.2CAA.LD-BEW.p027.10E10S.esaeleR.dehsaH.emoS.mkv`, "bin"}: extDefaultsReason,
	{"sonarr", `C:\Test\Fake.Dir.S01E01-Test\yrucreM-LN 1.5DD LD-BEW P0801 10E10S esaeleR dehsaH emoS.mkv`, "bin"}:   extDefaultsReason,
	// Sonarr — `720p-web-handbrake.mkv`: the oracle picks up "web" as WEB-DL,
	// our regex requires a more explicit "WEB" / "WEB-DL" / "WEBRip" form (the
	// freestanding lowercase "web" in a hyphen-separated path segment doesn't
	// match), so we fall back to the .mkv extension default (HDTV-720p). Same
	// extension-default trade as the cases above; the precise upstream regex
	// (a looser WEB token) is deferred with the per-domain extension lookup.
	{"sonarr", "into.the.Series.s03e16.h264.720p-web-handbrake.mkv", "bin"}: extDefaultsReason,

	// Sonarr — NTSC as a DVD signal (parsing OQ#12-adjacent). Oracle's
	// QualityParser maps NTSC to the DVD bin; our engine does not currently
	// treat NTSC as a source token (no entry in sourceRegex), so the resolver
	// falls through to the extension-less SDTV default. Wiring NTSC/PAL as
	// DVD signals is the same shape as the pre-release sources OQ#12 work and
	// is deferred with it.
	{"sonarr", "The.Series.S01E13.NTSC.x264-CtrlSD", "bin"}: prereleaseSourceReason,
}

// Shared reason strings for the static allowlist entries above.
const (
	extDefaultsReason      = "shared extension-table defaults favor Sonarr; per-domain extension lookup deferred to future quality-engine tuning"
	prereleaseSourceReason = "pre-release source detection (parsing OQ#12) deferred: SCREENER modifier and NTSC/PAL → DVD signals are scaffolded but not wired"
)

// isPredicateAllowlisted reports whether any allowlistPredicate matches the
// given mismatch — i.e. the divergence is one of the documented class-shaped
// intentional ones (vs the static map's enumerated one-offs).
func isPredicateAllowlisted(tool, input, field, expected, actual string) bool {
	for _, p := range allowlistPredicates {
		if p.Match(tool, input, field, expected, actual) {
			return true
		}
	}
	return false
}

// fieldSpec names a compared field and whether a mismatch fails the test.
type fieldSpec struct {
	name     string
	enforced bool
}

// Enforced fields match the goldens exactly; a mismatch fails the build:
//   - Sonarr: title, year, season, episodes, group, languages, bin
//   - Radarr: title, year, edition, group, languages, bin
//
// The bin field is the per-domain projection of parsing's quality attribute
// core through internal/quality.BinFor — Sonarr's flattened "Bluray-1080p
// Remux" for series, Radarr's modifier-promoted "Remux-1080p" / "BR-DISK" for
// movies. It is enforced against the goldens via the intentional-divergence
// allowlist above: one predicate covers Class A (identity-independent quality —
// parsing OQ#13), and static entries enumerate Class B (the residual extension-
// table defaults and the deferred pre-release-source modifier).
//
// Reported-only (measured, not gated), deliberately deferred:
//   - version / isRepack — revision modeling is still a v0-shaped counter and
//     will be reshaped alongside the broader revision rework.
//   - absolute (Sonarr) — anime absolute numbering, out of the v1 enforced
//     claim.
//
// Don't add allowlist masks to chase the reported-field numbers — those belong
// to their own promotion step.

func TestParitySonarr(t *testing.T) {
	runParity(t, "sonarr", sonarrGolden, decodeSonarr, []fieldSpec{
		{"bin", true}, {"version", false}, {"isRepack", false}, {"group", true},
		{"title", true}, {"year", true}, {"season", true}, {"episodes", true}, {"languages", true},
		{"absolute", false},
	})
}

func TestParityRadarr(t *testing.T) {
	runParity(t, "radarr", radarrGolden, decodeRadarr, []fieldSpec{
		{"bin", true}, {"version", false}, {"isRepack", false}, {"edition", true}, {"group", true},
		{"title", true}, {"year", true}, {"languages", true},
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

	// Parse in the tool's domain: series inputs → Sonarr patterns, movie →
	// Radarr. The same domain selects which quality vocabulary the bin
	// projection renders into — Sonarr's flattened "Bluray-1080p Remux" vs
	// Radarr's modifier-promoted "Remux-1080p" / "BR-DISK".
	domainOpt := parsing.AsSeries()
	binDomain := quality.DomainSeries
	if tool == "radarr" {
		domainOpt = parsing.AsMovie()
		binDomain = quality.DomainMovie
	}

	for _, e := range entries {
		want := decode(e.Output)
		got := parsing.Parse(e.Input, domainOpt).Values()

		for _, f := range fields {
			expected, actual := compareField(f.name, want, got, binDomain)
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
			if isPredicateAllowlisted(tool, e.Input, f.name, expected, actual) {
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
// oracle→ours normalizations applied. The bin field is projected from the
// parsed core (Source, Resolution, Modifier) into the tool's vocabulary —
// parsing itself is domain-agnostic and emits no bin.
func compareField(field string, want oracleFields, got parsing.Values, binDomain quality.Domain) (string, string) {
	switch field {
	case "bin":
		bin := quality.BinFor(
			parsing.Source(got.Quality.Source),
			parsing.Resolution(got.Quality.Resolution),
			parsing.Modifier(got.Quality.Modifier),
			binDomain,
		)
		actual := bin.Name
		if actual == "" {
			// The oracle reports the not-detected case as "Unknown"; preserve
			// that mapping so the per-tool compat % stays comparable across
			// the projection switch.
			actual = "Unknown"
		}
		return want.bin, actual
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
