// Package acquisition implements the acquisition worker that claims pending
// wants and drives them through the autonomous front-half of acquisition
// (search → pick → enqueue → grabbed). The created download_job is then picked
// up by the download worker.
package acquisition

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
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
}

// Config holds worker configuration.
type Config struct {
	PollInterval time.Duration
	ClaimLimit   int32
	MaxAttempts  int
}

// DefaultConfig returns default worker configuration, matching the download
// worker's cadence.
func DefaultConfig() Config {
	return Config{
		PollInterval: 3 * time.Second,
		ClaimLimit:   20,
		MaxAttempts:  3,
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
	// ClaimRunnableWants flips claimed wants to 'searching'. A crash between
	// here and a terminal transition wedges the want in 'searching', since the
	// claim only reclaims 'pending'. A stale-claim reaper is Phase 5.
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

func (w *Worker) handle(ctx context.Context, want model.Want) {
	grabbed, err := w.svc.ProcessWant(ctx, want)
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

	// grabbed: ProcessWant already flipped the want to 'grabbed' in its tx.
	// Re-read post-commit to emit the transition for the frontend pill; the
	// download worker drives the subsequent 'downloading' delta.
	if grabbedWant, err := w.repo.GetWant(ctx, want.ID); err == nil {
		realtime.Emit(ctx, w.broker, realtime.WantUpdated(grabbedWant))
	}
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

// fail marks a want failed (terminal) and emits the transition.
func (w *Worker) fail(ctx context.Context, wantID uuid.UUID, msg string) {
	if want, err := w.repo.MarkWantFailed(ctx, wantID, msg); err == nil {
		realtime.Emit(ctx, w.broker, realtime.WantUpdated(want))
	}
}

// reschedule returns the want to 'pending' with exponential backoff.
func (w *Worker) reschedule(ctx context.Context, want model.Want, lastError string) {
	attempt := int(want.AttemptCount) + 1
	backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
	nextRun := time.Now().Add(backoff)

	updated, err := w.repo.ScheduleWantRetry(ctx, repo.ScheduleWantRetryParams{
		ID:        want.ID,
		LastError: lastError,
		NextRunAt: nextRun,
	})
	if err == nil {
		realtime.Emit(ctx, w.broker, realtime.WantUpdated(updated))
	}
}
