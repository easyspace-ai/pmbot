package engine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"polymarket-btc-bot/internal/types"
)

var (
	ErrBusClosed = errors.New("event bus closed")
)

// Bus is a lightweight event bus with optional backpressure strategy.
// It assigns a strictly increasing Seq to every published event.
type Bus struct {
	seq atomic.Uint64

	ch chan types.Event

	mu     sync.Mutex
	closed bool
}

// NewBus creates a bus with a bounded internal buffer.
// When the buffer is full, Publish will block until space becomes available
// or ctx is done.
func NewBus(buffer int) *Bus {
	if buffer <= 0 {
		buffer = 1
	}
	return &Bus{ch: make(chan types.Event, buffer)}
}

func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	close(b.ch)
}

func (b *Bus) Chan() <-chan types.Event { return b.ch }

// Publish pushes an event onto the bus.
// Seq and TsLocal are assigned automatically if not provided.
func (b *Bus) Publish(ctx context.Context, e types.Event) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return ErrBusClosed
	}
	b.mu.Unlock()

	if e.TsLocal.IsZero() {
		e.TsLocal = time.Now().UTC()
	}
	e.Seq = b.seq.Add(1)

	select {
	case b.ch <- e:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
