package agy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

// Keep the escalation window below the externally observable cancellation SLA.
// AGY tools can detach into a different process group, so waiting the full SLA
// before escalating would allow those descendants to outlive cancellation.
const defaultCancelGrace = 2 * time.Second

type Config struct {
	Binary        string
	Models        []string
	WorkingDir    string
	Environment   []string
	StderrLimit   int
	HealthTimeout time.Duration
	CancelGrace   time.Duration
}

type Adapter struct {
	config Config
}

func NewAdapter(config Config) (*Adapter, error) {
	if strings.TrimSpace(config.Binary) == "" {
		config.Binary = "agy-graft"
	}
	if len(config.Models) == 0 {
		config.Models = []string{"default"}
	}
	if config.StderrLimit <= 0 {
		config.StderrLimit = 64 << 10
	}
	if config.HealthTimeout <= 0 {
		config.HealthTimeout = 5 * time.Second
	}
	if config.CancelGrace <= 0 {
		config.CancelGrace = defaultCancelGrace
	}
	if _, err := exec.LookPath(config.Binary); err != nil {
		return nil, fmt.Errorf("AGY binary is unavailable: %w", err)
	}
	return &Adapter{config: config}, nil
}

func NewAdapterForTest(config Config) *Adapter {
	if strings.TrimSpace(config.Binary) == "" {
		config.Binary = "agy-graft"
	}
	if len(config.Models) == 0 {
		config.Models = []string{"default"}
	}
	if config.StderrLimit <= 0 {
		config.StderrLimit = 64 << 10
	}
	if config.CancelGrace <= 0 {
		config.CancelGrace = defaultCancelGrace
	}
	return &Adapter{config: config}
}

func (a *Adapter) Descriptor(context.Context) (openruntime.AdapterDescriptor, error) {
	return openruntime.AdapterDescriptor{
		AdapterID: "agy-batch", BackendType: "agy", Version: "1", LaunchProtocol: "argv",
		Models: append([]string(nil), a.config.Models...), ReasoningModes: []domain.ReasoningMode{
			domain.ReasoningBackendDefault, domain.ReasoningEffort,
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
	modelSupported := false
	for _, model := range a.config.Models {
		if spec.Model == model {
			modelSupported = true
			break
		}
	}
	if !modelSupported {
		return domain.ErrUnsupportedCapability
	}
	if spec.Session.Mode != domain.SessionModeNew && spec.Session.Mode != domain.SessionModeResume {
		return domain.ErrUnsupportedCapability
	}
	if spec.Reasoning.Mode == domain.ReasoningBudgetTokens || spec.Budget.MaxTokens != 0 {
		return domain.ErrUnsupportedCapability
	}
	if spec.Sandbox != "" {
		return domain.ErrUnsupportedCapability
	}
	if spec.Reasoning.Mode == domain.ReasoningEffort {
		switch spec.Reasoning.Value {
		case "low", "medium", "high":
		default:
			return domain.ErrInvalidInput("AGY reasoning effort must be low, medium, or high")
		}
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
	spec := request.Execution.Spec
	args := []string{
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--print-timeout", spec.Timeout.String(),
		"--dangerously-skip-permissions",
	}
	if spec.Model != "" && spec.Model != "default" {
		args = append(args, "--model", spec.Model)
	}
	if spec.Reasoning.Mode == domain.ReasoningEffort {
		args = append(args, "--effort", spec.Reasoning.Value)
	}
	if binding := request.SessionBinding; binding != nil && binding.ProviderSessionID != "" {
		args = append(args, "--conversation", binding.ProviderSessionID)
	}
	// --print requires a value. An explicit empty prompt selects print mode while
	// stream-json stdin remains the sole source of turn messages.
	args = append(args, "--print", "")
	turnContext, cancel := context.WithTimeout(ctx, spec.Timeout)
	command := exec.CommandContext(turnContext, a.config.Binary, args...)
	command.Dir = a.config.WorkingDir
	command.Env = append(os.Environ(), a.config.Environment...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = a.config.CancelGrace
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return signalProcessGroup(command.Process.Pid, syscall.SIGKILL)
	}
	prompt := buildPrompt(request, a.config.WorkingDir)
	input, err := encodeStreamInput(prompt)
	if err != nil {
		cancel()
		return nil, err
	}
	command.Stdin = bytes.NewReader(input)
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create AGY stdout pipe: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create AGY stderr pipe: %w", err)
	}
	if err := command.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start AGY process: %w", err)
	}
	startTime, err := processStartTime(command.Process.Pid)
	if err != nil {
		_ = signalProcessGroup(command.Process.Pid, syscall.SIGKILL)
		cancel()
		return nil, fmt.Errorf("read AGY process identity: %w", err)
	}
	handle := &turnHandle{command: command, stdout: stdout, stderr: stderr, sink: sink,
		stderrLimit: a.config.StderrLimit, prompt: prompt, cancel: cancel, done: make(chan struct{}), cancelGrace: a.config.CancelGrace,
		process: processRef{pid: command.Process.Pid, startTime: startTime}}
	go handle.collect()
	return handle, nil
}

func encodeStreamInput(prompt string) ([]byte, error) {
	envelope := struct {
		Event   string `json:"event"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}{Event: "user"}
	envelope.Message.Content = prompt
	input, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("encode AGY stream input: %w", err)
	}
	return append(input, '\n'), nil
}

func buildPrompt(request openruntime.TurnRequest, workingDir string) string {
	var builder strings.Builder
	if workingDir != "" {
		builder.WriteString("OpenAgentX Runtime Context (authoritative):\n")
		builder.WriteString("- Task ID: ")
		builder.WriteString(request.Task.ID)
		builder.WriteString("\n- Workspace root: ")
		builder.WriteString(fmt.Sprintf("%q", workingDir))
		builder.WriteString("\n- Treat the workspace root as the current project for all file operations.\n")
		builder.WriteString("- For every run_command call, set Cwd to the workspace root explicitly; the AGY tool scratch directory is not the task workspace.\n\nTask:\n")
	}
	builder.WriteString(request.Task.Content)
	for _, message := range request.Messages {
		if message.Content == "" {
			continue
		}
		builder.WriteString("\n\nFollow-up:\n")
		builder.WriteString(message.Content)
	}
	return builder.String()
}

type turnHandle struct {
	command     *exec.Cmd
	stdout      io.ReadCloser
	stderr      io.ReadCloser
	sink        openruntime.EventSink
	stderrLimit int
	prompt      string
	cancel      context.CancelFunc
	done        chan struct{}
	cancelGrace time.Duration
	process     processRef
	once        sync.Once
	cancelOnce  sync.Once
	result      openruntime.TurnResult
	err         error
}

func (h *turnHandle) collect() {
	defer h.cancel()
	stderrResult := make(chan stderrCapture, 1)
	go func() {
		stderrResult <- captureStderr(h.stderr, h.stderrLimit)
	}()
	result, parseErr := parseStreamJSON(h.stdout, h.sink)
	stderr := <-stderrResult
	waitErr := h.command.Wait()
	stderrDetail := sanitizeDiagnostic(stderr.output, h.stderrLimit, h.prompt)
	if waitErr != nil {
		if result.Status == openruntime.TurnResultSucceeded {
			result.Status = openruntime.TurnResultUncertain
			result.SideEffectsKnown = false
		}
	}
	runtimeErr := errors.Join(parseErr, stderr.err, waitErr)
	if runtimeErr != nil {
		result.Status = openruntime.TurnResultUncertain
		result.SideEffectsKnown = false
	}
	if runtimeErr != nil || result.Status == openruntime.TurnResultFailed || result.Status == openruntime.TurnResultUncertain {
		result.Error = joinDiagnostic(result.Error, parseErr, stderr.err, waitErr, stderrDetail, stderr.truncated)
	}
	h.once.Do(func() {
		h.result, h.err = result, runtimeErr
		close(h.done)
	})
}

type stderrCapture struct {
	output    []byte
	truncated bool
	err       error
}

func captureStderr(reader io.Reader, limit int) stderrCapture {
	writer := &boundedCaptureWriter{limit: limit}
	_, err := io.Copy(writer, reader)
	return stderrCapture{output: writer.buffer.Bytes(), truncated: writer.truncated, err: err}
}

type boundedCaptureWriter struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (w *boundedCaptureWriter) Write(value []byte) (int, error) {
	originalLength := len(value)
	remaining := w.limit - w.buffer.Len()
	if remaining < len(value) {
		w.truncated = true
		if remaining < 0 {
			remaining = 0
		}
		value = value[:remaining]
	}
	if len(value) > 0 {
		_, _ = w.buffer.Write(value)
	}
	return originalLength, nil
}

func joinDiagnostic(existing string, parseErr error, stderrErr error, waitErr error, stderr string, truncated bool) string {
	parts := make([]string, 0, 5)
	if strings.TrimSpace(existing) != "" {
		parts = append(parts, strings.TrimSpace(existing))
	}
	if parseErr != nil {
		parts = append(parts, parseErr.Error())
	}
	if stderrErr != nil {
		parts = append(parts, fmt.Sprintf("read AGY stderr: %v", stderrErr))
	}
	if waitErr != nil {
		parts = append(parts, fmt.Sprintf("AGY process exited: %v", waitErr))
	}
	if stderr != "" {
		parts = append(parts, "AGY stderr: "+stderr)
	}
	if truncated {
		parts = append(parts, "AGY stderr was truncated")
	}
	return strings.Join(parts, "; ")
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
	var cancelErr error
	h.cancelOnce.Do(func() {
		descendants := descendantProcesses(h.process.pid)
		if err := signalProcessGroup(h.process.pid, syscall.SIGTERM); err != nil {
			cancelErr = fmt.Errorf("signal AGY process group: %w", err)
			return
		}
		for _, child := range descendants {
			_ = signalProcess(child, syscall.SIGTERM)
		}
		time.AfterFunc(h.cancelGrace, func() {
			_ = signalProcessGroup(h.process.pid, syscall.SIGKILL)
			for _, child := range descendants {
				_ = signalProcess(child, syscall.SIGKILL)
			}
		})
	})
	return cancelErr
}

func signalProcessGroup(pgid int, signal syscall.Signal) error {
	if err := syscall.Kill(-pgid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

type processRef struct {
	pid       int
	startTime uint64
}

// descendantProcesses snapshots the complete child tree before the leader is
// signaled. This covers tools that call setsid and therefore escape the
// leader's process group while retaining the original parent relationship.
func descendantProcesses(rootPID int) []processRef {
	type processInfo struct {
		ref  processRef
		ppid int
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	children := make(map[int][]processInfo)
	for _, entry := range entries {
		pid, err := parsePID(entry.Name())
		if err != nil {
			continue
		}
		ppid, startTime, err := readProcessStat(pid)
		if err != nil {
			continue
		}
		children[ppid] = append(children[ppid], processInfo{ref: processRef{pid: pid, startTime: startTime}, ppid: ppid})
	}
	var descendants []processRef
	queue := append([]int(nil), rootPID)
	seen := map[int]struct{}{rootPID: {}}
	for len(queue) != 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, child := range children[parent] {
			if _, ok := seen[child.ref.pid]; ok {
				continue
			}
			seen[child.ref.pid] = struct{}{}
			descendants = append(descendants, child.ref)
			queue = append(queue, child.ref.pid)
		}
	}
	return descendants
}

func signalProcess(process processRef, signal syscall.Signal) error {
	startTime, err := processStartTime(process.pid)
	if err != nil || startTime != process.startTime {
		return nil
	}
	if err := syscall.Kill(process.pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

func processStartTime(pid int) (uint64, error) {
	_, startTime, err := readProcessStat(pid)
	return startTime, err
}

func readProcessStat(pid int) (int, uint64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, 0, err
	}
	closeParen := strings.LastIndexByte(string(data), ')')
	if closeParen < 0 {
		return 0, 0, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(string(data[closeParen+1:]))
	if len(fields) <= 19 {
		return 0, 0, fmt.Errorf("incomplete /proc/%d/stat", pid)
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, err
	}
	startTime, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return ppid, startTime, nil
}

func parsePID(value string) (int, error) {
	if value == "" {
		return 0, errors.New("empty PID")
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, errors.New("not a PID")
		}
	}
	return strconv.Atoi(value)
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

var (
	urlCredentialPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/@\s]+@`)
	authorizationPattern = regexp.MustCompile(`(?i)\bauthorization\b\s*[:=]\s*[^\r\n]+`)
	secretValuePattern   = regexp.MustCompile(`(?i)\b(authorization|api[_-]?key|token|password|passwd|secret)\b\s*[:=]\s*[^\s,;]+`)
)

func sanitizeDiagnostic(output []byte, limit int, prompt string) string {
	if len(output) > limit {
		output = output[:limit]
	}
	text := string(output)
	if prompt != "" {
		text = strings.ReplaceAll(text, prompt, "[REDACTED_PROMPT]")
		if encoded, err := json.Marshal(prompt); err == nil && len(encoded) >= 2 {
			text = strings.ReplaceAll(text, string(encoded[1:len(encoded)-1]), "[REDACTED_PROMPT]")
		}
		for _, line := range strings.Split(prompt, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				text = strings.ReplaceAll(text, line, "[REDACTED_PROMPT]")
			}
		}
		for _, field := range strings.Fields(prompt) {
			if len(field) >= 4 {
				text = strings.ReplaceAll(text, field, "[REDACTED_PROMPT]")
			}
		}
	}
	text = sanitizeOutput([]byte(text), len(text))
	text = urlCredentialPattern.ReplaceAllString(text, `${1}[REDACTED]@`)
	text = authorizationPattern.ReplaceAllString(text, `authorization=[REDACTED]`)
	return secretValuePattern.ReplaceAllString(text, `${1}=[REDACTED]`)
}

var _ openruntime.AgentRuntimeAdapter = (*Adapter)(nil)
var _ openruntime.TurnHandle = (*turnHandle)(nil)
