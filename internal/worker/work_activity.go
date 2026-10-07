package worker

import (
	"encoding/json"
	"sync"

	openruntime "openagentx/internal/runtime"
)

// workActivity is a display-only projection. It never changes capacity, Run
// state or approval authority. Each turn owns its projection, so late events
// cannot reactivate a completed turn's indicator.
type workActivity struct {
	mu      sync.Mutex
	observe func(bool)
	ended   bool
	pending map[string]bool
}

func startWorkActivity(observe func(bool)) *workActivity {
	a := &workActivity{observe: observe, pending: make(map[string]bool)}
	if observe != nil {
		observe(true)
	}
	return a
}

func (a *workActivity) event(event openruntime.RuntimeEvent) {
	if event.Type != "approval.requested" {
		return
	}
	var payload struct {
		ID string `json:"approval_request_id"`
	}
	if json.Unmarshal(event.Payload, &payload) != nil || payload.ID == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.ended {
		a.pending[payload.ID] = true
		if a.observe != nil {
			a.observe(false)
		}
	}
}

func (a *workActivity) decided(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.pending, id)
	if !a.ended && len(a.pending) == 0 && a.observe != nil {
		a.observe(true)
	}
}

func (a *workActivity) finish() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ended = true
	if a.observe != nil {
		a.observe(false)
	}
}
