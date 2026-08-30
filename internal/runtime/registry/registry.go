package registry

import (
	"context"
	"sync"

	"agentbus/internal/domain"
	openruntime "agentbus/internal/runtime"
)

type Entry struct {
	Registration openruntime.BackendRegistration
	Adapter      openruntime.AgentRuntimeAdapter
}

// Registry is an in-process, typed catalogue of Runtime Adapters. Workers
// publish its descriptors to the daemon; no dynamic Go plugins or raw argv are
// accepted at this boundary.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]Entry
}

func New() *Registry { return &Registry{entries: make(map[string]Entry)} }

func (r *Registry) Register(ctx context.Context, entry Entry) error {
	if entry.Adapter == nil {
		return domain.ErrInvalidInput("Runtime Adapter is required")
	}
	if err := entry.Registration.Validate(); err != nil {
		return err
	}
	descriptor, err := entry.Adapter.Descriptor(ctx)
	if err != nil {
		return err
	}
	if descriptor.AdapterID != entry.Registration.Descriptor.AdapterID {
		return domain.ErrConflict("Adapter descriptor does not match registration")
	}
	key := entry.Registration.Descriptor.AdapterID + "/" + entry.Registration.BackendID
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.entries[key]; exists {
		return domain.ErrConflict("Runtime Backend registration already exists")
	}
	r.entries[key] = entry
	return nil
}

func (r *Registry) Resolve(ctx context.Context, adapterID, backendID string) (Entry, error) {
	r.mu.RLock()
	entry, ok := r.entries[adapterID+"/"+backendID]
	r.mu.RUnlock()
	if !ok {
		return Entry{}, domain.ErrUnsupportedCapability
	}
	if entry.Registration.Health == openruntime.BackendUnavailable {
		return Entry{}, domain.ErrUnsupportedCapability
	}
	if err := entry.Adapter.Health(ctx); err != nil {
		return Entry{}, domain.ErrUnsupportedCapability
	}
	return entry, nil
}

func (r *Registry) List() []openruntime.BackendRegistration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]openruntime.BackendRegistration, 0, len(r.entries))
	for _, entry := range r.entries {
		result = append(result, entry.Registration)
	}
	return result
}
