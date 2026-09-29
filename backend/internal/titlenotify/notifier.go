// Package titlenotify coalesces title-status invalidations and emits them as
// realtime kicks.
//
// Acquisition writes are bursty in a way that maps badly onto per-row events: a
// season-pack import transitions every episode of a season, and a scan can touch
// a whole library. Emitting one event per write pushes that burst onto every
// client. Callers here announce "this media item changed" — a mutex and a map
// insert, cheap enough to sit on any hot path — and the notifier collapses a
// window of those into at most one event per title.
//
// A periodic sweep backs the explicit calls. Any write that reaches the database
// without a matching Notify is picked up within a sweep interval, which is what
// makes a forgotten call site a latency bug rather than a stale-UI bug. Explicit
// calls exist for immediacy; the sweep exists for correctness.
//
// The package holds no repository. Resolving a media item to its wire identity
// and finding recently-touched items are both injected, so this stays a
// coalescing and emitting concern and can be tested without a database.
package titlenotify

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/realtime"
	"github.com/kyleaupton/arrflix/internal/sse"
)

// Ref is a title's wire identity. Clients key on TMDB id, not on our media item
// id, so every kick has to carry the former.
type Ref struct {
	MediaType string
	TmdbID    int64
}

// Resolver maps a media item id to its wire identity.
type Resolver func(ctx context.Context, mediaItemID uuid.UUID) (Ref, error)

// Sweeper returns the media items whose acquisition-relevant rows changed at or
// after since.
type Sweeper func(ctx context.Context, since time.Time) ([]uuid.UUID, error)

const (
	// defaultFlushInterval bounds how long a change waits before it reaches the
	// client. Short enough to read as immediate, long enough to swallow the
	// write burst of a season-pack import.
	defaultFlushInterval = 250 * time.Millisecond

	// defaultSweepInterval bounds how stale a missed invalidation can get.
	defaultSweepInterval = 30 * time.Second

	// sweepGrace re-reads a window either side of the watermark. A row committed
	// by a transaction that started before the previous sweep's snapshot can
	// carry an updated_at earlier than the watermark it lands after; without the
	// overlap it would never be swept.
	sweepGrace = 10 * time.Second

	// maxPending caps the coalescing set. Exceeding it means dropping kicks, and
	// the sweep is what makes that survivable — see Notify.
	maxPending = 4096
)

// Notifier coalesces per-title invalidations and emits them onto the broker.
// The zero value is not usable; construct with New. A nil *Notifier is a safe
// no-op, so callers that run without realtime need no branch.
type Notifier struct {
	broker  *sse.Broker
	resolve Resolver
	sweep   Sweeper
	log     *logger.Logger

	flushInterval time.Duration
	sweepInterval time.Duration

	mu      sync.Mutex
	pending map[uuid.UUID]struct{}

	// refs caches media item id → wire identity. A media item's tmdb id and type
	// are fixed at creation, so an entry never needs invalidating.
	refs sync.Map
}

// New builds a Notifier. sweep may be nil, which disables the safety net and
// leaves delivery dependent on every call site being correct — acceptable in
// tests, not in production.
func New(broker *sse.Broker, resolve Resolver, sweep Sweeper, log *logger.Logger) *Notifier {
	return &Notifier{
		broker:        broker,
		resolve:       resolve,
		sweep:         sweep,
		log:           log,
		flushInterval: defaultFlushInterval,
		sweepInterval: defaultSweepInterval,
		pending:       make(map[uuid.UUID]struct{}),
	}
}

// Notify records that a media item's acquisition state may have changed. It is
// safe to call from any goroutine and does no I/O.
//
// Overflow drops the invalidation rather than growing without bound or blocking
// the caller. That is a deliberate trade: the sweep will pick the item up within
// a sweep interval, so the cost of a drop is latency, and the alternatives
// (unbounded memory, or backpressuring an import) are both worse.
func (n *Notifier) Notify(mediaItemID uuid.UUID) {
	if n == nil || mediaItemID == uuid.Nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.pending) >= maxPending {
		return
	}
	n.pending[mediaItemID] = struct{}{}
}

// Run drives the flush and sweep loops until ctx is canceled.
func (n *Notifier) Run(ctx context.Context) {
	if n == nil {
		return
	}

	flush := time.NewTicker(n.flushInterval)
	defer flush.Stop()
	sweep := time.NewTicker(n.sweepInterval)
	defer sweep.Stop()

	watermark := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case <-flush.C:
			n.flushPending(ctx)
		case <-sweep.C:
			watermark = n.runSweep(ctx, watermark)
		}
	}
}

// flushPending emits one kick per distinct title accumulated since the last
// flush.
func (n *Notifier) flushPending(ctx context.Context) {
	n.mu.Lock()
	if len(n.pending) == 0 {
		n.mu.Unlock()
		return
	}
	batch := n.pending
	n.pending = make(map[uuid.UUID]struct{})
	n.mu.Unlock()

	n.emitAll(ctx, batch)
}

// runSweep finds items changed since the watermark and kicks them, returning the
// next watermark. On failure the watermark is left where it was, so the next
// sweep retries the same window rather than skipping past it.
func (n *Notifier) runSweep(ctx context.Context, watermark time.Time) time.Time {
	if n.sweep == nil {
		return watermark
	}

	started := time.Now()
	ids, err := n.sweep(ctx, watermark.Add(-sweepGrace))
	if err != nil {
		n.log.Warn().Err(err).Msg("title status sweep failed")
		return watermark
	}

	batch := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		batch[id] = struct{}{}
	}
	n.emitAll(ctx, batch)

	return started
}

// emitAll resolves each item to its wire identity and emits its kick.
func (n *Notifier) emitAll(ctx context.Context, items map[uuid.UUID]struct{}) {
	for id := range items {
		ref, ok := n.lookup(ctx, id)
		if !ok {
			continue
		}
		realtime.Emit(ctx, n.broker, realtime.TitleStatus(ref.MediaType, ref.TmdbID))
	}
}

// lookup resolves a media item id to its wire identity, memoizing the result.
func (n *Notifier) lookup(ctx context.Context, id uuid.UUID) (Ref, bool) {
	if v, ok := n.refs.Load(id); ok {
		return v.(Ref), true
	}
	ref, err := n.resolve(ctx, id)
	if err != nil {
		n.log.Warn().Err(err).Str("media_item_id", id.String()).Msg("failed to resolve title for status kick")
		return Ref{}, false
	}
	n.refs.Store(id, ref)
	return ref, true
}
