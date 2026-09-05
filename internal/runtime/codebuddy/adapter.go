package codebuddy

import (
	"bytes"
	"context"
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
)

const (
	defaultOutputLimit = 1 << 20
	defaultCancelGrace = 10 * time.Second
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
	CancelGrace time.Duration
	Network     domain.NetworkPolicy
}

type Adapter struct{ config Config }

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
		Streams: false, MaxConcurrency: 1, NetworkModes: []string{"inherit", "direct"}, BackendOptionsJSON: []byte(`{"type":"object"}`),
	}, nil
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
	env, err := a.environment(a.config.Network)
	if err != nil {
		return err
	}
	command.Env = env
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("CodeBuddy health probe failed: %w (%s)", err, boundedText(output, 4096))
	}
	return nil
}

func (a *Adapter) StartTurn(ctx context.Context, request openruntime.TurnRequest, _ openruntime.EventSink) (openruntime.TurnHandle, error) {
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
	stdout := newBoundedBuffer(a.config.OutputLimit)
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
		policy = a.config.Network
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
	stdout, stderr  *boundedBuffer
	done            chan struct{}
	cancelGrace     time.Duration
	once            sync.Once
	cancelRequested atomic.Bool
	result          openruntime.TurnResult
	err             error
}

func (h *turnHandle) collect() {
	waitErr := h.command.Wait()
	result := openruntime.TurnResult{SideEffectsKnown: true}
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
