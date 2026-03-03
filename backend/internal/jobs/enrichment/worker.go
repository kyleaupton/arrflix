// Package enrichment implements the background metadata enrichment worker.
package enrichment

import (
	"context"
	"time"

	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/service"
)

const (
	batchSize          = 100
	stalenessThreshold = 7 * 24 * time.Hour
	backfillPause      = 5 * time.Second
	refreshInterval    = 15 * time.Minute
)

// Worker polls for media items with stale or missing metadata and enriches them.
type Worker struct {
	enrichment *service.EnrichmentService
	log        *logger.Logger
}

// New creates a new enrichment worker.
func New(enrichment *service.EnrichmentService, log *logger.Logger) *Worker {
	return &Worker{enrichment: enrichment, log: log}
}

// Run starts the enrichment worker with a two-phase strategy:
// Phase 1 (backfill): tight loop processing batches until caught up.
// Phase 2 (refresh): 15-minute ticker for ongoing staleness refresh.
func (w *Worker) Run(ctx context.Context) {
	w.log.Info().Msg("enrichment worker started, beginning backfill")

	// Phase 1: Aggressive backfill — tight loop until caught up
	for {
		enriched := w.tick(ctx)
		if enriched == 0 {
			break
		}
		w.log.Info().Int("count", enriched).Msg("backfill batch complete")
		select {
		case <-ctx.Done():
			w.log.Info().Msg("enrichment worker stopped during backfill")
			return
		case <-time.After(backfillPause):
		}
	}
	w.log.Info().Msg("backfill complete, switching to refresh cadence")

	// Phase 2: Normal cadence for ongoing staleness refresh
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			w.log.Info().Msg("enrichment worker stopped")
			return
		case <-ticker.C:
			if enriched := w.tick(ctx); enriched > 0 {
				w.log.Info().Int("count", enriched).Msg("enriched media items")
			}
		}
	}
}

func (w *Worker) tick(ctx context.Context) int {
	staleBefore := time.Now().Add(-stalenessThreshold)
	enriched, err := w.enrichment.EnrichBatch(ctx, staleBefore, batchSize)
	if err != nil {
		w.log.Error().Err(err).Msg("enrichment batch failed")
		return 0
	}
	return enriched
}
