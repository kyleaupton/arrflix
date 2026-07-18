// Package session implements the background sweeper that reclaims terminal
// (revoked or expired) session rows once they age past the retention window.
package session

import (
	"context"
	"time"

	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/service"
)

const (
	// sweepInterval is the GC cadence. The app never reads terminal session rows,
	// so nothing depends on this being tight — twice a day keeps the table small.
	sweepInterval = 12 * time.Hour
	// sessionRetention keeps a terminal session row queryable for this long past
	// the point it went terminal, as a debugging buffer. The durable auth record
	// is auth_audit, so this can afford to be short.
	sessionRetention = 7 * 24 * time.Hour
)

// Worker periodically hard-deletes terminal session rows.
type Worker struct {
	sessions *service.SessionService
	log      *logger.Logger
}

// New creates a new session-sweeper worker.
func New(sessions *service.SessionService, log *logger.Logger) *Worker {
	return &Worker{sessions: sessions, log: log}
}

// Run sweeps once on startup, then on the sweep cadence, until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	w.log.Info().Msg("session sweeper started")
	w.tick(ctx)

	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			w.log.Info().Msg("session sweeper stopped")
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	n, err := w.sessions.SweepTerminal(ctx, sessionRetention)
	if err != nil {
		w.log.Error().Err(err).Msg("session sweep failed")
		return
	}
	if n > 0 {
		w.log.Info().Int64("count", n).Msg("swept terminal sessions")
	}
}
