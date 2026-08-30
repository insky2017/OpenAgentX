package controlplane

import "sync"

type WakeupBroker interface {
	Subscribe(topic string) (<-chan struct{}, func())
	Publish(topic string)
}

type MemoryWakeupBroker struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan struct{}]struct{}
}

func NewMemoryWakeupBroker() *MemoryWakeupBroker {
	return &MemoryWakeupBroker{subscribers: make(map[string]map[chan struct{}]struct{})}
}

func (b *MemoryWakeupBroker) Subscribe(topic string) (<-chan struct{}, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel := make(chan struct{}, 1)
	if b.subscribers[topic] == nil {
		b.subscribers[topic] = make(map[chan struct{}]struct{})
	}
	b.subscribers[topic][channel] = struct{}{}
	var once sync.Once
	return channel, func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			delete(b.subscribers[topic], channel)
			if len(b.subscribers[topic]) == 0 {
				delete(b.subscribers, topic)
			}
		})
	}
}

func (b *MemoryWakeupBroker) Publish(topic string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for channel := range b.subscribers[topic] {
		select {
		case channel <- struct{}{}:
		default:
		}
	}
}

func AgentMailboxTopic(agentID string) string {
	return "agent-mailbox:" + agentID
}

func WorkerControlTopic(workerID string) string {
	return "worker-control:" + workerID
}
