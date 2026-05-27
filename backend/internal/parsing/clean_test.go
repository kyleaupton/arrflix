package parsing

import "testing"

// These tests exercise the pre-match simplify pipeline (clean.go) per stage. They
// assert on the three working strings (release/simple) and the reject signal.
// clean.go is not wired into Parse() yet; this is its standalone coverage.

func TestRemoveFileExtensionClean(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"The.Office.S03E05.720p.HDTV.x264-DIMENSION.mkv", "The.Office.S03E05.720p.HDTV.x264-DIMENSION"},
		{"Some.Movie.2009.1080p.BluRay.x264.mp4", "Some.Movie.2009.1080p.BluRay.x264"},
		// Not a media extension — must be preserved.
		{"Some.Show.S01E01.foo", "Some.Show.S01E01.foo"},
		// .par2 / .nzb are usenet extensions and are stripped.
		{"Some.Release.par2", "Some.Release"},
	}
	for _, tt := range tests {
		if got := removeFileExtensionClean(tt.in); got != tt.want {
			t.Errorf("removeFileExtensionClean(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFullwidthBrackets(t *testing.T) {
	c := cleanSeries("【SubGroup】Some Anime - 01")
	if got := c.release; got != "[SubGroup]Some Anime - 01" {
		t.Errorf("fullwidth brackets not normalized: release=%q", got)
	}
}

func TestWebsitePrefixStripped(t *testing.T) {
	// SimpleTitleRegex first strips 720p and x264 (leaving the dot artifacts that
	// upstream also leaves); the website prefix is then removed from the front.
	c := cleanSeries("www.Torrenting.com - The.Office.S03E05.720p.HDTV.x264-DIMENSION")
	if got := c.simple; got != "The.Office.S03E05..HDTV.-DIMENSION" {
		t.Errorf("website prefix not stripped: simple=%q", got)
	}
}

func TestWebsitePostfixStripped(t *testing.T) {
	// Postfix strip runs on the simple string. x264 is stripped by SimpleTitleRegex.
	c := cleanSeries("The.Office.S03E05.HDTV.x264-DIMENSION[www.example.com]")
	if got := c.simple; got != "The.Office.S03E05.HDTV.-DIMENSION" {
		t.Errorf("website postfix not stripped: simple=%q", got)
	}
}

func TestTorrentSuffixStripped(t *testing.T) {
	c := cleanSeries("The.Office.S03E05.HDTV.x264-DIMENSION[rartv]")
	if got := c.simple; got != "The.Office.S03E05.HDTV.-DIMENSION" {
		t.Errorf("torrent suffix not stripped: simple=%q", got)
	}
}

func TestSixDigitAirDateNormalized(t *testing.T) {
	c := cleanSeries("The.Daily.Show.140210.Some.Guest.HDTV.x264-LMAO")
	// 140210 -> 2014.02.10 (x264 also stripped by SimpleTitleRegex).
	if got := c.simple; got != "The.Daily.Show.2014.02.10.Some.Guest.HDTV.-LMAO" {
		t.Errorf("six-digit airdate not normalized: simple=%q", got)
	}
}

func TestPreSubstitutionApplied(t *testing.T) {
	// Korean PreSubstitution rule: .E<eps>.<6digits>.<...-NEXT> -> .S01E<eps>.<...>
	c := cleanSeries("Show.E07.210208.720p-NEXT")
	if got := c.release; got != "Show.S01E07.720p-NEXT" {
		t.Errorf("PreSubstitution (Korean) not applied: release=%q", got)
	}
}

func TestSimpleTitleRegexStripsQualityTokens(t *testing.T) {
	// SimpleTitleRegex strips both 1080p (resolution) and x264 (codec, via the
	// [xh][\W_]?26[45] alternative), leaving the dot artifacts upstream leaves.
	c := cleanSeries("The.Office.S03E05.1080p.x264-DIMENSION")
	if got := c.simple; got != "The.Office.S03E05..-DIMENSION" {
		t.Errorf("SimpleTitleRegex did not strip resolution+codec: simple=%q", got)
	}
}

func TestQualityBracketStrippedVsPreserved(t *testing.T) {
	// A recognized quality bracket is stripped...
	stripped := cleanSeries("Some.Show.S01E01[HDTV-720p]")
	if got := stripped.simple; got != "Some.Show.S01E01" {
		t.Errorf("recognized quality bracket not stripped: simple=%q", got)
	}
	// ...a non-quality trailing bracket is preserved.
	kept := cleanSeries("Some.Show.S01E01[NotAQuality]")
	if got := kept.simple; got != "Some.Show.S01E01[NotAQuality]" {
		t.Errorf("non-quality bracket should be preserved: simple=%q", got)
	}
}

func TestMovieTrimsTrailingDash(t *testing.T) {
	c := cleanMovie("Some.Movie.2009.1080p.BluRay.x264-")
	// Radarr-specific Trim('-','_') removes the trailing dash from release.
	if len(c.release) == 0 || c.release[len(c.release)-1] == '-' {
		t.Errorf("movie pipeline did not trim trailing dash: release=%q", c.release)
	}
}

func TestMovieNoSixDigitAirDate(t *testing.T) {
	// Movies have no six-digit-airdate stage — a YYMMDD-looking token stays as-is.
	c := cleanMovie("Some.Movie.140210.1080p.BluRay.x264-GROUP")
	// 140210 stays as-is (no airdate stage); 1080p and x264 are stripped.
	if got := c.simple; got != "Some.Movie.140210..BluRay.-GROUP" {
		t.Errorf("movie airdate should not be normalized: simple=%q", got)
	}
}

func TestRejectSeriesHashedAndCrap(t *testing.T) {
	// Harvested-corpus-style negatives: hashed releases and crap names reject.
	rejects := []string{
		"0e895c37245186812cb08aab1529cf8ee389dd05", // 40-char hash (matches 32/30/26/24 prefix rules)
		"abc",      // ^abc$
		"123",      // ^123$
		"b00bs",    // ^b00bs$
		"Season 3", // SeasonFolderRegexes (Sonarr only)
		"Specials", // SeasonFolderRegexes
		"!!!",      // no letter or digit
		"this is a password protected yenc release", // password + yenc
	}
	for _, in := range rejects {
		if c := cleanSeries(in); !c.rejected {
			t.Errorf("cleanSeries(%q) should be rejected", in)
		}
	}
}

func TestAcceptNormalSeriesTitles(t *testing.T) {
	accepts := []string{
		"The.Office.S03E05.720p.HDTV.x264-DIMENSION",
		"[HorribleSubs] One Piece - 1071 [1080p].mkv",
		"The.Daily.Show.2014.02.10.Some.Guest.HDTV.x264-LMAO",
	}
	for _, in := range accepts {
		if c := cleanSeries(in); c.rejected {
			t.Errorf("cleanSeries(%q) should NOT be rejected", in)
		}
	}
}

func TestRejectMovieHashedNoSeasonFolder(t *testing.T) {
	// Movie pipeline rejects hashed/crap but NOT season-folder names (Radarr has
	// no SeasonFolder reject).
	if c := cleanMovie("abc"); !c.rejected {
		t.Errorf("cleanMovie(%q) should be rejected", "abc")
	}
	if c := cleanMovie("Season 3"); c.rejected {
		t.Errorf("cleanMovie(%q) should NOT be rejected (no season-folder reject in Radarr)", "Season 3")
	}
}

func TestReversedTitleCorrected(t *testing.T) {
	// The reversed-title marker is p027 / p0801 between separators (a reversed
	// "720p" / "1080p"). When present, the (extension-stripped) title is reversed.
	in := "eltit.eht-p027-50E30S"
	c := cleanSeries(in)
	if c.release == in {
		t.Errorf("reversed-title stage did not reverse: release=%q", c.release)
	}
	// The reversed release should now read forwards (e.g. contain "S03E05").
	if want := reverseString(in); c.release != want {
		t.Errorf("reversed-title release = %q, want %q", c.release, want)
	}
}

func TestOriginalPreserved(t *testing.T) {
	in := "The.Office.S03E05.720p.HDTV.x264-DIMENSION.mkv"
	c := cleanSeries(in)
	if c.original != in {
		t.Errorf("original should be untouched: got %q want %q", c.original, in)
	}
}
