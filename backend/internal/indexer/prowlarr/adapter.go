package prowlarr

import (
	"context"
	"fmt"
	"strings"

	"golift.io/starr/prowlarr"

	"github.com/kyleaupton/arrflix/internal/indexer"
	"github.com/kyleaupton/arrflix/internal/logger"
)

// ProwlarrSource implements IndexerSource using Prowlarr as the backend.
type ProwlarrSource struct {
	client *prowlarr.Prowlarr
	logger *logger.Logger
}

// New creates a new ProwlarrSource.
func New(client *prowlarr.Prowlarr, logger *logger.Logger) *ProwlarrSource {
	return &ProwlarrSource{
		client: client,
		logger: logger,
	}
}

// Search performs a search query against Prowlarr and returns validated results.
func (p *ProwlarrSource) Search(ctx context.Context, query indexer.SearchQuery) ([]indexer.SearchResult, error) {
	input := prowlarr.SearchInput{
		Query: query.Query,
		Limit: query.Limit,
	}

	// Set search type based on media type
	switch query.MediaType {
	case indexer.MediaTypeSeries:
		input.Type = "tvsearch"
	default:
		input.Type = "search"
	}

	results, err := p.client.SearchContext(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("prowlarr search: %w", err)
	}

	// Map and validate results
	validated := make([]indexer.SearchResult, 0, len(results))
	for _, r := range results {
		sr, err := p.mapResult(r)
		if err != nil {
			p.logger.Debug().
				Str("guid", r.GUID).
				Str("title", r.Title).
				Err(err).
				Msg("Filtering invalid search result")
			continue
		}
		validated = append(validated, sr)
	}

	p.logger.Debug().
		Str("query", query.Query).
		Int("raw_count", len(results)).
		Int("valid_count", len(validated)).
		Msg("Prowlarr search completed")

	return validated, nil
}

// ListIndexers returns information about all configured indexers.
func (p *ProwlarrSource) ListIndexers(ctx context.Context) ([]indexer.IndexerInfo, error) {
	indexers, err := p.client.GetIndexersContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("prowlarr get indexers: %w", err)
	}

	result := make([]indexer.IndexerInfo, len(indexers))
	for i, idx := range indexers {
		result[i] = indexer.IndexerInfo{
			ID:       idx.ID,
			Name:     idx.Name,
			Protocol: string(idx.Protocol),
			Enabled:  idx.Enable,
		}
	}

	return result, nil
}

// Test verifies connectivity to Prowlarr.
func (p *ProwlarrSource) Test(ctx context.Context) error {
	_, err := p.client.GetIndexersContext(ctx)
	if err != nil {
		return fmt.Errorf("prowlarr connectivity test: %w", err)
	}
	return nil
}

// mapResult converts a prowlarr.Search to an indexer.SearchResult with validation.
// Returns an error if the result is invalid and should be filtered out.
func (p *ProwlarrSource) mapResult(r *prowlarr.Search) (indexer.SearchResult, error) {
	// Handle Prowlarr quirks - find download URL with fallbacks
	// Priority: DownloadURL > GUID magnet (has trackers) > constructed magnet (bare)
	downloadURL := r.DownloadURL
	if downloadURL == "" {
		// Check if GUID is a magnet link (preferred - includes trackers)
		if strings.HasPrefix(r.GUID, "magnet:") {
			downloadURL = r.GUID
		}
	}
	if downloadURL == "" {
		// Last resort: construct bare magnet from info hash
		if r.InfoHash != "" {
			downloadURL = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", r.InfoHash, r.Title)
		}
	}

	// Validate required fields
	if downloadURL == "" {
		return indexer.SearchResult{}, fmt.Errorf("no download URL available")
	}
	if r.Title == "" {
		return indexer.SearchResult{}, fmt.Errorf("empty title")
	}

	// Extract categories
	categories := make([]string, 0, len(r.Categories))
	for _, cat := range r.Categories {
		if cat != nil && cat.Name != "" {
			categories = append(categories, cat.Name)
		}
	}

	// Map seeders/leechers to pointers (they may be 0 for usenet)
	var seeders, leechers *int
	if r.Seeders > 0 || string(r.Protocol) == "torrent" {
		seeders = &r.Seeders
	}
	if r.Leechers > 0 || string(r.Protocol) == "torrent" {
		leechers = &r.Leechers
	}

	return indexer.SearchResult{
		IndexerID:    r.IndexerID,
		IndexerName:  r.Indexer,
		GUID:         r.GUID,
		Title:        r.Title,
		DownloadURL:  downloadURL,
		Protocol:     string(r.Protocol),
		Size:         r.Size,
		Seeders:      seeders,
		Leechers:     leechers,
		Age:          r.Age,
		AgeHours:     r.AgeHours,
		PublishDate:  r.PublishDate,
		Categories:   categories,
		Grabs:        r.Grabs,
		IndexerFlags: r.IndexerFlags,
	}, nil
}
