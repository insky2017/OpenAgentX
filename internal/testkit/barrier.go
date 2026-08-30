package testkit

import (
	"context"
	"sync"
)

type Barrier struct {
	mu        sync.Mutex
	remaining int
	released  chan struct{}
}

func NewBarrier(participants int) *Barrier {
	if participants <= 0 {
		panic("barrier participants must be positive")
	}
	return &Barrier{remaining: participants, released: make(chan struct{})}
}

func (b *Barrier) ArriveAndWait(ctx context.Context) error {
	b.mu.Lock()
	if b.remaining > 0 {
		b.remaining--
		if b.remaining == 0 {
			close(b.released)
		}
	}
	released := b.released
	b.mu.Unlock()

	select {
	case <-released:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
