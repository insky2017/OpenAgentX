package agy

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"agentbus/internal/domain"
	openruntime "agentbus/internal/runtime"
)

type Config struct {
	Binary        string
	WorkingDir    string
	Environment   []string
	StderrLimit   int
	HealthTimeout time.Duration
}

type Adapter struct {
	config Config
}

func NewAdapter(config Config) (*Adapter, error) {
	if strings.TrimSpace(config.Binary) == "" {
		config.Binary = "agy"
	}
	if config.StderrLimit <= 0 {
		config.StderrLimit = 64 << 10
	}
	if config.HealthTimeout <= 0 {
		config.HealthTimeout = 5 * time.Second
	}
	if _, err := exec.LookPath(config.Binary); err != nil {
		return nil, fmt.Errorf("AGY binary is unavailable: %w", err)
	}
	return &Adapter{config: config}, nil
}

func NewAdapterForTest(config Config) *Adapter {
	if strings.TrimSpace(config.Binary) == "" {
		config.Binary = "agy"
	}
	if config.StderrLimit <= 0 {
		config.StderrLimit = 64 << 10
	}
	return &Adapter{config: config}
}

func (a *Adapter) Descriptor(context.Context) (openruntime.AdapterDescriptor, error) {
	return openruntime.AdapterDescriptor{
		AdapterID: "agy-batch", BackendType: "agy", Version: "1", LaunchProtocol: "argv",
		Models: []string{"default"}, ReasoningModes: []domain.ReasoningMode{
			domain.ReasoningBackendDefault, domain.ReasoningEffort, domain.ReasoningBudgetTokens,
		}, SessionModes: []domain.SessionMode{domain.SessionModeNew, domain.SessionModeResume},
		Steer: openruntime.SteerQueued, Approval: openruntime.ApprovalPreflight,
		Cancel: openruntime.CancelProcessSignal, Streams: true, MaxConcurrency: 1,
		BackendOptionsJSON: []byte(`{"type":"object"}`),
	}, nil
}

func (a *Adapter) Validate(_ context.Context, spec domain.ExecutionSpec) error {
	if err := spec.ValidateShape(); err != nil {
		return err
	}
	if spec.AdapterID != "agy-batch" {
		return domain.ErrUnsupportedCapability
	}
	if spec.Session.Mode != domain.SessionModeNew && spec.Session.Mode != domain.SessionModeResume {
		return domain.ErrUnsupportedCapability
	}
	return nil
}

func (a *Adapter) Health(ctx context.Context) error {
	healthContext, cancel := context.WithTimeout(ctx, a.config.HealthTimeout)
	defer cancel()
	command := exec.CommandContext(healthContext, a.config.Binary, "--version")
	command.Dir = a.config.WorkingDir
	command.Env = append(os.Environ(), a.config.Environment...)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("AGY health probe failed: %w (%s)", err, sanitizeOutput(output, a.config.StderrLimit))
	}
	return nil
}

func (a *Adapter) StartTurn(ctx context.Context, request openruntime.TurnRequest, sink openruntime.EventSink) (openruntime.TurnHandle, error) {
	if err := a.Validate(ctx, request.Execution.Spec); err != nil {
		return nil, err
	}
	args := []string{"--print", "--output-format", "stream-json"}
	if binding := request.SessionBinding; binding != nil && binding.ProviderSessionID != "" {
		args = append(args, "--conversation", binding.ProviderSessionID)
	}
	command := exec.CommandContext(ctx, a.config.Binary, args...)
	command.Dir = a.config.WorkingDir
	command.Env = append(os.Environ(), a.config.Environment...)
	prompt := buildPrompt(request)
	command.Stdin = strings.NewReader(prompt)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create AGY stdout pipe: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("create AGY stderr pipe: %w", err)
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start AGY process: %w", err)
	}
	handle := &turnHandle{command: command, stdout: stdout, stderr: stderr, sink: sink,
		stderrLimit: a.config.StderrLimit, done: make(chan struct{})}
	go handle.collect()
	return handle, nil
}

func buildPrompt(request openruntime.TurnRequest) string {
	var builder strings.Builder
	builder.WriteString(request.Task.Content)
	for _, message := range request.Messages {
		if message.Content == "" {
			continue
		}
		builder.WriteString("\n\nFollow-up:\n")
		builder.WriteString(message.Content)
	}
	return builder.String() + "\n"
}

type turnHandle struct {
	command     *exec.Cmd
	stdout      io.ReadCloser
	stderr      io.ReadCloser
	sink        openruntime.EventSink
	stderrLimit int
	done        chan struct{}
	once        sync.Once
	result      openruntime.TurnResult
	err         error
}

func (h *turnHandle) collect() {
	result, parseErr := parseStreamJSON(h.stdout, h.sink)
	stderrBytes, stderrErr := io.ReadAll(io.LimitReader(h.stderr, int64(h.stderrLimit)+1))
	if len(stderrBytes) > h.stderrLimit {
		parseErr = fmt.Errorf("AGY stderr exceeded limit")
	}
	if stderrErr != nil && parseErr == nil {
		parseErr = stderrErr
	}
	waitErr := h.command.Wait()
	if waitErr != nil {
		if parseErr == nil {
			parseErr = fmt.Errorf("AGY process exited: %w", waitErr)
		}
		if result.Status == openruntime.TurnResultSucceeded {
			result.Status = openruntime.TurnResultUncertain
			result.SideEffectsKnown = false
		}
		if result.Error == "" {
			result.Error = "AGY process exited unsuccessfully"
		}
	}
	if parseErr != nil {
		result.Status = openruntime.TurnResultUncertain
		result.SideEffectsKnown = false
		if result.Error == "" {
			result.Error = parseErr.Error()
		}
	}
	h.once.Do(func() {
		h.result, h.err = result, parseErr
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
	if err := h.command.Process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("signal AGY process: %w", err)
	}
	return nil
}

func sanitizeOutput(output []byte, limit int) string {
	if len(output) > limit {
		output = output[:limit]
	}
	return strings.TrimSpace(string(bytes.Map(func(value rune) rune {
		if value == '\n' || value == '\r' || (value >= 0x20 && value <= 0x7e) {
			return value
		}
		return '?'
	}, output)))
}

func parseStderrLines(reader io.Reader, limit int) ([]string, error) {
	scanner := bufio.NewScanner(io.LimitReader(reader, int64(limit)+1))
	lines := make([]string, 0)
	for scanner.Scan() {
		lines = append(lines, sanitizeOutput(scanner.Bytes(), limit))
	}
	return lines, scanner.Err()
}

var _ openruntime.AgentRuntimeAdapter = (*Adapter)(nil)
var _ openruntime.TurnHandle = (*turnHandle)(nil)
