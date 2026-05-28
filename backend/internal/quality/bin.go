// Package quality projects parsing's orthogonal quality attribute core into the
// per-domain bin vocabulary a quality profile speaks.
//
// Parsing emits the shared, domain-agnostic core (Source, Resolution, Modifier,
// Revision) — Radarr's superset model — and never names a bin. The bin is a
// presentation/binning layer that consumes the core and a domain (series or
// movie) and renders the upstream quality vocabulary the corresponding tool
// uses: Sonarr's flattened "Bluray-1080p Remux" for series, Radarr's modifier-
// promoted "Remux-1080p" / "BR-DISK" for movies. Same detection, two
// vocabularies; safe because quality profiles are already (tier, media_type)-
// scoped.
//
// The bin is a struct, not just a string — its identity is the (Source,
// Resolution, Modifier) triple, and the Name is the display render. This is the
// "bins as keys, not strings" pattern from
// specs/modules/quality-profiles/README.md: future profile work will reference
// bins by identity (orderings, cutoffs, custom formats) and only render the
// display name at the UI / serialization edge.
//
// The two vocabulary tables (seriesBins, movieBins) are ports of the upstream
// Quality.cs tables from submodules/sonarr and submodules/radarr; their order
// matches upstream so default quality lists drawn from them stay faithful.
package quality

import "github.com/kyleaupton/arrflix/internal/parsing"

// Domain names which vocabulary BinFor projects into. Quality profiles are
// (tier, media_type)-scoped, so the caller always knows the domain by the time
// the projection runs — parsing's type hint never reaches this layer.
type Domain string

const (
	// DomainUnknown is the zero value: BinFor returns the zero Bin for it.
	DomainUnknown Domain = ""
	// DomainSeries selects the Sonarr quality vocabulary.
	DomainSeries Domain = "series"
	// DomainMovie selects the Radarr quality vocabulary.
	DomainMovie Domain = "movie"
)

// Bin is a per-domain quality bin: the (Source, Resolution, Modifier) triple is
// the canonical identity, and Name is the rendered string the upstream tool
// uses (verbatim from Sonarr/Radarr Quality.cs). The zero Bin is the explicit
// "no matching bin" sentinel.
//
// Bins are values, not strings: profile orderings, cutoffs, and gates compare
// on identity. The rendered Name is for display, serialization, and the
// historical "quality.full" surface — not the key.
type Bin struct {
	Name       string
	Source     parsing.Source
	Resolution parsing.Resolution
	Modifier   parsing.Modifier
}

// BinFor projects a parsing quality core (source, resolution, modifier) into
// the bin for the given domain. Pure and deterministic: identical inputs always
// return the same Bin, and the zero Bin (empty Name and zero axes) means "no
// matching bin in this vocabulary" — never an error.
//
// The two domains diverge on the modifier axis:
//
//   - Series (Sonarr): no modifier axis. REMUX is rendered as a " Remux" suffix
//     on the Bluray bin and only exists at 1080p/2160p. BR-DISK is folded back
//     into plain Bluray (Sonarr has no BR-DISK bin). RAWHD renders as the
//     resolution-agnostic "Raw-HD" bin.
//
//   - Movie (Radarr): the modifier is promoted to a top-level bin. REMUX
//     becomes "Remux-{res}" (1080p/2160p only). BR-DISK becomes the single
//     "BR-DISK" bin (fixed at 1080p in upstream). RAWHD renders as "Raw-HD".
func BinFor(source parsing.Source, resolution parsing.Resolution, modifier parsing.Modifier, domain Domain) Bin {
	switch domain {
	case DomainSeries:
		return projectSeries(source, resolution, modifier)
	case DomainMovie:
		return projectMovie(source, resolution, modifier)
	default:
		return Bin{}
	}
}

// projectSeries renders the core into the Sonarr vocabulary. Sonarr folds
// remux/raw-ness into source values upstream (BlurayRaw, TelevisionRaw); here
// the core's Source axis stays the orthogonal "BluRay" / "TV" and the modifier
// drives the projection.
func projectSeries(source parsing.Source, resolution parsing.Resolution, modifier parsing.Modifier) Bin {
	// RAWHD is resolution-agnostic in Sonarr and overrides whatever source the
	// caller passed (Sonarr's RAWHD is at TelevisionRaw, our orthogonal source
	// is TV).
	if modifier == parsing.ModRawHD {
		return lookupSeries("Raw-HD")
	}

	// BR-DISK folds into plain Bluray (Sonarr has no BR-DISK bin). The fold is
	// resolution-preserving: a BR-DISK at 2160p becomes Bluray-2160p, at 1080p
	// becomes Bluray-1080p. The engine fixes BR-DISK at 1080p today but the
	// projection stays a pure function of its inputs.
	if modifier == parsing.ModBRDisk {
		return lookupSeries(blurayName(resolution))
	}

	switch source {
	case parsing.SourceBluRay:
		base := blurayName(resolution)
		if base == "" {
			return Bin{}
		}
		// Sonarr only carries Bluray Remux at 1080p and 2160p (BlurayRaw); a
		// remux at any other resolution has no Sonarr bin, so fall back to
		// plain Bluray-{res} for that resolution.
		if modifier == parsing.ModRemux && (resolution == parsing.Res1080p || resolution == parsing.Res2160p) {
			return lookupSeries(base + " Remux")
		}
		return lookupSeries(base)

	case parsing.SourceWEBDL:
		return lookupSeries(resolutionPrefixedName("WEBDL-", resolution))

	case parsing.SourceWEBRip:
		return lookupSeries(resolutionPrefixedName("WEBRip-", resolution))

	case parsing.SourceTV:
		switch resolution {
		case parsing.Res720p, parsing.Res1080p, parsing.Res2160p:
			return lookupSeries("HDTV-" + string(resolution))
		case parsing.Res480p, parsing.Res576p, parsing.ResSD:
			return lookupSeries("SDTV")
		}
		return Bin{}

	case parsing.SourceDVD:
		return lookupSeries("DVD")
	}

	// Sonarr has no bins for the pre-release sources (CAM/Telesync/Telecine/
	// Workprint) or Unknown.
	return Bin{}
}

// projectMovie renders the core into the Radarr vocabulary. Radarr's Quality
// table carries the Modifier axis directly, so the projection is closer to a
// straight identity lookup with a few collapse rules at the edges.
func projectMovie(source parsing.Source, resolution parsing.Resolution, modifier parsing.Modifier) Bin {
	// RAWHD is its own bin in Radarr too. Upstream fixes it at 1080p / source
	// TV; we render the bin from the table regardless of incoming resolution.
	if modifier == parsing.ModRawHD {
		return lookupMovie("Raw-HD")
	}

	// BR-DISK is a single bin in Radarr (fixed at 1080p, source BLURAY). It
	// takes precedence over the source/resolution the caller passed — a
	// BR-DISK at 2160p has no separate bin upstream and projects to the same
	// BR-DISK entry.
	if modifier == parsing.ModBRDisk {
		return lookupMovie("BR-DISK")
	}

	if modifier == parsing.ModRemux {
		switch source {
		case parsing.SourceBluRay:
			// Radarr only carries Remux at 1080p and 2160p (Remux1080p /
			// Remux2160p). A remux at 720p/480p falls back to plain
			// Bluray-{res}.
			if resolution == parsing.Res1080p || resolution == parsing.Res2160p {
				return lookupMovie("Remux-" + string(resolution))
			}
		case parsing.SourceDVD:
			// Radarr's DVDR (Quality.cs:93) is the DVD/480p/REMUX bin.
			if resolution == parsing.Res480p || resolution == parsing.ResUnknown {
				return lookupMovie("DVD-R")
			}
		}
		// Fall through: render as the plain-source bin below.
	}

	// Deferred modifiers carry their own dedicated bin in Radarr's table:
	// SCREENER → DVDSCR, REGIONAL → REGIONAL. Parsing's engine never sets
	// these today (ModScreener / ModRegional are stubs that mirror upstream's
	// enum), but the projection accepts them so the vocabulary table is
	// reachable end-to-end and a future detector wiring them up needs no
	// further changes here.
	if modifier == parsing.ModScreener {
		return lookupMovie("DVDSCR")
	}
	if modifier == parsing.ModRegional {
		return lookupMovie("REGIONAL")
	}

	switch source {
	case parsing.SourceBluRay:
		return lookupMovie(blurayName(resolution))

	case parsing.SourceWEBDL:
		return lookupMovie(resolutionPrefixedName("WEBDL-", resolution))

	case parsing.SourceWEBRip:
		return lookupMovie(resolutionPrefixedName("WEBRip-", resolution))

	case parsing.SourceTV:
		switch resolution {
		case parsing.Res720p, parsing.Res1080p, parsing.Res2160p:
			return lookupMovie("HDTV-" + string(resolution))
		case parsing.Res480p, parsing.Res576p, parsing.ResSD:
			return lookupMovie("SDTV")
		}
		return Bin{}

	case parsing.SourceDVD:
		return lookupMovie("DVD")

	case parsing.SourceCAM:
		return lookupMovie("CAM")
	case parsing.SourceTelesync:
		return lookupMovie("TELESYNC")
	case parsing.SourceTelecine:
		return lookupMovie("TELECINE")
	case parsing.SourceWorkprint:
		return lookupMovie("WORKPRINT")
	}

	return Bin{}
}

// blurayName renders the "Bluray-{res}" segment, or "" for a resolution that
// has no Bluray bin in either vocabulary.
func blurayName(r parsing.Resolution) string {
	switch r {
	case parsing.Res480p, parsing.Res576p, parsing.Res720p, parsing.Res1080p, parsing.Res2160p:
		return "Bluray-" + string(r)
	}
	return ""
}

// resolutionPrefixedName renders "{prefix}{res}" for a resolution that has a
// bin under that prefix (WEBDL / WEBRip both carry 480/720/1080/2160), or ""
// otherwise. Both vocabularies agree on these.
func resolutionPrefixedName(prefix string, r parsing.Resolution) string {
	switch r {
	case parsing.Res480p, parsing.Res720p, parsing.Res1080p, parsing.Res2160p:
		return prefix + string(r)
	}
	return ""
}

// lookupSeries returns the seriesBins entry by Name, or the zero Bin.
func lookupSeries(name string) Bin {
	if name == "" {
		return Bin{}
	}
	for _, b := range seriesBins {
		if b.Name == name {
			return b
		}
	}
	return Bin{}
}

// lookupMovie returns the movieBins entry by Name, or the zero Bin.
func lookupMovie(name string) Bin {
	if name == "" {
		return Bin{}
	}
	for _, b := range movieBins {
		if b.Name == name {
			return b
		}
	}
	return Bin{}
}
