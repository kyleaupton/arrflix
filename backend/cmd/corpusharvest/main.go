// Command corpusharvest grows the parser test corpus
// (internal/parsing/testdata/inputs.json) by lifting real release-title strings
// out of the pinned Sonarr/Radarr NUnit test fixtures.
//
// It is intentionally mechanical and re-runnable: when the pinned Sonarr/Radarr
// submodule versions are bumped, re-run this to refresh the harvested inputs.
//
//	docker compose exec -T -w /app/backend arrflix-dev go run ./cmd/corpusharvest
//
// Contract (see the Phase-1 harvest spec):
//   - Extract ONLY the first string-literal argument of each [TestCase("...", ...)].
//   - Handle both C# literal forms: regular "..." (with \" \\ \t \n escapes) and
//     verbatim @"..." (where "" -> " and backslash is literal).
//   - Skip commented-out cases (line // comments and /* */ block comments).
//   - Skip empty/whitespace-only extracted inputs.
//   - Also harvest HashedReleaseFixture's TestCaseSource data array (the first
//     string of each `new object[] { @"...".AsOsAgnostic(), ... }` row).
//
// Domain = source repo: sonarr fixtures -> "series", radarr -> "movie".
// Dedup is on the (input, domain) PAIR, against both the existing corpus and
// within the harvest. Existing entries are preserved first; harvested additions
// are appended in a stable (domain, input) order.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// corpusEntry mirrors the inputs.json schema.
type corpusEntry struct {
	Input  string `json:"input"`
	Domain string `json:"domain"`
}

const (
	domainSeries = "series"
	domainMovie  = "movie"
)

// fixtureSet is the curated allowlist of release-title-producing fixtures per
// repo. Paths are relative to each repo's ParserTests dir.
type fixtureSet struct {
	repoDir  string // submodule path containing src/...
	domain   string
	fixtures []string
}

var sonarrSet = fixtureSet{
	repoDir: "submodules/sonarr",
	domain:  domainSeries,
	fixtures: []string{
		"SingleEpisodeParserFixture.cs",
		"MultiEpisodeParserFixture.cs",
		"DailyEpisodeParserFixture.cs",
		"AbsoluteEpisodeNumberParserFixture.cs",
		"SeasonParserFixture.cs",
		"MiniSeriesEpisodeParserFixture.cs",
		"ParserFixture.cs",
		"ReleaseGroupParserFixture.cs",
		"QualityParserFixture.cs",
		"ExtendedQualityParserRegex.cs",
		"UnicodeReleaseParserFixture.cs",
		"AnimeMetadataParserFixture.cs",
		"AnimeVersionFixture.cs",
		"LanguageParserFixture.cs",
		"UrlFixture.cs",
		"CrapParserFixture.cs",
		"HashedReleaseFixture.cs",
	},
}

var radarrSet = fixtureSet{
	repoDir: "submodules/radarr",
	domain:  domainMovie,
	fixtures: []string{
		"ParserFixture.cs",
		"ReleaseGroupParserFixture.cs",
		"EditionParserFixture.cs",
		"QualityParserFixture.cs",
		"ExtendedQualityParserRegex.cs",
		"LanguageParserFixture.cs",
		"AnimeVersionFixture.cs",
		"UrlFixture.cs",
		"CrapParserFixture.cs",
		"HashedReleaseFixture.cs",
	},
}

const (
	parserTestsRel = "src/NzbDrone.Core.Test/ParserTests"
	inputsRel      = "backend/internal/parsing/testdata/inputs.json"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "corpusharvest:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := findRepoRoot()
	if err != nil {
		return err
	}
	inputsPath := filepath.Join(root, inputsRel)

	existing, err := loadExisting(inputsPath)
	if err != nil {
		return err
	}

	// seen tracks (input, domain) pairs already present so we never re-add.
	seen := map[corpusEntry]bool{}
	for _, e := range existing {
		seen[e] = true
	}

	var harvested []corpusEntry
	type fileStat struct {
		file  string
		count int
		err   error
	}
	var stats []fileStat

	for _, set := range []fixtureSet{sonarrSet, radarrSet} {
		for _, fx := range set.fixtures {
			path := filepath.Join(root, set.repoDir, parserTestsRel, fx)
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				stats = append(stats, fileStat{file: set.domain + "/" + fx, err: rerr})
				continue
			}
			inputs := extractInputs(string(src))
			for _, in := range inputs {
				e := corpusEntry{Input: in, Domain: set.domain}
				if seen[e] {
					continue
				}
				seen[e] = true
				harvested = append(harvested, e)
			}
			stats = append(stats, fileStat{file: set.domain + "/" + fx, count: len(inputs)})
		}
	}

	// Stable order for appended entries: by domain, then input.
	sort.SliceStable(harvested, func(i, j int) bool {
		if harvested[i].Domain != harvested[j].Domain {
			return harvested[i].Domain < harvested[j].Domain
		}
		return harvested[i].Input < harvested[j].Input
	})

	out := append(append([]corpusEntry{}, existing...), harvested...)
	if err := writeInputs(inputsPath, out); err != nil {
		return err
	}

	// Report.
	fmt.Println("Per-fixture extracted (raw string-literal) counts:")
	for _, s := range stats {
		if s.err != nil {
			fmt.Printf("  %-50s ERROR: %v\n", s.file, s.err)
			continue
		}
		fmt.Printf("  %-50s %d\n", s.file, s.count)
	}
	series, movie := 0, 0
	for _, e := range out {
		switch e.Domain {
		case domainSeries:
			series++
		case domainMovie:
			movie++
		}
	}
	fmt.Printf("\nExisting: %d | Newly appended: %d | New total: %d (series %d, movie %d)\n",
		len(existing), len(harvested), len(out), series, movie)
	return nil
}

// findRepoRoot walks up from the working directory to the repo root: the first
// ancestor containing both a `submodules` dir (the pinned Sonarr/Radarr sources)
// and a `backend` dir (where the corpus lives). This lets the harvester run from
// either the repo root or backend/ without path juggling.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		_, subErr := os.Stat(filepath.Join(dir, "submodules"))
		_, beErr := os.Stat(filepath.Join(dir, "backend"))
		if subErr == nil && beErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not locate repo root (a dir with both submodules/ and backend/) above %s", "cwd")
		}
		dir = parent
	}
}

func loadExisting(path string) ([]corpusEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read existing inputs: %w", err)
	}
	var entries []corpusEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("decode existing inputs: %w", err)
	}
	return entries, nil
}

func writeInputs(path string, entries []corpusEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// extractInputs returns the first string-literal of every active (non-commented)
// [TestCase(...)] in the source, plus the first string of each row in any
// TestCaseSource data array (HashedReleaseParserCases). The source is first
// scrubbed of /* */ block comments (string-literal aware), then scanned.
func extractInputs(src string) []string {
	src = stripBlockComments(src)
	var out []string
	out = append(out, extractTestCases(src)...)
	out = append(out, extractDataArray(src)...)
	return out
}

// stripBlockComments removes /* ... */ regions, replacing them with spaces so
// byte offsets within a line stay roughly intact for line-comment detection. It
// is string-literal aware so a "/*" inside a string is not treated as a comment
// opener. Line comments (//) are left in place and handled per-line later.
func stripBlockComments(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	const (
		stCode = iota
		stString
		stVerbatim
		stChar
		stLineComment
		stBlockComment
	)
	state := stCode
	for i := 0; i < len(src); i++ {
		c := src[i]
		next := byte(0)
		if i+1 < len(src) {
			next = src[i+1]
		}
		switch state {
		case stCode:
			switch {
			case c == '/' && next == '*':
				state = stBlockComment
				b.WriteByte(' ')
				b.WriteByte(' ')
				i++
			case c == '/' && next == '/':
				state = stLineComment
				b.WriteByte(c)
			case c == '@' && next == '"':
				state = stVerbatim
				b.WriteByte(c)
				b.WriteByte(next)
				i++
			case c == '"':
				state = stString
				b.WriteByte(c)
			case c == '\'':
				state = stChar
				b.WriteByte(c)
			default:
				b.WriteByte(c)
			}
		case stString:
			b.WriteByte(c)
			switch c {
			case '\\': // escape next char
				if i+1 < len(src) {
					b.WriteByte(next)
					i++
				}
			case '"':
				state = stCode
			}
		case stVerbatim:
			b.WriteByte(c)
			if c == '"' {
				if next == '"' { // escaped quote, stays in verbatim
					b.WriteByte(next)
					i++
				} else {
					state = stCode
				}
			}
		case stChar:
			b.WriteByte(c)
			switch c {
			case '\\':
				if i+1 < len(src) {
					b.WriteByte(next)
					i++
				}
			case '\'':
				state = stCode
			}
		case stLineComment:
			b.WriteByte(c)
			if c == '\n' {
				state = stCode
			}
		case stBlockComment:
			if c == '*' && next == '/' {
				state = stCode
				b.WriteByte(' ')
				b.WriteByte(' ')
				i++
			} else if c == '\n' {
				b.WriteByte('\n') // preserve line structure
			} else {
				b.WriteByte(' ')
			}
		}
	}
	return b.String()
}

// extractTestCases finds active [TestCase("...", ...)] attributes line by line.
// A line whose first non-space content is "//" is treated as commented out.
func extractTestCases(src string) []string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		idx := strings.Index(line, "[TestCase(")
		if idx < 0 {
			continue
		}
		// Guard: ensure the [TestCase( isn't itself preceded by a line comment
		// earlier on the line.
		if cm := strings.Index(line, "//"); cm >= 0 && cm < idx {
			continue
		}
		rest := line[idx+len("[TestCase("):]
		lit, ok := scanFirstStringLiteral(rest)
		if !ok {
			continue
		}
		if strings.TrimSpace(lit) == "" {
			continue
		}
		out = append(out, lit)
	}
	return out
}

// extractDataArray harvests the first string literal of each `new object[] {
// ... }` row inside a `public static object[] <Name> = { ... };` declaration.
// This is the HashedReleaseFixture's TestCaseSource shape. We locate each
// `new object[]` and scan forward for the first string literal that follows.
func extractDataArray(src string) []string {
	var out []string
	const marker = "new object[]"
	rest := src
	for {
		i := strings.Index(rest, marker)
		if i < 0 {
			break
		}
		after := rest[i+len(marker):]
		// The first string literal after the opening brace is the row's input.
		// Find the opening brace first to avoid grabbing something earlier.
		brace := strings.IndexByte(after, '{')
		if brace < 0 {
			break
		}
		lit, ok := scanFirstStringLiteral(after[brace+1:])
		if ok && strings.TrimSpace(lit) != "" {
			out = append(out, lit)
		}
		rest = after
	}
	return out
}

// scanFirstStringLiteral finds the first C# string literal (regular or verbatim)
// in s and returns its decoded value. Leading whitespace and other tokens before
// the opening quote are skipped. Returns ok=false if no string literal is found
// before a structural terminator that implies there is no string arg.
func scanFirstStringLiteral(s string) (string, bool) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '@' && i+1 < len(s) && s[i+1] == '"':
			return decodeVerbatim(s[i+2:])
		case c == '"':
			return decodeRegular(s[i+1:])
		case c == ' ' || c == '\t':
			continue
		default:
			// Some attributes lead with non-string args (rare for our fixtures,
			// where the input is always first). Keep scanning until a quote so we
			// don't miss e.g. `[TestCase( "x" )]` with odd spacing; but bail at a
			// closing paren which means no string arg at all.
			if c == ')' {
				return "", false
			}
		}
	}
	return "", false
}

// decodeRegular decodes a regular C# string literal body (content after the
// opening quote), stopping at the unescaped closing quote.
func decodeRegular(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' {
			if i+1 >= len(s) {
				return "", false
			}
			esc := s[i+1]
			i++
			switch esc {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '0':
				b.WriteByte(0)
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			case '\'':
				b.WriteByte('\'')
			default:
				// Unknown escape: keep the char as-is (rare in titles).
				b.WriteByte(esc)
			}
			continue
		}
		if c == '"' {
			return b.String(), true
		}
		b.WriteByte(c)
	}
	return "", false
}

// decodeVerbatim decodes a verbatim @"..." literal body. Inside, "" is a single
// quote and backslash is literal; a lone " terminates.
func decodeVerbatim(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			if i+1 < len(s) && s[i+1] == '"' {
				b.WriteByte('"')
				i++
				continue
			}
			return b.String(), true
		}
		b.WriteByte(c)
	}
	return "", false
}
