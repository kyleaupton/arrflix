package parsing

// config holds the options Parse was called with.
type config struct {
	isPath bool
}

// Option configures a Parse call.
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

	// Identity — edition is the one identity field the quality engine already
	// extracts; the rest await the identity port.
	p.Identity.Edition = strPtrField(raw.edition, "edition parser")

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
