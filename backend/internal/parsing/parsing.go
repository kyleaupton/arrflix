package parsing

// Domain selects which identity pattern set Parse applies. Quality and release
// metadata are domain-free and always parsed; only identity (title, year,
// numbering) is domain-specific.
type Domain string

const (
	// DomainAuto lets Parse detect series vs movie from the title.
	DomainAuto Domain = ""
	// DomainSeries applies the Sonarr series patterns only.
	DomainSeries Domain = "series"
	// DomainMovie applies the Radarr movie patterns only.
	DomainMovie Domain = "movie"
)

// config holds the options Parse was called with.
type config struct {
	isPath bool
	domain Domain
}

// Option configures a Parse call.
type Option func(*config)

// AsSeries parses identity with the Sonarr series patterns (title, season,
// episode, absolute, daily). Domain-aware callers — scan, download, matching —
// pass this; they know the domain.
func AsSeries() Option {
	return func(c *config) { c.domain = DomainSeries }
}

// AsMovie parses identity with the Radarr movie patterns (title, year).
func AsMovie() Option {
	return func(c *config) { c.domain = DomainMovie }
}

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
// Quality (resolution, source, bin, remux, proper/repack) is parsed for every
// input by the ported Sonarr/Radarr quality engine. Identity (title, year,
// numbering, edition), release group, and languages come from the
// domain-specific pass selected by the Domain option, or by detection from the
// title; fields a given input doesn't carry stay zero-valued.
func Parse(input string, opts ...Option) ParsedRelease {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}

	raw := parseRaw(input)
	p := ParsedRelease{Input: input, IsPath: cfg.isPath}

	// Quality — the orthogonal core (source, resolution, modifier) plus revision.
	// The core's detection confidence is shared across its three axes; a core is
	// "detected" when any axis fired. The per-domain bin (Sonarr's flattened
	// "Bluray-1080p Remux" / Radarr's "Remux-1080p") is a downstream projection
	// in internal/quality; parsing never names a bin.
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
	domain := cfg.domain
	if domain == DomainAuto {
		domain = detectDomain(input)
	}
	switch domain {
	case DomainSeries:
		parseSeriesIdentity(&p, input)
	case DomainMovie:
		parseMovieIdentity(&p, input)
	}
	p.Identity.TypeHint = Field[string]{Value: string(domain), Confidence: confDetected, Evidence: "domain dispatch"}

	return p
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
