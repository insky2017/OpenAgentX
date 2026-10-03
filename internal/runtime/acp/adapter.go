package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"openagentx/internal/safeoutput"
)

const (
	maxACPStreamLine = 1 << 20
	maxACPStderr     = 64 << 10
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

func (a *Adapter) VerifyRuntimeIdentity(ctx context.Context, expected domain.RuntimeIdentity) (bool, error) {
	if expected.IsZero() && a.config.RuntimeIdentity.IsZero() {
		return false, nil
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
	changed, err := a.VerifyRuntimeIdentity(ctx, expectedIdentity)
	if err != nil {
		return nil, err
	}
	if err := a.Validate(ctx, request.Execution.Spec); err != nil {
		return nil, err
	}
	if changed {
		if err := runtimenetwork.EmitExecutableChangeWarning(ctx, sink); err != nil {
			return nil, err
		}
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
	stderr := &boundedBuffer{limit: maxACPStderr}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ACP process: %w", err)
	}
	prompt := map[string]any{"method": "session/prompt", "task": request.Task, "messages": request.Messages, "execution": request.Execution, "session": request.SessionBinding}
	encoded, _ := json.Marshal(prompt)
	_, _ = stdin.Write(append(encoded, '\n'))
	_ = stdin.Close()
	h := &turnHandle{cmd: cmd, stdout: stdout, stderr: stderr, sink: sink, done: make(chan struct{})}
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
	stderr *boundedBuffer
	sink   openruntime.EventSink
	done   chan struct{}
	once   sync.Once
	result openruntime.TurnResult
	err    error
}

func (h *turnHandle) collect() {
	result := openruntime.TurnResult{}
	scanner := bufio.NewScanner(h.stdout)
	scanner.Buffer(make([]byte, 64<<10), maxACPStreamLine)
	var parseErr error
	var sinkErr error
	seen := false
	terminal := false
	emitted := 0
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		seen = true
		var event struct {
			Type              string          `json:"type"`
			Status            string          `json:"status"`
			Result            string          `json:"result"`
			Error             string          `json:"error"`
			ProviderSessionID string          `json:"provider_session_id"`
			Payload           json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			if parseErr == nil {
				parseErr = fmt.Errorf("decode ACP event: %w", err)
			}
			continue
		}
		if event.ProviderSessionID != "" {
			result.ProviderSessionID = event.ProviderSessionID
		}
		if event.Result != "" {
			projected := safeoutput.ProjectResultText(event.Result)
			result.Result = projected.Text
			result.ResultTruncated = result.ResultTruncated || projected.Truncated
		}
		if event.Error != "" {
			projected := safeoutput.ProjectText(event.Error)
			result.Error = projected.Text
			result.ErrorTruncated = result.ErrorTruncated || projected.Truncated
		}
		if event.Status != "" {
			status := openruntime.TurnResultStatus(event.Status)
			if status.Valid() {
				if terminal && parseErr == nil {
					parseErr = fmt.Errorf("ACP reported multiple terminal statuses")
				}
				result.Status = status
				terminal = true
			} else if event.Status != "starting" && event.Status != "running" && parseErr == nil {
				parseErr = fmt.Errorf("ACP reported unsupported status %q", event.Status)
			}
		}
		if event.Type != "" && h.sink != nil && sinkErr == nil && emitted < openruntime.MaxPublicOutputEvents {
			payload := event.Payload
			if len(payload) == 0 {
				payload = json.RawMessage(`{}`)
			}
			if err := h.sink.Emit(context.Background(), openruntime.RuntimeEvent{Type: event.Type, Payload: payload, OccurredAt: time.Now().UTC()}); err != nil {
				sinkErr = fmt.Errorf("emit ACP Runtime Event: %w", err)
			}
			emitted++
		}
	}
	if err := scanner.Err(); err != nil {
		parseErr = errors.Join(parseErr, fmt.Errorf("read ACP stream: %w", err))
	}
	waitErr := h.cmd.Wait()
	if !seen {
		parseErr = errors.Join(parseErr, fmt.Errorf("ACP stream was empty"))
	}
	if !terminal {
		parseErr = errors.Join(parseErr, fmt.Errorf("ACP stream ended without a terminal result"))
	}
	if waitErr != nil {
		parseErr = errors.Join(parseErr, fmt.Errorf("ACP process exited: %w", waitErr))
	}
	runtimeErr := errors.Join(parseErr, sinkErr)
	if runtimeErr != nil {
		result.Status = openruntime.TurnResultUncertain
		result.SideEffectsKnown = false
		if result.Error == "" {
			result.Error = "ACP Runtime ended without a verifiable result"
		}
		result.Error = safeoutput.RedactText(result.Error)
		if diagnostic := strings.TrimSpace(safeoutput.RedactText(h.stderr.String())); diagnostic != "" {
			result.Error = safeoutput.RedactText(result.Error + "; ACP stderr: " + diagnostic)
		}
	}
	result = safeoutput.SanitizeTurnResult(result)
	h.once.Do(func() { h.result, h.err = result, runtimeErr; close(h.done) })
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

type boundedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(value []byte) (int, error) {
	written := len(value)
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = true
		return written, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
		b.truncated = true
	}
	_, _ = b.buffer.Write(value)
	return written, nil
}

func (b *boundedBuffer) String() string { return b.buffer.String() }

var _ openruntime.AgentRuntimeAdapter = (*Adapter)(nil)
var _ openruntime.TurnHandle = (*turnHandle)(nil)
