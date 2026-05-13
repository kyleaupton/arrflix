package sse

import (
	"encoding/json"
	"sync"
	"time"
)

// Event is a single message published onto the in-process event bus.
// It maps 1:1 to an SSE event on the wire.
type Event struct {
	Type string          // SSE event name
	Data json.RawMessage // JSON payload
	ID   string          // optional SSE event id
	At   time.Time       // server timestamp
}

type Broker struct {
	mu     sync.RWMutex
	nextID int
	subs   map[int]chan Event
}

func NewBroker() *Broker {
	return &Broker{subs: make(map[int]chan Event)}
}

// Subscribe returns a receive-only channel plus a cancel function.
// The channel is buffered; if a subscriber can't keep up, events are dropped
// for that subscriber (publish is non-blocking).
func (b *Broker) Subscribe() (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID
	b.nextID++

	ch := make(chan Event, 64)
	b.subs[id] = ch

	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if c, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(c)
		}
	}

	return ch, cancel
}

// Publish broadcasts an event to all subscribers.
// This call never blocks; slow subscribers simply miss events.
func (b *Broker) Publish(ev Event) {
	if ev.At.IsZero() {
		ev.At = time.Now()
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		select {
		case ch <- ev:
		default:
			// drop for this subscriber
		}
	}
}
