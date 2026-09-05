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
	mu        sync.RWMutex
	backends  map[string]RuntimeBackend
	available map[string]bool
	blocked   map[string]bool
}

func NewBackendPool(backends []RuntimeBackend) (*BackendPool, error) {
	if len(backends) == 0 {
		return nil, domain.ErrInvalidInput("Worker requires at least one Runtime Backend")
	}
	pool := &BackendPool{
		backends: make(map[string]RuntimeBackend, len(backends)), available: make(map[string]bool, len(backends)),
		blocked: make(map[string]bool, len(backends)),
	}
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
		if backend.Network.IsZero() {
			backend.Network.Mode = domain.NetworkInherit
		}
		pool.backends[backend.ID] = backend
		pool.available[backend.ID] = true
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

func (p *BackendPool) ApplyNetworkBinding(binding domain.NetworkBinding) error {
	if binding.Profile == nil {
		return domain.ErrInvalidInput("network binding profile payload is required")
	}
	if err := binding.Validate(); err != nil {
		return err
	}
	if err := binding.Profile.Validate(); err != nil || binding.Profile.Status != domain.NetworkProfilePublished ||
		binding.Profile.ID != binding.ProfileID || binding.Profile.Version != binding.ProfileVersion {
		return domain.ErrInvalidInput("network binding profile payload does not match binding")
	}
	policy := domain.NetworkPolicy{
		Mode: domain.NetworkNamedProfile, ProfileID: binding.Profile.ID, ProfileVersion: binding.Profile.Version,
		ProxyMode: binding.Profile.Mode, ConfigFile: binding.Profile.ConfigFile, BindingRevision: binding.Version,
	}
	if err := policy.Validate(); err != nil {
		return err
	}
	p.mu.RLock()
	backend, exists := p.backends[binding.BackendID]
	p.mu.RUnlock()
	if !exists {
		return domain.ErrNotFound
	}
	applier, ok := backend.Adapter.(openruntime.NetworkPolicyApplier)
	if !ok {
		return domain.ErrUnsupportedCapability
	}
	if err := applier.ApplyNetworkPolicy(policy); err != nil {
		p.mu.Lock()
		p.blocked[binding.BackendID] = true
		p.available[binding.BackendID] = false
		p.mu.Unlock()
		return err
	}
	p.mu.Lock()
	backend.Network = policy
	p.backends[binding.BackendID] = backend
	p.blocked[binding.BackendID] = false
	p.available[binding.BackendID] = true
	p.mu.Unlock()
	return nil
}

func (p *BackendPool) Observe(ctx context.Context) ([]openruntime.BackendRegistration, map[string]openruntime.BackendHealth, error) {
	p.mu.RLock()
	ids := make([]string, 0, len(p.backends))
	for id := range p.backends {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	backends := make([]RuntimeBackend, 0, len(ids))
	blocked := make(map[string]bool, len(ids))
	for _, id := range ids {
		backends = append(backends, p.backends[id])
		blocked[id] = p.blocked[id]
	}
	p.mu.RUnlock()

	registrations := make([]openruntime.BackendRegistration, 0, len(backends))
	health := make(map[string]openruntime.BackendHealth, len(backends))
	available := make(map[string]bool, len(backends))
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
		}
		if blocked[backend.ID] {
			backendHealth = openruntime.BackendUnavailable
		}
		available[backend.ID] = !blocked[backend.ID] &&
			(backendHealth == openruntime.BackendHealthy || backendHealth == openruntime.BackendDegraded)
		health[backend.ID] = backendHealth
		registrations = append(registrations, openruntime.BackendRegistration{
			BackendID: backend.ID, Descriptor: descriptor, Health: backendHealth, Network: backend.Network,
		})
	}
	p.mu.Lock()
	for backendID, isAvailable := range available {
		p.available[backendID] = isAvailable
	}
	p.mu.Unlock()
	return registrations, health, nil
}

func (p *BackendPool) HasAvailableBackend() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, available := range p.available {
		if available {
			return true
		}
	}
	return false
}
