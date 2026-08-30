package testkit

import (
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type fakeTimer struct {
	at time.Time
	ch chan time.Time
}

type FakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []fakeTimer
}

func NewFakeClock(now time.Time) *FakeClock {
	return &FakeClock{now: now}
}

func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *FakeClock) After(duration time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	deadline := c.now.Add(duration)
	if duration <= 0 {
		ch <- deadline
		close(ch)
		return ch
	}
	c.timers = append(c.timers, fakeTimer{at: deadline, ch: ch})
	return ch
}

func (c *FakeClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	now := c.now
	pending := c.timers[:0]
	var ready []fakeTimer
	for _, timer := range c.timers {
		if timer.at.After(now) {
			pending = append(pending, timer)
		} else {
			ready = append(ready, timer)
		}
	}
	c.timers = pending
	c.mu.Unlock()

	for _, timer := range ready {
		timer.ch <- timer.at
		close(timer.ch)
	}
}
