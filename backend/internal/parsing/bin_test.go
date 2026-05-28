package parsing

import (
	"reflect"
	"testing"
)

// TestBinFor_SpecWorkedExamples enforces every row of the worked-example table
// in specs/modules/quality-profiles/README.md#the-two-vocabularies. If this
// fails after a change, either the spec or the projection moved and the two
// need to be reconciled.
func TestBinFor_SpecWorkedExamples(t *testing.T) {
	cases := []struct {
		name       string
		source     Source
		resolution Resolution
		modifier   Modifier
		seriesName string
		movieName  string
	}{
		{
			name:       "(BluRay, 2160p, REMUX)",
			source:     SourceBluRay,
			resolution: Res2160p,
			modifier:   ModRemux,
			seriesName: "Bluray-2160p Remux",
			movieName:  "Remux-2160p",
		},
		{
			name:       "(BluRay, 1080p, REMUX)",
			source:     SourceBluRay,
			resolution: Res1080p,
			modifier:   ModRemux,
			seriesName: "Bluray-1080p Remux",
			movieName:  "Remux-1080p",
		},
		{
			name:       "(BluRay, 2160p, BR-DISK)",
			source:     SourceBluRay,
			resolution: Res2160p,
			modifier:   ModBRDisk,
			seriesName: "Bluray-2160p", // folded
			movieName:  "BR-DISK",      // own tier
		},
		{
			name:       "(BluRay, 1080p, NONE)",
			source:     SourceBluRay,
			resolution: Res1080p,
			modifier:   ModNone,
			seriesName: "Bluray-1080p",
			movieName:  "Bluray-1080p",
		},
		{
			name:       "(WEB-DL, 1080p, NONE)",
			source:     SourceWEBDL,
			resolution: Res1080p,
			modifier:   ModNone,
			seriesName: "WEBDL-1080p",
			movieName:  "WEBDL-1080p",
		},
		{
			name: "(HDTV, 1080p, RAWHD)",
			// Orthogonal core: source TV (Radarr's TV / Sonarr's TV;
			// Sonarr's RAWHD upstream is TelevisionRaw, the modifier
			// disambiguates here).
			source:     SourceTV,
			resolution: Res1080p,
			modifier:   ModRawHD,
			seriesName: "Raw-HD",
			movieName:  "Raw-HD",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotSeries := BinFor(tc.source, tc.resolution, tc.modifier, DomainSeries)
			if gotSeries.Name != tc.seriesName {
				t.Errorf("series: got %q, want %q", gotSeries.Name, tc.seriesName)
			}
			gotMovie := BinFor(tc.source, tc.resolution, tc.modifier, DomainMovie)
			if gotMovie.Name != tc.movieName {
				t.Errorf("movie: got %q, want %q", gotMovie.Name, tc.movieName)
			}
		})
	}
}

// TestSeriesVocabularyReachable ensures every entry in seriesBins is reachable
// via BinFor with the (Source, Resolution, Modifier) triple the table records.
// This catches typos in the table — if a bin's stored identity doesn't round-
// trip through the projection, either the table is wrong or the projection is
// wrong, and one needs to be fixed.
func TestSeriesVocabularyReachable(t *testing.T) {
	for _, want := range seriesBins {
		t.Run(want.Name, func(t *testing.T) {
			got := BinFor(want.Source, want.Resolution, want.Modifier, DomainSeries)
			if got.Name != want.Name {
				t.Errorf("BinFor(%v, %v, %v, series) = %q, want %q",
					want.Source, want.Resolution, want.Modifier, got.Name, want.Name)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("BinFor returned %+v, want %+v", got, want)
			}
		})
	}
}

// TestMovieVocabularyReachable is the movie-domain analogue of
// TestSeriesVocabularyReachable.
func TestMovieVocabularyReachable(t *testing.T) {
	for _, want := range movieBins {
		t.Run(want.Name, func(t *testing.T) {
			got := BinFor(want.Source, want.Resolution, want.Modifier, DomainMovie)
			if got.Name != want.Name {
				t.Errorf("BinFor(%v, %v, %v, movie) = %q, want %q",
					want.Source, want.Resolution, want.Modifier, got.Name, want.Name)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("BinFor returned %+v, want %+v", got, want)
			}
		})
	}
}

// TestSeriesVocabularyMatchesSonarr cross-checks seriesBins against the
// canonical upstream Sonarr Quality.cs table — every non-Unknown Quality name
// in submodules/sonarr/.../Quality.cs must be in seriesBins. Hard-coded set
// (rather than parsing the .cs file) because the failure mode we want to catch
// is a port typo in vocab.go.
func TestSeriesVocabularyMatchesSonarr(t *testing.T) {
	// Sonarr Quality.cs:128-152 All list, minus Unknown (the not-detected
	// sentinel — not a real bin a profile orders or gates on).
	wantNames := []string{
		"SDTV",
		"DVD",
		"WEBRip-480p",
		"WEBDL-480p",
		"Bluray-480p",
		"Bluray-576p",
		"HDTV-720p",
		"WEBRip-720p",
		"WEBDL-720p",
		"Bluray-720p",
		"Bluray-1080p",
		"HDTV-1080p",
		"WEBRip-1080p",
		"WEBDL-1080p",
		"Raw-HD",
		"HDTV-2160p",
		"WEBRip-2160p",
		"WEBDL-2160p",
		"Bluray-2160p",
		"Bluray-1080p Remux",
		"Bluray-2160p Remux",
	}

	have := make(map[string]bool, len(seriesBins))
	for _, b := range seriesBins {
		have[b.Name] = true
	}

	for _, want := range wantNames {
		if !have[want] {
			t.Errorf("seriesBins missing upstream Sonarr Quality %q", want)
		}
	}

	if len(seriesBins) != len(wantNames) {
		t.Errorf("seriesBins length %d, want %d (extra or missing bins vs Sonarr Quality.cs)",
			len(seriesBins), len(wantNames))
	}
}

// TestMovieVocabularyMatchesRadarr cross-checks movieBins against the canonical
// upstream Radarr Quality.cs table.
func TestMovieVocabularyMatchesRadarr(t *testing.T) {
	// Radarr Quality.cs:128-160 All list, minus Unknown.
	wantNames := []string{
		"WORKPRINT",
		"CAM",
		"TELESYNC",
		"TELECINE",
		"DVDSCR",
		"REGIONAL",
		"SDTV",
		"DVD",
		"DVD-R",
		"HDTV-720p",
		"HDTV-1080p",
		"HDTV-2160p",
		"WEBDL-480p",
		"WEBDL-720p",
		"WEBDL-1080p",
		"WEBDL-2160p",
		"WEBRip-480p",
		"WEBRip-720p",
		"WEBRip-1080p",
		"WEBRip-2160p",
		"Bluray-480p",
		"Bluray-576p",
		"Bluray-720p",
		"Bluray-1080p",
		"Bluray-2160p",
		"Remux-1080p",
		"Remux-2160p",
		"BR-DISK",
		"Raw-HD",
	}

	have := make(map[string]bool, len(movieBins))
	for _, b := range movieBins {
		have[b.Name] = true
	}

	for _, want := range wantNames {
		if !have[want] {
			t.Errorf("movieBins missing upstream Radarr Quality %q", want)
		}
	}

	if len(movieBins) != len(wantNames) {
		t.Errorf("movieBins length %d, want %d (extra or missing bins vs Radarr Quality.cs)",
			len(movieBins), len(wantNames))
	}
}

// TestBinFor_UnknownDomain returns the zero Bin for a domain outside the
// {series, movie} set. Parse never calls BinFor with anything else (its domain
// argument is required), but the safety net keeps direct callers honest.
func TestBinFor_UnknownDomain(t *testing.T) {
	got := BinFor(SourceBluRay, Res1080p, ModNone, Domain(""))
	if got != (Bin{}) {
		t.Errorf("BinFor with unknown domain returned %+v, want zero Bin", got)
	}
}

// TestBinFor_NoMatch returns the zero Bin for triples no domain projects (e.g.
// an Unknown source or a resolution outside any vocabulary's coverage). The
// projection is total: never an error, just a zero Bin signalling "no match".
func TestBinFor_NoMatch(t *testing.T) {
	cases := []struct {
		name       string
		source     Source
		resolution Resolution
		modifier   Modifier
		domain     Domain
	}{
		{
			name:       "unknown source, series",
			source:     SourceUnknown,
			resolution: Res1080p,
			modifier:   ModNone,
			domain:     DomainSeries,
		},
		{
			name:       "unknown source, movie",
			source:     SourceUnknown,
			resolution: Res1080p,
			modifier:   ModNone,
			domain:     DomainMovie,
		},
		{
			name:       "unknown resolution on TV source, series",
			source:     SourceTV,
			resolution: ResUnknown,
			modifier:   ModNone,
			domain:     DomainSeries,
		},
		{
			name:       "CAM in series vocabulary (no Sonarr bin)",
			source:     SourceCAM,
			resolution: ResUnknown,
			modifier:   ModNone,
			domain:     DomainSeries,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BinFor(tc.source, tc.resolution, tc.modifier, tc.domain)
			if got != (Bin{}) {
				t.Errorf("got %+v, want zero Bin", got)
			}
		})
	}
}

// TestBinFor_Pure ensures BinFor is deterministic: invoked repeatedly on the
// same inputs it returns the same Bin. Cheap to assert and the contract
// downstream profile code depends on.
func TestBinFor_Pure(t *testing.T) {
	cases := []struct {
		source     Source
		resolution Resolution
		modifier   Modifier
		domain     Domain
	}{
		{SourceBluRay, Res1080p, ModRemux, DomainSeries},
		{SourceBluRay, Res2160p, ModBRDisk, DomainMovie},
		{SourceWEBDL, Res720p, ModNone, DomainSeries},
		{SourceTV, Res1080p, ModRawHD, DomainMovie},
	}
	for _, tc := range cases {
		first := BinFor(tc.source, tc.resolution, tc.modifier, tc.domain)
		for i := 0; i < 5; i++ {
			again := BinFor(tc.source, tc.resolution, tc.modifier, tc.domain)
			if !reflect.DeepEqual(first, again) {
				t.Fatalf("BinFor not pure: %+v vs %+v on inputs %+v", first, again, tc)
			}
		}
	}
}

// TestBinFor_ModifierProjections asserts the modifier-axis projection rules
// from the brief beyond the spec's worked examples — the cases where the two
// vocabularies diverge and the projection has to do real work.
func TestBinFor_ModifierProjections(t *testing.T) {
	cases := []struct {
		name       string
		source     Source
		resolution Resolution
		modifier   Modifier
		domain     Domain
		wantName   string
	}{
		// Series: BR-DISK folds into plain Bluray at the input resolution.
		{
			name:       "series BR-DISK at 1080p folds to Bluray-1080p",
			source:     SourceBluRay,
			resolution: Res1080p,
			modifier:   ModBRDisk,
			domain:     DomainSeries,
			wantName:   "Bluray-1080p",
		},
		// Movie: BR-DISK is a single bin regardless of input resolution.
		{
			name:       "movie BR-DISK at 2160p still maps to BR-DISK",
			source:     SourceBluRay,
			resolution: Res2160p,
			modifier:   ModBRDisk,
			domain:     DomainMovie,
			wantName:   "BR-DISK",
		},
		// Movie: RAWHD overrides whatever else and renders Raw-HD.
		{
			name:       "movie RAWHD at any resolution renders Raw-HD",
			source:     SourceTV,
			resolution: Res720p,
			modifier:   ModRawHD,
			domain:     DomainMovie,
			wantName:   "Raw-HD",
		},
		// Series: RAWHD is resolution-agnostic.
		{
			name:       "series RAWHD at 720p renders Raw-HD",
			source:     SourceTV,
			resolution: Res720p,
			modifier:   ModRawHD,
			domain:     DomainSeries,
			wantName:   "Raw-HD",
		},
		// Movie: DVDR is the (DVD, 480p, REMUX) bin.
		{
			name:       "movie REMUX on DVD/480p renders DVD-R",
			source:     SourceDVD,
			resolution: Res480p,
			modifier:   ModRemux,
			domain:     DomainMovie,
			wantName:   "DVD-R",
		},
		// Pre-release sources are first-class movie bins, absent from series.
		{
			name:       "movie CAM source renders CAM bin",
			source:     SourceCAM,
			resolution: ResUnknown,
			modifier:   ModNone,
			domain:     DomainMovie,
			wantName:   "CAM",
		},
		// HDTV-{res} agree between domains.
		{
			name:       "series HDTV-1080p",
			source:     SourceTV,
			resolution: Res1080p,
			modifier:   ModNone,
			domain:     DomainSeries,
			wantName:   "HDTV-1080p",
		},
		{
			name:       "movie HDTV-1080p",
			source:     SourceTV,
			resolution: Res1080p,
			modifier:   ModNone,
			domain:     DomainMovie,
			wantName:   "HDTV-1080p",
		},
		// SDTV agrees between domains for sub-720 TV.
		{
			name:       "series TV 480p renders SDTV",
			source:     SourceTV,
			resolution: Res480p,
			modifier:   ModNone,
			domain:     DomainSeries,
			wantName:   "SDTV",
		},
		{
			name:       "movie TV 480p renders SDTV",
			source:     SourceTV,
			resolution: Res480p,
			modifier:   ModNone,
			domain:     DomainMovie,
			wantName:   "SDTV",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BinFor(tc.source, tc.resolution, tc.modifier, tc.domain)
			if got.Name != tc.wantName {
				t.Errorf("got %q, want %q", got.Name, tc.wantName)
			}
		})
	}
}
