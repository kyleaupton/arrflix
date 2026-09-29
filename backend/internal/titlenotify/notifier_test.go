package titlenotify

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/authz"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/realtime"
	"github.com/kyleaupton/arrflix/internal/sse"
)

// The flush and sweep loops are driven directly rather than through Run, so the
// assertions don't race a ticker.

// watcher is a session subscribed to everything the notifier emits.
type watcher struct {
	att sse.Attachment
}

func newWatcher(t *testing.T, b *sse.Broker) *watcher {
	t.Helper()
	att := b.Attach(sse.AttachParams{
		UserID:       uuid.New(),
		Capabilities: []string{authz.LibraryRead},
	})
	t.Cleanup(att.Cancel)
	return &watcher{att: att}
}

// titles returns the tmdb ids of every title_status event delivered so far.
func (w *watcher) titles() []int64 {
	var out []int64
	for {
		select {
		case ev := <-w.att.Out:
			if ev.Type != realtime.NameTitleStatus {
				continue
			}
			var p realtime.TitleStatusPayload
			if err := json.Unmarshal(ev.Data, &p); err == nil {
				out = append(out, p.TmdbID)
			}
		default:
			return out
		}
	}
}

// stubResolver maps media item ids to tmdb ids by insertion order and counts
// calls, so tests can assert on memoization.
type stubResolver struct {
	mu    sync.Mutex
	refs  map[uuid.UUID]Ref
	calls int
}

func newStubResolver() *stubResolver {
	return &stubResolver{refs: make(map[uuid.UUID]Ref)}
}

func (s *stubResolver) add(tmdbID int64) uuid.UUID {
	id := uuid.New()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refs[id] = Ref{MediaType: "series", TmdbID: tmdbID}
	return id
}

func (s *stubResolver) resolve(_ context.Context, id uuid.UUID) (Ref, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	ref, ok := s.refs[id]
	if !ok {
		return Ref{}, apperrors.NotFoundf("no ref for %s", id)
	}
	return ref, nil
}

func (s *stubResolver) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func newTestNotifier(t *testing.T, resolve Resolver, sweep Sweeper) (*Notifier, *watcher) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	b := sse.NewBroker(ctx)
	w := newWatcher(t, b)
	return New(b, resolve, sweep, logger.New(false)), w
}

// A season-pack import transitions every episode of a season. Emitting one
// event per want is exactly the burst this package exists to absorb.
func TestBurstOfWritesCollapsesToOneEventPerTitle(t *testing.T) {
	t.Parallel()

	res := newStubResolver()
	rickAndMorty := res.add(60625)
	breakingBad := res.add(1396)

	n, w := newTestNotifier(t, res.resolve, nil)

	for i := 0; i < 50; i++ {
		n.Notify(rickAndMorty)
	}
	n.Notify(breakingBad)
	n.flushPending(context.Background())

	got := w.titles()
	if len(got) != 2 {
		t.Fatalf("51 notifications over 2 titles produced %d events, want 2: %v", len(got), got)
	}
	seen := map[int64]bool{got[0]: true, got[1]: true}
	if !seen[60625] || !seen[1396] {
		t.Fatalf("both titles must be announced exactly once, got %v", got)
	}
}

func TestFlushClearsPendingSoAQuietWindowEmitsNothing(t *testing.T) {
	t.Parallel()

	res := newStubResolver()
	item := res.add(60625)

	n, w := newTestNotifier(t, res.resolve, nil)

	n.Notify(item)
	n.flushPending(context.Background())
	if got := w.titles(); len(got) != 1 {
		t.Fatalf("first flush emitted %d events, want 1", len(got))
	}

	n.flushPending(context.Background())
	if got := w.titles(); len(got) != 0 {
		t.Fatalf("flush with nothing pending emitted %d events, want 0", len(got))
	}
}

// A media item's tmdb id is fixed at creation, so resolving it once is enough
// however many times the title churns.
func TestResolutionIsMemoizedAcrossFlushes(t *testing.T) {
	t.Parallel()

	res := newStubResolver()
	item := res.add(60625)

	n, _ := newTestNotifier(t, res.resolve, nil)

	for i := 0; i < 5; i++ {
		n.Notify(item)
		n.flushPending(context.Background())
	}

	if got := res.callCount(); got != 1 {
		t.Fatalf("resolver called %d times across 5 flushes, want 1", got)
	}
}

// An unresolvable item must not stop its batch-mates from being announced.
func TestUnresolvableItemIsSkippedNotFatal(t *testing.T) {
	t.Parallel()

	res := newStubResolver()
	good := res.add(60625)
	orphan := uuid.New() // never added to the resolver

	n, w := newTestNotifier(t, res.resolve, nil)

	n.Notify(orphan)
	n.Notify(good)
	n.flushPending(context.Background())

	got := w.titles()
	if len(got) != 1 || got[0] != 60625 {
		t.Fatalf("resolvable title must still be announced, got %v", got)
	}
}

func TestNotifyIgnoresTheZeroID(t *testing.T) {
	t.Parallel()

	res := newStubResolver()
	n, w := newTestNotifier(t, res.resolve, nil)

	n.Notify(uuid.Nil)
	n.flushPending(context.Background())

	if got := w.titles(); len(got) != 0 {
		t.Fatalf("the zero media item id must announce nothing, got %v", got)
	}
	if got := res.callCount(); got != 0 {
		t.Fatalf("the zero media item id must not reach the resolver, got %d calls", got)
	}
}

// The sweep is what makes a forgotten Notify a latency bug rather than a
// stale-UI bug: a write that never announced itself still reaches the client.
func TestSweepAnnouncesTitlesThatNeverCalledNotify(t *testing.T) {
	t.Parallel()

	res := newStubResolver()
	item := res.add(60625)

	sweep := func(_ context.Context, _ time.Time) ([]uuid.UUID, error) {
		return []uuid.UUID{item}, nil
	}
	n, w := newTestNotifier(t, res.resolve, sweep)

	n.runSweep(context.Background(), time.Now())

	got := w.titles()
	if len(got) != 1 || got[0] != 60625 {
		t.Fatalf("sweep must announce the unannounced title, got %v", got)
	}
}

// A failed sweep must not advance past the window it failed to read, or the
// rows in that window are never swept again.
func TestFailedSweepDoesNotAdvanceTheWatermark(t *testing.T) {
	t.Parallel()

	res := newStubResolver()
	sweep := func(_ context.Context, _ time.Time) ([]uuid.UUID, error) {
		return nil, apperrors.Internalf("database unavailable")
	}
	n, _ := newTestNotifier(t, res.resolve, sweep)

	watermark := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	if got := n.runSweep(context.Background(), watermark); !got.Equal(watermark) {
		t.Fatalf("watermark = %v after a failed sweep, want it held at %v", got, watermark)
	}
}

// The window handed to the sweeper overlaps the previous one, so a row committed
// late by a long transaction still lands inside a swept window.
func TestSweepWindowOverlapsThePreviousOne(t *testing.T) {
	t.Parallel()

	res := newStubResolver()
	var asked time.Time
	sweep := func(_ context.Context, since time.Time) ([]uuid.UUID, error) {
		asked = since
		return nil, nil
	}
	n, _ := newTestNotifier(t, res.resolve, sweep)

	watermark := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	n.runSweep(context.Background(), watermark)

	if !asked.Before(watermark) {
		t.Fatalf("sweep asked from %v, want strictly before the watermark %v", asked, watermark)
	}
}

func TestNilNotifierIsANoOp(t *testing.T) {
	t.Parallel()

	var n *Notifier
	n.Notify(uuid.New()) // must not panic
	n.Run(context.Background())
}
