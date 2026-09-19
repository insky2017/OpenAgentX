package console

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	consoleclient "openagentx/internal/client/console"
	"openagentx/internal/consolemodel"
	"openagentx/internal/domain"
)

type fakeTUIActions struct {
	sessionCalls int
	loginCalls   int
	logoutCalls  int
	prepareCalls []initialAttach
	bindCalls    []struct {
		id      uint64
		agentID string
		confirm bool
	}
	followCalls int
	controls    []controlRequest
}

func (a *fakeTUIActions) sessionCmd() tea.Cmd {
	a.sessionCalls++
	return func() tea.Msg { return sessionResultMsg{} }
}
func (a *fakeTUIActions) loginCmd(_, _ string) tea.Cmd {
	a.loginCalls++
	return func() tea.Msg { return loginResultMsg{} }
}
func (a *fakeTUIActions) logoutCmd() tea.Cmd {
	a.logoutCalls++
	return func() tea.Msg { return logoutResultMsg{} }
}
func (a *fakeTUIActions) prepareAttachCmd(mode, agentID string) tea.Cmd {
	a.prepareCalls = append(a.prepareCalls, initialAttach{AgentID: agentID, Mode: mode})
	return func() tea.Msg { return prepareAttachResultMsg{} }
}
func (a *fakeTUIActions) bindCmd(id uint64, agentID string, confirm bool) tea.Cmd {
	a.bindCalls = append(a.bindCalls, struct {
		id      uint64
		agentID string
		confirm bool
	}{id: id, agentID: agentID, confirm: confirm})
	return func() tea.Msg { return bindResultMsg{} }
}
func (a *fakeTUIActions) startFollowCmd(_ uint64, _ string) tea.Cmd {
	a.followCalls++
	return func() tea.Msg { return followStartedMsg{} }
}
func (a *fakeTUIActions) controlCmd(_ uint64, request controlRequest) tea.Cmd {
	a.controls = append(a.controls, request)
	return func() tea.Msg { return controlResultMsg{Kind: request.Kind, Outcome: testControlOutcome(request.Kind)} }
}

func testControlOutcome(kind controlKind) controlOutcome {
	switch kind {
	case controlDispatch, controlCancel:
		return controlOutcome{TaskID: "task-1", TaskVersion: 11, TaskStatus: domain.TaskStatusCancelRequested, Sequence: 41}
	case controlSteer:
		return controlOutcome{TaskID: "task-1", TaskVersion: 8, TaskStatus: domain.TaskStatusRunning,
			MessageID: "message-1", MessageVersion: 1, Sequence: 42}
	default:
		return controlOutcome{ApprovalID: "approval-1", DecisionID: "decision-1",
			Decision: domain.ApprovalDecisionApprove, DecisionState: domain.ApprovalDecisionPersisted, Sequence: 43}
	}
}

func fixedNow() time.Time { return time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC) }

func taskProjection(taskID string, version int64, status domain.TaskStatus) *openapi.ConsoleTaskReadModel {
	outcome := "pending"
	if status == domain.TaskStatusSucceeded || status == domain.TaskStatusFailed ||
		status == domain.TaskStatusCanceled || status == domain.TaskStatusUncertain {
		outcome = "not_recorded"
	}
	return &openapi.ConsoleTaskReadModel{TaskID: taskID, Version: version, AgentID: "quote", Status: status,
		Content: "safe task", OutcomeState: outcome, CreatedAt: fixedNow().Format(time.RFC3339Nano),
		UpdatedAt: fixedNow().Add(time.Duration(version) * time.Second).Format(time.RFC3339Nano)}
}

func attachedModel(t *testing.T, actions *fakeTUIActions) tuiModel {
	t.Helper()
	m := newTUIModel(actions, fixedNow, nil)
	m.screen = screenAttach
	m.selectedAgent = "quote"
	m.mode = consoleapi.ModeNormal
	m.preparationID = 7
	m.session = sessionStatus{Authenticated: true, Username: "owner", ExpiresAt: fixedNow().Add(time.Hour), SocketPath: "/tmp/oax.sock"}
	m.connection = consoleclient.ConnectionConnected
	snapshot := consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
		WorkerInstanceID: "worker-current", Generation: 48, WorkerStatus: domain.WorkerStatusOnline,
		LastHeartbeatAt: fixedNow().Add(-time.Second), LeaseUntil: fixedNow().Add(time.Minute), SnapshotSequence: 1204}
	var err error
	m.reducer, err = consolemodel.New(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	m.input.Focus()
	m.resize(100, 30)
	return m
}

func updateModel(t *testing.T, model tuiModel, message tea.Msg) (tuiModel, tea.Cmd) {
	t.Helper()
	updated, cmd := model.Update(message)
	result, ok := updated.(tuiModel)
	if !ok {
		t.Fatalf("Update returned %T", updated)
	}
	return result, cmd
}

func key(value string) tea.KeyMsg {
	switch value {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
	}
}

func TestMenuContainsAndExecutesEveryFrozenItem(t *testing.T) {
	actions := &fakeTUIActions{}
	m := newTUIModel(actions, fixedNow, nil)
	want := []string{"Normal Console Attach", "Diagnostic Attach", "Login", "Logout", foregroundUnavailable, "Exit"}
	if got := m.menuItems(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("menu=%v want=%v", got, want)
	}

	m.menuCursor = 0
	m, _ = updateModel(t, m, key("enter"))
	if len(actions.prepareCalls) != 1 || actions.prepareCalls[0].Mode != consoleapi.ModeNormal {
		t.Fatalf("normal Attach calls=%+v", actions.prepareCalls)
	}
	m.screen = screenMenu
	m.menuCursor = 1
	m, _ = updateModel(t, m, key("enter"))
	if len(actions.prepareCalls) != 2 || actions.prepareCalls[1].Mode != consoleapi.ModeDiagnostic {
		t.Fatalf("diagnostic Attach calls=%+v", actions.prepareCalls)
	}
	m.screen = screenMenu
	m.menuCursor = 2
	m, _ = updateModel(t, m, key("enter"))
	if m.screen != screenLogin || !m.username.Focused() || m.password.Focused() {
		t.Fatalf("login screen state=%+v", m)
	}
	m.screen = screenMenu
	m.menuCursor = 3
	m, _ = updateModel(t, m, key("enter"))
	if actions.logoutCalls != 1 {
		t.Fatalf("logout calls=%d", actions.logoutCalls)
	}
	m.screen = screenMenu
	m.menuCursor = 4
	m, _ = updateModel(t, m, key("enter"))
	if m.notice != foregroundUnavailable {
		t.Fatalf("foreground notice=%q", m.notice)
	}
	m.menuCursor = 5
	_, cmd := updateModel(t, m, key("enter"))
	if cmd == nil {
		t.Fatal("Exit did not return tea.Quit")
	}
}

func TestLoginViewMasksAndClearsPassword(t *testing.T) {
	actions := &fakeTUIActions{}
	m := newTUIModel(actions, fixedNow, nil)
	m.screen = screenLogin
	m.loginFocus = 1
	m.focusLoginField()
	m.username.SetValue("owner")
	password := "do-not-render-password"
	m.password.SetValue(password)
	if view := m.View(); strings.Contains(view, password) || !strings.Contains(view, "********") {
		t.Fatalf("password masking failed: %q", view)
	}
	m, _ = updateModel(t, m, loginResultMsg{Status: sessionStatus{Authenticated: true, Username: "owner"}})
	if m.password.Value() != "" || m.screen != screenMenu {
		t.Fatalf("password was not cleared after login: screen=%v value=%q", m.screen, m.password.Value())
	}
}

func TestSelectorSupportsMoreThanOnePageAndBindingConfirmation(t *testing.T) {
	actions := &fakeTUIActions{}
	m := newTUIModel(actions, fixedNow, nil)
	agents := make([]domain.ConsoleAgentOption, 125)
	for index := range agents {
		agents[index] = domain.ConsoleAgentOption{AgentID: fmt.Sprintf("agent-%03d", index), OrganizationID: "org-main",
			DisplayName: fmt.Sprintf("Agent %03d", index), WorkerStatus: domain.WorkerStatusOffline}
	}
	m, _ = updateModel(t, m, prepareAttachResultMsg{Preparation: attachPreparation{ID: 12, Mode: consoleapi.ModeNormal, Agents: agents}})
	if m.screen != screenSelector || len(m.agents.Items()) != 125 {
		t.Fatalf("selector screen=%v items=%d", m.screen, len(m.agents.Items()))
	}
	m, _ = updateModel(t, m, key("enter"))
	if len(actions.bindCalls) != 1 || actions.bindCalls[0].agentID != "agent-000" || actions.bindCalls[0].confirm {
		t.Fatalf("initial binding calls=%+v", actions.bindCalls)
	}
	m, _ = updateModel(t, m, bindResultMsg{PreparationID: 12, AgentID: "agent-000", ConfirmationRequired: true})
	if m.overlay != overlayConfirmation {
		t.Fatalf("confirmation overlay=%v", m.overlay)
	}
	m, _ = updateModel(t, m, key("n"))
	if len(actions.bindCalls) != 1 || m.overlay != overlayNone || m.screen != screenSelector {
		t.Fatalf("cancel mutated binding calls=%+v screen=%v", actions.bindCalls, m.screen)
	}
	m, _ = updateModel(t, m, bindResultMsg{PreparationID: 12, AgentID: "agent-000", ConfirmationRequired: true})
	m, _ = updateModel(t, m, key("y"))
	if len(actions.bindCalls) != 2 || !actions.bindCalls[1].confirm {
		t.Fatalf("confirmed binding calls=%+v", actions.bindCalls)
	}
}

func TestAttachAsyncMessagesPreserveInputDraftCursorAndFocus(t *testing.T) {
	actions := &fakeTUIActions{}
	m := attachedModel(t, actions)
	m.input.SetValue("draft command")
	m.input.SetCursor(5)
	wantCursor := m.input.LineInfo().CharOffset

	messages := []tea.Msg{
		tea.WindowSizeMsg{Width: 42, Height: 12},
		tickMsg{Now: fixedNow().Add(time.Second)},
		followConnectionMsg{State: consoleclient.FollowState{State: consoleclient.ConnectionReconnecting, Cursor: 1204}},
		followEventMsg{Event: oldWorkerEvent(1205)},
	}
	for _, message := range messages {
		m, _ = updateModel(t, m, message)
		if m.input.Value() != "draft command" || m.input.LineInfo().CharOffset != wantCursor || !m.input.Focused() {
			t.Fatalf("message %T changed input value=%q cursor=%d focused=%v", message, m.input.Value(), m.input.LineInfo().CharOffset, m.input.Focused())
		}
	}
	m.overlay = overlayStatus
	m, _ = updateModel(t, m, key("esc"))
	if m.input.Value() != "draft command" || m.input.LineInfo().CharOffset != wantCursor || !m.input.Focused() {
		t.Fatalf("overlay changed input value=%q cursor=%d focused=%v", m.input.Value(), m.input.LineInfo().CharOffset, m.input.Focused())
	}
	if m.reducer.Snapshot().Generation != 48 || m.timeline.Len() != 0 {
		t.Fatalf("old generation changed state or Timeline: state=%+v timeline=%v", m.reducer.Snapshot(), m.timeline.entries)
	}
}

func TestCompactAttachFitsActualWindowAndKeepsReducerResponsive(t *testing.T) {
	actions := &fakeTUIActions{}
	m := attachedModel(t, actions)
	m.reducer = nil
	m.follow = make(chan tea.Msg)
	m.input.SetValue("draft command")
	m.input.SetCursor(5)
	m.resize(80, 5)

	snapshotAck := make(chan error, 1)
	snapshot := consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
		WorkerInstanceID: "worker-current", Generation: 48, WorkerStatus: domain.WorkerStatusOnline,
		LastHeartbeatAt: fixedNow().Add(-time.Second), LeaseUntil: fixedNow().Add(time.Minute), SnapshotSequence: 1204}
	m, _ = updateModel(t, m, followSnapshotMsg{Snapshot: snapshot, Ack: snapshotAck})
	if err := <-snapshotAck; err != nil {
		t.Fatalf("compact snapshot ack error=%v", err)
	}
	eventAck := make(chan error, 1)
	m, _ = updateModel(t, m, followEventMsg{Event: openapi.JournalEventReadModel{Sequence: 1205,
		ID: "event-task", AggregateType: "task", AggregateID: "task-1", EventType: "task.updated",
		Task: taskProjection("task-1", 1, domain.TaskStatusQueued)}, Ack: eventAck})
	if err := <-eventAck; err != nil {
		t.Fatalf("compact event ack error=%v", err)
	}
	cursor := int64(-1)
	if m.reducer != nil {
		cursor = m.reducer.Cursor()
	}
	if cursor != 1205 || m.input.Value() != "draft command" ||
		m.input.LineInfo().CharOffset != 5 || !m.input.Focused() {
		t.Fatalf("compact state cursor=%d draft=%q input_cursor=%d focused=%v",
			cursor, m.input.Value(), m.input.LineInfo().CharOffset, m.input.Focused())
	}
	assertTerminalViewFits(t, m.View(), 80, 5)

	for _, size := range []struct{ width, height int }{{20, 3}, {8, 1}, {1, 1}} {
		m.resize(size.width, size.height)
		assertTerminalViewFits(t, m.View(), size.width, size.height)
		for _, overlay := range []overlayKind{overlayStatus, overlayHelp, overlayDiagnostic, overlayConfirmation, overlayError} {
			m.overlay = overlay
			assertTerminalViewFits(t, m.View(), size.width, size.height)
		}
		m.overlay = overlayNone
		if m.input.Value() != "draft command" || !m.input.Focused() {
			t.Fatalf("resize %dx%d changed input draft=%q focused=%v", size.width, size.height, m.input.Value(), m.input.Focused())
		}
	}
}

func TestTaskProjectionConflictIsRejectedBeforeFollowAck(t *testing.T) {
	actions := &fakeTUIActions{}
	m := attachedModel(t, actions)
	validAck := make(chan error, 1)
	m, _ = updateModel(t, m, followEventMsg{Event: openapi.JournalEventReadModel{Sequence: 1205,
		ID: "event-task-valid", AggregateType: "task", AggregateID: "task-1", EventType: "task.updated",
		Task: taskProjection("task-1", 1, domain.TaskStatusQueued)}, Ack: validAck})
	if err := <-validAck; err != nil {
		t.Fatalf("valid Task event ack=%v", err)
	}
	conflictAck := make(chan error, 1)
	m, _ = updateModel(t, m, followEventMsg{Event: openapi.JournalEventReadModel{Sequence: 1206,
		ID: "event-task-conflict", AggregateType: "task", AggregateID: "task-1", EventType: "task.updated",
		Task: taskProjection("task-1", 1, domain.TaskStatusRunning)}, Ack: conflictAck})
	if err := <-conflictAck; err == nil || m.reducer.Cursor() != 1205 ||
		m.connection != consoleclient.ConnectionDisconnected || m.reducer.State().Connection != consolemodel.ConnectionDisconnected {
		t.Fatalf("conflicting Task event ack=%v cursor=%d connection=%s reducer=%s",
			err, m.reducer.Cursor(), m.connection, m.reducer.State().Connection)
	}
}

func TestEveryConsoleScreenFitsCompactWindow(t *testing.T) {
	actions := &fakeTUIActions{}
	menu := newTUIModel(actions, fixedNow, nil)
	login := newTUIModel(actions, fixedNow, nil)
	login.screen = screenLogin
	loading := newTUIModel(actions, fixedNow, nil)
	loading.screen = screenLoading
	selector := newTUIModel(actions, fixedNow, nil)
	selector, _ = updateModel(t, selector, prepareAttachResultMsg{Preparation: attachPreparation{ID: 12,
		Mode: consoleapi.ModeNormal, Agents: []domain.ConsoleAgentOption{{AgentID: "quote", DisplayName: "Quote"}}}})
	attach := attachedModel(t, actions)

	for name, initial := range map[string]tuiModel{
		"menu": menu, "login": login, "loading": loading, "selector": selector, "attach": attach,
	} {
		t.Run(name, func(t *testing.T) {
			model := initial
			for _, size := range []struct{ width, height int }{{80, 5}, {20, 3}, {8, 1}, {1, 1}} {
				model.resize(size.width, size.height)
				assertTerminalViewFits(t, model.View(), size.width, size.height)
			}
		})
	}
}

func assertTerminalViewFits(t *testing.T, view string, width, height int) {
	t.Helper()
	plain := ansi.Strip(view)
	lines := strings.Split(plain, "\n")
	if len(lines) > height {
		t.Fatalf("rendered lines=%d exceed height=%d: %q", len(lines), height, plain)
	}
	for index, line := range strings.Split(view, "\n") {
		if lineWidth := ansi.StringWidth(line); lineWidth > width {
			t.Fatalf("rendered line %d width=%d exceeds width=%d: %q", index, lineWidth, width, plain)
		}
	}
}

func oldWorkerEvent(sequence int64) openapi.JournalEventReadModel {
	return openapi.JournalEventReadModel{Sequence: sequence, ID: "event-old", AggregateType: "worker_instance",
		AggregateID: "worker-old", EventType: "worker.heartbeat", Worker: &openapi.WorkerReadModel{
			WorkerInstanceID: "worker-old", AgentID: "quote", Generation: 42, Status: domain.WorkerStatusOffline}}
}

func TestControlCommandsAreExactlyOnceCASAndDisabledWhenDisconnected(t *testing.T) {
	actions := &fakeTUIActions{}
	m := attachedModel(t, actions)
	tests := []struct {
		line    string
		kind    controlKind
		target  string
		version int64
	}{
		{"/dispatch investigate latency", controlDispatch, "", 0},
		{"/steer task-1 7 narrow scope", controlSteer, "task-1", 7},
		{"/cancel task-1 8", controlCancel, "task-1", 8},
		{"/approve approval-1 9", controlApprove, "approval-1", 9},
		{"/reject approval-2 10", controlReject, "approval-2", 10},
	}
	for _, testCase := range tests {
		m.pending = false
		before := len(actions.controls)
		var cmd tea.Cmd
		m, cmd = executeLine(t, m, testCase.line)
		if cmd == nil || len(actions.controls) != before+1 {
			t.Fatalf("%s calls=%d cmd=%v", testCase.line, len(actions.controls)-before, cmd)
		}
		request := actions.controls[len(actions.controls)-1]
		if request.Kind != testCase.kind || request.AgentID != "quote" || request.TargetID != testCase.target || request.ExpectedVersion != testCase.version {
			t.Fatalf("%s request=%+v", testCase.line, request)
		}
		m, _ = updateModel(t, m, controlResultMsg{Kind: testCase.kind, Outcome: testControlOutcome(testCase.kind)})
	}
	state := m.reducer.State()
	if state.FocusedTask == nil || state.FocusedTask.TaskID != "task-1" ||
		state.FocusedTask.Version != 11 || state.FocusSource != consolemodel.FocusDispatch {
		t.Fatalf("dispatch outcome did not become reducer focus: %+v source=%q", state.FocusedTask, state.FocusSource)
	}
	m.connection = consoleclient.ConnectionDisconnected
	before := len(actions.controls)
	m, cmd := executeLine(t, m, "/cancel task-1 11")
	if cmd != nil || len(actions.controls) != before || !strings.Contains(m.timeline.String(), "disabled") {
		t.Fatalf("disconnected command cmd=%v calls=%d timeline=%q", cmd, len(actions.controls)-before, m.timeline.String())
	}
	if _, cmd = executeLine(t, m, "/quit"); cmd == nil {
		t.Fatal("quit did not detach TUI")
	}
}

func executeLine(t *testing.T, model tuiModel, line string) (tuiModel, tea.Cmd) {
	t.Helper()
	model.input.SetValue(line)
	updated, cmd := model.executeInput(line)
	result, ok := updated.(tuiModel)
	if !ok {
		t.Fatalf("executeInput returned %T", updated)
	}
	return result, cmd
}

func TestConsumedOverlayCommandsClearDraftButRejectedCommandsRemainEditable(t *testing.T) {
	for _, line := range []string{"/status", "/help", "/diagnostic"} {
		m := attachedModel(t, &fakeTUIActions{})
		m, _ = executeLine(t, m, line)
		if m.overlay == overlayNone || m.input.Value() != "" {
			t.Fatalf("%s overlay=%v draft=%q", line, m.overlay, m.input.Value())
		}
		m, _ = updateModel(t, m, key("enter"))
		if m.overlay != overlayNone {
			t.Fatalf("%s repeated after closing overlay", line)
		}
	}

	m := attachedModel(t, &fakeTUIActions{})
	m.connection = consoleclient.ConnectionDisconnected
	m, _ = executeLine(t, m, "/cancel task-1 7")
	if m.input.Value() != "/cancel task-1 7" {
		t.Fatalf("disconnected command draft=%q", m.input.Value())
	}
	m.connection = consoleclient.ConnectionConnected
	m, _ = executeLine(t, m, "/cancel task-1 invalid")
	if m.input.Value() != "/cancel task-1 invalid" {
		t.Fatalf("invalid command draft=%q", m.input.Value())
	}
}

func TestSessionExpiryDisconnectsAndDisablesWritesWithoutChangingDraft(t *testing.T) {
	actions := &fakeTUIActions{}
	m := attachedModel(t, actions)
	m.input.SetValue("/dispatch unfinished")
	m.input.SetCursor(8)
	wantCursor := m.input.LineInfo().CharOffset
	m, _ = updateModel(t, m, tickMsg{Now: m.session.ExpiresAt})
	if m.session.Authenticated || m.connection != consoleclient.ConnectionDisconnected ||
		m.input.Value() != "/dispatch unfinished" || m.input.LineInfo().CharOffset != wantCursor ||
		!strings.Contains(m.timeline.String(), "console login") {
		t.Fatalf("expired session auth=%v connection=%s draft=%q cursor=%d timeline=%q",
			m.session.Authenticated, m.connection, m.input.Value(), m.input.LineInfo().CharOffset, m.timeline.String())
	}
	m, _ = updateModel(t, m, followConnectionMsg{State: consoleclient.FollowState{State: consoleclient.ConnectionConnected}})
	if m.connection != consoleclient.ConnectionDisconnected || m.input.Value() != "/dispatch unfinished" {
		t.Fatalf("expired session was restored by connection message: connection=%s draft=%q", m.connection, m.input.Value())
	}
	before := len(actions.controls)
	m, cmd := executeLine(t, m, "/dispatch retry after login")
	if cmd != nil || len(actions.controls) != before || m.input.Value() != "/dispatch retry after login" {
		t.Fatalf("expired write cmd=%v calls=%d draft=%q", cmd, len(actions.controls)-before, m.input.Value())
	}
}

func TestTimelineRendersDiagnosticOnlyInDiagnosticMode(t *testing.T) {
	event := openapi.JournalEventReadModel{Sequence: 12, EventType: "runtime.turn.output",
		Output: &openapi.SafeOutputReadModel{Text: "safe", Diagnostic: "stderr=[REDACTED]"}}
	normal := eventSummary(event, consoleapi.ModeNormal)
	diagnostic := eventSummary(event, consoleapi.ModeDiagnostic)
	if strings.Contains(normal, "stderr") || !strings.Contains(diagnostic, "stderr=[REDACTED]") {
		t.Fatalf("mode projection normal=%q diagnostic=%q", normal, diagnostic)
	}
}

func TestStatusOverlayAndSafeBoundedTimeline(t *testing.T) {
	actions := &fakeTUIActions{}
	m := attachedModel(t, actions)
	for index := 0; index < maxTimelineEntries+40; index++ {
		m.timeline.Add(strings.Repeat("x", maxTimelineEntryBytes+100) + fmt.Sprint(index))
	}
	if m.timeline.Len() > maxTimelineEntries || m.timeline.Bytes() > maxTimelineBytes {
		t.Fatalf("Timeline exceeded bounds entries=%d bytes=%d", m.timeline.Len(), m.timeline.Bytes())
	}
	if len(m.timeline.String()) != m.timeline.Bytes() {
		t.Fatalf("Timeline byte accounting=%d rendered=%d", m.timeline.Bytes(), len(m.timeline.String()))
	}
	if got := boundedSafeText(strings.Repeat("z", 20), 10); len(got) != 10 || got != "zzzzzzz..." {
		t.Fatalf("bounded text=%q len=%d", got, len(got))
	}
	m.overlay = overlayStatus
	status := m.View()
	for _, expected := range []string{"CLI username: owner", "Socket: /tmp/oax.sock", "Agent: quote", "Worker: worker-current",
		"Generation: 48", "Worker status: online", "Heartbeat age:", "Lease until:", "Connection: connected", "Event cursor: 1204", "Mode: normal"} {
		if !strings.Contains(status, expected) {
			t.Fatalf("status missing %q: %s", expected, status)
		}
	}
	sensitive := "opaque-sensitive-value"
	errView := safeErrorSummary(errors.New(sensitive))
	if strings.Contains(errView, sensitive) || errView != "operation failed" {
		t.Fatalf("unsafe error summary=%q", errView)
	}
	apiView := safeErrorSummary(&consoleclient.APIError{StatusCode: 401, Code: openapi.ErrorCLIUnauthenticated, Message: sensitive})
	if strings.Contains(apiView, sensitive) {
		t.Fatalf("API error leaked response message: %q", apiView)
	}
}

func TestFollowConnectionAndRetentionMessages(t *testing.T) {
	actions := &fakeTUIActions{}
	m := attachedModel(t, actions)
	updates := make(chan tea.Msg, 1)
	m.follow = updates
	for _, state := range []consoleclient.ConnectionState{consoleclient.ConnectionConnecting, consoleclient.ConnectionConnected,
		consoleclient.ConnectionDisconnected, consoleclient.ConnectionReconnecting, consoleclient.ConnectionRetentionReattach} {
		m, _ = updateModel(t, m, followConnectionMsg{State: consoleclient.FollowState{State: state, Cursor: 1204}})
		if m.connection != state || string(m.reducer.State().Connection) != string(state) {
			t.Fatalf("connection=%s reducer=%s want=%s", m.connection, m.reducer.State().Connection, state)
		}
	}
	if !strings.Contains(m.timeline.String(), "history expired") {
		t.Fatalf("retention refresh was not visible: %q", m.timeline.String())
	}
}

func TestTimelineScrollAndHeartbeatBurstPreserveDraft(t *testing.T) {
	actions := &fakeTUIActions{}
	m := attachedModel(t, actions)
	for index := 0; index < 80; index++ {
		m.timeline.Add(fmt.Sprintf("timeline entry %03d", index))
	}
	m.syncTimeline(true)
	m.input.SetValue("unfinished input")
	m.input.SetCursor(4)
	bottom := m.viewport.YOffset
	m, _ = updateModel(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.viewport.YOffset >= bottom || m.input.Value() != "unfinished input" || m.input.LineInfo().CharOffset != 4 {
		t.Fatalf("scroll offset=%d bottom=%d input=%q cursor=%d", m.viewport.YOffset, bottom, m.input.Value(), m.input.LineInfo().CharOffset)
	}
	beforeTimeline := m.timeline.Len()
	wantOffset := m.viewport.YOffset
	m, _ = updateModel(t, m, followEventMsg{Event: openapi.JournalEventReadModel{Sequence: 1205,
		ID: "event-task", AggregateType: "task", AggregateID: "task-1", EventType: "task.updated",
		Task: taskProjection("task-1", 1, domain.TaskStatusQueued)}})
	m, _ = updateModel(t, m, followConnectionMsg{State: consoleclient.FollowState{
		State: consoleclient.ConnectionRetentionReattach, Cursor: 1205}})
	m, _ = updateModel(t, m, controlResultMsg{Kind: controlDispatch, Outcome: testControlOutcome(controlDispatch)})
	if m.viewport.YOffset != wantOffset {
		t.Fatalf("background updates moved scroll offset=%d want=%d", m.viewport.YOffset, wantOffset)
	}
	beforeTimeline = m.timeline.Len()
	for sequence := int64(1206); sequence < 1226; sequence++ {
		event := openapi.JournalEventReadModel{Sequence: sequence, ID: fmt.Sprintf("event-%d", sequence),
			AggregateType: "worker_instance", AggregateID: "worker-current", EventType: "worker.heartbeat",
			Worker: &openapi.WorkerReadModel{WorkerInstanceID: "worker-current", AgentID: "quote", Generation: 48,
				Status: domain.WorkerStatusOnline, LastHeartbeatAt: fixedNow().Add(time.Duration(sequence) * time.Second),
				LeaseUntil: fixedNow().Add(time.Hour)}}
		m, _ = updateModel(t, m, followEventMsg{Event: event})
	}
	if m.timeline.Len() != beforeTimeline || m.reducer.Cursor() != 1225 || m.input.Value() != "unfinished input" || m.input.LineInfo().CharOffset != 4 {
		t.Fatalf("heartbeat burst timeline=%d/%d cursor=%d input=%q", m.timeline.Len(), beforeTimeline, m.reducer.Cursor(), m.input.Value())
	}
}

func TestDirectAttachFailureIsFatalButMenuFailureReturnsToMenu(t *testing.T) {
	actions := &fakeTUIActions{}
	direct := newTUIModel(actions, fixedNow, &initialAttach{AgentID: "quote", Mode: consoleapi.ModeNormal})
	direct, _ = updateModel(t, direct, prepareAttachResultMsg{Err: errors.New("opaque-sensitive-value")})
	if !direct.fatal || direct.screen != screenLoading || direct.overlay != overlayError || strings.Contains(direct.View(), "opaque-sensitive-value") {
		t.Fatalf("direct failure state fatal=%v screen=%v view=%q", direct.fatal, direct.screen, direct.View())
	}
	menu := newTUIModel(actions, fixedNow, nil)
	menu.screen = screenLoading
	menu, _ = updateModel(t, menu, prepareAttachResultMsg{Err: errors.New("failure")})
	if menu.fatal || menu.screen != screenMenu || menu.overlay != overlayError {
		t.Fatalf("menu failure state fatal=%v screen=%v overlay=%v", menu.fatal, menu.screen, menu.overlay)
	}
}
