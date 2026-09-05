package worker

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	openapi "openagentx/internal/api"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	runtimenetwork "openagentx/internal/runtime/network"
)

type RuntimeBackend struct {
	ID      string
	Adapter openruntime.AgentRuntimeAdapter
	Network domain.NetworkPolicy
}

type BackendPool struct {
	mu           sync.RWMutex
	backends     map[string]RuntimeBackend
	available    map[string]bool
	blocked      map[string]bool
	materializer *runtimenetwork.Materializer
}

func (p *BackendPool) SetNetworkMaterializer(materializer *runtimenetwork.Materializer) {
	p.mu.Lock()
	p.materializer = materializer
	p.mu.Unlock()
}

func (p *BackendPool) ProcessNetworkWork(ctx context.Context, envelope *openapi.NetworkWorkEnvelope) openapi.NetworkWorkAckRequest {
	started := time.Now()
	ack := openapi.NetworkWorkAckRequest{State: "failed"}
	if envelope == nil {
		ack.DiagnosticCode = domain.NetworkDiagnosticInvalidConfig
		return ack
	}
	work := envelope.Work
	if work.Kind == domain.NetworkWorkTest {
		ack.ProbeResults = pendingNetworkProbeResults()
	}
	p.mu.RLock()
	backend, exists := p.backends[work.BackendID]
	materializer := p.materializer
	p.mu.RUnlock()
	if !exists {
		return failNetworkProbe(ack, domain.NetworkProbeConfiguration, domain.NetworkDiagnosticUnsupported, started)
	}
	if verifier, ok := backend.Adapter.(openruntime.RuntimeIdentityVerifier); ok {
		if err := verifier.VerifyRuntimeIdentity(ctx, work.RuntimeIdentity); err != nil {
			return failNetworkProbe(ack, domain.NetworkProbeConfiguration, domain.NetworkDiagnosticIdentity, started)
		}
	}
	descriptor, err := backend.Adapter.Descriptor(ctx)
	if err != nil || descriptor.RuntimeIdentity != work.RuntimeIdentity {
		return failNetworkProbe(ack, domain.NetworkProbeConfiguration, domain.NetworkDiagnosticIdentity, started)
	}
	wantedMode := work.Mode
	if work.Kind == domain.NetworkWorkImport {
		wantedMode = domain.NetworkNamedProfile
	}
	supported := false
	for _, mode := range descriptor.NetworkModes {
		if mode == string(wantedMode) {
			supported = true
			break
		}
	}
	if !supported {
		return failNetworkProbe(ack, domain.NetworkProbeConfiguration, domain.NetworkDiagnosticUnsupported, started)
	}
	if work.Kind == domain.NetworkWorkImport {
		if backend.Network.ConfigFile == "" {
			ack.DiagnosticCode = domain.NetworkDiagnosticInvalidConfig
			return ack
		}
		if materializer == nil {
			ack.DiagnosticCode = domain.NetworkDiagnosticUnsupported
			return ack
		}
		imported, err := materializer.ImportConfig(backend.Network.ConfigFile)
		if err != nil {
			ack.DiagnosticCode = domain.NetworkDiagnosticInvalidConfig
			return ack
		}
		ack.State = "succeeded"
		ack.Imported = &imported
		ack.DurationMS = time.Since(started).Milliseconds()
		return ack
	}
	var policy domain.NetworkPolicy
	configurationStarted := time.Now()
	if work.Mode == domain.NetworkNamedProfile {
		if materializer == nil {
			return failNetworkProbe(ack, domain.NetworkProbeConfiguration, domain.NetworkDiagnosticUnsupported, started)
		}
		if work.Content == nil {
			return failNetworkProbe(ack, domain.NetworkProbeConfiguration, domain.NetworkDiagnosticInvalidConfig, started)
		}
		if work.SecretVersion != "" && envelope.Secret == nil {
			setNetworkProbe(&ack, domain.NetworkProbeConfiguration, domain.NetworkProbePassed, "", time.Since(configurationStarted))
			return failNetworkProbe(ack, domain.NetworkProbeSecret, domain.NetworkDiagnosticSecretMissing, started)
		}
		policy, err = materializer.Materialize(*work.Content, envelope.Secret, work.RuntimeIdentity)
		if err != nil {
			return failNetworkProbe(ack, domain.NetworkProbeConfiguration, domain.NetworkDiagnosticMaterialize, started)
		}
		if _, err := materializer.PrepareRun(policy); err != nil {
			return failNetworkProbe(ack, domain.NetworkProbeConfiguration, domain.NetworkDiagnosticMaterialize, started)
		}
		setNetworkProbe(&ack, domain.NetworkProbeConfiguration, domain.NetworkProbePassed, "", time.Since(configurationStarted))
		if work.SecretVersion == "" {
			setNetworkProbe(&ack, domain.NetworkProbeSecret, domain.NetworkProbeNotApplicable, "", 0)
		} else {
			setNetworkProbe(&ack, domain.NetworkProbeSecret, domain.NetworkProbePassed, "", 0)
		}
	} else {
		policy = domain.NetworkPolicy{Mode: work.Mode, PolicyVersion: work.PolicyVersion, ManifestDigest: work.ManifestDigest, RuntimeIdentity: work.RuntimeIdentity}
		if err := policy.Validate(); err != nil {
			return failNetworkProbe(ack, domain.NetworkProbeConfiguration, domain.NetworkDiagnosticInvalidConfig, started)
		}
		setNetworkProbe(&ack, domain.NetworkProbeConfiguration, domain.NetworkProbePassed, "", time.Since(configurationStarted))
		if work.Mode == domain.NetworkInherit {
			setNetworkProbe(&ack, domain.NetworkProbeSecret, domain.NetworkProbeNotVerified, domain.NetworkDiagnosticInheritUnknown, 0)
			setNetworkProbe(&ack, domain.NetworkProbeEndpoint, domain.NetworkProbeNotVerified, domain.NetworkDiagnosticInheritUnknown, 0)
		} else {
			setNetworkProbe(&ack, domain.NetworkProbeSecret, domain.NetworkProbeNotApplicable, "", 0)
			setNetworkProbe(&ack, domain.NetworkProbeEndpoint, domain.NetworkProbeNotApplicable, "", 0)
		}
	}
	policy.BindingRevision = work.BindingRevision
	if work.Kind == domain.NetworkWorkTest {
		if work.Mode == domain.NetworkNamedProfile {
			endpointStarted := time.Now()
			if err := runtimenetwork.ProbeEndpoint(ctx, *work.Content, envelope.Secret); err != nil {
				if errors.Is(err, runtimenetwork.ErrHTTPProxyAuthenticationRequired) {
					return failNetworkProbe(ack, domain.NetworkProbeEndpoint, domain.NetworkDiagnosticProxyAuth, started)
				}
				return failNetworkProbe(ack, domain.NetworkProbeEndpoint, domain.NetworkDiagnosticEndpoint, started)
			}
			setNetworkProbe(&ack, domain.NetworkProbeEndpoint, domain.NetworkProbePassed, "", time.Since(endpointStarted))
		}
		cloner, ok := backend.Adapter.(openruntime.NetworkProbeCloner)
		if !ok {
			return failNetworkProbe(ack, domain.NetworkProbeDirectRules, domain.NetworkDiagnosticUnsupported, started)
		}
		rulesStarted := time.Now()
		probe, err := cloner.CloneForNetworkProbe(policy)
		if err != nil {
			return failNetworkProbe(ack, domain.NetworkProbeDirectRules, domain.NetworkDiagnosticInvalidConfig, started)
		}
		if applier, ok := probe.(openruntime.NetworkPolicyApplier); ok {
			if err := applier.ApplyNetworkPolicy(policy); err != nil {
				return failNetworkProbe(ack, domain.NetworkProbeDirectRules, domain.NetworkDiagnosticInvalidConfig, started)
			}
		}
		if work.Mode == domain.NetworkNamedProfile && len(work.Content.DirectIPs) > 0 {
			setNetworkProbe(&ack, domain.NetworkProbeDirectRules, domain.NetworkProbePassed, "", time.Since(rulesStarted))
		} else {
			setNetworkProbe(&ack, domain.NetworkProbeDirectRules, domain.NetworkProbeNotApplicable, "", 0)
		}
		healthStarted := time.Now()
		if err := probe.Health(ctx); err != nil {
			return failNetworkProbe(ack, domain.NetworkProbeRuntimeHealth, domain.NetworkDiagnosticRuntime, started)
		}
		setNetworkProbe(&ack, domain.NetworkProbeRuntimeHealth, domain.NetworkProbePassed, "", time.Since(healthStarted))
		ack.State = "succeeded"
		ack.DurationMS = time.Since(started).Milliseconds()
		return ack
	}
	if work.Kind != domain.NetworkWorkApply {
		ack.DiagnosticCode = domain.NetworkDiagnosticUnsupported
		return ack
	}
	applier, ok := backend.Adapter.(openruntime.NetworkPolicyApplier)
	if !ok {
		ack.DiagnosticCode = domain.NetworkDiagnosticUnsupported
		return ack
	}
	if err := applier.ApplyNetworkPolicy(policy); err != nil {
		ack.DiagnosticCode = domain.NetworkDiagnosticInvalidConfig
		return ack
	}
	p.mu.Lock()
	backend.Network = policy
	p.backends[work.BackendID] = backend
	p.blocked[work.BackendID] = false
	p.available[work.BackendID] = true
	p.mu.Unlock()
	sanitized := policy
	sanitized.ConfigFile = ""
	sanitized.BlackIPFile = ""
	ack.State = "succeeded"
	ack.DurationMS = time.Since(started).Milliseconds()
	ack.Policy = &sanitized
	return ack
}

func pendingNetworkProbeResults() []domain.NetworkProbeResult {
	layers := []domain.NetworkProbeLayer{
		domain.NetworkProbeConfiguration, domain.NetworkProbeSecret, domain.NetworkProbeEndpoint,
		domain.NetworkProbeDirectRules, domain.NetworkProbeRuntimeHealth,
		domain.NetworkProbeNetworkEffect, domain.NetworkProbeModelCall,
	}
	results := make([]domain.NetworkProbeResult, 0, len(layers))
	for _, layer := range layers {
		results = append(results, domain.NetworkProbeResult{Layer: layer, State: domain.NetworkProbeNotVerified, DiagnosticCode: domain.NetworkDiagnosticNotVerified})
	}
	return results
}

func setNetworkProbe(ack *openapi.NetworkWorkAckRequest, layer domain.NetworkProbeLayer, state domain.NetworkProbeState, diagnostic string, duration time.Duration) {
	for index := range ack.ProbeResults {
		if ack.ProbeResults[index].Layer == layer {
			ack.ProbeResults[index].State = state
			ack.ProbeResults[index].DiagnosticCode = diagnostic
			ack.ProbeResults[index].DurationMS = duration.Milliseconds()
			return
		}
	}
}

func failNetworkProbe(ack openapi.NetworkWorkAckRequest, layer domain.NetworkProbeLayer, diagnostic string, started time.Time) openapi.NetworkWorkAckRequest {
	ack.State = "failed"
	ack.DiagnosticCode = diagnostic
	ack.DurationMS = time.Since(started).Milliseconds()
	if len(ack.ProbeResults) != 0 {
		setNetworkProbe(&ack, layer, domain.NetworkProbeFailed, diagnostic, time.Since(started))
	}
	return ack
}

func (p *BackendPool) PrepareRunNetwork(ctx context.Context, request openruntime.TurnRequest) (openruntime.TurnRequest, openruntime.AgentRuntimeAdapter, error) {
	p.mu.RLock()
	backend, ok := p.backends[request.Execution.Spec.BackendID]
	materializer := p.materializer
	p.mu.RUnlock()
	if !ok {
		return request, nil, domain.ErrUnsupportedCapability
	}
	descriptor, err := backend.Adapter.Descriptor(ctx)
	if err != nil {
		return request, nil, err
	}
	expected := request.Execution.Spec.Network.RuntimeIdentity
	if request.Execution.Spec.Network.Mode == domain.NetworkNamedProfile && expected.IsZero() {
		return request, nil, domain.ErrConflict("runtime identity is missing")
	}
	if !expected.IsZero() && descriptor.RuntimeIdentity != expected {
		return request, nil, domain.ErrConflict("runtime identity changed")
	}
	if verifier, ok := backend.Adapter.(openruntime.RuntimeIdentityVerifier); ok {
		if err := verifier.VerifyRuntimeIdentity(ctx, expected); err != nil {
			return request, nil, domain.ErrConflict("runtime identity changed")
		}
	}
	if request.Execution.Spec.Network.Mode == domain.NetworkNamedProfile {
		if materializer == nil {
			return request, nil, domain.ErrUnsupportedCapability
		}
		local, err := materializer.PrepareRun(request.Execution.Spec.Network)
		if err != nil {
			return request, nil, err
		}
		request.Execution.Spec.Network = local
	}
	return request, backend.Adapter, nil
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
	if binding.Mode == "" {
		binding.Mode = domain.NetworkNamedProfile
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
