// Package notification implements the delivery worker that drains the
// notification outbox. It is the single path from an enqueued row to a delivered
// (or dead) one: it claims queued rows past their backoff, hands each to its
// channel adapter, and advances the row's status. Producers only ever enqueue;
// nothing else transitions an outbox row.
package notification

import (
	"context"
	"time"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/jobs/jobutil"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/notifications"
	"github.com/kyleaupton/arrflix/internal/repo"
)

const (
	defaultPollInterval = 5 * time.Second
	defaultBatchSize    = 50
	// maxAttempts caps retries before a transiently-failing row is declared dead.
	// A row is delivered on attempts 0..maxAttempts-1, then killed.
	maxAttempts = 10
	// maxBackoff plateaus the exponential retry delay so an extended outage backs
	// off to a steady ceiling rather than a delay measured in days.
	maxBackoff = time.Hour
	// defaultReapAfter is how long a row may sit in 'delivering' before the reaper
	// returns it to the queue — the crash-window self-heal, matching the
	// AcquisitionWorker's window for wants wedged in 'searching'. It sits well
	// beyond any legitimate delivery: the push and SMTP transports each cap a
	// single send at 10s, so a row still 'delivering' minutes later means the
	// process died mid-flight rather than that the adapter is slow.
	defaultReapAfter = 5 * time.Minute
)

// staleDeliveringError is the last_error the reaper stamps on a row it heals, so
// an operator reading the row can tell a crash-window reclaim apart from an
// adapter's own failure.
const staleDeliveringError = "reset from stale 'delivering' (crash-window reaper)"

// Worker drains the outbox on a fixed cadence.
type Worker struct {
	repo         *repo.Repository
	adapters     map[model.NotificationChannel]notifications.ChannelAdapter
	log          *logger.Logger
	pollInterval time.Duration
	batchSize    int32
	reapAfter    time.Duration
}

// New builds the worker, parsing and verifying templates up front: a missing or
// malformed template for a wired channel is a loud startup error here, not a
// first-delivery surprise. Returns an error the caller (main) should treat as
// fatal.
func New(r *repo.Repository, log *logger.Logger, adapters ...notifications.ChannelAdapter) (*Worker, error) {
	renderer, err := notifications.NewRenderer()
	if err != nil {
		return nil, err
	}
	byChannel := make(map[model.NotificationChannel]notifications.ChannelAdapter, len(adapters))
	channels := make([]model.NotificationChannel, 0, len(adapters))
	for _, a := range adapters {
		byChannel[a.Name()] = a
		channels = append(channels, a.Name())
	}
	if err := renderer.Verify(notifications.Registered, channels); err != nil {
		return nil, err
	}
	return &Worker{
		repo:         r,
		adapters:     byChannel,
		log:          log,
		pollInterval: defaultPollInterval,
		batchSize:    defaultBatchSize,
		reapAfter:    defaultReapAfter,
	}, nil
}

// Run polls the outbox until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	w.log.Info().Msg("notification worker started")
	for {
		select {
		case <-ctx.Done():
			w.log.Info().Msg("notification worker stopped")
			return
		case <-ticker.C:
			if n, err := w.Drain(ctx); err != nil {
				w.log.Error().Err(err).Msg("notification drain failed")
			} else if n > 0 {
				w.log.Debug().Int("delivered", n).Msg("drained notifications")
			}
		}
	}
}

// Drain processes one batch of due rows and returns how many it delivered.
// Exposed so tests can drive a single pass without the ticker.
func (w *Worker) Drain(ctx context.Context) (int, error) {
	// Reap first: MarkOutboxDelivering flips a claimed row to 'delivering', and a
	// crash before it settles wedges the row there — ListDueOutbox only selects
	// 'queued', so nothing would ever look at it again. The reaper's WHERE makes
	// it a cheap no-op when nothing is stale, so running it every pass is fine.
	w.reap(ctx)

	rows, err := w.repo.ListDueOutbox(ctx, w.batchSize)
	if err != nil {
		return 0, err
	}
	delivered := 0
	for _, row := range rows {
		if w.handle(ctx, row) {
			delivered++
		}
	}
	return delivered, nil
}

// reap returns rows wedged in 'delivering' past the reapAfter window to the
// queue so the next claim picks them up. A reclaim is at-least-once: the adapter
// may have completed the send before the crash, so the recipient can see the
// notification twice. That is the deliberate trade — the alternative is losing it
// outright, and delivery has been at-least-once since the first retry.
//
// A reap failure is logged and swallowed: it is a self-heal, and failing the
// drain over it would block the rows that are fine.
func (w *Worker) reap(ctx context.Context) {
	reclaimed, err := w.repo.ReclaimStaleDelivering(ctx, time.Now().Add(-w.reapAfter), staleDeliveringError)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to reclaim stale delivering notifications")
		return
	}
	for _, row := range reclaimed {
		w.log.Warn().
			Str("id", row.ID.String()).
			Str("channel", row.Channel).
			Int32("attempts", row.Attempts).
			Msg("reclaimed stale delivering notification")
	}
}

// handle drives one row through its adapter and reports whether it was delivered.
func (w *Worker) handle(ctx context.Context, row model.NotificationOutbox) bool {
	adapter, ok := w.adapters[model.NotificationChannel(row.Channel)]
	if !ok {
		// No adapter serves this channel — permanent. Killing it stops a row we
		// can never deliver from polling forever.
		w.markDead(ctx, row.ID, "no adapter for channel "+row.Channel)
		return false
	}

	// The channel's adapter may not be configured yet (email without SMTP). Park
	// the row as awaiting_config rather than claiming it: ListDueOutbox skips
	// non-queued rows, so it waits until a provider save requeues it. This also
	// covers config removed after enqueue. Checked before the claim so an
	// unconfigured row never enters the delivering state.
	if !adapter.IsConfigured(ctx) {
		if _, err := w.repo.MarkOutboxAwaitingConfig(ctx, row.ID); err != nil && !apperrors.IsNotFound(err) {
			w.log.Error().Err(err).Str("id", row.ID.String()).Msg("park notification awaiting_config failed")
		}
		return false
	}

	// Claim the row with a CAS on status='queued'. A NotFound means another
	// worker beat us to it between poll and claim — skip without touching it.
	claimed, err := w.repo.MarkOutboxDelivering(ctx, row.ID)
	if err != nil {
		if !apperrors.IsNotFound(err) {
			w.log.Error().Err(err).Str("id", row.ID.String()).Msg("claim notification failed")
		}
		return false
	}

	if err := adapter.Deliver(ctx, claimed); err != nil {
		w.retryOrKill(ctx, claimed, err)
		return false
	}
	if _, err := w.repo.MarkOutboxDelivered(ctx, claimed.ID); err != nil {
		w.log.Error().Err(err).Str("id", claimed.ID.String()).Msg("mark delivered failed")
		return false
	}
	return true
}

// retryOrKill reschedules a transient failure with capped exponential backoff, or
// marks the row dead when the failure is permanent or the attempt budget is spent.
func (w *Worker) retryOrKill(ctx context.Context, row model.NotificationOutbox, cause error) {
	msg := cause.Error()
	if !apperrors.IsRetryable(cause) || int(row.Attempts)+1 >= maxAttempts {
		w.markDead(ctx, row.ID, msg)
		return
	}
	next := time.Now().Add(jobutil.BackoffCapped(int(row.Attempts)+1, maxBackoff))
	if _, err := w.repo.RescheduleOutbox(ctx, repo.RescheduleOutboxParams{
		ID:            row.ID,
		NextAttemptAt: next,
		LastError:     &msg,
	}); err != nil {
		w.log.Error().Err(err).Str("id", row.ID.String()).Msg("reschedule notification failed")
	}
}

func (w *Worker) markDead(ctx context.Context, id uuid.UUID, msg string) {
	if _, err := w.repo.MarkOutboxDead(ctx, id, &msg); err != nil {
		w.log.Error().Err(err).Str("id", id.String()).Msg("mark notification dead failed")
	}
}
