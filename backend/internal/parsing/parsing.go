package parsing

import "strings"

// Domain selects which identity pattern set Parse applies and which bin
// vocabulary Parse renders the quality result into. Every production consumer
// knows the domain at the call site (TMDB-anchored search, importer's
// media-type-typed job, library-scoped scan, routing's dry-run endpoint),
// so Parse takes it as a required positional argument — there is no
// auto-detect.
type Domain string

const (
	// DomainSeries applies the Sonarr series patterns and renders the bin in
	// Sonarr's vocabulary (e.g. "Bluray-1080p Remux").
	DomainSeries Domain = "series"
	// DomainMovie applies the Radarr movie patterns and renders the bin in
	// Radarr's vocabulary (e.g. "Remux-1080p").
	DomainMovie Domain = "movie"
)

// config holds the options Parse was called with.
type config struct {
	isPath bool
}

// Option configures a Parse call. Domain is required positional; Option is
// reserved for path-flavor and any future path-only switches.
type Option func(*config)

// AsPath parses input as an on-disk path (filename + folder context) rather
// than a release title. For now it only records the flavor on the result; the
// folder-context handling (series name from the show folder, season from the
// season folder) lands with path-flavor mode.
func AsPath() Option {
	return func(c *config) { c.isPath = true }
}

// Confidence levels are deliberately coarse for v1 — calibration against the
// labeled corpus is pending (a shared dependency with the matcher). A detected
// value gets confDetected; an absent one gets 0.
const confDetected = 0.9

// Parse turns a release title or filename into the advertised ParsedRelease.
// It is pure, deterministic, and total: unparseable input yields low-confidence
// (zero) fields, never an error.
//
// Quality (resolution, source, modifier, revision) and the rendered per-domain
// bin (Quality.Name / Quality.Full) come from the ported Sonarr/Radarr quality
// engine. Identity (title, year, numbering, edition), release group, and
// languages come from the domain-specific pass selected by the Domain argument;
// fields a given input doesn't carry stay zero-valued.
func Parse(input string, domain Domain, opts ...Option) ParsedRelease {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}

	raw := parseQuality(input)
	p := ParsedRelease{Input: input, IsPath: cfg.isPath}

	// Quality — the orthogonal core (source, resolution, modifier) plus
	// revision. The core's detection confidence is shared across its three
	// axes; a core is "detected" when any axis fired.
	detected := raw.source != SourceUnknown || raw.resolution != ResUnknown || raw.modifier != ModNone
	coreConf := 0.0
	if detected {
		coreConf = confDetected
	}
	p.Quality.Source = Field[string]{Value: string(raw.source), Confidence: coreConf, Evidence: "quality core"}
	p.Quality.Resolution = Field[string]{Value: string(raw.resolution), Confidence: coreConf, Evidence: "quality core"}
	p.Quality.Modifier = Field[string]{Value: string(raw.modifier), Confidence: coreConf, Evidence: "quality core"}

	// Revision — presence-based confidence.
	p.Quality.IsRepack = boolField(raw.revision.IsRepack, "repack token")
	p.Quality.Version = intField(raw.revision.Version, "version token")
	p.Quality.Real = intField(raw.revision.Real, "real token")

	// Quality.Name / Quality.Full — the per-domain bin rendered at parse time.
	// Name is the bin's stable display label (Sonarr's "Bluray-1080p Remux" /
	// Radarr's "Remux-1080p"); Full is Name plus the revision-render suffix
	// (FileNameBuilder.cs:359 {Quality Full} semantics).
	bin := BinFor(raw.source, raw.resolution, raw.modifier, domain)
	binConf := 0.0
	if detected && bin.Name != "" {
		binConf = confDetected
	}
	p.Quality.Name = Field[string]{Value: bin.Name, Confidence: binConf, Evidence: "bin render"}
	p.Quality.Full = Field[string]{Value: renderQualityFull(bin.Name, raw.revision), Confidence: binConf, Evidence: "bin render + revision"}

	// Release. ReleaseGroup / ReleaseHash come from the domain-specific identity
	// pass below (parseSeriesGroup / parseMovieGroup in group.go — the faithful
	// Sonarr/Radarr ports), mirroring upstream which derives the group from the
	// post-match release/simpleReleaseTitle and the winning match's subgroup/hash.
	// Languages are parsed in the same pass — Sonarr from result.ReleaseTokens
	// (the post-S/E substring), Radarr from the group-blanked simpleReleaseTitle —
	// matching upstream's input.

	// Identity — title/year/numbering come from the domain-specific identity
	// pass. Edition is movie-domain only and is set by parseMovieIdentity's
	// faithful Radarr port; the series path leaves it empty.
	switch domain {
	case DomainSeries:
		parseSeriesIdentity(&p, input)
	case DomainMovie:
		parseMovieIdentity(&p, input)
	}
	p.Identity.TypeHint = Field[string]{Value: string(domain), Confidence: confDetected, Evidence: "domain dispatch"}

	return p
}

// renderQualityFull renders the {Quality Full} naming-token (Radarr
// Organizer/FileNameBuilder.cs:359):
//
//	string.Format("{0} {1} {2}", title, proper, real)
//
// where title is the quality bin name; proper resolves to "Proper" when the
// revision version is >1, "Repack" when isRepack is set on an original
// revision, otherwise empty; and real is "REAL" repeated `real` times (a single
// "REAL" upstream — Radarr only carries a 0/1 counter, but the loop keeps
// repeats faithful for future-proofing). Trailing whitespace is trimmed so a
// release with no proper/real renders as just the bin name.
func renderQualityFull(name string, rev Revision) string {
	if name == "" {
		return ""
	}
	var proper string
	switch {
	case rev.Version > 1:
		proper = "Proper"
	case rev.IsRepack:
		proper = "Repack"
	}
	var real string
	if rev.Real > 0 {
		real = strings.TrimSpace(strings.Repeat("REAL ", rev.Real))
	}
	return strings.TrimRight(name+" "+proper+" "+real, " ")
}

// boolField sets confidence only when the flag is true (a positive detection);
// a false flag is the indistinguishable-from-absent zero state.
func boolField(v bool, evidence string) Field[bool] {
	if v {
		return Field[bool]{Value: true, Confidence: confDetected, Evidence: evidence}
	}
	return Field[bool]{}
}

// intField sets confidence only for a non-zero value.
func intField(v int, evidence string) Field[int] {
	if v != 0 {
		return Field[int]{Value: v, Confidence: confDetected, Evidence: evidence}
	}
	return Field[int]{}
}
