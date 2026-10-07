package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	runtimenetwork "openagentx/internal/runtime/network"
)

const AdapterID = "codex-app-server"

// DefaultTimeout leaves normal Codex work running until completion or cancellation.
// A positive explicit timeout remains authoritative for each frozen Run.
const DefaultTimeout time.Duration = 0

type Config struct {
	DiscoverModels  bool
	Binary          string
	WorkingDir      string
	Models          []string
	Timeout         time.Duration
	Environment     []string
	Network         domain.NetworkPolicy
	RuntimeIdentity domain.RuntimeIdentity
	Endpoint        string
	ThreadID        string
	HandoffFile     string
	StateDir        string
}

// SessionState is private worker-owned handoff metadata, never public output.
type SessionState struct {
	Endpoint  string    `json:"endpoint"`
	ThreadID  string    `json:"thread_id"`
	TurnID    string    `json:"turn_id,omitempty"`
	TaskID    string    `json:"task_id,omitempty"`
	RunID     string    `json:"run_id,omitempty"`
	State     string    `json:"state"`
	PID       int       `json:"pid,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Adapter struct {
	modelEfforts    map[string][]string
	catalogLoaded   bool
	mu              sync.Mutex
	config          Config
	client          *RPCClient
	process         *exec.Cmd
	processIdentity processIdentity
	processDone     chan error
	processLog      *os.File
	socketDir       string
	endpoint        string
	state           SessionState
	active          *turnHandle
	starting        bool
	closed          bool
	unresolved      bool
	handoff         string
	handoffUsed     bool
}

func NewAdapter(config Config) (*Adapter, error) {
	if config.Binary == "" {
		config.Binary = "codex"
	}
	if _, err := exec.LookPath(config.Binary); err != nil {
		return nil, fmt.Errorf("Codex binary unavailable: %w", err)
	}
	if len(config.Models) == 0 {
		return nil, domain.ErrInvalidInput("Codex Adapter requires at least one model")
	}
	for _, m := range config.Models {
		if err := domain.ValidateIdentifier("Codex model", m); err != nil {
			return nil, err
		}
	}
	if config.Timeout < 0 {
		return nil, domain.ErrInvalidInput("Codex timeout cannot be negative; zero means no execution deadline")
	}
	if config.WorkingDir == "" {
		config.WorkingDir, _ = os.Getwd()
	}
	if config.StateDir == "" {
		base, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		base = filepath.Join(base, ".local", "state", "openagentx", "codex")
		if err = os.MkdirAll(base, 0700); err != nil {
			return nil, err
		}
		config.StateDir, err = os.MkdirTemp(base, "worker-")
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(config.StateDir, 0700); err != nil {
		return nil, err
	}
	if config.Endpoint != "" && !config.Network.IsZero() && config.Network.Mode != domain.NetworkInherit {
		return nil, domain.ErrUnsupportedCapability
	}
	var handoff string
	if config.HandoffFile != "" {
		info, err := os.Stat(config.HandoffFile)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return nil, errors.New("Codex handoff must be a regular file no larger than 1 MiB")
		}
		data, err := os.ReadFile(config.HandoffFile)
		if err != nil {
			return nil, err
		}
		handoff = string(data)
	}
	a := &Adapter{config: config, endpoint: config.Endpoint, handoff: handoff}
	if raw, err := os.ReadFile(a.StatePath()); err == nil {
		var saved SessionState
		if json.Unmarshal(raw, &saved) == nil {
			saved.Endpoint = config.Endpoint
			saved.PID = 0
			if saved.State == "uncertain" || saved.State == "running" || saved.State == "starting" {
				a.unresolved = true
				saved.State = "uncertain"
			} else {
				saved.State = "restored_unconfirmed"
			}
			a.state = saved
		}
	}
	return a, nil
}

func (a *Adapter) Descriptor(ctx context.Context) (openruntime.AdapterDescriptor, error) {
	a.mu.Lock()
	discover := a.config.DiscoverModels && !a.catalogLoaded
	a.mu.Unlock()
	if discover {
		probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		a.mu.Lock()
		err := a.ensureLocked(probeCtx)
		client := a.client
		a.mu.Unlock()
		if err != nil {
			return openruntime.AdapterDescriptor{}, err
		}
		if err := a.loadModelCatalog(probeCtx, client); err != nil {
			return openruntime.AdapterDescriptor{}, err
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return openruntime.AdapterDescriptor{AdapterID: AdapterID, BackendType: "codex", Version: "1", SessionHandoff: true, LaunchProtocol: "app-server", Models: append([]string(nil), a.config.Models...), ModelReasoningEfforts: cloneModelEfforts(a.modelEfforts),
		ReasoningModes: []domain.ReasoningMode{domain.ReasoningBackendDefault, domain.ReasoningEffort}, SessionModes: []domain.SessionMode{domain.SessionModeNew, domain.SessionModeResume},
		Steer: openruntime.SteerNative, Approval: openruntime.ApprovalNative, Cancel: openruntime.CancelNative, Permissions: []string{"never", "on-request"}, Sandboxes: []string{"read-only", "workspace-write", "danger-full-access"},
		NetworkModes: []string{"inherit", "direct"}, Streams: true, MaxConcurrency: 1, DefaultTimeout: a.config.Timeout, BackendOptionsJSON: json.RawMessage(`{"type":"object","additionalProperties":false}`), RuntimeIdentity: a.config.RuntimeIdentity}, nil
}

func (a *Adapter) Validate(_ context.Context, s domain.ExecutionSpec) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := s.ValidateShape(); err != nil {
		return err
	}
	if s.AdapterID != AdapterID || (s.Session.Mode != domain.SessionModeNew && s.Session.Mode != domain.SessionModeResume) {
		return domain.ErrUnsupportedCapability
	}
	found := false
	for _, m := range a.config.Models {
		found = found || m == s.Model
	}
	if !found {
		return domain.ErrUnsupportedCapability
	}
	if s.Reasoning.Mode != domain.ReasoningBackendDefault && s.Reasoning.Mode != domain.ReasoningEffort {
		return domain.ErrUnsupportedCapability
	}
	if s.Reasoning.Mode == domain.ReasoningEffort && a.catalogLoaded && !slices.Contains(a.modelEfforts[s.Model], s.Reasoning.Value) {
		return domain.ErrUnsupportedCapability
	}
	if s.Reasoning.Mode == domain.ReasoningEffort && !a.catalogLoaded {
		switch s.Reasoning.Value {
		case "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra":
		default:
			return domain.ErrUnsupportedCapability
		}
	}
	if s.ApprovalPolicy != "" && s.ApprovalPolicy != "never" && s.ApprovalPolicy != "on-request" {
		return domain.ErrUnsupportedCapability
	}
	if s.Sandbox != "" && s.Sandbox != "read-only" && s.Sandbox != "workspace-write" && s.Sandbox != "danger-full-access" {
		return domain.ErrUnsupportedCapability
	}
	if s.Budget.MaxTokens != 0 {
		return domain.ErrUnsupportedCapability
	}
	if len(s.BackendOptions) > 0 && string(s.BackendOptions) != "{}" && string(s.BackendOptions) != "null" {
		return domain.ErrUnsupportedCapability
	}
	return nil
}

func (a *Adapter) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	a.mu.Lock()
	cfg := a.config
	a.mu.Unlock()
	env, err := runtimenetwork.Environment(append(os.Environ(), cfg.Environment...), cfg.Network, AdapterID, cfg.Binary)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, cfg.Binary, "--version")
	cmd.Dir = cfg.WorkingDir
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Codex version probe: %w", err)
	}
	a.mu.Lock()
	if err = a.ensureLocked(ctx); err != nil {
		a.mu.Unlock()
		return err
	}
	if a.state.State == "" {
		a.state.State = "idle"
	}
	client := a.client
	joinedID := a.config.ThreadID
	checkJoined := joinedID != "" && a.active == nil && !a.starting
	err = a.writeStateLocked()
	a.mu.Unlock()
	if err != nil {
		return err
	}
	if cfg.DiscoverModels {
		if err := a.loadModelCatalog(ctx, client); err != nil {
			return err
		}
	}
	if checkJoined {
		var response threadResponse
		if err = client.Call(ctx, "thread/read", map[string]any{"threadId": joinedID}, &response); err != nil {
			return err
		}
		if threadActive(response) {
			return errors.New("Codex joined session is busy with existing work; waiting for idle before claiming a task")
		}
	}
	return nil
}
func (a *Adapter) VerifyRuntimeIdentity(ctx context.Context, expected domain.RuntimeIdentity) (bool, error) {
	if expected.IsZero() && a.config.RuntimeIdentity.IsZero() {
		return false, nil
	}
	return runtimenetwork.VerifyRuntimeIdentity(ctx, expected, a.config.Binary, "")
}
func (a *Adapter) Endpoint() string    { a.mu.Lock(); defer a.mu.Unlock(); return a.endpoint }
func (a *Adapter) StatePath() string   { return filepath.Join(a.config.StateDir, "state.json") }
func (a *Adapter) ThreadID() string    { a.mu.Lock(); defer a.mu.Unlock(); return a.state.ThreadID }
func (a *Adapter) State() SessionState { a.mu.Lock(); defer a.mu.Unlock(); return a.state }

func (a *Adapter) ApplyNetworkPolicy(p domain.NetworkPolicy) error {
	if _, err := runtimenetwork.Environment(nil, p, AdapterID, a.config.Binary); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if reflect.DeepEqual(p, a.config.Network) {
		return nil
	}
	if sameNetworkEnvironment(p, a.config.Network, a.config.Binary) {
		a.config.Network = p
		return nil
	}
	if a.unresolved {
		return errors.New("Codex prior turn is unresolved; network changes require explicit recovery")
	}
	if a.config.Endpoint != "" {
		return errors.New("external Codex app-server cannot inherit a changed worker network environment")
	}
	if a.active != nil || a.starting {
		return errors.New("cannot change Codex network policy during an active turn")
	}
	a.stopLocked()
	a.config.Network = p
	return nil
}
func (a *Adapter) CloneForNetworkProbe(p domain.NetworkPolicy) (openruntime.AgentRuntimeAdapter, error) {
	a.mu.Lock()
	cfg := a.config
	a.mu.Unlock()
	cfg.Endpoint = ""
	cfg.ThreadID = ""
	cfg.StateDir = ""
	cfg.HandoffFile = ""
	cfg.Network = p
	return NewAdapter(cfg)
}

func (a *Adapter) ensureLocked(ctx context.Context) error {
	if a.closed {
		return errors.New("Codex Adapter is closed")
	}
	if a.unresolved {
		return errors.New("Codex prior turn is unresolved; explicit recovery is required before new execution")
	}
	if a.client != nil {
		select {
		case <-a.client.Done():
			a.stopLocked()
		default:
			return nil
		}
	}
	if a.config.Endpoint == "" {
		dir, err := os.MkdirTemp("", "oax-codex-")
		if err != nil {
			return err
		}
		a.socketDir = dir
		a.endpoint = "unix://" + filepath.Join(dir, "server.sock")
		env, err := runtimenetwork.Environment(append(os.Environ(), a.config.Environment...), a.config.Network, AdapterID, a.config.Binary)
		if err != nil {
			return err
		}
		log, err := os.OpenFile(filepath.Join(a.config.StateDir, "app-server.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		cmd := exec.Command(a.config.Binary, "app-server", "--listen", a.endpoint)
		cmd.Dir = a.config.WorkingDir
		cmd.Env = env
		cmd.Stdout = log
		cmd.Stderr = log
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err = cmd.Start(); err != nil {
			log.Close()
			return err
		}
		a.process = cmd
		a.processIdentity, _, err = readIdentity(cmd.Process.Pid)
		if err != nil {
			cmd.Process.Kill()
			cmd.Wait()
			log.Close()
			a.process = nil
			return err
		}
		a.processLog = log
		a.processDone = make(chan error, 1)
		go func() { a.processDone <- cmd.Wait() }()
	}
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var last error
	for {
		client, err := DialRPC(dialCtx, a.endpoint)
		if err == nil {
			if err = client.Initialize(dialCtx, "openagentx_worker"); err == nil {
				a.client = client
				return nil
			}
			client.Close()
		}
		last = err
		select {
		case <-dialCtx.Done():
			a.stopLocked()
			return fmt.Errorf("Codex app-server unavailable: %w", last)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (a *Adapter) stopLocked() {
	var tracker *processTracker
	if a.process != nil && a.process.Process != nil {
		tracker = newProcessTracker(a.processIdentity)
		if a.active != nil && a.active.processes != nil {
			tracker = a.active.processes
		}
		tracker.capture()
	}
	if a.client != nil {
		a.client.Close()
		a.client = nil
	}
	if a.process != nil && a.process.Process != nil {
		_ = syscall.Kill(-a.process.Process.Pid, syscall.SIGTERM)
		select {
		case <-a.processDone:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-a.process.Process.Pid, syscall.SIGKILL)
			<-a.processDone
		}
		a.process = nil
	}
	if tracker != nil {
		_, _ = tracker.stop()
	}
	if a.processLog != nil {
		a.processLog.Close()
		a.processLog = nil
	}
	if a.socketDir != "" {
		os.RemoveAll(a.socketDir)
		a.socketDir = ""
	}
}
func (a *Adapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	a.stopLocked()
	if a.unresolved {
		a.state.State = "uncertain"
	} else {
		a.state.State = "closed"
	}
	return a.writeStateLocked()
}

func sameNetworkEnvironment(left, right domain.NetworkPolicy, binary string) bool {
	lmode, rmode := left.Mode, right.Mode
	if lmode == "" {
		lmode = domain.NetworkInherit
	}
	if rmode == "" {
		rmode = domain.NetworkInherit
	}
	if lmode != rmode {
		return false
	}
	base := os.Environ()
	l, le := runtimenetwork.Environment(base, left, AdapterID, binary)
	r, re := runtimenetwork.Environment(base, right, AdapterID, binary)
	return le == nil && re == nil && reflect.DeepEqual(l, r)
}
func (a *Adapter) writeStateLocked() error {
	a.state.Endpoint = a.endpoint
	a.state.UpdatedAt = time.Now().UTC()
	if a.process != nil && a.process.Process != nil {
		a.state.PID = a.process.Process.Pid
	}
	data, err := json.MarshalIndent(a.state, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(a.StatePath()), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(a.StatePath()), ".codex-session-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, a.StatePath()); err != nil {
		return err
	}
	if a.state.TaskID != "" {
		if err = domain.ValidateIdentifier("task id", a.state.TaskID); err != nil {
			return err
		}
		dir := filepath.Join(a.config.StateDir, "tasks")
		if err = os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		taskFile, err := os.CreateTemp(dir, ".task-")
		if err != nil {
			return err
		}
		tmp := taskFile.Name()
		defer os.Remove(tmp)
		if _, err = taskFile.Write(data); err != nil {
			taskFile.Close()
			return err
		}
		if err = taskFile.Sync(); err != nil {
			taskFile.Close()
			return err
		}
		if err = taskFile.Close(); err != nil {
			return err
		}
		return os.Rename(tmp, filepath.Join(dir, a.state.TaskID+".json"))
	}
	return nil
}

type threadResponse struct {
	Thread struct {
		ID     string `json:"id"`
		Status struct {
			Type string `json:"type"`
		} `json:"status"`
		Turns []providerTurn `json:"turns"`
	} `json:"thread"`
}
type providerTurn struct {
	ID     string          `json:"id"`
	Status string          `json:"status"`
	Items  []providerItem  `json:"items"`
	Error  json.RawMessage `json:"error"`
}
type providerItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Text      string `json:"text"`
	Phase     string `json:"phase"`
	ProcessID string `json:"processId"`
	ExitCode  *int   `json:"exitCode"`
}

func (a *Adapter) StartTurn(ctx context.Context, request openruntime.TurnRequest, sink openruntime.EventSink) (openruntime.TurnHandle, error) {
	if err := a.Validate(ctx, request.Execution.Spec); err != nil {
		return nil, err
	}
	if request.Execution.Spec.Session.ForceNew && request.SessionBinding != nil {
		return nil, errors.New("a fresh session cannot resume an existing Task binding")
	}
	expected := request.Execution.Spec.Network.RuntimeIdentity
	if expected.IsZero() {
		expected = a.config.RuntimeIdentity
	}
	changed, err := a.VerifyRuntimeIdentity(ctx, expected)
	if err != nil {
		return nil, err
	}
	if changed {
		if err = runtimenetwork.EmitExecutableChangeWarning(ctx, sink); err != nil {
			return nil, err
		}
	}
	var deadline time.Time
	if request.Execution.Spec.Timeout > 0 {
		deadline = time.Now().Add(request.Execution.Spec.Timeout)
	}
	if frozen := request.Execution.DeadlineAt; !frozen.IsZero() && (deadline.IsZero() || frozen.Before(deadline)) {
		deadline = frozen
	}
	var turnCtx context.Context
	var cancel context.CancelFunc
	if deadline.IsZero() {
		turnCtx, cancel = context.WithCancel(ctx)
	} else {
		turnCtx, cancel = context.WithDeadline(ctx, deadline)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	fail := func(err error) (openruntime.TurnHandle, error) { cancel(); return nil, err }
	if a.active != nil || a.starting {
		return fail(errors.New("Codex worker already owns an active turn"))
	}
	a.starting = true
	defer func() { a.starting = false }()
	policy := request.Execution.Spec.Network
	if !policy.IsZero() && !reflect.DeepEqual(policy, a.config.Network) {
		return fail(errors.New("Codex execution network policy differs from installed host policy"))
	}
	if err = a.ensureLocked(turnCtx); err != nil {
		return fail(err)
	}
	cwd := a.config.WorkingDir
	if request.Execution.AgentInput != nil && request.Execution.AgentInput.WorkspaceRoot != "" {
		cwd = request.Execution.AgentInput.WorkspaceRoot
	}
	tid := ""
	source := "new"
	if request.SessionBinding != nil {
		tid = request.SessionBinding.ProviderSessionID
		source = "resume"
	} else if a.config.ThreadID != "" && !request.Execution.Spec.Session.ForceNew {
		tid = a.config.ThreadID
		source = "joined"
	}
	if tid == "" && request.Execution.Spec.Session.Mode == domain.SessionModeResume {
		return fail(openruntime.ErrSessionUnsupported)
	}
	params := map[string]any{"cwd": cwd, "model": request.Execution.Spec.Model}
	if v := request.Execution.Spec.ApprovalPolicy; v != "" {
		params["approvalPolicy"] = v
	}
	if v := request.Execution.Spec.Sandbox; v != "" {
		params["sandbox"] = v
	}
	if input := request.Execution.AgentInput; input != nil {
		params["developerInstructions"] = input.InstructionsContent
	}
	method := "thread/start"
	if tid != "" {
		method = "thread/resume"
		params["threadId"] = tid
	}
	var thread threadResponse
	if err = a.callUnlocked(turnCtx, method, params, &thread); err != nil {
		return fail(err)
	}
	tid = thread.Thread.ID
	if tid == "" {
		return fail(errors.New("Codex response omitted thread id"))
	}
	// Existing external clients may have active work. Never use turn/start to
	// implicitly steer it. Admission is serialized for this managed Worker;
	// arbitrary clients of the raw endpoint remain outside this guarantee.
	for threadActive(thread) {
		a.mu.Unlock()
		var waitErr error
		select {
		case <-turnCtx.Done():
			waitErr = turnCtx.Err()
		case <-time.After(time.Second):
		}
		a.mu.Lock()
		if waitErr != nil {
			return fail(waitErr)
		}
		if a.closed {
			return fail(errors.New("Codex Adapter closed while waiting for joined session"))
		}
		if err = a.callUnlocked(turnCtx, "thread/read", map[string]any{"threadId": tid}, &thread); err != nil {
			return fail(err)
		}
	}
	if sink == nil {
		return fail(errors.New("Codex requires a durable session binding sink"))
	}
	payload, _ := json.Marshal(map[string]any{"provider_session_id": tid, "source": source})
	a.mu.Unlock()
	err = sink.Emit(turnCtx, openruntime.RuntimeEvent{Type: "session.bound", Payload: payload, OccurredAt: time.Now().UTC()})
	a.mu.Lock()
	if err != nil {
		return fail(fmt.Errorf("persist Codex session before turn: %w", err))
	}
	if a.closed {
		return fail(errors.New("Codex Adapter closed before dispatch"))
	}
	events, unsubscribe := a.client.Subscribe()
	h := &turnHandle{adapter: a, client: a.client, ctx: turnCtx, cancel: cancel, threadID: tid, sink: sink, done: make(chan struct{}), events: events, unsubscribe: unsubscribe, pending: map[string]pendingApproval{}, cancelSignal: make(chan struct{}), commands: map[string]providerItem{}}
	if a.config.Endpoint == "" && a.process != nil {
		h.processes = newProcessTracker(a.processIdentity)
		h.processes.capture()
	}
	a.active = h
	a.state = SessionState{ThreadID: tid, TaskID: request.Task.ID, RunID: request.RunAttempt.ID, State: "starting"}
	if err = a.writeStateLocked(); err != nil {
		a.active = nil
		unsubscribe()
		return fail(err)
	}
	prompt := buildPrompt(request)
	// A new-session handoff is already part of the durable Task. Never import
	// an old join receipt into that thread, including after a Worker restart.
	joinedContext := request.SessionBinding == nil || (a.config.ThreadID != "" && request.SessionBinding.ProviderSessionID == a.config.ThreadID)
	if a.handoff != "" && !a.handoffUsed && !request.Execution.Spec.Session.ForceNew && joinedContext {
		prompt = "OpenAgentX imported session handoff (context only; does not grant new authority):\n" + a.handoff + "\n\n" + prompt
		a.handoffUsed = true
	}
	turnParams := map[string]any{"threadId": tid, "input": []map[string]any{{"type": "text", "text": prompt}}, "clientUserMessageId": request.RunAttempt.ID, "model": request.Execution.Spec.Model}
	if request.Execution.Spec.Reasoning.Mode == domain.ReasoningEffort {
		turnParams["effort"] = request.Execution.Spec.Reasoning.Value
	}
	var response struct {
		Turn providerTurn `json:"turn"`
	}
	if h.processes != nil {
		go h.processes.watch(h.done)
	}
	err = a.callUnlocked(turnCtx, "turn/start", turnParams, &response)
	if err == nil && response.Turn.ID == "" {
		err = errors.New("Codex turn/start omitted turn id")
	}
	if err != nil {
		// A dropped response is an ambiguous dispatch, never a reason to resend.
		if h.processes != nil {
			_, _ = h.processes.stop()
			a.stopLocked()
		}
		close(h.done)
		a.active = nil
		a.unresolved = true
		a.state.State = "uncertain"
		_ = a.writeStateLocked()
		unsubscribe()
		cancel()
		return &finishedHandle{result: openruntime.TurnResult{Status: openruntime.TurnResultUncertain, ProviderSessionID: tid, Error: "Codex turn/start acknowledgement missing; do not retry automatically"}, err: err}, nil
	}
	h.turnID = response.Turn.ID
	a.state.TurnID = h.turnID
	a.state.State = "running"
	if err = a.writeStateLocked(); err != nil {
		h.initialError = err
	}
	go h.collect()
	return h, nil
}

// The caller holds admission via starting, but releases the state mutex during
// RPC so heartbeat Health and cancellation observation never wait on the model.
func (a *Adapter) callUnlocked(ctx context.Context, method string, params any, result any) error {
	client := a.client
	if client == nil {
		return errors.New("Codex host is unavailable")
	}
	a.mu.Unlock()
	rpcCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err := client.Call(rpcCtx, method, params, result)
	cancel()
	a.mu.Lock()
	if err != nil {
		return err
	}
	if a.client != client || a.closed {
		return errors.New("Codex host changed during request")
	}
	return nil
}
func threadActive(t threadResponse) bool {
	if t.Thread.Status.Type == "active" {
		return true
	}
	for _, turn := range t.Thread.Turns {
		if turn.Status == "inProgress" {
			return true
		}
	}
	return false
}
func buildPrompt(request openruntime.TurnRequest) string {
	var b strings.Builder
	if input := request.Execution.AgentInput; input != nil {
		fmt.Fprintf(&b, "OpenAgentX frozen Agent role (profile %d, SHA-256 %s):\n%s\n\n", input.ProfileVersion, input.InstructionsSHA256, input.InstructionsContent)
	}
	fmt.Fprintf(&b, "OpenAgentX Task ID: %s\nRun ID: %s\n\n%s", request.Task.ID, request.RunAttempt.ID, request.Task.Content)
	for _, m := range request.Messages {
		if m.Content != "" {
			b.WriteString("\n\nFollow-up:\n" + m.Content)
		}
	}
	return b.String()
}

var _ openruntime.AgentRuntimeAdapter = (*Adapter)(nil)
var _ openruntime.NetworkPolicyApplier = (*Adapter)(nil)
