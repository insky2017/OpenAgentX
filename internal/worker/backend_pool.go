package worker

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

type RuntimeBackend struct {
	ID      string
	Adapter openruntime.AgentRuntimeAdapter
	Network domain.NetworkPolicy
}

type BackendPool struct {
	mu       sync.RWMutex
	backends map[string]RuntimeBackend
}

func NewBackendPool(backends []RuntimeBackend) (*BackendPool, error) {
	if len(backends) == 0 {
		return nil, domain.ErrInvalidInput("Worker requires at least one Runtime Backend")
	}
	pool := &BackendPool{backends: make(map[string]RuntimeBackend, len(backends))}
	for _, backend := range backends {
		if err := domain.ValidateIdentifier("backend_id", backend.ID); err != nil {
			return nil, err
		}
		if backend.Adapter == nil {
			return nil, domain.ErrInvalidInput("Runtime Backend Adapter is required")
		}
		if _, exists := pool.backends[backend.ID]; exists {
			return nil, domain.ErrInvalidInput("Runtime Backend IDs must be unique")
		}
		pool.backends[backend.ID] = backend
	}
	return pool, nil
}

func (p *BackendPool) Resolve(ctx context.Context, adapterID string, backendID string) (openruntime.AgentRuntimeAdapter, error) {
	p.mu.RLock()
	backend, exists := p.backends[backendID]
	p.mu.RUnlock()
	if !exists {
		return nil, domain.ErrUnsupportedCapability
	}
	descriptor, err := backend.Adapter.Descriptor(ctx)
	if err != nil {
		return nil, fmt.Errorf("inspect Adapter descriptor: %w", err)
	}
	if descriptor.AdapterID != adapterID {
		return nil, domain.ErrUnsupportedCapability
	}
	return backend.Adapter, nil
}

func (p *BackendPool) Observe(ctx context.Context) ([]openruntime.BackendRegistration, map[string]openruntime.BackendHealth, error) {
	p.mu.RLock()
	ids := make([]string, 0, len(p.backends))
	for id := range p.backends {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	backends := make([]RuntimeBackend, 0, len(ids))
	for _, id := range ids {
		backends = append(backends, p.backends[id])
	}
	p.mu.RUnlock()

	registrations := make([]openruntime.BackendRegistration, 0, len(backends))
	health := make(map[string]openruntime.BackendHealth, len(backends))
	healthy := 0
	for _, backend := range backends {
		descriptor, err := backend.Adapter.Descriptor(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("describe Backend %s: %w", backend.ID, err)
		}
		if err := descriptor.Validate(); err != nil {
			return nil, nil, fmt.Errorf("validate Backend %s descriptor: %w", backend.ID, err)
		}
		backendHealth := openruntime.BackendHealthy
		if err := backend.Adapter.Health(ctx); err != nil {
			backendHealth = openruntime.BackendUnavailable
		} else {
			healthy++
		}
		health[backend.ID] = backendHealth
		registrations = append(registrations, openruntime.BackendRegistration{
			BackendID: backend.ID, Descriptor: descriptor, Health: backendHealth, Network: backend.Network,
		})
	}
	if healthy == 0 {
		return registrations, health, domain.ErrUnsupportedCapability
	}
	return registrations, health, nil
}
