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
}

func newConsoleApplication(ctx context.Context, socket string, store CredentialStore, deps Dependencies) *consoleApplication {
	return &consoleApplication{ctx: ctx, deps: deps, socket: socket, store: store,
		workspace: fleetmodel.Workspace{Runner: deps.Tmux}, prepared: make(map[uint64]preparedAttach)}
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
	defer a.mu.Unlock()
	a.prepared = make(map[uint64]preparedAttach)
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
	return func() tea.Msg {
		prepared, ok := a.preparation(preparationID)
		if !ok {
			return followDoneMsg{Err: fmt.Errorf("Console Attach preparation expired")}
		}
		updates := make(chan tea.Msg, 64)
		send := func(msg tea.Msg) error {
			select {
			case updates <- msg:
				return nil
			case <-a.ctx.Done():
				return a.ctx.Err()
			}
		}
		go func() {
			err := prepared.client.Follow(a.ctx, agentID, prepared.mode,
				func(snapshot consoleapi.AttachResponse) error { return send(followSnapshotMsg{Snapshot: snapshot}) },
				func(event openapi.JournalEventReadModel) error { return send(followEventMsg{Event: event}) },
				func(state consoleclient.FollowState) error { return send(followConnectionMsg{State: state}) })
			_ = send(followDoneMsg{Err: err})
			close(updates)
		}()
		return followStartedMsg{Updates: updates}
	}
}

func (a *consoleApplication) controlCmd(preparationID uint64, request controlRequest) tea.Cmd {
	return func() tea.Msg {
		prepared, ok := a.preparation(preparationID)
		if !ok {
			return controlResultMsg{Kind: request.Kind, Err: fmt.Errorf("Console Attach preparation expired")}
		}
		option, ok := prepared.agents[request.AgentID]
		if !ok {
			return controlResultMsg{Kind: request.Kind, Err: fmt.Errorf("Console Agent authorization expired")}
		}
		meta := openapi.CommandMeta{IdempotencyKey: consoleclient.IdempotencyKey("console-" + string(request.Kind)), ExpectedVersion: request.ExpectedVersion}
		var err error
		switch request.Kind {
		case controlDispatch:
			_, err = prepared.client.Dispatch(a.ctx, openapi.CreateTaskRequest{Meta: meta,
				TargetAgentID: request.AgentID, OrganizationID: option.OrganizationID,
				DispatchMode: domain.DispatchModeDirect, Content: request.Content})
		case controlSteer:
			_, err = prepared.client.Steer(a.ctx, request.TargetID,
				openapi.CreateMessageRequest{Meta: meta, Content: request.Content})
		case controlCancel:
			_, err = prepared.client.Cancel(a.ctx, request.TargetID, openapi.CancelTaskRequest{Meta: meta})
		case controlApprove, controlReject:
			decision := domain.ApprovalDecisionApprove
			if request.Kind == controlReject {
				decision = domain.ApprovalDecisionReject
			}
			_, err = prepared.client.DecideApproval(a.ctx, request.TargetID,
				openapi.DecideApprovalRequest{Meta: meta, Decision: decision})
		default:
			err = fmt.Errorf("unsupported Console control command")
		}
		return controlResultMsg{Kind: request.Kind, Err: err}
	}
}

type tuiActions interface {
	sessionCmd() tea.Cmd
	loginCmd(string, string) tea.Cmd
	logoutCmd() tea.Cmd
	prepareAttachCmd(string, string) tea.Cmd
	bindCmd(uint64, string, bool) tea.Cmd
	startFollowCmd(uint64, string) tea.Cmd
	controlCmd(uint64, controlRequest) tea.Cmd
}
