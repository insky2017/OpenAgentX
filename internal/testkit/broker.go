package testkit

import "sync"

type FakeBroker struct {
	mu          sync.Mutex
	dropWakeups bool
	subscribers map[string]map[chan struct{}]struct{}
}

func NewFakeBroker() *FakeBroker {
	return &FakeBroker{subscribers: make(map[string]map[chan struct{}]struct{})}
}

func (b *FakeBroker) SetDropWakeups(drop bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dropWakeups = drop
}

func (b *FakeBroker) Subscribe(topic string) (<-chan struct{}, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan struct{}, 1)
	if b.subscribers[topic] == nil {
		b.subscribers[topic] = make(map[chan struct{}]struct{})
	}
	b.subscribers[topic][ch] = struct{}{}
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subscribers[topic], ch)
		if len(b.subscribers[topic]) == 0 {
			delete(b.subscribers, topic)
		}
	}
}

func (b *FakeBroker) Publish(topic string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.dropWakeups {
		return 0
	}
	delivered := 0
	for subscriber := range b.subscribers[topic] {
		select {
		case subscriber <- struct{}{}:
			delivered++
		default:
		}
	}
	return delivered
}
