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
// This currently populates the quality half (resolution, source, bin, remux,
// proper/repack), the release group, and the movie edition — the ported
// Sonarr/Radarr quality engine. The identity half (title, year, season/episode,
// daily, absolute, languages) is not yet extracted; those fields stay
// zero-valued until the identity port.
func Parse(input string, opts ...Option) ParsedRelease {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}

	raw := parseRaw(input)
	p := ParsedRelease{Input: input, IsPath: cfg.isPath}

	// Quality — the bin-derived fields share the bin's detection confidence.
	binConf := 0.0
	if raw.quality != Unknown {
		binConf = confDetected
	}
	p.Quality.Resolution = Field[string]{Value: string(raw.quality.Resolution()), Confidence: binConf, Evidence: "quality bin"}
	p.Quality.Source = Field[string]{Value: string(raw.quality.Source()), Confidence: binConf, Evidence: "quality bin"}
	p.Quality.Full = Field[string]{Value: raw.quality.String(), Confidence: binConf, Evidence: "quality bin"}
	p.Quality.IsRemux = Field[bool]{Value: raw.quality.IsRemux(), Confidence: binConf, Evidence: "quality bin"}

	// Revision — presence-based confidence.
	p.Quality.IsRepack = boolField(raw.revision.IsRepack, "repack token")
	p.Quality.Version = intField(raw.revision.Version, "version token")
	p.Quality.Real = intField(raw.revision.Real, "real token")

	// Release.
	p.Release.ReleaseGroup = strPtrField(raw.group, "release-group parser")
	if langs := parseLanguages(input); len(langs) > 0 {
		p.Release.Languages = Field[[]string]{Value: langs, Confidence: confDetected, Evidence: "language parser"}
	}

	// Identity — edition is set by the quality engine; title/year/numbering come
	// from the domain-specific identity pass.
	p.Identity.Edition = strPtrField(raw.edition, "edition parser")

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

// strPtrField maps a *string (nil = not found) to a Field[string].
func strPtrField(v *string, evidence string) Field[string] {
	if v != nil {
		return Field[string]{Value: *v, Confidence: confDetected, Evidence: evidence}
	}
	return Field[string]{}
}
