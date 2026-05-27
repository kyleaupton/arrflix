package parsing

// Tier-1 parity: hermetic, fast, runs in `just check`. It diffs Parse against
// the committed goldens that the Tier-2 reference (internal/test/parity)
// captured from live pinned Sonarr/Radarr. No containers, no network — the
// goldens are embedded.
//
// Enforced fields (a mismatch fails the test, modulo the allowlist): quality
// bin, proper/repack revision, version, and (movies) edition. Reported-only
// fields (compat measured but not enforced while the port stabilizes): release
// group and the identity fields (title, year, season, episodes, absolute,
// languages). The codec/audio/HDR/dual-audio fields have no parse-oracle and are
// not compared here at all.
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
var allowlist = map[allowlistKey]string{
	// Sonarr couples quality to a successful EPISODE parse: a title with no
	// valid season/episode (movie-ish or malformed) comes back Unknown. arrflix
	// extracts quality independent of identity, so it reports the real bin.
	{"sonarr", "Sans.Series.De.Traces.FRENCH.720p.BluRay.x264-FHD", "bin"}:                             "no S/E → Sonarr Unknown; arrflix quality is identity-independent",
	{"sonarr", "Series Away(2001) Bluray FHD Hi10P.mkv", "bin"}:                                        "no S/E → Sonarr Unknown; arrflix quality is identity-independent",
	{"sonarr", "Series.Title.S05EO1.Episode.Title.2160p.BDRip.AAC.7.1.HDR10.x265.10bit-Markll", "bin"}: "malformed S05EO1 (letter O) → Sonarr Unknown; arrflix quality is identity-independent",
	{"sonarr", "The.Series.The.Lost.Sonarr.Summer.HR.WS.PDTV.x264-DHD", "bin"}:                         "no S/E → Sonarr Unknown; arrflix quality is identity-independent",
	{"sonarr", "[Vodes] Series Title - Other Title (2020) [BDRemux 1080p HEVC Dual-Audio]", "bin"}:     "no episode number → Sonarr Unknown; arrflix quality is identity-independent",
	{"sonarr", "[coldhell] Series v2 [BD1080p][5A45EABE].mkv", "bin"}:                                  "no episode number → Sonarr Unknown; arrflix quality is identity-independent",
	{"sonarr", "[coldhell] Series v3 [BD720p][03192D4C]", "bin"}:                                       "no episode number → Sonarr Unknown; arrflix quality is identity-independent",

	// Radarr v6 uses a different movie quality vocabulary than the Sonarr-style
	// names arrflix emits uniformly: a distinct Remux-1080p/2160p bin (vs
	// "Bluray-Xp Remux") and BR-DISK for full discs.
	{"radarr", "The Movie 2023 2160p BluRay REMUX HEVC DTS-HD MA TrueHD 7.1 Atmos-GROUP", "bin"}: "Radarr v6 'Remux-2160p' vs arrflix unified 'Bluray-2160p Remux'",
	{"radarr", "The.Movie.2018.1080p.BluRay.REMUX.AVC.DTS-HD.MA.5.1-FraMeSToR", "bin"}:           "Radarr v6 'Remux-1080p' vs arrflix unified 'Bluray-1080p Remux'",
	{"radarr", "The.Movie.2020.Hybrid.2160p.UHD.BluRay.Remux.DV.HDR.HEVC.Atmos-GROUP", "bin"}:    "Radarr v6 'Remux-2160p' vs arrflix unified 'Bluray-2160p Remux'",
	{"radarr", "The.Movie.2018.1080p.BluRay.AVC.TrueHD.7.1.Atmos-GROUP", "bin"}:                  "Radarr v6 'BR-DISK' (full disc) vs arrflix 'Bluray-1080p'",

	// Identity: Sonarr fed a movie-ish/yearless title splits the YEAR into a
	// season+episode (2016 → S20E16, 2017 → season 2017) or reads a bundle's
	// "Part 1" as S01E01. There is no real episode identity; arrflix correctly
	// extracts none.
	{"sonarr", "Death.Series.2017.German.DD51.DL.1080p.NetflixHD.x264-TVS", "season"}:      "Sonarr uses year 2017 as season; no real season",
	{"sonarr", "S for Series 2005 1080p UHD BluRay DD+7.1 x264-LoRD.mkv", "season"}:        "Sonarr uses year 2005 as season; no real season",
	{"sonarr", "Series 2016 German DD51 DL 720p NetflixHD x264-TVS", "season"}:             "Sonarr splits year 2016 into season 20; no real season",
	{"sonarr", "Series.Title.2011.1080p.UHD.BluRay.DD5.1.HDR.x265-CtrlHD.mkv", "season"}:   "Sonarr splits year 2011 into season 20; no real season",
	{"sonarr", "Series.Title.2014.2160p.UHD.BluRay.X265-IAMABLE.mkv", "season"}:            "Sonarr splits year 2014 into season 20; no real season",
	{"sonarr", "[DameDesuYo] Series Bundle - Part 1 (BD 4K 8bit FLAC)", "season"}:          "Sonarr reads bundle 'Part 1' as season 1",
	{"sonarr", "Series 2016 German DD51 DL 720p NetflixHD x264-TVS", "episodes"}:           "Sonarr splits year 2016 into episode 16; no real episode",
	{"sonarr", "Series.Title.2011.1080p.UHD.BluRay.DD5.1.HDR.x265-CtrlHD.mkv", "episodes"}: "Sonarr splits year 2011 into episode 11; no real episode",
	{"sonarr", "Series.Title.2014.2160p.UHD.BluRay.X265-IAMABLE.mkv", "episodes"}:          "Sonarr splits year 2014 into episode 14; no real episode",
	{"sonarr", "[DameDesuYo] Series Bundle - Part 1 (BD 4K 8bit FLAC)", "episodes"}:        "Sonarr reads bundle 'Part 1' as episode 1",
	{"sonarr", "Sans.Series.De.Traces.FRENCH.720p.BluRay.x264-FHD", "languages"}:           "Sonarr can't parse as series → Unknown; arrflix correctly detects French",

	// Title divergences. Group 1: Sonarr force-parses a year as S/E and is left
	// with a partial title; arrflix parses no series identity, so no title.
	{"sonarr", "Death.Series.2017.German.DD51.DL.1080p.NetflixHD.x264-TVS", "title"}:    "Sonarr force-parses year-as-S/E (title 'Death'); arrflix parses no series identity",
	{"sonarr", "S for Series 2005 1080p UHD BluRay DD+7.1 x264-LoRD.mkv", "title"}:      "Sonarr force-parses year-as-S/E; arrflix parses no series identity",
	{"sonarr", "Series 2016 German DD51 DL 720p NetflixHD x264-TVS", "title"}:           "Sonarr force-parses year-as-S/E; arrflix parses no series identity",
	{"sonarr", "Series.Title.2011.1080p.UHD.BluRay.DD5.1.HDR.x265-CtrlHD.mkv", "title"}: "Sonarr force-parses year-as-S/E; arrflix parses no series identity",
	{"sonarr", "Series.Title.2014.2160p.UHD.BluRay.X265-IAMABLE.mkv", "title"}:          "Sonarr force-parses year-as-S/E; arrflix parses no series identity",
	{"sonarr", "The Series (BD)(640x480(RAW) (BATCH 1) (1-13)", "title"}:                "malformed batch title; out of scope",
	// Group 2: anime absolute numbering — best-effort and out of the v1 claim;
	// title is affected where the absolute parse is partial.
	{"sonarr", "Series Slayer 04 vostfr FHD.mkv", "title"}:                           "anime absolute (non-bracket); best-effort, out of v1 claim",
	{"sonarr", "The Online Series Alicization 04 vostfr FHD", "title"}:               "anime absolute (non-bracket); best-effort, out of v1 claim",
	{"sonarr", "[DameDesuYo] Series Bundle - Part 1 (BD 4K 8bit FLAC)", "title"}:     "anime bundle; absolute best-effort, out of v1 claim",
	{"sonarr", "[Doremi].The.Series.5.Go.Go!.31.[1280x720].[C65D4B1F].mkv", "title"}: "anime absolute; best-effort, out of v1 claim",
}

// fieldSpec names a compared field and whether a mismatch fails the test.
// Enforced fields are the validated ones; group is reported-only for now — it
// was never parity-tested before and its long-tail divergences (both tools
// over/under-extracting) need a dedicated triage pass.
type fieldSpec struct {
	name     string
	enforced bool
}

func TestParitySonarr(t *testing.T) {
	runParity(t, "sonarr", sonarrGolden, decodeSonarr, []fieldSpec{
		{"bin", true}, {"version", true}, {"isRepack", true}, {"group", false},
		// identity — title/year/season/episodes/languages enforced; absolute is
		// best-effort (out of the v1 claim).
		{"title", true}, {"year", true}, {"season", true}, {"episodes", true}, {"languages", true},
		{"absolute", false},
	})
}

func TestParityRadarr(t *testing.T) {
	runParity(t, "radarr", radarrGolden, decodeRadarr, []fieldSpec{
		{"bin", true}, {"version", true}, {"isRepack", true}, {"edition", true}, {"group", false},
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
