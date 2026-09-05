package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	runtimenetwork "openagentx/internal/runtime/network"
)

type Config struct {
	Binary          string
	Args            []string
	Descriptor      openruntime.AdapterDescriptor
	WorkingDir      string
	Environment     []string
	HealthTimeout   time.Duration
	Network         domain.NetworkPolicy
	RuntimeIdentity domain.RuntimeIdentity
}

type Adapter struct {
	config    Config
	networkMu sync.RWMutex
}

func (a *Adapter) VerifyRuntimeIdentity(ctx context.Context, expected domain.RuntimeIdentity) error {
	if expected.IsZero() && a.config.RuntimeIdentity.IsZero() {
		return nil
	}
	return runtimenetwork.VerifyRuntimeIdentity(ctx, expected, a.config.Binary, "")
}

func (a *Adapter) ApplyNetworkPolicy(policy domain.NetworkPolicy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	if _, err := runtimenetwork.Environment(nil, policy, a.config.Descriptor.AdapterID, a.config.Binary); err != nil {
		return err
	}
	a.networkMu.Lock()
	a.config.Network = policy
	a.networkMu.Unlock()
	return nil
}

func (a *Adapter) configuredNetwork() domain.NetworkPolicy {
	a.networkMu.RLock()
	defer a.networkMu.RUnlock()
	return a.config.Network
}

func NewAdapter(config Config) (*Adapter, error) {
	if strings.TrimSpace(config.Binary) == "" {
		return nil, fmt.Errorf("ACP binary is required")
	}
	if err := config.Descriptor.Validate(); err != nil {
		return nil, err
	}
	if _, err := exec.LookPath(config.Binary); err != nil {
		return nil, fmt.Errorf("ACP binary is unavailable: %w", err)
	}
	if config.HealthTimeout <= 0 {
		config.HealthTimeout = 5 * time.Second
	}
	return &Adapter{config: config}, nil
}

func NewAdapterForTest(config Config) (*Adapter, error) {
	if err := config.Descriptor.Validate(); err != nil {
		return nil, err
	}
	if config.HealthTimeout <= 0 {
		config.HealthTimeout = 5 * time.Second
	}
	return &Adapter{config: config}, nil
}

func (a *Adapter) Descriptor(context.Context) (openruntime.AdapterDescriptor, error) {
	descriptor := a.config.Descriptor
	descriptor.RuntimeIdentity = a.config.RuntimeIdentity
	return descriptor, nil
}

func (a *Adapter) CloneForNetworkProbe(policy domain.NetworkPolicy) (openruntime.AgentRuntimeAdapter, error) {
	a.networkMu.RLock()
	config := a.config
	a.networkMu.RUnlock()
	config.Network = policy
	return NewAdapter(config)
}
func (a *Adapter) Validate(_ context.Context, spec domain.ExecutionSpec) error {
	if err := spec.ValidateShape(); err != nil {
		return err
	}
	if spec.AdapterID != a.config.Descriptor.AdapterID || spec.BackendID == "" {
		return domain.ErrUnsupportedCapability
	}
	return nil
}
func (a *Adapter) Health(ctx context.Context) error {
	healthCtx, cancel := context.WithTimeout(ctx, a.config.HealthTimeout)
	defer cancel()
	cmd := exec.CommandContext(healthCtx, a.config.Binary, append([]string{}, a.config.Args...)...)
	env, err := a.environment(a.configuredNetwork())
	if err != nil {
		return err
	}
	cmd.Dir, cmd.Env = a.config.WorkingDir, env
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ACP health probe failed: %w", err)
	}
	return nil
}

func (a *Adapter) StartTurn(ctx context.Context, request openruntime.TurnRequest, sink openruntime.EventSink) (openruntime.TurnHandle, error) {
	expectedIdentity := request.Execution.Spec.Network.RuntimeIdentity
	if expectedIdentity.IsZero() {
		expectedIdentity = a.config.RuntimeIdentity
	}
	if err := a.VerifyRuntimeIdentity(ctx, expectedIdentity); err != nil {
		return nil, err
	}
	if err := a.Validate(ctx, request.Execution.Spec); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, a.config.Binary, a.config.Args...)
	env, err := a.environment(request.Execution.Spec.Network)
	if err != nil {
		return nil, err
	}
	cmd.Dir, cmd.Env = a.config.WorkingDir, env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if _, err := cmd.StderrPipe(); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ACP process: %w", err)
	}
	prompt := map[string]any{"method": "session/prompt", "task": request.Task, "messages": request.Messages, "execution": request.Execution, "session": request.SessionBinding}
	encoded, _ := json.Marshal(prompt)
	_, _ = stdin.Write(append(encoded, '\n'))
	_ = stdin.Close()
	h := &turnHandle{cmd: cmd, stdout: stdout, sink: sink, done: make(chan struct{})}
	go h.collect()
	return h, nil
}

func (a *Adapter) environment(policy domain.NetworkPolicy) ([]string, error) {
	if policy.IsZero() {
		policy = a.configuredNetwork()
	}
	base := append([]string{}, os.Environ()...)
	base = append(base, a.config.Environment...)
	return runtimenetwork.Environment(base, policy, a.config.Descriptor.AdapterID, a.config.Binary)
}

type turnHandle struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	sink   openruntime.EventSink
	done   chan struct{}
	once   sync.Once
	result openruntime.TurnResult
	err    error
}

func (h *turnHandle) collect() {
	result := openruntime.TurnResult{SideEffectsKnown: true}
	scanner := bufio.NewScanner(h.stdout)
	var parseErr error
	for scanner.Scan() {
		var event struct {
			Type              string          `json:"type"`
			Status            string          `json:"status"`
			Result            string          `json:"result"`
			ProviderSessionID string          `json:"provider_session_id"`
			Payload           json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			parseErr = err
			continue
		}
		if event.ProviderSessionID != "" {
			result.ProviderSessionID = event.ProviderSessionID
		}
		if event.Result != "" {
			result.Result = event.Result
		}
		if event.Status != "" {
			result.Status = openruntime.TurnResultStatus(event.Status)
		}
		if event.Type != "" && h.sink != nil {
			_ = h.sink.Emit(context.Background(), openruntime.RuntimeEvent{Type: event.Type, Payload: event.Payload, OccurredAt: time.Now().UTC()})
		}
	}
	if err := scanner.Err(); err != nil {
		parseErr = err
	}
	if err := h.cmd.Wait(); err != nil {
		parseErr = fmt.Errorf("ACP process exited: %w", err)
	}
	if parseErr != nil {
		result.Status = openruntime.TurnResultUncertain
		result.SideEffectsKnown = false
		result.Error = parseErr.Error()
	}
	if result.Status == "" {
		result.Status = openruntime.TurnResultSucceeded
		result.SideEffectsKnown = true
	}
	h.once.Do(func() { h.result, h.err = result, parseErr; close(h.done) })
}
func (h *turnHandle) Wait(ctx context.Context) (openruntime.TurnResult, error) {
	select {
	case <-h.done:
		return h.result, h.err
	case <-ctx.Done():
		return openruntime.TurnResult{}, ctx.Err()
	}
}
func (h *turnHandle) Steer(context.Context, domain.Message) error {
	return openruntime.ErrSteerUnsupported
}
func (h *turnHandle) DecideApproval(context.Context, domain.ApprovalDecision) error {
	return openruntime.ErrApprovalUnsupported
}
func (h *turnHandle) RequestCancel(_ context.Context) error {
	if h.cmd.Process == nil {
		return openruntime.ErrCancelUnsupported
	}
	return h.cmd.Process.Signal(syscall.SIGTERM)
}

var _ openruntime.AgentRuntimeAdapter = (*Adapter)(nil)
var _ openruntime.TurnHandle = (*turnHandle)(nil)
