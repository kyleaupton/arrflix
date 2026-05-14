package feed

import (
	"context"
	"sort"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// Composer builds the complete home feed
type Composer struct {
	registry     *Registry
	sources      *TMDBSourceFactory
	heroStrategy HeroStrategy
	repo         *repo.Repository
	freshness    FreshnessTracker
}

// NewComposer creates a new feed composer
func NewComposer(
	registry *Registry,
	sources *TMDBSourceFactory,
	heroStrategy HeroStrategy,
	repo *repo.Repository,
	freshness FreshnessTracker,
) *Composer {
	return &Composer{
		registry:     registry,
		sources:      sources,
		heroStrategy: heroStrategy,
		repo:         repo,
		freshness:    freshness,
	}
}

// BuildFeed constructs the complete feed with hero and rows
func (c *Composer) BuildFeed(ctx context.Context) (*model.Feed, error) {
	// Initialize global dedupe set
	seen := make(map[string]bool)

	// 1. Select hero via strategy
	hero, heroKey, err := c.heroStrategy.SelectHero(ctx)
	if err != nil {
		// Log error but continue without hero
		hero = nil
	}
	if heroKey != "" {
		seen[heroKey] = true // Dedupe hero from rows
	}

	// 2. Select eligible rows
	hasSignals := false // TODO: Check for user signals when implemented
	eligibleRows := c.registry.SelectRows(ctx, hasSignals)

	// 3. Score and order rows dynamically
	orderedRows := c.registry.ScoreAndOrder(eligibleRows, c.freshness, hasSignals)

	// 4. Build each row
	rows := make([]model.FeedRow, 0)
	shownIntents := make([]model.RowIntent, 0)

	for _, def := range orderedRows {
		row, err := c.buildRow(ctx, def, seen)
		if err != nil {
			// Log error and skip this row
			continue
		}
		if len(row.Items) > 0 {
			rows = append(rows, row)
			shownIntents = append(shownIntents, def.Intent)
		}
	}

	// 5. Record shown rows for freshness tracking
	c.freshness.RecordShown(shownIntents)

	return &model.Feed{
		Hero: hero,
		Rows: rows,
	}, nil
}

// buildRow processes a single row definition
func (c *Composer) buildRow(ctx context.Context, def model.RowDefinition, seen map[string]bool) (model.FeedRow, error) {
	// Fetch candidates from all sources
	candidates := make([]model.Title, 0)
	for _, sourceConfig := range def.Sources {
		provider, err := c.sources.GetProvider(sourceConfig)
		if err != nil {
			continue // Skip invalid sources
		}

		titles, err := provider.Fetch(ctx, def.FetchSize)
		if err != nil {
			continue // Skip failed fetches
		}

		candidates = append(candidates, titles...)
	}

	if len(candidates) == 0 {
		return model.FeedRow{}, nil // Empty row
	}

	// Apply ranking
	candidates = c.applyRanking(candidates, def.Ranking)

	// Select items with dedupe and diversity
	selected := c.selectItems(candidates, def.TargetSize, def.Diversity, seen)

	// Mark selected items as globally seen
	for _, item := range selected {
		seen[item.TitleKey()] = true
	}

	// Hydrate with user overlay (IsInLibrary, IsDownloading)
	hydrated := c.hydrateUserOverlay(ctx, selected)

	return model.FeedRow{
		ID:       string(def.Intent),
		Title:    def.Title,
		Subtitle: def.Subtitle,
		Items:    hydrated,
	}, nil
}

// selectItems applies deduplication and diversity rules
func (c *Composer) selectItems(
	candidates []model.Title,
	target int,
	diversity *model.DiversityRules,
	seen map[string]bool,
) []model.Title {
	selected := make([]model.Title, 0, target)

	// Track diversity constraints
	genreCounts := make(map[int64]int)
	langCounts := make(map[string]int)

	for _, candidate := range candidates {
		if len(selected) >= target {
			break
		}

		key := candidate.TitleKey()

		// Skip if already seen globally
		if seen[key] {
			continue
		}

		// Check diversity constraints
		if diversity != nil && !c.passesDiversity(candidate, diversity, genreCounts, langCounts) {
			continue
		}

		selected = append(selected, candidate)

		// Update diversity tracking
		for _, gid := range candidate.GenreIDs {
			genreCounts[gid]++
		}
		if candidate.Language != "" {
			langCounts[candidate.Language]++
		}
	}

	return selected
}

// passesDiversity checks if a title passes diversity constraints
func (c *Composer) passesDiversity(
	t model.Title,
	rules *model.DiversityRules,
	genres map[int64]int,
	langs map[string]int,
) bool {
	if rules.MaxPerGenre > 0 {
		for _, gid := range t.GenreIDs {
			if genres[gid] >= rules.MaxPerGenre {
				return false
			}
		}
	}
	if rules.MaxPerLanguage > 0 && t.Language != "" {
		if langs[t.Language] >= rules.MaxPerLanguage {
			return false
		}
	}
	return true
}

// applyRanking sorts titles according to the ranking strategy
func (c *Composer) applyRanking(items []model.Title, strategy model.RankingStrategy) []model.Title {
	switch strategy {
	case model.RankingPopularity:
		sort.Slice(items, func(i, j int) bool {
			return items[i].Popularity > items[j].Popularity
		})
	case model.RankingRating:
		sort.Slice(items, func(i, j int) bool {
			return items[i].VoteAverage > items[j].VoteAverage
		})
	case model.RankingRecent:
		sort.Slice(items, func(i, j int) bool {
			return items[i].ReleaseDate > items[j].ReleaseDate
		})
	default:
		// keep original order
	}
	return items
}

// hydrateUserOverlay adds user-specific state to titles
func (c *Composer) hydrateUserOverlay(ctx context.Context, titles []model.Title) []model.HydratedTitle {
	hydrated := make([]model.HydratedTitle, len(titles))

	for i, t := range titles {
		hydrated[i] = model.HydratedTitle{
			Title:         t,
			IsInLibrary:   c.isInLibrary(ctx, t.TmdbID, t.MediaType),
			IsDownloading: c.hasActiveDownloads(ctx, t.TmdbID, t.MediaType),
		}
	}

	return hydrated
}

// isInLibrary checks if a title is in the user's library
func (c *Composer) isInLibrary(ctx context.Context, tmdbID int64, typ model.MediaType) bool {
	_, err := c.repo.GetMediaItemByTmdbIDAndType(ctx, tmdbID, string(typ))
	return err == nil
}

// hasActiveDownloads checks if a title has active download jobs
func (c *Composer) hasActiveDownloads(ctx context.Context, tmdbID int64, mediaType model.MediaType) bool {
	activeStatuses := map[string]bool{
		"created":     true,
		"enqueued":    true,
		"downloading": true,
		"importing":   true,
	}

	switch mediaType {
	case model.MediaTypeMovie:
		jobs, err := c.repo.ListDownloadJobsByTmdbMovieID(ctx, tmdbID)
		if err != nil {
			return false
		}
		for _, job := range jobs {
			if activeStatuses[job.Status] {
				return true
			}
		}
	case model.MediaTypeSeries:
		jobs, err := c.repo.ListDownloadJobsByTmdbSeriesID(ctx, tmdbID)
		if err != nil {
			return false
		}
		for _, job := range jobs {
			if activeStatuses[job.Status] {
				return true
			}
		}
	}

	return false
}
