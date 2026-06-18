// Package acquisition implements the acquisition worker that claims pending
// wants and drives them through the autonomous front-half of acquisition
// (search → pick → enqueue → grabbed). The created download_job is then picked
// up by the download worker.
package acquisition

import (
	"context"
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
	repo      *repo.Repository
	svc       *service.AcquisitionService
	scheduler *service.SchedulerService
	log       *logger.Logger
	broker    *sse.Broker

	pollInterval    time.Duration
	claimLimit      int32
	maxRetryBackoff time.Duration
	reapAfter       time.Duration
}

// Config holds worker configuration.
type Config struct {
	PollInterval time.Duration
	ClaimLimit   int32
	// MaxRetryBackoff caps the per-want retry backoff. A retryable front-half
	// error never terminally fails an in-scope want — it backs off and retries
	// indefinitely — so the exponential ramp plateaus here: during an outage the
	// want retries at most once per this interval.
	MaxRetryBackoff time.Duration
	// ReapAfter is how long a want may sit in 'searching' before the reaper
	// resets it to 'pending' — the crash-window self-heal.
	ReapAfter time.Duration
}

// DefaultConfig returns default worker configuration, matching the download
// worker's cadence.
func DefaultConfig() Config {
	return Config{
		PollInterval:    3 * time.Second,
		ClaimLimit:      20,
		MaxRetryBackoff: time.Hour,
		ReapAfter:       5 * time.Minute,
	}
}

// New creates a new acquisition worker with the default configuration.
func New(r *repo.Repository, svc *service.AcquisitionService, scheduler *service.SchedulerService, log *logger.Logger, broker *sse.Broker) *Worker {
	return NewWithConfig(r, svc, scheduler, log, broker, DefaultConfig())
}

// NewWithConfig creates a new acquisition worker with explicit configuration —
// the seam tests use to drive the loop on a fast cadence.
func NewWithConfig(r *repo.Repository, svc *service.AcquisitionService, scheduler *service.SchedulerService, log *logger.Logger, broker *sse.Broker, cfg Config) *Worker {
	return &Worker{
		repo:            r,
		svc:             svc,
		scheduler:       scheduler,
		log:             log,
		broker:          broker,
		pollInterval:    cfg.PollInterval,
		claimLimit:      cfg.ClaimLimit,
		maxRetryBackoff: cfg.MaxRetryBackoff,
		reapAfter:       cfg.ReapAfter,
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
		// No eligible release — the title may simply not be out yet, but the
		// indexer was reached. Recheck on the air-date-aware cadence and reset
		// attempt_count: a successful reach clears the consecutive-error counter so
		// a later infra blip backs off from scratch.
		w.recheck(ctx, want, "no eligible release")
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

	// A non-retryable error is genuine bad state — terminally fail the want.
	if !apperrors.IsRetryable(err) {
		w.fail(ctx, want.ID, msg)
		return
	}

	// A retryable error is a transient upstream condition (an indexer briefly
	// unreachable), never a reason to abandon an in-scope, unacquired want. Back
	// off (capped) and retry indefinitely — the want recovers on its own when the
	// indexer returns.
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

// recheckFallback is the conservative cadence used when the scheduler can't
// resolve a want's curve — a transient repo blip must not wedge the want, so it
// rechecks in a few hours rather than not at all.
const recheckFallback = 6 * time.Hour

// recheck returns the want to 'pending' on the air-date-aware cadence and resets
// attempt_count — the "no eligible release yet" path, where the indexer was
// reached so the consecutive-error counter clears. The cadence rides the want's
// release anchor: a just-aired episode rechecks on the ~15m peak tier, a cold
// back-catalog gap on the ~daily tier. The pick path is untouched, so a future
// RSS source can feed results into the same grab core unchanged. A superseded
// want (ok=false — the reaper reset it and another worker re-claimed) is a
// no-op: the want is no longer 'searching', so nothing changed and nothing is
// emitted.
func (w *Worker) recheck(ctx context.Context, want model.Want, lastError string) {
	now := time.Now()
	nextRun, err := w.scheduler.NextSearchAt(ctx, want, now)
	if err != nil {
		w.log.Warn().
			Err(err).
			Str("want_id", want.ID.String()).
			Msg("failed to resolve recheck cadence; falling back to conservative interval")
		nextRun = now.Add(recheckFallback)
	}

	updated, ok, err := w.repo.RescheduleWantRecheck(ctx, repo.ScheduleWantRetryParams{
		ID:        want.ID,
		LastError: lastError,
		NextRunAt: nextRun,
	})
	if err == nil && ok {
		realtime.Emit(ctx, w.broker, realtime.WantUpdated(updated))
	}
}

// reschedule returns the want to 'pending' with a capped exponential backoff and
// bumps attempt_count — the transient-error retry path. The backoff is capped at
// maxRetryBackoff so an indefinitely retried want plateaus at a steady ceiling
// during an outage rather than ramping unbounded. A superseded want (ok=false —
// the reaper reset it and another worker re-claimed) is a no-op: the want is no
// longer 'searching', so nothing changed and nothing is emitted.
func (w *Worker) reschedule(ctx context.Context, want model.Want, lastError string) {
	attempt := int(want.AttemptCount) + 1
	nextRun := time.Now().Add(jobutil.BackoffCapped(attempt, w.maxRetryBackoff))

	updated, ok, err := w.repo.ScheduleWantRetry(ctx, repo.ScheduleWantRetryParams{
		ID:        want.ID,
		LastError: lastError,
		NextRunAt: nextRun,
	})
	if err == nil && ok {
		realtime.Emit(ctx, w.broker, realtime.WantUpdated(updated))
	}
}
