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
	runtimenetwork "openagentx/internal/runtime/network"
)

// Keep the escalation window below the externally observable cancellation SLA.
// AGY tools can detach into a different process group, so waiting the full SLA
// before escalating would allow those descendants to outlive cancellation.
const defaultCancelGrace = 2 * time.Second

// A timeout still gives the traced AGY process a short chance to exit before
// cleaning up its tracer, without exceeding os/exec's WaitDelay budget.
const timeoutTraceeGrace = 100 * time.Millisecond

// SIGKILL should reap a tracee quickly. Keep the tracer alive through this
// bounded confirmation window before proceeding to tracer cancellation.
const descendantKillGrace = 500 * time.Millisecond

const cancellationPollInterval = 20 * time.Millisecond

const cancellationStableScans = 3

type Config struct {
	Binary        string
	Models        []string
	WorkingDir    string
	Environment   []string
	StderrLimit   int
	HealthTimeout time.Duration
	CancelGrace   time.Duration
	Network       domain.NetworkPolicy
}

type Adapter struct {
	config    Config
	networkMu sync.RWMutex
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
	if err := config.Network.Validate(); err != nil {
		return nil, err
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

func (a *Adapter) ApplyNetworkPolicy(policy domain.NetworkPolicy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	if _, err := runtimenetwork.Environment(nil, policy, "agy-batch", a.config.Binary); err != nil {
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

func (a *Adapter) Descriptor(context.Context) (openruntime.AdapterDescriptor, error) {
	return openruntime.AdapterDescriptor{
		AdapterID: "agy-batch", BackendType: "agy", Version: "1", LaunchProtocol: "argv",
		Models: append([]string(nil), a.config.Models...), ReasoningModes: []domain.ReasoningMode{
			domain.ReasoningBackendDefault, domain.ReasoningEffort,
		}, SessionModes: []domain.SessionMode{domain.SessionModeNew, domain.SessionModeResume},
		Steer: openruntime.SteerQueued, Approval: openruntime.ApprovalPreflight,
		Cancel: openruntime.CancelProcessSignal, Streams: true, MaxConcurrency: 1,
		NetworkModes:       []string{"inherit", "named_profile"},
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
	env, err := a.environment(a.configuredNetwork())
	if err != nil {
		return err
	}
	command.Env = env
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
	env, err := a.environment(spec.Network)
	if err != nil {
		cancel()
		return nil, err
	}
	command.Env = env
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = a.config.CancelGrace
	cancelController := newProcessCancelController(command, a.config.CancelGrace)
	command.Cancel = func() error { return cancelController.cancel(true) }
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
		cleanupErr := signalProcessGroup(command.Process.Pid, syscall.SIGKILL)
		cancel()
		return nil, errors.Join(fmt.Errorf("read AGY process identity: %w", err), cleanupErr)
	}
	cancelController.setRoot(processRef{pid: command.Process.Pid, startTime: startTime})
	handle := &turnHandle{command: command, stdout: stdout, stderr: stderr, sink: sink,
		stderrLimit: a.config.StderrLimit, prompt: prompt, cancel: cancel, done: make(chan struct{}), cancelController: cancelController}
	go handle.collect()
	return handle, nil
}

func (a *Adapter) environment(policy domain.NetworkPolicy) ([]string, error) {
	if policy.IsZero() {
		policy = a.configuredNetwork()
	}
	base := append([]string{}, os.Environ()...)
	base = append(base, a.config.Environment...)
	return runtimenetwork.Environment(base, policy, "agy-batch", a.config.Binary)
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
	command          *exec.Cmd
	stdout           io.ReadCloser
	stderr           io.ReadCloser
	sink             openruntime.EventSink
	stderrLimit      int
	prompt           string
	cancel           context.CancelFunc
	done             chan struct{}
	cancelController *processCancelController
	once             sync.Once
	result           openruntime.TurnResult
	err              error
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
	cancelErr := h.cancelController.wait()
	stderrDetail := sanitizeDiagnostic(stderr.output, h.stderrLimit, h.prompt)
	if waitErr != nil {
		if result.Status == openruntime.TurnResultSucceeded {
			result.Status = openruntime.TurnResultUncertain
			result.SideEffectsKnown = false
		}
	}
	runtimeErr := errors.Join(parseErr, stderr.err, waitErr, cancelErr)
	if runtimeErr != nil {
		result.Status = openruntime.TurnResultUncertain
		result.SideEffectsKnown = false
	}
	if runtimeErr != nil || result.Status == openruntime.TurnResultFailed || result.Status == openruntime.TurnResultUncertain {
		result.Error = joinDiagnostic(result.Error, parseErr, stderr.err, waitErr, cancelErr, stderrDetail, stderr.truncated)
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

func joinDiagnostic(existing string, parseErr error, stderrErr error, waitErr error, cancelErr error, stderr string, truncated bool) string {
	parts := make([]string, 0, 6)
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
	if cancelErr != nil {
		parts = append(parts, fmt.Sprintf("AGY cancellation cleanup: %v", cancelErr))
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
	if h.command.Process == nil || h.cancelController == nil {
		return openruntime.ErrCancelUnsupported
	}
	return h.cancelController.cancel(false)
}

// processCancelController keeps the tracer alive while its traced AGY
// descendants receive cancellation. Killing the process group first can leave
// a SECCOMP_RET_TRACE child without a tracer, which manifests as ENOSYS from
// the next connect(2) call.
type processCancelController struct {
	command        *exec.Cmd
	grace          time.Duration
	rootMu         sync.RWMutex
	root           processRef
	once           sync.Once
	cleanupMu      sync.Mutex
	cleanupStarted bool
	cleanupDone    chan struct{}
	cleanupErr     error
}

func newProcessCancelController(command *exec.Cmd, grace time.Duration) *processCancelController {
	return &processCancelController{command: command, grace: grace, cleanupDone: make(chan struct{})}
}

func (c *processCancelController) setRoot(root processRef) {
	c.rootMu.Lock()
	c.root = root
	c.rootMu.Unlock()
}

func (c *processCancelController) getRoot() processRef {
	c.rootMu.RLock()
	defer c.rootMu.RUnlock()
	return c.root
}

func (c *processCancelController) cancel(timeout bool) error {
	var cancelErr error
	c.once.Do(func() {
		root := c.getRoot()
		if root.pid == 0 && c.command.Process != nil {
			startTime, err := processStartTime(c.command.Process.Pid)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					cancelErr = fmt.Errorf("read AGY root process identity before cancellation: %w", err)
					c.recordError(cancelErr)
				}
				return
			}
			root = processRef{pid: c.command.Process.Pid, startTime: startTime}
			c.setRoot(root)
		}
		if root.pid == 0 || c.command.Process == nil {
			return
		}
		c.cleanupMu.Lock()
		c.cleanupStarted = true
		c.cleanupMu.Unlock()
		descendants := descendantProcesses(root.pid)
		// Signal descendants deepest-first while the mgraftcp tracer remains alive.
		for index := len(descendants) - 1; index >= 0; index-- {
			if err := signalProcess(descendants[index], syscall.SIGTERM); err != nil {
				wrapped := fmt.Errorf("signal AGY descendant %d with SIGTERM: %w", descendants[index].pid, err)
				c.recordError(wrapped)
				cancelErr = errors.Join(cancelErr, wrapped)
			}
		}
		traceeGrace := c.grace
		if timeout && traceeGrace > timeoutTraceeGrace {
			traceeGrace = timeoutTraceeGrace
		}
		go c.finish(root, descendants, traceeGrace)
	})
	return cancelErr
}

func (c *processCancelController) finish(root processRef, descendants []processRef, traceeGrace time.Duration) {
	defer close(c.cleanupDone)
	known := append([]processRef(nil), descendants...)
	// Re-scan during the TERM phase so children spawned by a tracee after the
	// initial snapshot are still terminated while the tracer remains alive.
	termDeadline := time.Now().Add(traceeGrace)
	emptyScans := 0
	for time.Now().Before(termDeadline) {
		current := descendantProcesses(root.pid)
		known = mergeProcessRefs(known, current)
		for index := len(current) - 1; index >= 0; index-- {
			if err := signalProcess(current[index], syscall.SIGTERM); err != nil {
				c.recordError(fmt.Errorf("signal AGY descendant %d with SIGTERM: %w", current[index].pid, err))
			}
		}
		if len(current) == 0 {
			emptyScans++
			if emptyScans >= cancellationStableScans {
				break
			}
		} else {
			emptyScans = 0
		}
		time.Sleep(cancellationPollInterval)
	}
	// A traced child must be dead before its tracer is touched. This second
	// phase handles tracees that ignored SIGTERM without creating an ENOSYS
	// window in their seccomp-traced connect(2) calls.
	descendantDeadline := time.Now().Add(descendantKillGrace)
	emptyScans = 0
	for time.Now().Before(descendantDeadline) {
		current := descendantProcesses(root.pid)
		known = mergeProcessRefs(known, current)
		for index := len(current) - 1; index >= 0; index-- {
			if processRefsAlive([]processRef{current[index]}) {
				if err := signalProcess(current[index], syscall.SIGKILL); err != nil {
					c.recordError(fmt.Errorf("signal AGY descendant %d with SIGKILL: %w", current[index].pid, err))
				}
			}
		}
		if !processRefsAlive(known) {
			emptyScans++
			if emptyScans >= cancellationStableScans {
				break
			}
		} else {
			emptyScans = 0
		}
		time.Sleep(cancellationPollInterval)
	}
	if processRefsAlive(known) {
		c.recordError(fmt.Errorf("AGY descendants survived SIGKILL grace period"))
		return
	}

	// Capture every member of the original process group while the root is
	// still alive. This lets the final group sweep remain safe even if the root
	// exits before an ordinary group child does.
	groupMembers := processGroupMembers(root.pid)
	known = mergeProcessRefs(known, groupMembers)
	groupTracees := make([]processRef, 0, len(groupMembers))
	for _, member := range groupMembers {
		if member.pid != root.pid {
			groupTracees = append(groupTracees, member)
		}
	}
	// A child can have exited its parent tree while retaining the original
	// process group. Treat those members as tracees too, before touching root.
	for _, member := range groupTracees {
		if err := signalProcess(member, syscall.SIGTERM); err != nil {
			c.recordError(fmt.Errorf("signal AGY process-group tracee %d with SIGTERM: %w", member.pid, err))
		}
	}
	groupTraceeDeadline := time.Now().Add(traceeGrace)
	for time.Now().Before(groupTraceeDeadline) && processRefsAlive(groupTracees) {
		time.Sleep(cancellationPollInterval)
	}
	for _, member := range groupTracees {
		if processRefsAlive([]processRef{member}) {
			if err := signalProcess(member, syscall.SIGKILL); err != nil {
				c.recordError(fmt.Errorf("signal AGY process-group tracee %d with SIGKILL: %w", member.pid, err))
			}
		}
	}
	groupKillDeadline := time.Now().Add(descendantKillGrace)
	for time.Now().Before(groupKillDeadline) && processRefsAlive(groupTracees) {
		time.Sleep(cancellationPollInterval)
	}
	if processRefsAlive(groupTracees) {
		c.recordError(fmt.Errorf("AGY process-group tracees survived SIGKILL grace period"))
		return
	}

	// Only now is it safe to terminate the mgraftcp/AGY root tracer.
	if err := signalProcess(root, syscall.SIGTERM); err != nil {
		c.recordError(fmt.Errorf("signal AGY root %d with SIGTERM: %w", root.pid, err))
	}
	rootDeadline := time.Now().Add(c.grace)
	for time.Now().Before(rootDeadline) && processRefsAlive([]processRef{root}) {
		time.Sleep(20 * time.Millisecond)
	}
	if processRefsAlive([]processRef{root}) {
		if err := signalProcess(root, syscall.SIGKILL); err != nil {
			c.recordError(fmt.Errorf("signal AGY root %d with SIGKILL: %w", root.pid, err))
		}
	}
	// A process-group sweep is only a last-resort cleanup after identity-
	// checked individual signals, never the first cancellation action.
	start, err := processStartTime(root.pid)
	if err == nil && start == root.startTime {
		if groupErr := signalProcessGroup(root.pid, syscall.SIGKILL); groupErr != nil {
			c.recordError(fmt.Errorf("signal AGY process group %d with SIGKILL: %w", root.pid, groupErr))
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		c.recordError(fmt.Errorf("read AGY root process identity before process-group cleanup: %w", err))
	} else if processGroupHasKnownMember(root.pid, known) {
		if groupErr := signalProcessGroup(root.pid, syscall.SIGKILL); groupErr != nil {
			c.recordError(fmt.Errorf("signal AGY process group %d with SIGKILL: %w", root.pid, groupErr))
		}
	}
}

func mergeProcessRefs(existing, discovered []processRef) []processRef {
	seen := make(map[processRef]struct{}, len(existing)+len(discovered))
	merged := make([]processRef, 0, len(existing)+len(discovered))
	for _, process := range append(append([]processRef(nil), existing...), discovered...) {
		if _, ok := seen[process]; ok {
			continue
		}
		seen[process] = struct{}{}
		merged = append(merged, process)
	}
	return merged
}

func processGroupHasKnownMember(pgid int, processes []processRef) bool {
	for _, process := range processes {
		if !processRefAlive(process) {
			continue
		}
		group, err := processGroupID(process.pid)
		if err == nil && group == pgid {
			return true
		}
	}
	return false
}

func processGroupMembers(pgid int) []processRef {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	members := make([]processRef, 0)
	for _, entry := range entries {
		pid, err := parsePID(entry.Name())
		if err != nil {
			continue
		}
		_, startTime, err := readProcessStat(pid)
		if err != nil {
			continue
		}
		group, err := processGroupID(pid)
		if err == nil && group == pgid {
			members = append(members, processRef{pid: pid, startTime: startTime})
		}
	}
	return members
}

func processGroupID(pid int) (int, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	closeParen := strings.LastIndexByte(string(data), ')')
	if closeParen < 0 {
		return 0, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(string(data[closeParen+1:]))
	if len(fields) <= 2 {
		return 0, fmt.Errorf("incomplete /proc/%d/stat", pid)
	}
	return strconv.Atoi(fields[2])
}

func (c *processCancelController) recordError(err error) {
	if err == nil {
		return
	}
	c.cleanupMu.Lock()
	c.cleanupErr = errors.Join(c.cleanupErr, err)
	c.cleanupMu.Unlock()
}

func (c *processCancelController) wait() error {
	c.cleanupMu.Lock()
	started := c.cleanupStarted
	done := c.cleanupDone
	c.cleanupMu.Unlock()
	if started {
		<-done
	}
	c.cleanupMu.Lock()
	defer c.cleanupMu.Unlock()
	return c.cleanupErr
}

func processRefsAlive(processes []processRef) bool {
	for _, process := range processes {
		if processRefAlive(process) {
			return true
		}
	}
	return false
}

func processRefAlive(process processRef) bool {
	start, err := processStartTime(process.pid)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil || start != process.startTime {
		return err != nil && !errors.Is(err, os.ErrNotExist)
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", process.pid))
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil {
		return true
	}
	closeParen := strings.LastIndexByte(string(data), ')')
	if closeParen < 0 {
		return true
	}
	fields := strings.Fields(string(data[closeParen+1:]))
	return len(fields) == 0 || fields[0] != "Z"
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
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read process identity: %w", err)
	}
	if startTime != process.startTime {
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
