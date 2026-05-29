package resolvers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/matcher"
	"github.com/kyleaupton/arrflix/internal/metadata"
)

func TestPathEmbed_Movie_TMDB(t *testing.T) {
	t.Parallel()
	pe := NewPathEmbed()
	res, err := pe.Resolve(context.Background(), matcher.FileRef{
		ID:   uuid.New(),
		Path: "/media/movies/A Minecraft Movie (2025) {tmdb-950387}.mp4",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d (%+v)", len(res.Candidates), res.Candidates)
	}
	c := res.Candidates[0]
	if c.Ref.Source != metadata.SourceTMDB || c.Ref.ExternalID != "950387" {
		t.Fatalf("expected tmdb:950387, got %s:%s", c.Ref.Source, c.Ref.ExternalID)
	}
	if c.Confidence != 1.0 {
		t.Fatalf("expected confidence 1.0 (resolver-local), got %v", c.Confidence)
	}
	if c.Episode != nil {
		t.Fatalf("expected nil episode on movie, got %+v", c.Episode)
	}
}

func TestPathEmbed_Series_TVDB_WithEpisode(t *testing.T) {
	t.Parallel()
	pe := NewPathEmbed()
	res, err := pe.Resolve(context.Background(), matcher.FileRef{
		ID:   uuid.New(),
		Path: "/media/tv/South Park (1997) {tvdb-75897}/Season 27/South Park (1997) - S27E02 - Got A Nut [WEBDL-1080p].mkv",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(res.Candidates))
	}
	c := res.Candidates[0]
	if c.Ref.Source != metadata.SourceTVDB || c.Ref.ExternalID != "75897" {
		t.Fatalf("expected tvdb:75897, got %s:%s", c.Ref.Source, c.Ref.ExternalID)
	}
	if c.Episode == nil || c.Episode.Season != 27 || c.Episode.Episode != 2 {
		t.Fatalf("expected episode S27E02, got %+v", c.Episode)
	}
}

func TestPathEmbed_Movie_IMDB_PreservesTTPrefix(t *testing.T) {
	t.Parallel()
	pe := NewPathEmbed()
	res, err := pe.Resolve(context.Background(), matcher.FileRef{
		ID:   uuid.New(),
		Path: "/media/movies/A Minecraft Movie (2025) {imdb-tt3566834}.mp4",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(res.Candidates))
	}
	c := res.Candidates[0]
	if c.Ref.Source != metadata.SourceIMDB || c.Ref.ExternalID != "tt3566834" {
		t.Fatalf("expected imdb:tt3566834, got %s:%s", c.Ref.Source, c.Ref.ExternalID)
	}
}

func TestPathEmbed_NoMatch(t *testing.T) {
	t.Parallel()
	pe := NewPathEmbed()
	res, err := pe.Resolve(context.Background(), matcher.FileRef{
		ID:   uuid.New(),
		Path: "/media/movies/Some Random Movie (2020).mkv",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Candidates) != 0 {
		t.Fatalf("expected 0 candidates, got %d", len(res.Candidates))
	}
	if len(res.Evidence) != 0 {
		t.Fatalf("expected empty evidence on no-match, got %s", res.Evidence)
	}
}

func TestPathEmbed_DedupSameRefInFolderAndFilename(t *testing.T) {
	t.Parallel()
	pe := NewPathEmbed()
	res, err := pe.Resolve(context.Background(), matcher.FileRef{
		ID:   uuid.New(),
		Path: "/media/movies/21 Jump Street (2012) {tmdb-64688}/21 Jump Street (2012) {tmdb-64688} [Bluray-1080p].mkv",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// Both folder and filename embed the same tmdb-64688; dedup leaves one.
	if len(res.Candidates) != 1 {
		t.Fatalf("expected 1 candidate after dedup, got %d", len(res.Candidates))
	}
}

func TestPathEmbed_MultipleDifferentSources(t *testing.T) {
	t.Parallel()
	pe := NewPathEmbed()
	res, err := pe.Resolve(context.Background(), matcher.FileRef{
		ID:   uuid.New(),
		Path: "/media/movies/Movie {tmdb-1} {imdb-tt2}/Movie.mkv",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Candidates) != 2 {
		t.Fatalf("expected 2 candidates (tmdb + imdb), got %d", len(res.Candidates))
	}
	// Confidence stays at 1.0 per resolver — corroboration is the aggregator's
	// job, not the resolver's.
	for _, c := range res.Candidates {
		if c.Confidence != 1.0 {
			t.Fatalf("expected confidence 1.0 per candidate, got %v", c.Confidence)
		}
	}
}

func TestPathEmbed_BracketStyle(t *testing.T) {
	t.Parallel()
	// Some libraries use [tmdb-X] instead of {tmdb-X}. The regex is
	// permissive on delimiters, so both shapes work.
	pe := NewPathEmbed()
	res, err := pe.Resolve(context.Background(), matcher.FileRef{
		ID:   uuid.New(),
		Path: "/media/movies/Movie (2020) [tmdb-999]/Movie.mkv",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(res.Candidates))
	}
	if res.Candidates[0].Ref.ExternalID != "999" {
		t.Fatalf("expected id 999, got %q", res.Candidates[0].Ref.ExternalID)
	}
}

func TestPathEmbed_EvidenceCarriesRawMatch(t *testing.T) {
	t.Parallel()
	pe := NewPathEmbed()
	res, err := pe.Resolve(context.Background(), matcher.FileRef{
		ID:   uuid.New(),
		Path: "/media/tv/Show {tvdb-100}/S02E05.mkv",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Evidence) == 0 {
		t.Fatalf("expected evidence payload")
	}
	var ev struct {
		Matches []struct {
			Source string `json:"source"`
			ID     string `json:"id"`
			Raw    string `json:"raw"`
		} `json:"matches"`
		Episode *matcher.EpisodeRef `json:"episode,omitempty"`
	}
	if err := json.Unmarshal(res.Evidence, &ev); err != nil {
		t.Fatalf("unmarshal evidence: %v", err)
	}
	if len(ev.Matches) != 1 || ev.Matches[0].Source != "tvdb" || ev.Matches[0].ID != "100" {
		t.Fatalf("unexpected evidence matches: %+v", ev.Matches)
	}
	if ev.Episode == nil || ev.Episode.Season != 2 || ev.Episode.Episode != 5 {
		t.Fatalf("unexpected episode in evidence: %+v", ev.Episode)
	}
}

func TestPathEmbed_NoFalsePositiveOnSimilarTokens(t *testing.T) {
	t.Parallel()
	// "tmdb-stats" lacks the digit-only suffix the regex requires, so a
	// folder by that name doesn't false-match.
	pe := NewPathEmbed()
	res, err := pe.Resolve(context.Background(), matcher.FileRef{
		ID:   uuid.New(),
		Path: "/media/tmdb-stats/Movie (2020).mkv",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Candidates) != 0 {
		t.Fatalf("expected 0 candidates, got %d", len(res.Candidates))
	}
}

func TestPathEmbed_UnicodeAndSpaceInTitle(t *testing.T) {
	t.Parallel()
	// Title carries non-ASCII characters; the regex anchors on the
	// provider-id token, so the surrounding title doesn't matter.
	pe := NewPathEmbed()
	res, err := pe.Resolve(context.Background(), matcher.FileRef{
		ID:   uuid.New(),
		Path: "/media/movies/喜剧之王 (1999) {tmdb-11820}/喜剧之王.mkv",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Candidates) != 1 || res.Candidates[0].Ref.ExternalID != "11820" {
		t.Fatalf("expected tmdb:11820, got %+v", res.Candidates)
	}
}

func TestPathEmbed_LowercaseSeasonEpisode(t *testing.T) {
	t.Parallel()
	// Lowercase s##e## is a common Plex shape; the regex matches both
	// cases (matching the original identity.go regex).
	pe := NewPathEmbed()
	res, err := pe.Resolve(context.Background(), matcher.FileRef{
		ID:   uuid.New(),
		Path: "/media/tv/Show {tvdb-1}/Season 1/show s01e03.mkv",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(res.Candidates))
	}
	ep := res.Candidates[0].Episode
	if ep == nil || ep.Season != 1 || ep.Episode != 3 {
		t.Fatalf("expected S01E03, got %+v", ep)
	}
}

// Compile-time check.
var _ matcher.Resolver = (*PathEmbed)(nil)
