package console

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	consoleclient "openagentx/internal/client/console"
	"openagentx/internal/credentialstore"
	"openagentx/internal/domain"
	fleetmodel "openagentx/internal/fleet"
)

var errLoginRequired = errors.New("CLI session is not authenticated; run openagentx console login")

type userVisibleError struct{ message string }

func (e userVisibleError) Error() string       { return e.message }
func (e userVisibleError) UserMessage() string { return e.message }

type sessionStatus struct {
	Authenticated bool
	Username      string
	ExpiresAt     time.Time
	SocketPath    string
}

type attachPreparation struct {
	ID            uint64
	Mode          string
	SelectedAgent string
	Source        string
	Agents        []domain.ConsoleAgentOption
	Session       sessionStatus
}

type preparedAttach struct {
	location fleetmodel.AttachLocation
	client   Client
	mode     string
	agents   map[string]domain.ConsoleAgentOption
	session  sessionStatus
}

type consoleApplication struct {
	ctx       context.Context
	deps      Dependencies
	socket    string
	store     CredentialStore
	workspace fleetmodel.Workspace

	mu          sync.Mutex
	nextPrepare uint64
	prepared    map[uint64]preparedAttach
	nextFollow  uint64
	follows     map[uint64]context.CancelFunc
}

func newConsoleApplication(ctx context.Context, socket string, store CredentialStore, deps Dependencies) *consoleApplication {
	return &consoleApplication{ctx: ctx, deps: deps, socket: socket, store: store,
		workspace: fleetmodel.Workspace{Runner: deps.Tmux,
			PaneLabel: func(string) string { return fleetmodel.ConsoleTerminalLabel },
			Warn:      func(err error) { fmt.Fprintf(deps.Err, "终端标签告警: %v\n", err) },
		}, prepared: make(map[uint64]preparedAttach),
		follows: make(map[uint64]context.CancelFunc)}
}

func (a *consoleApplication) session() (sessionStatus, error) {
	client, credential, session, err := a.authenticatedClient()
	_ = client
	if errors.Is(err, credentialstore.ErrNotFound) || errors.Is(err, errLoginRequired) {
		return sessionStatus{SocketPath: a.socket}, nil
	}
	if err != nil {
		return sessionStatus{SocketPath: a.socket}, err
	}
	return sessionStatus{Authenticated: true, Username: session.Principal.Username,
		ExpiresAt: credential.AbsoluteExpires, SocketPath: a.socket}, nil
}

func (a *consoleApplication) authenticatedClient() (Client, credentialstore.Credential, openapi.CLISessionResponse, error) {
	client, err := a.deps.NewClient(a.socket)
	if err != nil {
		return nil, credentialstore.Credential{}, openapi.CLISessionResponse{}, err
	}
	probe, err := client.ProbeInstallation(a.ctx)
	if err != nil {
		return nil, credentialstore.Credential{}, openapi.CLISessionResponse{}, err
	}
	credential, err := a.store.Load(a.socket, probe.InstallationID, "")
	if err != nil {
		return nil, credentialstore.Credential{}, openapi.CLISessionResponse{}, err
	}
	if !a.deps.Now().UTC().Before(credential.AbsoluteExpires.UTC()) {
		_, _ = a.store.Delete(credential)
		return nil, credentialstore.Credential{}, openapi.CLISessionResponse{}, errLoginRequired
	}
	if err := client.UseCredential(a.ctx, credential.InstallationID, credential.Token); err != nil {
		return nil, credentialstore.Credential{}, openapi.CLISessionResponse{}, err
	}
	session, err := client.Session(a.ctx)
	if err != nil {
		var apiErr *consoleclient.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 401 && apiErr.Code == openapi.ErrorCLIUnauthenticated {
			_, _ = a.store.Delete(credential)
			return nil, credentialstore.Credential{}, openapi.CLISessionResponse{}, errLoginRequired
		}
		return nil, credentialstore.Credential{}, openapi.CLISessionResponse{}, err
	}
	if err := validateCredentialSession(credential, session, a.deps.Now()); err != nil {
		return nil, credentialstore.Credential{}, openapi.CLISessionResponse{}, err
	}
	return client, credential, session, nil
}

func (a *consoleApplication) login(username, password string) (sessionStatus, error) {
	if username == "" || password == "" {
		return sessionStatus{}, fmt.Errorf("username and password are required")
	}
	client, err := a.deps.NewClient(a.socket)
	if err != nil {
		return sessionStatus{}, err
	}
	credential, err := a.store.Replace(func() (credentialstore.Credential, error) {
		issued, issueErr := client.LoginCredential(a.ctx, username, password)
		if issueErr != nil {
			return credentialstore.Credential{}, issueErr
		}
		credential := credentialstore.Credential{SocketPath: a.socket, InstallationID: issued.InstallationID,
			Username: issued.Principal.Username, TokenID: issued.Principal.TokenID, Token: issued.Token,
			AbsoluteExpires: issued.AbsoluteExpiresAt}
		session, sessionErr := client.Session(a.ctx)
		if sessionErr != nil {
			return credential, sessionErr
		}
		if err := validateCredentialSession(credential, session, a.deps.Now()); err != nil {
			return credential, err
		}
		return credential, nil
	})
	if err != nil {
		if credential.Token != "" {
			_ = client.Logout(a.ctx)
			a.clearPreparations()
		}
		return sessionStatus{}, err
	}
	a.clearPreparations()
	return sessionStatus{Authenticated: true, Username: credential.Username,
		ExpiresAt: credential.AbsoluteExpires, SocketPath: a.socket}, nil
}

func (a *consoleApplication) logout() (string, error) {
	credential, err := a.store.LoadCurrentForSocket(a.socket)
	if errors.Is(err, credentialstore.ErrNotFound) {
		a.clearPreparations()
		return "No local CLI credential is stored for this socket", nil
	}
	if err != nil {
		return "", err
	}
	warning := ""
	client, clientErr := a.deps.NewClient(a.socket)
	if clientErr != nil {
		warning = "daemon unavailable; server-side token status is unknown"
	} else {
		probe, probeErr := client.ProbeInstallation(a.ctx)
		if probeErr != nil {
			warning = "daemon unavailable; server-side token status is unknown"
		} else if probe.InstallationID != credential.InstallationID {
			warning = "installation identity changed; token was not sent to the replacement daemon"
		} else if useErr := client.UseCredential(a.ctx, credential.InstallationID, credential.Token); useErr != nil {
			warning = "credential audience validation failed; token was not sent"
		} else if logoutErr := client.Logout(a.ctx); logoutErr != nil {
			warning = "server-side token was already invalid or could not be revoked"
		}
	}
	if _, err := a.store.Delete(credential); err != nil {
		return "", fmt.Errorf("delete local Console credential: %w", err)
	}
	a.clearPreparations()
	if warning != "" {
		return "Local CLI credential deleted; " + warning, nil
	}
	return "CLI session revoked and local credential deleted", nil
}

func (a *consoleApplication) prepareAttach(mode, requestedAgent string) (attachPreparation, error) {
	location, err := a.workspace.PreflightAttach(a.ctx)
	if err != nil {
		return attachPreparation{}, userVisibleError{message: err.Error()}
	}
	client, credential, session, err := a.authenticatedClient()
	if err != nil {
		return attachPreparation{}, err
	}
	options, err := client.ListAgentOptions(a.ctx)
	if err != nil {
		return attachPreparation{}, err
	}
	byID := make(map[string]domain.ConsoleAgentOption, len(options))
	for _, option := range options {
		if err := option.Validate(); err != nil {
			return attachPreparation{}, fmt.Errorf("control plane returned an invalid Console Agent projection")
		}
		if _, duplicate := byID[option.AgentID]; duplicate {
			return attachPreparation{}, fmt.Errorf("control plane returned duplicate Console Agent %q", option.AgentID)
		}
		byID[option.AgentID] = option
	}
	selected, source := requestedAgent, "explicit"
	if selected == "" && location.BoundAgentID != "" {
		selected, source = location.BoundAgentID, "managed-window"
	}
	if selected != "" {
		if err := validateAgentSelection(selected, byID); err != nil {
			return attachPreparation{}, err
		}
	} else {
		source = "selector"
		if len(options) == 0 {
			return attachPreparation{}, fmt.Errorf("authenticated Console Agent list is empty")
		}
	}
	status := sessionStatus{Authenticated: true, Username: session.Principal.Username,
		ExpiresAt: credential.AbsoluteExpires, SocketPath: a.socket}
	a.mu.Lock()
	a.nextPrepare++
	id := a.nextPrepare
	a.prepared = map[uint64]preparedAttach{id: {
		location: location, client: client, mode: mode, agents: byID, session: status,
	}}
	a.mu.Unlock()
	return attachPreparation{ID: id, Mode: mode, SelectedAgent: selected, Source: source,
		Agents: append([]domain.ConsoleAgentOption(nil), options...), Session: status}, nil
}

func validateAgentSelection(agentID string, options map[string]domain.ConsoleAgentOption) error {
	if err := domain.ValidateIdentifier("agent_id", agentID); err != nil {
		return err
	}
	if agentID == fleetmodel.OverviewWindow {
		return fmt.Errorf("Agent ID %q conflicts with reserved overview window", agentID)
	}
	if _, ok := options[agentID]; !ok {
		return fmt.Errorf("Agent %q is not present in the authenticated Console Agent list", agentID)
	}
	return nil
}

func (a *consoleApplication) bind(preparationID uint64, agentID string, confirm bool) (preparedAttach, bool, error) {
	a.mu.Lock()
	prepared, ok := a.prepared[preparationID]
	a.mu.Unlock()
	if !ok {
		return preparedAttach{}, false, fmt.Errorf("Console Attach preparation expired")
	}
	if err := validateAgentSelection(agentID, prepared.agents); err != nil {
		return preparedAttach{}, false, err
	}
	_, err := a.workspace.BindCurrent(a.ctx, prepared.location, agentID, confirm)
	if err != nil {
		var confirmation *fleetmodel.ConfirmationRequiredError
		if errors.As(err, &confirmation) {
			return prepared, true, nil
		}
		return preparedAttach{}, false, userVisibleError{message: err.Error()}
	}
	return prepared, false, nil
}

func (a *consoleApplication) preparation(id uint64) (preparedAttach, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	prepared, ok := a.prepared[id]
	return prepared, ok
}

func (a *consoleApplication) clearPreparations() {
	a.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(a.follows))
	for _, cancel := range a.follows {
		cancels = append(cancels, cancel)
	}
	a.prepared = make(map[uint64]preparedAttach)
	a.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (a *consoleApplication) sessionCmd() tea.Cmd {
	return func() tea.Msg {
		status, err := a.session()
		return sessionResultMsg{Status: status, Err: err}
	}
}

func (a *consoleApplication) loginCmd(username, password string) tea.Cmd {
	return func() tea.Msg {
		status, err := a.login(username, password)
		return loginResultMsg{Status: status, Err: err}
	}
}

func (a *consoleApplication) logoutCmd() tea.Cmd {
	return func() tea.Msg {
		message, err := a.logout()
		return logoutResultMsg{Message: message, Err: err}
	}
}

func (a *consoleApplication) prepareAttachCmd(mode, requestedAgent string) tea.Cmd {
	return func() tea.Msg {
		prepared, err := a.prepareAttach(mode, requestedAgent)
		return prepareAttachResultMsg{Preparation: prepared, Err: err}
	}
}

func (a *consoleApplication) bindCmd(preparationID uint64, agentID string, confirm bool) tea.Cmd {
	return func() tea.Msg {
		prepared, confirmation, err := a.bind(preparationID, agentID, confirm)
		return bindResultMsg{PreparationID: preparationID, AgentID: agentID, Mode: prepared.mode,
			Session: prepared.session, ConfirmationRequired: confirmation, Err: err}
	}
}

func (a *consoleApplication) startFollowCmd(preparationID uint64, agentID string) tea.Cmd {
	return a.startFollowModeCmd(preparationID, agentID, "")
}

func (a *consoleApplication) startFollowModeCmd(preparationID uint64, agentID, requestedMode string) tea.Cmd {
	return func() tea.Msg {
		a.mu.Lock()
		prepared, ok := a.prepared[preparationID]
		if !ok {
			a.mu.Unlock()
			return followDoneMsg{Err: fmt.Errorf("Console Attach preparation expired")}
		}
		if _, authorized := prepared.agents[agentID]; !authorized {
			a.mu.Unlock()
			return followDoneMsg{Err: fmt.Errorf("Console Agent authorization expired")}
		}
		mode := requestedMode
		if mode == "" {
			mode = prepared.mode
		}
		if mode != consoleapi.ModeNormal && mode != consoleapi.ModeDiagnostic {
			a.mu.Unlock()
			return followDoneMsg{Mode: mode, Err: fmt.Errorf("unsupported Console Follow mode")}
		}
		if len(a.follows) != 0 {
			a.mu.Unlock()
			return followDoneMsg{Mode: mode, Err: fmt.Errorf("another Console Follow is still active")}
		}
		a.nextFollow++
		followID := a.nextFollow
		followContext, cancel := context.WithCancel(a.ctx)
		a.follows[followID] = cancel
		a.mu.Unlock()

		updates := make(chan tea.Msg, 64)
		send := func(msg tea.Msg) error {
			select {
			case updates <- msg:
				return nil
			case <-followContext.Done():
				return followContext.Err()
			}
		}
		go func() {
			defer close(updates)
			sendAndWait := func(message func(chan<- error) tea.Msg) error {
				ack := make(chan error, 1)
				if err := send(message(ack)); err != nil {
					return err
				}
				select {
				case err := <-ack:
					return err
				case <-followContext.Done():
					return followContext.Err()
				}
			}
			err := prepared.client.Follow(followContext, agentID, mode,
				func(snapshot consoleapi.AttachResponse) error {
					return sendAndWait(func(ack chan<- error) tea.Msg {
						return followSnapshotMsg{FollowID: followID, Snapshot: snapshot, Ack: ack}
					})
				},
				func(event openapi.JournalEventReadModel) error {
					return sendAndWait(func(ack chan<- error) tea.Msg {
						return followEventMsg{FollowID: followID, Event: event, Ack: ack}
					})
				},
				func(state consoleclient.FollowState) error {
					return send(followConnectionMsg{FollowID: followID, State: state})
				})
			a.mu.Lock()
			delete(a.follows, followID)
			a.mu.Unlock()
			select {
			case updates <- followDoneMsg{FollowID: followID, Mode: mode, Err: err}:
			case <-a.ctx.Done():
			}
		}()
		return followStartedMsg{FollowID: followID, Mode: mode, Updates: updates}
	}
}

func (a *consoleApplication) cancelFollowCmd(followID uint64) tea.Cmd {
	return func() tea.Msg {
		a.mu.Lock()
		cancel, ok := a.follows[followID]
		a.mu.Unlock()
		if ok {
			cancel()
		}
		return followCancelResultMsg{FollowID: followID, Found: ok}
	}
}

func (a *consoleApplication) taskOptionsCmd(preparationID uint64, agentID string) tea.Cmd {
	return func() tea.Msg {
		prepared, ok := a.preparation(preparationID)
		if !ok {
			return taskOptionsResultMsg{Err: fmt.Errorf("Console Attach preparation expired")}
		}
		if _, ok := prepared.agents[agentID]; !ok {
			return taskOptionsResultMsg{Err: fmt.Errorf("Console Agent authorization expired")}
		}
		options, err := prepared.client.ListTaskOptions(a.ctx, agentID)
		return taskOptionsResultMsg{Options: options, Err: err}
	}
}

func (a *consoleApplication) taskSnapshotCmd(preparationID uint64, agentID, taskID string, purpose taskSnapshotPurpose) tea.Cmd {
	return func() tea.Msg {
		prepared, ok := a.preparation(preparationID)
		if !ok {
			return taskSnapshotResultMsg{TaskID: taskID, Purpose: purpose,
				Err: fmt.Errorf("Console Attach preparation expired")}
		}
		if _, ok := prepared.agents[agentID]; !ok {
			return taskSnapshotResultMsg{TaskID: taskID, Purpose: purpose,
				Err: fmt.Errorf("Console Agent authorization expired")}
		}
		snapshot, err := prepared.client.TaskSnapshot(a.ctx, agentID, taskID)
		return taskSnapshotResultMsg{TaskID: taskID, Snapshot: snapshot, Purpose: purpose, Err: err}
	}
}

func (a *consoleApplication) controlCmd(preparationID uint64, request controlRequest) tea.Cmd {
	return func() tea.Msg {
		prepared, ok := a.preparation(preparationID)
		if !ok {
			return controlResultMsg{Kind: request.Kind, Request: request,
				Err: fmt.Errorf("Console Attach preparation expired")}
		}
		option, ok := prepared.agents[request.AgentID]
		if !ok {
			return controlResultMsg{Kind: request.Kind, Request: request,
				Err: fmt.Errorf("Console Agent authorization expired")}
		}
		meta := openapi.CommandMeta{IdempotencyKey: consoleclient.IdempotencyKey("console-" + string(request.Kind)), ExpectedVersion: request.ExpectedVersion}
		var outcome controlOutcome
		var err error
		switch request.Kind {
		case controlDispatch, controlContinue:
			var response openapi.CreateTaskResponse
			response, err = prepared.client.Dispatch(a.ctx, openapi.CreateTaskRequest{Meta: meta,
				TargetAgentID: request.AgentID, OrganizationID: option.OrganizationID,
				DispatchMode: domain.DispatchModeDirect, Intent: request.Intent, Content: request.Content, ParentTaskID: request.ParentTaskID, ContinueContext: request.Kind == controlContinue})
			if err == nil {
				outcome, err = dispatchOutcome(response)
			}
		case controlAcceptResult, controlRejectResult:
			reviewer, ok := prepared.client.(interface {
				ReviewTaskResult(context.Context, string, openapi.ReviewTaskRequest) (domain.TaskReview, error)
			})
			if !ok {
				err = fmt.Errorf("result review is unavailable in this Console client")
				break
			}
			decision := "accepted"
			if request.Kind == controlRejectResult {
				decision = "rejected"
			}
			var review domain.TaskReview
			review, err = reviewer.ReviewTaskResult(a.ctx, request.TargetID, openapi.ReviewTaskRequest{Meta: meta, RunID: request.RunID, RunVersion: request.RunVersion, Decision: decision, Note: request.Content})
			if err == nil {
				if review.TaskID != request.TargetID || review.RunID != request.RunID || review.RunVersion != request.RunVersion || review.Decision != decision || review.TaskVersion <= request.ExpectedVersion || review.Sequence <= 0 {
					err = fmt.Errorf("control plane returned an invalid result review receipt")
				} else {
					outcome = controlOutcome{Review: &review, Sequence: review.Sequence}
				}
			}
		case controlSteer:
			var response openapi.CreateMessageResponse
			response, err = prepared.client.Steer(a.ctx, request.TargetID,
				openapi.CreateMessageRequest{Meta: meta, Content: request.Content})
			if err == nil {
				outcome, err = steerOutcome(response)
			}
		case controlCancel:
			var response openapi.CancelTaskResponse
			response, err = prepared.client.Cancel(a.ctx, request.TargetID, openapi.CancelTaskRequest{Meta: meta})
			if err == nil {
				outcome, err = cancelOutcome(response)
			}
		case controlApprove, controlReject:
			decision := domain.ApprovalDecisionApprove
			if request.Kind == controlReject {
				decision = domain.ApprovalDecisionReject
			}
			var response openapi.DecideApprovalResponse
			response, err = prepared.client.DecideApproval(a.ctx, request.TargetID,
				openapi.DecideApprovalRequest{Meta: meta, Decision: decision})
			if err == nil {
				outcome, err = approvalOutcome(response)
			}
		default:
			err = fmt.Errorf("unsupported Console control command")
		}
		return controlResultMsg{Kind: request.Kind, Request: request, Outcome: outcome, Err: err}
	}
}

func dispatchOutcome(response openapi.CreateTaskResponse) (controlOutcome, error) {
	if domain.ValidateOpaqueID("task_id", response.TaskID) != nil || response.TaskVersion <= 0 ||
		!response.TaskStatus.Valid() || response.Sequence <= 0 {
		return controlOutcome{}, fmt.Errorf("control plane returned an invalid dispatch outcome")
	}
	return controlOutcome{TaskID: response.TaskID, TaskVersion: response.TaskVersion,
		TaskStatus: response.TaskStatus, Sequence: response.Sequence}, nil
}

func steerOutcome(response openapi.CreateMessageResponse) (controlOutcome, error) {
	if domain.ValidateOpaqueID("message_id", response.MessageID) != nil || response.MessageVersion <= 0 ||
		domain.ValidateOpaqueID("task_id", response.TaskID) != nil || response.TaskVersion <= 0 ||
		!response.TaskStatus.Valid() || response.Sequence <= 0 {
		return controlOutcome{}, fmt.Errorf("control plane returned an invalid steer outcome")
	}
	return controlOutcome{TaskID: response.TaskID, TaskVersion: response.TaskVersion,
		TaskStatus: response.TaskStatus, MessageID: response.MessageID,
		MessageVersion: response.MessageVersion, Sequence: response.Sequence}, nil
}

func cancelOutcome(response openapi.CancelTaskResponse) (controlOutcome, error) {
	if domain.ValidateOpaqueID("task_id", response.Task.ID) != nil || response.Task.Version <= 0 ||
		!response.Task.Status.Valid() || response.Sequence < 0 {
		return controlOutcome{}, fmt.Errorf("control plane returned an invalid cancel outcome")
	}
	return controlOutcome{TaskID: response.Task.ID, TaskVersion: response.Task.Version,
		TaskStatus: response.Task.Status, Sequence: response.Sequence}, nil
}

func approvalOutcome(response openapi.DecideApprovalResponse) (controlOutcome, error) {
	decision := response.Decision
	if domain.ValidateOpaqueID("approval_request_id", decision.ApprovalRequestID) != nil ||
		domain.ValidateOpaqueID("approval_decision_id", decision.ID) != nil || !decision.Decision.Valid() ||
		!decision.State.Valid() || response.Sequence < 0 {
		return controlOutcome{}, fmt.Errorf("control plane returned an invalid approval outcome")
	}
	return controlOutcome{ApprovalID: decision.ApprovalRequestID, DecisionID: decision.ID,
		Decision: decision.Decision, DecisionState: decision.State, Sequence: response.Sequence}, nil
}

type tuiActions interface {
	sessionCmd() tea.Cmd
	loginCmd(string, string) tea.Cmd
	logoutCmd() tea.Cmd
	prepareAttachCmd(string, string) tea.Cmd
	bindCmd(uint64, string, bool) tea.Cmd
	startFollowCmd(uint64, string) tea.Cmd
	startFollowModeCmd(uint64, string, string) tea.Cmd
	cancelFollowCmd(uint64) tea.Cmd
	taskOptionsCmd(uint64, string) tea.Cmd
	taskSnapshotCmd(uint64, string, string, taskSnapshotPurpose) tea.Cmd
	controlCmd(uint64, controlRequest) tea.Cmd
}

func (a *consoleApplication) statusReadinessCmd(preparationID uint64, agentID string) tea.Cmd {
	return func() tea.Msg {
		prepared, ok := a.preparation(preparationID)
		if !ok {
			return statusReadinessMsg{Err: fmt.Errorf("Console Attach preparation expired")}
		}
		snapshot, err := prepared.client.Attach(a.ctx, agentID, consoleapi.ModeNormal)
		return statusReadinessMsg{Snapshot: snapshot, Err: err}
	}
}
