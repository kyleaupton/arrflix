// Package acquisition implements the acquisition worker that claims pending
// wants and drives them through the autonomous front-half of acquisition
// (search → pick → enqueue → grabbed). The created download_job is then picked
// up by the download worker.
package acquisition

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/jobs/jobutil"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/realtime"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/sse"
)

// Worker claims pending wants and hands each to AcquisitionService.ProcessWant.
type Worker struct {
	repo   *repo.Repository
	svc    *service.AcquisitionService
	log    *logger.Logger
	broker *sse.Broker

	pollInterval time.Duration
	claimLimit   int32
	maxAttempts  int
	reapAfter    time.Duration
}

// Config holds worker configuration.
type Config struct {
	PollInterval time.Duration
	ClaimLimit   int32
	MaxAttempts  int
	// ReapAfter is how long a want may sit in 'searching' before the reaper
	// resets it to 'pending' — the crash-window self-heal.
	ReapAfter time.Duration
}

// DefaultConfig returns default worker configuration, matching the download
// worker's cadence.
func DefaultConfig() Config {
	return Config{
		PollInterval: 3 * time.Second,
		ClaimLimit:   20,
		MaxAttempts:  3,
		ReapAfter:    5 * time.Minute,
	}
}

// New creates a new acquisition worker.
func New(r *repo.Repository, svc *service.AcquisitionService, log *logger.Logger, broker *sse.Broker) *Worker {
	cfg := DefaultConfig()
	return &Worker{
		repo:         r,
		svc:          svc,
		log:          log,
		broker:       broker,
		pollInterval: cfg.PollInterval,
		claimLimit:   cfg.ClaimLimit,
		maxAttempts:  cfg.MaxAttempts,
		reapAfter:    cfg.ReapAfter,
	}
}

// Run starts the worker loop.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	w.log.Info().Msg("acquisition worker started")

	for {
		select {
		case <-ctx.Done():
			w.log.Info().Msg("acquisition worker stopped")
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	// Reap first: ClaimRunnableWants flips claimed wants to 'searching', and a
	// crash between that flip and a terminal transition wedges the want there
	// (the claim only reclaims 'pending'). The reaper's WHERE makes it a cheap
	// no-op when nothing is stale, so running it every tick is fine.
	w.reap(ctx)

	wants, err := w.repo.ClaimRunnableWants(ctx, w.claimLimit)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to claim wants")
		return
	}

	for _, want := range wants {
		// ClaimRunnableWants returned this want already flipped to 'searching'.
		realtime.Emit(ctx, w.broker, realtime.WantUpdated(want))
		w.handle(ctx, want)
	}
}

// reap resets wants wedged in 'searching' past the reapAfter window back to
// 'pending' so the next claim reclaims them. Reset-only: attempt_count is
// untouched and no want is failed, mirroring the "no release yet" reschedule.
func (w *Worker) reap(ctx context.Context) {
	staleBefore := time.Now().Add(-w.reapAfter)
	reclaimed, err := w.repo.ReclaimStaleSearchingWants(ctx, staleBefore, "reset from stale 'searching' (crash-window reaper)")
	if err != nil {
		w.log.Error().Err(err).Msg("failed to reclaim stale searching wants")
		return
	}
	for _, want := range reclaimed {
		w.log.Warn().
			Str("want_id", want.ID.String()).
			Msg("reclaimed stale searching want")
		realtime.Emit(ctx, w.broker, realtime.WantUpdated(want))
	}
}

func (w *Worker) handle(ctx context.Context, want model.Want) {
	grabbedWant, grabbed, err := w.svc.ProcessWant(ctx, want)
	if err != nil {
		w.handleError(ctx, want, err)
		return
	}

	if !grabbed {
		// No eligible release. Reschedule without a max-attempts ceiling — a
		// movie may simply not be out yet. Phase 4 replaces this with smart
		// scheduling and a real terminal.
		w.reschedule(ctx, want, "no eligible release")
		return
	}

	// grabbed: ProcessWant flipped the want to 'grabbed' in its tx and handed back
	// the updated row, so emit the transition for the frontend pill; the download
	// worker drives the subsequent 'downloading' delta.
	realtime.Emit(ctx, w.broker, realtime.WantUpdated(grabbedWant))
}

func (w *Worker) handleError(ctx context.Context, want model.Want, err error) {
	msg := err.Error()
	kind := apperrors.KindOf(err)

	w.log.Error().
		Err(err).
		Str("want_id", want.ID.String()).
		Str("kind", string(kind)).
		Msg("acquisition want error")

	// Non-retryable errors fail immediately.
	if !apperrors.IsRetryable(err) {
		w.fail(ctx, want.ID, msg)
		return
	}

	// Retryable errors: respect the max-attempts ceiling.
	attempt := int(want.AttemptCount) + 1
	if attempt >= w.maxAttempts {
		w.fail(ctx, want.ID, fmt.Sprintf("max attempts (%d) exceeded: %s", w.maxAttempts, msg))
		return
	}

	w.reschedule(ctx, want, msg)
}

// fail marks a want failed (terminal) and emits the transition. A superseded
// want (ok=false — the reaper reset it and another worker re-claimed) is a
// no-op: nothing changed, so nothing is emitted.
func (w *Worker) fail(ctx context.Context, wantID uuid.UUID, msg string) {
	if want, ok, err := w.repo.MarkWantFailed(ctx, wantID, msg); err == nil && ok {
		realtime.Emit(ctx, w.broker, realtime.WantUpdated(want))
	}
}

// reschedule returns the want to 'pending' with exponential backoff. A
// superseded want (ok=false — the reaper reset it and another worker
// re-claimed, including the benign grabbed=false path) is a no-op: the want is
// no longer 'searching', so nothing changed and nothing is emitted.
func (w *Worker) reschedule(ctx context.Context, want model.Want, lastError string) {
	attempt := int(want.AttemptCount) + 1
	nextRun := time.Now().Add(jobutil.Backoff(attempt))

	updated, ok, err := w.repo.ScheduleWantRetry(ctx, repo.ScheduleWantRetryParams{
		ID:        want.ID,
		LastError: lastError,
		NextRunAt: nextRun,
	})
	if err == nil && ok {
		realtime.Emit(ctx, w.broker, realtime.WantUpdated(updated))
	}
}
