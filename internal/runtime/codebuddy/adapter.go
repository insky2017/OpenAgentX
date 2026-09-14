package codebuddy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	runtimenetwork "openagentx/internal/runtime/network"
	"openagentx/internal/safeoutput"
)

const (
	defaultOutputLimit  = 1 << 20
	defaultCancelGrace  = 10 * time.Second
	maxLiveOutputEvents = 256
)

type Config struct {
	Binary         string
	WorkingDir     string
	Environment    []string
	Models         []string
	DefaultEffort  string
	PermissionMode string
	MaxTurns       int
	OutputLimit    int
	HealthTimeout  time.Duration
	// CancelGrace bounds the time between a process-group SIGTERM and the
	// SIGKILL escalation, and is also used as the exec WaitDelay so a CLI
	// exit cannot leave Wait blocked on pipes held open by grandchildren.
	CancelGrace     time.Duration
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
	if _, err := runtimenetwork.Environment(nil, policy, "codebuddy-cli", a.config.Binary); err != nil {
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
		config.Binary = "codebuddy"
	}
	if _, err := exec.LookPath(config.Binary); err != nil {
		return nil, fmt.Errorf("CodeBuddy binary is unavailable: %w", err)
	}
	if len(config.Models) == 0 {
		return nil, domain.ErrInvalidInput("CodeBuddy Adapter requires at least one model")
	}
	for _, model := range config.Models {
		if err := domain.ValidateIdentifier("CodeBuddy model", model); err != nil {
			return nil, err
		}
	}
	if config.DefaultEffort == "" {
		config.DefaultEffort = "high"
	}
	if !validEffort(config.DefaultEffort) {
		return nil, domain.ErrInvalidInput("unsupported CodeBuddy effort")
	}
	if config.PermissionMode == "" {
		config.PermissionMode = "acceptEdits"
	}
	if !validPermissionMode(config.PermissionMode) {
		return nil, domain.ErrInvalidInput("unsupported CodeBuddy permission mode")
	}
	if config.MaxTurns <= 0 {
		config.MaxTurns = 40
	}
	if config.OutputLimit <= 0 {
		config.OutputLimit = defaultOutputLimit
	}
	if config.HealthTimeout <= 0 {
		config.HealthTimeout = 10 * time.Second
	}
	if config.CancelGrace <= 0 {
		config.CancelGrace = defaultCancelGrace
	}
	return &Adapter{config: config}, nil
}

func (a *Adapter) Descriptor(context.Context) (openruntime.AdapterDescriptor, error) {
	return openruntime.AdapterDescriptor{
		AdapterID: "codebuddy-cli", BackendType: "codebuddy", Version: "1", LaunchProtocol: "argv",
		Models: append([]string(nil), a.config.Models...), ReasoningModes: []domain.ReasoningMode{
			domain.ReasoningBackendDefault, domain.ReasoningEffort,
		},
		SessionModes: []domain.SessionMode{domain.SessionModeNew}, Steer: openruntime.SteerQueued,
		Approval: openruntime.ApprovalPreflight, Cancel: openruntime.CancelProcessSignal,
		Streams: true, MaxConcurrency: 1, NetworkModes: []string{"inherit", "direct"}, BackendOptionsJSON: []byte(`{"type":"object"}`), RuntimeIdentity: a.config.RuntimeIdentity,
	}, nil
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
	if spec.AdapterID != "codebuddy-cli" || spec.Session.Mode != domain.SessionModeNew {
		return domain.ErrUnsupportedCapability
	}
	if !contains(a.config.Models, spec.Model) {
		return domain.ErrUnsupportedCapability
	}
	if spec.Reasoning.Mode == domain.ReasoningEffort && !validEffort(spec.Reasoning.Value) {
		return domain.ErrUnsupportedCapability
	}
	if spec.Reasoning.Mode != domain.ReasoningBackendDefault && spec.Reasoning.Mode != domain.ReasoningEffort {
		return domain.ErrUnsupportedCapability
	}
	return nil
}

func (a *Adapter) Health(ctx context.Context) error {
	healthContext, cancel := context.WithTimeout(ctx, a.config.HealthTimeout)
	defer cancel()
	command := exec.CommandContext(healthContext, a.config.Binary, "--version")
	command.Dir = a.config.WorkingDir
	env, err := a.environment(a.configuredNetwork())
	if err != nil {
		return err
	}
	command.Env = env
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("CodeBuddy health probe failed: %w (%s)", err, boundedText(output, 4096))
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
	spec := request.Execution.Spec
	effort := a.config.DefaultEffort
	if spec.Reasoning.Mode == domain.ReasoningEffort {
		effort = spec.Reasoning.Value
	}
	args := []string{
		"--print", "--output-format", "text", "--model", spec.Model,
		"--effort", effort, "--permission-mode", a.config.PermissionMode,
		"--max-turns", strconv.Itoa(a.config.MaxTurns), "--no-session-persistence",
	}
	command := exec.CommandContext(ctx, a.config.Binary, args...)
	command.Dir = a.config.WorkingDir
	env, err := a.environment(spec.Network)
	if err != nil {
		return nil, err
	}
	command.Env = env
	// The CLI runs in its own process group so cancellation signals reach
	// every descendant, not only the CLI process itself.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = a.config.CancelGrace
	command.Stdin = strings.NewReader(buildPrompt(request))
	stdout := newLiveOutputBuffer(a.config.OutputLimit, sink)
	stderr := newBoundedBuffer(a.config.OutputLimit)
	command.Stdout = stdout
	command.Stderr = stderr
	handle := &turnHandle{command: command, stdout: stdout, stderr: stderr, done: make(chan struct{}), cancelGrace: a.config.CancelGrace}
	// Context cancellation escalates straight to a process-group SIGKILL,
	// mirroring exec's default kill semantics but covering children too.
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return signalProcessGroup(command.Process.Pid, syscall.SIGKILL)
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start CodeBuddy process: %w", err)
	}
	go handle.collect()
	return handle, nil
}

func (a *Adapter) environment(policy domain.NetworkPolicy) ([]string, error) {
	if policy.IsZero() {
		policy = a.configuredNetwork()
	}
	base := append([]string{}, os.Environ()...)
	base = append(base, a.config.Environment...)
	return runtimenetwork.Environment(base, policy, "codebuddy-cli", a.config.Binary)
}

func buildPrompt(request openruntime.TurnRequest) string {
	var builder strings.Builder
	builder.WriteString("OpenAgentX Task ID: ")
	builder.WriteString(request.Task.ID)
	builder.WriteString("\n\n")
	builder.WriteString(request.Task.Content)
	for _, message := range request.Messages {
		if strings.TrimSpace(message.Content) == "" {
			continue
		}
		builder.WriteString("\n\nFollow-up:\n")
		builder.WriteString(message.Content)
	}
	builder.WriteString("\n\nComplete the requested work in the current workspace. Report the outcome and concrete verification evidence.\n")
	return builder.String()
}

type turnHandle struct {
	command         *exec.Cmd
	stdout          *liveOutputBuffer
	stderr          *boundedBuffer
	done            chan struct{}
	cancelGrace     time.Duration
	once            sync.Once
	cancelRequested atomic.Bool
	result          openruntime.TurnResult
	err             error
}

func (h *turnHandle) collect() {
	waitErr := h.command.Wait()
	h.stdout.Flush()
	streamErr := h.stdout.EventError()
	result := openruntime.TurnResult{}
	var resultErr error
	switch {
	case h.cancelRequested.Load():
		result.Status = openruntime.TurnResultCanceled
		result.SideEffectsKnown = false
	case waitErr != nil:
		result.Status = openruntime.TurnResultUncertain
		result.SideEffectsKnown = false
		result.Error = "CodeBuddy process exited without a verifiable result"
		if details := strings.TrimSpace(h.stderr.String()); details != "" {
			result.Error += ": " + details
		}
		resultErr = fmt.Errorf("CodeBuddy process exited: %w", waitErr)
	case streamErr != nil:
		result.Status = openruntime.TurnResultUncertain
		result.SideEffectsKnown = false
		result.Error = "CodeBuddy output event could not be persisted"
		resultErr = fmt.Errorf("emit CodeBuddy output event: %w", streamErr)
	case h.stdout.Truncated() || h.stderr.Truncated():
		result.Status = openruntime.TurnResultUncertain
		result.SideEffectsKnown = false
		result.Error = "CodeBuddy output exceeded the configured limit"
		resultErr = errors.New(result.Error)
	default:
		result.Status = openruntime.TurnResultSucceeded
		result.Result = strings.TrimSpace(h.stdout.String())
	}
	h.once.Do(func() {
		h.result, h.err = result, resultErr
		close(h.done)
	})
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
	if h.command.Process == nil {
		return openruntime.ErrCancelUnsupported
	}
	// Capture the group id while the leader is still running: after Wait
	// reaps the leader the group must still be signalled by id, not re-read
	// from a process that no longer exists.
	pgid := h.command.Process.Pid
	h.cancelRequested.Store(true)
	// The CLI leads its own process group, so the negative PID delivers the
	// signal to the CLI and every descendant it has not detached.
	if err := signalProcessGroup(pgid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("signal CodeBuddy process group: %w", err)
	}
	// Escalate to SIGKILL after the grace period so descendants that ignore
	// or delay SIGTERM cannot be left running in the background. The
	// escalation must not be cancelled by h.done: the leader exiting on
	// SIGTERM closes h.done while its children are still alive, and those are
	// exactly the processes the escalation exists to stop. ESRCH on a group
	// that is already gone is treated as success by signalProcessGroup.
	go func() {
		timer := time.NewTimer(h.cancelGrace)
		defer timer.Stop()
		<-timer.C
		_ = signalProcessGroup(pgid, syscall.SIGKILL)
	}()
	return nil
}

// signalProcessGroup sends signal to the process group led by pgid. A missing
// group (ESRCH) means the processes are already gone and is not an error.
func signalProcessGroup(pgid int, signal syscall.Signal) error {
	if err := syscall.Kill(-pgid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

type boundedBuffer struct {
	mu        sync.Mutex
	data      bytes.Buffer
	limit     int
	truncated bool
}

// liveOutputBuffer keeps the authoritative bounded final stdout while
// publishing only complete, redacted lines. Event count and event size are
// bounded so a noisy CLI cannot turn stdout into an unbounded Journal stream.
type liveOutputBuffer struct {
	storage  *boundedBuffer
	sink     openruntime.EventSink
	mu       sync.Mutex
	pending  []byte
	captured int
	events   int
	eventErr error
}

func newLiveOutputBuffer(limit int, sink openruntime.EventSink) *liveOutputBuffer {
	return &liveOutputBuffer{storage: newBoundedBuffer(limit), sink: sink}
}

func (b *liveOutputBuffer) Write(value []byte) (int, error) {
	written, _ := b.storage.Write(value)
	if b.sink == nil {
		return written, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.storage.limit - b.captured
	if remaining <= 0 || b.events >= maxLiveOutputEvents {
		return written, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
	}
	b.captured += len(value)
	b.pending = append(b.pending, value...)
	for b.events < maxLiveOutputEvents {
		newline := bytes.IndexByte(b.pending, '\n')
		if newline < 0 {
			break
		}
		line := append([]byte(nil), b.pending[:newline]...)
		b.pending = b.pending[newline+1:]
		b.emitLocked(line)
	}
	return written, nil
}

func (b *liveOutputBuffer) Flush() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.pending) == 0 || b.events >= maxLiveOutputEvents {
		return
	}
	line := append([]byte(nil), b.pending...)
	b.pending = nil
	b.emitLocked(line)
}

func (b *liveOutputBuffer) emitLocked(line []byte) {
	if b.eventErr != nil {
		return
	}
	text := strings.TrimSpace(safeoutput.RedactText(string(line)))
	if text == "" {
		return
	}
	payload, _ := json.Marshal(map[string]any{"stage": "output", "status": "running", "text": text, "has_output": true})
	if err := b.sink.Emit(context.Background(), openruntime.RuntimeEvent{Type: "turn.output", Payload: payload, OccurredAt: time.Now().UTC()}); err != nil && b.eventErr == nil {
		b.eventErr = err
	}
	b.events++
}

func (b *liveOutputBuffer) String() string  { return b.storage.String() }
func (b *liveOutputBuffer) Truncated() bool { return b.storage.Truncated() }

func (b *liveOutputBuffer) EventError() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.eventErr
}

func newBoundedBuffer(limit int) *boundedBuffer { return &boundedBuffer{limit: limit} }

func (b *boundedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	written := len(value)
	remaining := b.limit - b.data.Len()
	if remaining <= 0 {
		b.truncated = true
		return written, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
		b.truncated = true
	}
	_, _ = b.data.Write(value)
	return written, nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(append([]byte(nil), b.data.Bytes()...))
}

func (b *boundedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

func boundedText(value []byte, limit int) string {
	if len(value) > limit {
		value = value[:limit]
	}
	return strings.TrimSpace(strings.ToValidUTF8(string(value), "?"))
}

func validEffort(value string) bool {
	switch value {
	case "minimal", "low", "medium", "high", "xhigh", "max":
		return true
	default:
		return false
	}
}

func validPermissionMode(value string) bool {
	switch value {
	case "acceptEdits", "bypassPermissions", "default", "plan", "dontAsk", "auto":
		return true
	default:
		return false
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

var _ openruntime.AgentRuntimeAdapter = (*Adapter)(nil)
var _ openruntime.TurnHandle = (*turnHandle)(nil)
