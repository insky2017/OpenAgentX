package console

import (
	"context"
	"encoding/json"
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
	openruntime "openagentx/internal/runtime"
	runtimenetwork "openagentx/internal/runtime/network"
	"openagentx/internal/safeoutput"
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
	followCalls     int
	nextFollowID    uint64
	followModeCalls []string
	cancelCalls     []uint64
	controls        []controlRequest
	taskListCalls   int
	taskDetailCalls []struct {
		taskID  string
		purpose taskSnapshotPurpose
	}
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
	a.nextFollowID++
	id := a.nextFollowID
	return func() tea.Msg { return followStartedMsg{FollowID: id, Mode: consoleapi.ModeNormal} }
}
func (a *fakeTUIActions) startFollowModeCmd(_ uint64, _, mode string) tea.Cmd {
	a.followCalls++
	a.followModeCalls = append(a.followModeCalls, mode)
	a.nextFollowID++
	id := a.nextFollowID
	return func() tea.Msg { return followStartedMsg{FollowID: id, Mode: mode} }
}
func (a *fakeTUIActions) cancelFollowCmd(followID uint64) tea.Cmd {
	a.cancelCalls = append(a.cancelCalls, followID)
	return func() tea.Msg { return followCancelResultMsg{FollowID: followID, Found: true} }
}
func (a *fakeTUIActions) taskOptionsCmd(_ uint64, _ string) tea.Cmd {
	a.taskListCalls++
	return func() tea.Msg { return taskOptionsResultMsg{} }
}
func (a *fakeTUIActions) taskSnapshotCmd(_ uint64, _, taskID string, purpose taskSnapshotPurpose) tea.Cmd {
	a.taskDetailCalls = append(a.taskDetailCalls, struct {
		taskID  string
		purpose taskSnapshotPurpose
	}{taskID: taskID, purpose: purpose})
	return func() tea.Msg { return taskSnapshotResultMsg{TaskID: taskID, Purpose: purpose} }
}
func (a *fakeTUIActions) controlCmd(_ uint64, request controlRequest) tea.Cmd {
	a.controls = append(a.controls, request)
	return func() tea.Msg {
		return controlResultMsg{Kind: request.Kind, Request: request, Outcome: testControlOutcome(request.Kind)}
	}
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
	return &openapi.ConsoleTaskReadModel{Intent: domain.TaskIntentMutation, TaskID: taskID, Version: version, AgentID: "quote", Status: status,
		Content: "safe task", OutcomeState: outcome, CreatedAt: fixedNow().Format(time.RFC3339Nano),
		UpdatedAt: fixedNow().Add(time.Duration(version) * time.Second).Format(time.RFC3339Nano)}
}

func taskSnapshot(taskID string, version int64, status domain.TaskStatus) openapi.ConsoleTaskSnapshot {
	return openapi.ConsoleTaskSnapshot{Task: *taskProjection(taskID, version, status), SnapshotSequence: 1204}
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
	m.followEpoch = m.reducer.StreamEpoch()
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

func TestFocusedTaskShortcutsUseReducerCASAndExplicitSyntax(t *testing.T) {
	actions := &fakeTUIActions{}
	m := attachedModel(t, actions)
	if err := m.reducer.ApplyControlTask(consolemodel.ControlTaskUpdate{AgentID: "quote", TaskID: "task-focused",
		Version: 7, Status: domain.TaskStatusRunning, Focus: true}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		line    string
		kind    controlKind
		target  string
		version int64
		content string
	}{
		{line: "/steer only answer with the current time", kind: controlSteer, target: "task-focused", version: 7,
			content: "only answer with the current time"},
		{line: "/cancel", kind: controlCancel, target: "task-focused", version: 7},
		{line: "/steer --task task-other --version 9 explicit content", kind: controlSteer,
			target: "task-other", version: 9, content: "explicit content"},
		{line: "/cancel --task task-other --version 10", kind: controlCancel, target: "task-other", version: 10},
		{line: "/steer task-focused 7 legacy content", kind: controlSteer,
			target: "task-focused", version: 7, content: "legacy content"},
	}
	for _, testCase := range tests {
		m.pending = false
		before := len(actions.controls)
		var cmd tea.Cmd
		m, cmd = executeLine(t, m, testCase.line)
		if cmd == nil || len(actions.controls) != before+1 {
			t.Fatalf("%q calls=%d cmd=%v", testCase.line, len(actions.controls)-before, cmd)
		}
		request := actions.controls[len(actions.controls)-1]
		if request.Kind != testCase.kind || request.TargetID != testCase.target ||
			request.ExpectedVersion != testCase.version || request.Content != testCase.content {
			t.Fatalf("%q request=%+v", testCase.line, request)
		}
		m, _ = updateModel(t, m, controlResultMsg{Kind: request.Kind, Request: request,
			Outcome: testControlOutcome(request.Kind)})
	}

	noFocusActions := &fakeTUIActions{}
	noFocus := attachedModel(t, noFocusActions)
	noFocus, cmd := executeLine(t, noFocus, "/steer task-untracked 3 legacy explicit")
	if cmd == nil || len(noFocusActions.controls) != 1 {
		t.Fatalf("legacy explicit control without focus cmd=%v", cmd)
	}
	noFocus.pending = false
	noFocus, cmd = executeLine(t, noFocus, "/cancel")
	if cmd != nil || !strings.Contains(noFocus.timeline.String(), "no focused Task") || noFocus.input.Value() != "/cancel" {
		t.Fatalf("no-focus command cmd=%v draft=%q timeline=%q", cmd, noFocus.input.Value(), noFocus.timeline.String())
	}
	terminal := taskSnapshot("task-done", 3, domain.TaskStatusSucceeded)
	if err := noFocus.reducer.ApplyTaskSnapshot(terminal, consolemodel.FocusManual); err != nil {
		t.Fatal(err)
	}
	noFocus, cmd = executeLine(t, noFocus, "/steer retry")
	if cmd != nil || !strings.Contains(noFocus.timeline.String(), "terminal") {
		t.Fatalf("terminal shortcut cmd=%v timeline=%q", cmd, noFocus.timeline.String())
	}
}

func TestTaskOverlayFocusAndTerminalResultsUseAuthoritativeProjection(t *testing.T) {
	actions := &fakeTUIActions{}
	m := attachedModel(t, actions)
	m, listCmd := executeLine(t, m, "/tasks")
	if listCmd == nil || actions.taskListCalls != 1 || m.overlay != overlayTasks || !m.taskLoading || m.input.Value() != "" {
		t.Fatalf("Task list command calls=%d overlay=%v loading=%v draft=%q cmd=%v",
			actions.taskListCalls, m.overlay, m.taskLoading, m.input.Value(), listCmd)
	}
	options := []openapi.ConsoleTaskOption{
		{Intent: domain.TaskIntentMutation, TaskID: "task-running-full-id", Version: 4, Status: domain.TaskStatusRunning,
			Summary: "running task", UpdatedAt: fixedNow().Add(time.Minute).Format(time.RFC3339Nano)},
		{Intent: domain.TaskIntentMutation, TaskID: "task-terminal-full-id", Version: 5, Status: domain.TaskStatusSucceeded,
			Summary: "terminal task", UpdatedAt: fixedNow().Format(time.RFC3339Nano)},
	}
	m, _ = updateModel(t, m, taskOptionsResultMsg{Options: options})
	if m.overlay != overlayTasks || len(m.tasks.Items()) != 2 {
		t.Fatalf("Task overlay=%v items=%d", m.overlay, len(m.tasks.Items()))
	}
	for _, size := range []struct{ width, height int }{{80, 5}, {20, 3}, {8, 1}} {
		m.resize(size.width, size.height)
		assertTerminalViewFits(t, m.View(), size.width, size.height)
	}
	m.resize(100, 30)
	m, cmd := updateModel(t, m, key("enter"))
	if cmd == nil || len(actions.taskDetailCalls) != 1 || actions.taskDetailCalls[0].taskID != "task-running-full-id" {
		t.Fatalf("Task focus calls=%+v cmd=%v", actions.taskDetailCalls, cmd)
	}
	running := taskSnapshot("task-running-full-id", 4, domain.TaskStatusRunning)
	running.WorkDelivery = &openapi.ConsoleMailboxReadModel{MailboxItemID: "mailbox-running",
		Kind: domain.MailboxKindTask, Lane: domain.MailboxLaneWork,
		State: domain.MailboxStateAccepted, CreatedAt: fixedNow()}
	m, _ = updateModel(t, m, taskSnapshotResultMsg{TaskID: running.Task.TaskID,
		Snapshot: running, Purpose: taskSnapshotFocus})
	if m.reducer.State().FocusedTask == nil || m.reducer.State().FocusedTask.TaskID != running.Task.TaskID ||
		m.overlay != overlayNone || !strings.Contains(m.statusView(), "Task ID: task-running-full-id") {
		t.Fatalf("focused state=%+v overlay=%v status=%q", m.reducer.State().FocusedTask, m.overlay, m.statusView())
	}

	terminal := taskSnapshot("task-terminal-full-id", 5, domain.TaskStatusSucceeded)
	result := "safe Task outcome"
	terminal.Task.Result, terminal.Task.OutcomeState = &result, "available"
	generation := int64(48)
	known := true
	terminal.LatestRun = &openapi.RunAttemptReadModel{ID: "run-terminal-full-id", TaskID: terminal.Task.TaskID,
		AgentID: "quote", Version: 2, Status: domain.RunAttemptSucceeded, WorkerInstanceID: "worker-current",
		WorkerGeneration: &generation, TurnResultState: "available", StartedAt: fixedNow(), UpdatedAt: fixedNow().Add(time.Second),
		TurnResult: &openapi.TurnResultReadModel{RuntimeStatus: openruntime.TurnResultSucceeded,
			Body: "safe Runtime reply", RuntimeSideEffectsKnown: &known, SideEffectsSource: "runtime_reported",
			BusinessVerificationSource: "not_recorded"}}
	m, _ = updateModel(t, m, taskSnapshotResultMsg{TaskID: terminal.Task.TaskID,
		Snapshot: terminal, Purpose: taskSnapshotFocus})
	status := m.statusView()
	for _, expected := range []string{"Task ID: task-terminal-full-id", "Task version: 5", "Task status: succeeded",
		"Run ID: run-terminal-full-id", "Runtime reply: safe Runtime reply", "Task result: safe Task outcome"} {
		if !strings.Contains(status, expected) {
			t.Fatalf("status missing %q: %s", expected, status)
		}
	}
	if !strings.Contains(m.timeline.String(), "Task outcome result: safe Task outcome") ||
		!strings.Contains(m.timeline.String(), "Runtime reply: safe Runtime reply") {
		t.Fatalf("terminal summaries missing: %q", m.timeline.String())
	}
}

func TestTaskOverlaySanitizesTerminalControlSequences(t *testing.T) {
	m := attachedModel(t, &fakeTUIActions{})
	options := []openapi.ConsoleTaskOption{{Intent: domain.TaskIntentMutation, TaskID: "task-safe-list", Version: 1,
		Status: domain.TaskStatusRunning, Summary: "safe\x1b]8;;https://example.invalid\aunsafe\x1b]8;;\a summary",
		UpdatedAt: fixedNow().Format(time.RFC3339Nano)}}
	m, _ = updateModel(t, m, taskOptionsResultMsg{Options: options})
	item, ok := m.tasks.Items()[0].(taskItem)
	if !ok {
		t.Fatalf("Task list item type=%T", m.tasks.Items()[0])
	}
	for _, rendered := range []string{item.Title(), item.Description(), item.FilterValue()} {
		if strings.ContainsAny(rendered, "\x1b\a") {
			t.Fatalf("Task item retained terminal control bytes: %q", rendered)
		}
	}
	view := m.View()
	if strings.Contains(view, "\x1b]8;;") || !strings.Contains(ansi.Strip(view), "unsafe") {
		t.Fatalf("Task overlay did not safely render summary: %q", view)
	}
}

func TestSuggestedTaskDetailKeepsCurrentOverlayAndFailureIsNonDisruptive(t *testing.T) {
	m := attachedModel(t, &fakeTUIActions{})
	m.overlay = overlayHelp
	snapshot := taskSnapshot("task-suggested", 2, domain.TaskStatusRunning)
	m.taskLoading = true
	m, _ = updateModel(t, m, taskSnapshotResultMsg{TaskID: snapshot.Task.TaskID,
		Snapshot: snapshot, Purpose: taskSnapshotInitial})
	if m.overlay != overlayHelp || m.reducer.State().FocusedTask == nil ||
		m.reducer.State().FocusedTask.TaskID != "task-suggested" {
		t.Fatalf("suggested Task changed overlay=%v focus=%+v", m.overlay, m.reducer.State().FocusedTask)
	}
	m.taskLoading = true
	m, _ = updateModel(t, m, taskSnapshotResultMsg{TaskID: "task-suggested",
		Purpose: taskSnapshotInitial, Err: errors.New("temporary failure")})
	if m.overlay != overlayHelp || !strings.Contains(m.timeline.String(), "Suggested Task detail failed") {
		t.Fatalf("suggested Task failure changed overlay=%v timeline=%q", m.overlay, m.timeline.String())
	}
}

func TestTerminalNoResultReasonsAreExplicit(t *testing.T) {
	generation := int64(48)
	tests := []struct {
		name string
		task *consolemodel.TaskState
		want string
	}{
		{name: "task not recorded", task: &consolemodel.TaskState{TaskID: "task-one", Status: domain.TaskStatusSucceeded,
			Detail: taskProjection("task-one", 2, domain.TaskStatusSucceeded)}, want: "no safe Task outcome was recorded"},
		{name: "runtime not recorded", task: &consolemodel.TaskState{TaskID: "task-two", Status: domain.TaskStatusSucceeded,
			LatestRun: &openapi.RunAttemptReadModel{TurnResultState: "not_recorded", WorkerGeneration: &generation}},
			want: "Runtime reply was not recorded"},
		{name: "runtime empty", task: &consolemodel.TaskState{TaskID: "task-three", Status: domain.TaskStatusSucceeded,
			LatestRun: &openapi.RunAttemptReadModel{TurnResultState: "empty", WorkerGeneration: &generation}},
			want: "without a safe displayable reply"},
		{name: "runtime invalid", task: &consolemodel.TaskState{TaskID: "task-four", Status: domain.TaskStatusFailed,
			LatestRun: &openapi.RunAttemptReadModel{TurnResultState: "invalid", WorkerGeneration: &generation}},
			want: "rejected as invalid"},
		{name: "uncertain", task: &consolemodel.TaskState{TaskID: "task-five", Status: domain.TaskStatusUncertain},
			want: "remains uncertain"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := strings.Join(taskResultSummaries(testCase.task), "\n"); !strings.Contains(got, testCase.want) {
				t.Fatalf("summary=%q want %q", got, testCase.want)
			}
		})
	}
}

func TestLiveTaskLifecycleShowsDeliveryRunOutcomeAndRuntimeReply(t *testing.T) {
	m := attachedModel(t, &fakeTUIActions{})
	if err := m.reducer.ApplyControlTask(consolemodel.ControlTaskUpdate{AgentID: "quote", TaskID: "task-live",
		Version: 1, Status: domain.TaskStatusQueued, Focus: true}); err != nil {
		t.Fatal(err)
	}
	m.input.SetValue("draft remains")
	m.input.SetCursor(5)
	claimedTask := taskProjection("task-live", 2, domain.TaskStatusDispatching)
	mailbox := &openapi.ConsoleMailboxReadModel{MailboxItemID: "mailbox-live", Kind: domain.MailboxKindTask,
		Lane: domain.MailboxLaneWork, State: domain.MailboxStateClaimed,
		WorkerInstanceID: "worker-current", CreatedAt: fixedNow(), LeaseUntil: timePointer(fixedNow().Add(time.Minute))}
	m, _ = updateModel(t, m, followEventMsg{Event: openapi.JournalEventReadModel{Sequence: 1205,
		ID: "event-mailbox-live", AggregateType: "mailbox_item", AggregateID: mailbox.MailboxItemID,
		EventType: "mailbox.claimed", Task: claimedTask, Mailbox: mailbox}})
	generation := int64(48)
	running := &openapi.RunAttemptReadModel{ID: "run-live", TaskID: "task-live", AgentID: "quote", Version: 1,
		Status: domain.RunAttemptRunning, WorkerInstanceID: "worker-current", WorkerGeneration: &generation,
		TurnResultState: "not_recorded", StartedAt: fixedNow(), UpdatedAt: fixedNow().Add(time.Second)}
	m, _ = updateModel(t, m, followEventMsg{Event: openapi.JournalEventReadModel{Sequence: 1206,
		ID: "event-run-live", AggregateType: "run_attempt", AggregateID: running.ID,
		EventType: "run_attempt.running", Run: running}})
	terminalRun := *running
	terminalRun.Version = 2
	terminalRun.Status = domain.RunAttemptSucceeded
	terminalRun.UpdatedAt = fixedNow().Add(2 * time.Second)
	known := true
	terminalRun.TurnResultState = "available"
	terminalRun.TurnResult = &openapi.TurnResultReadModel{RuntimeStatus: openruntime.TurnResultSucceeded,
		Body: "live safe reply", RuntimeSideEffectsKnown: &known, SideEffectsSource: "runtime_reported",
		BusinessVerificationSource: "not_recorded"}
	m, _ = updateModel(t, m, followEventMsg{Event: openapi.JournalEventReadModel{Sequence: 1207,
		ID: "event-run-live-terminal", AggregateType: "run_attempt", AggregateID: terminalRun.ID,
		EventType: "run_attempt.succeeded", Run: &terminalRun}})
	finished := taskProjection("task-live", 3, domain.TaskStatusSucceeded)
	result := "live Task result"
	finished.Result, finished.OutcomeState = &result, "available"
	m, _ = updateModel(t, m, followEventMsg{Event: openapi.JournalEventReadModel{Sequence: 1208,
		ID: "event-task-live-terminal", AggregateType: "task", AggregateID: finished.TaskID,
		EventType: "task.succeeded", Task: finished}})

	state := m.reducer.State()
	if state.FocusedTask == nil || state.FocusedTask.Status != domain.TaskStatusSucceeded || state.FocusedTask.Version != 3 ||
		m.input.Value() != "draft remains" || m.input.LineInfo().CharOffset != 5 {
		t.Fatalf("live lifecycle state=%+v draft=%q cursor=%d", state.FocusedTask, m.input.Value(), m.input.LineInfo().CharOffset)
	}
	for _, expected := range []string{"work delivery claimed", "run status running", "Runtime reply: live safe reply",
		"Task outcome result: live Task result"} {
		if !strings.Contains(m.timeline.String(), expected) {
			t.Fatalf("Timeline missing %q: %s", expected, m.timeline.String())
		}
	}
}

func TestTerminalTimelineSummaryKeepsOutcomeAndRuntimeReplyOnDedicatedLines(t *testing.T) {
	summary := timelineItemSummary(consolemodel.TimelineItem{
		Sequence:          1208,
		EventType:         "task.succeeded",
		TaskID:            "task-live",
		TaskVersion:       3,
		TaskStatus:        domain.TaskStatusSucceeded,
		RunID:             "run-live",
		RunVersion:        2,
		RunStatus:         domain.RunAttemptSucceeded,
		TaskOutcomeState:  "available",
		TaskResult:        "live Task result",
		RuntimeReplyState: "available",
		RuntimeReply:      "live safe reply",
	}, consoleapi.ModeNormal)
	lines := strings.Split(summary, "\n")
	if len(lines) != 3 || strings.Contains(lines[0], "live Task result") ||
		strings.Contains(lines[0], "live safe reply") ||
		lines[1] != "Task outcome result: live Task result" ||
		lines[2] != "Runtime reply: live safe reply" {
		t.Fatalf("terminal details were not rendered on dedicated lines: %#v", lines)
	}
	var timeline timelineBuffer
	timeline.Add(summary)
	if buffered := timeline.String(); buffered != summary {
		t.Fatalf("Timeline buffer collapsed terminal detail lines: %q", buffered)
	}
}

func timePointer(value time.Time) *time.Time { return &value }

func TestStaleCASRefreshesWithoutRetryAndPreservesDraft(t *testing.T) {
	actions := &fakeTUIActions{}
	m := attachedModel(t, actions)
	if err := m.reducer.ApplyControlTask(consolemodel.ControlTaskUpdate{AgentID: "quote", TaskID: "task-focused",
		Version: 7, Status: domain.TaskStatusRunning, Focus: true}); err != nil {
		t.Fatal(err)
	}
	line := "/steer corrected instruction"
	m, cmd := executeLine(t, m, line)
	if cmd == nil || len(actions.controls) != 1 || m.input.Value() != "" {
		t.Fatalf("initial write calls=%d draft=%q cmd=%v", len(actions.controls), m.input.Value(), cmd)
	}
	request := actions.controls[0]
	m, refresh := updateModel(t, m, controlResultMsg{Kind: controlSteer, Request: request,
		Err: &consoleclient.APIError{StatusCode: 409, Code: openapi.ErrorStaleVersion}})
	if refresh == nil || len(actions.controls) != 1 || len(actions.taskDetailCalls) != 1 ||
		actions.taskDetailCalls[0].purpose != taskSnapshotStale || m.input.Value() != line {
		t.Fatalf("stale refresh calls=%d details=%+v draft=%q cmd=%v",
			len(actions.controls), actions.taskDetailCalls, m.input.Value(), refresh)
	}
	refreshed := taskSnapshot("task-focused", 8, domain.TaskStatusWaitingInput)
	m, _ = updateModel(t, m, taskSnapshotResultMsg{TaskID: refreshed.Task.TaskID,
		Snapshot: refreshed, Purpose: taskSnapshotStale})
	if len(actions.controls) != 1 || m.reducer.State().FocusedTask.Version != 8 || m.input.Value() != line ||
		m.overlay != overlayStatus || !strings.Contains(m.timeline.String(), "without retrying") {
		t.Fatalf("stale result calls=%d focus=%+v draft=%q overlay=%v timeline=%q",
			len(actions.controls), m.reducer.State().FocusedTask, m.input.Value(), m.overlay, m.timeline.String())
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
	for _, line := range []string{"/status", "/help"} {
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

func TestSessionExpiryWhileControlPendingRestoresSubmittedDraft(t *testing.T) {
	actions := &fakeTUIActions{}
	m := attachedModel(t, actions)
	if err := m.reducer.ApplyControlTask(consolemodel.ControlTaskUpdate{AgentID: "quote", TaskID: "task-focused",
		Version: 7, Status: domain.TaskStatusRunning, Focus: true}); err != nil {
		t.Fatal(err)
	}
	line := "/steer answer only with the current time"
	m, cmd := executeLine(t, m, line)
	if cmd == nil || !m.pending || m.input.Value() != "" || len(actions.controls) != 1 {
		t.Fatalf("pending command state pending=%v draft=%q calls=%d cmd=%v",
			m.pending, m.input.Value(), len(actions.controls), cmd)
	}
	m, _ = updateModel(t, m, tickMsg{Now: m.session.ExpiresAt})
	if m.pending || m.pendingDraft != "" || m.input.Value() != line || m.session.Authenticated ||
		m.connection != consoleclient.ConnectionDisconnected {
		t.Fatalf("expiry did not restore pending draft pending=%v saved=%q draft=%q auth=%v connection=%s",
			m.pending, m.pendingDraft, m.input.Value(), m.session.Authenticated, m.connection)
	}
}

func TestControlIsDisabledDuringAuthoritativeTaskRefresh(t *testing.T) {
	actions := &fakeTUIActions{}
	m := attachedModel(t, actions)
	if err := m.reducer.ApplyControlTask(consolemodel.ControlTaskUpdate{AgentID: "quote", TaskID: "task-focused",
		Version: 7, Status: domain.TaskStatusRunning, Focus: true}); err != nil {
		t.Fatal(err)
	}
	m.taskLoading = true
	line := "/cancel"
	m, cmd := executeLine(t, m, line)
	if cmd != nil || len(actions.controls) != 0 || m.input.Value() != line ||
		!strings.Contains(m.timeline.String(), "authoritative Task state is loading") {
		t.Fatalf("control during Task refresh cmd=%v calls=%d draft=%q timeline=%q",
			cmd, len(actions.controls), m.input.Value(), m.timeline.String())
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

func TestRuntimeExecutableWarningIsVisibleInNormalConsole(t *testing.T) {
	err := runtimenetwork.EmitExecutableChangeWarning(context.Background(), openruntime.EventSinkFunc(func(_ context.Context, event openruntime.RuntimeEvent) error {
		payload, err := safeoutput.ProjectRuntimePayload(event.Payload)
		if err != nil {
			return err
		}
		var output openapi.SafeOutputReadModel
		if err := json.Unmarshal(payload, &output); err != nil {
			return err
		}
		summary := timelineItemSummary(consolemodel.TimelineItem{Sequence: 12, EventType: "runtime." + event.Type, Output: &output}, consoleapi.ModeNormal)
		if !strings.Contains(summary, "warning") || !strings.Contains(summary, "Runtime executable changed") || !strings.Contains(summary, "continuing") {
			t.Fatalf("normal Console did not render warning: %q", summary)
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
}

func TestTimelineNeverFallsBackFromDiagnosticToNormal(t *testing.T) {
	var timeline timelineBuffer
	timeline.AddVariants("", "diagnostic-only")
	if normal := timeline.StringForMode(consoleapi.ModeNormal); normal != "" {
		t.Fatalf("Normal timeline exposed Diagnostic-only text: %q", normal)
	}
	if diagnostic := timeline.StringForMode(consoleapi.ModeDiagnostic); diagnostic != "diagnostic-only" {
		t.Fatalf("Diagnostic timeline=%q", diagnostic)
	}
}

func TestInPlaceDiagnosticAndNormalModeSwitchUsesSingleFollowEpoch(t *testing.T) {
	actions := &fakeTUIActions{}
	m := attachedModel(t, actions)
	m.followID = 41
	m.followEpoch = m.reducer.StreamEpoch()
	m.follow = make(chan tea.Msg)
	if err := m.reducer.ApplyControlTask(consolemodel.ControlTaskUpdate{AgentID: "quote", TaskID: "task-focused",
		Version: 7, Status: domain.TaskStatusRunning, Focus: true}); err != nil {
		t.Fatal(err)
	}

	m, cancelCmd := executeLine(t, m, "/diagnostic")
	if cancelCmd == nil || !m.switching || m.switchTarget != consoleapi.ModeDiagnostic ||
		len(actions.cancelCalls) != 1 || actions.cancelCalls[0] != 41 ||
		m.reducer.State().Console.Mode != consoleapi.ModeNormal || m.reducer.State().PendingMode != consoleapi.ModeDiagnostic {
		t.Fatalf("diagnostic switch state=%+v cancels=%v cmd=%v", m.reducer.State(), actions.cancelCalls, cancelCmd)
	}
	staleAck := make(chan error, 1)
	cursor := m.reducer.Cursor()
	m, _ = updateModel(t, m, followEventMsg{FollowID: 41, Event: openapi.JournalEventReadModel{Sequence: cursor + 1,
		ID: "event-old-mode", AggregateType: "runtime", AggregateID: "run-old", EventType: "runtime.output",
		Output: &openapi.SafeOutputReadModel{Text: "safe", Diagnostic: "old diagnostic", HasOutput: true, HasError: true}},
		Ack: staleAck})
	if err := <-staleAck; !errors.Is(err, context.Canceled) || m.reducer.Cursor() != cursor {
		t.Fatalf("old mode event ack=%v cursor=%d/%d", err, m.reducer.Cursor(), cursor)
	}
	m, startCmd := updateModel(t, m, followDoneMsg{FollowID: 41, Mode: consoleapi.ModeNormal, Err: context.Canceled})
	if startCmd == nil || len(actions.followModeCalls) != 1 || actions.followModeCalls[0] != consoleapi.ModeDiagnostic {
		t.Fatalf("new diagnostic Follow calls=%v cmd=%v", actions.followModeCalls, startCmd)
	}
	started := startCmd().(followStartedMsg)
	m, _ = updateModel(t, m, started)
	m, _ = updateModel(t, m, followConnectionMsg{FollowID: started.FollowID,
		State: consoleclient.FollowState{State: consoleclient.ConnectionConnected, Cursor: cursor}})
	diagnosticAck := make(chan error, 1)
	diagnosticSnapshot := consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeDiagnostic,
		WorkerInstanceID: "worker-current", Generation: 48, WorkerStatus: domain.WorkerStatusOnline,
		BackendHealth: map[string]openruntime.BackendHealth{"local": openruntime.BackendHealthy},
		Diagnostic: &consoleapi.DiagnosticView{LastHeartbeatAt: fixedNow(), LeaseUntil: fixedNow().Add(time.Minute),
			StartedAt: fixedNow().Add(-time.Hour), UpdatedAt: fixedNow(), Draining: false}, SnapshotSequence: cursor}
	m, _ = updateModel(t, m, followSnapshotMsg{FollowID: started.FollowID, Snapshot: diagnosticSnapshot, Ack: diagnosticAck})
	if err := <-diagnosticAck; err != nil || m.switching || m.mode != consoleapi.ModeDiagnostic ||
		m.reducer.State().FocusedTask == nil || m.reducer.State().FocusedTask.TaskID != "task-focused" {
		t.Fatalf("diagnostic snapshot ack=%v switching=%v mode=%s focus=%+v",
			err, m.switching, m.mode, m.reducer.State().FocusedTask)
	}
	eventAck := make(chan error, 1)
	m, _ = updateModel(t, m, followEventMsg{FollowID: started.FollowID, Event: openapi.JournalEventReadModel{
		Sequence: cursor + 1, ID: "event-diagnostic", AggregateType: "runtime", AggregateID: "run-current",
		EventType: "runtime.output", Output: &openapi.SafeOutputReadModel{Stage: "tool", Status: "waiting",
			Text: "safe output", Diagnostic: "stderr=[REDACTED]", HasOutput: true, HasError: true}}, Ack: eventAck})
	if err := <-eventAck; err != nil || !strings.Contains(m.timeline.StringForMode(consoleapi.ModeDiagnostic), "stderr=[REDACTED]") ||
		strings.Contains(m.timeline.StringForMode(consoleapi.ModeNormal), "stderr") {
		t.Fatalf("diagnostic event ack=%v normal=%q diagnostic=%q", err,
			m.timeline.StringForMode(consoleapi.ModeNormal), m.timeline.StringForMode(consoleapi.ModeDiagnostic))
	}
	if view := m.overlayView(); !strings.Contains(view, "Backend health local=healthy") ||
		!strings.Contains(view, "diagnostic stderr=[REDACTED]") {
		t.Fatalf("diagnostic overlay=%q", view)
	}

	m.overlay = overlayNone
	m, cancelCmd = executeLine(t, m, "/normal")
	if cancelCmd == nil || m.displayMode() != consoleapi.ModeNormal || strings.Contains(m.View(), "stderr=[REDACTED]") {
		t.Fatalf("normal switch did not immediately hide Diagnostic: mode=%s view=%q", m.displayMode(), m.View())
	}
	m, startCmd = updateModel(t, m, followDoneMsg{FollowID: started.FollowID, Mode: consoleapi.ModeDiagnostic,
		Err: context.Canceled})
	normalStarted := startCmd().(followStartedMsg)
	m, _ = updateModel(t, m, normalStarted)
	normalAck := make(chan error, 1)
	normalSnapshot := diagnosticSnapshot
	normalSnapshot.Mode = consoleapi.ModeNormal
	normalSnapshot.Diagnostic = nil
	normalSnapshot.SnapshotSequence = cursor + 1
	m, _ = updateModel(t, m, followSnapshotMsg{FollowID: normalStarted.FollowID, Snapshot: normalSnapshot, Ack: normalAck})
	if err := <-normalAck; err != nil || m.mode != consoleapi.ModeNormal || m.switching ||
		m.reducer.Snapshot().Diagnostic != nil || strings.Contains(m.timeline.StringForMode(consoleapi.ModeNormal), "stderr") {
		t.Fatalf("normal result ack=%v mode=%s switching=%v snapshot=%+v timeline=%q", err, m.mode,
			m.switching, m.reducer.Snapshot(), m.timeline.StringForMode(consoleapi.ModeNormal))
	}
}

func TestDiagnosticFailureRestoresNormalWithoutRetryLoop(t *testing.T) {
	actions := &fakeTUIActions{}
	m := attachedModel(t, actions)
	m.followID = 51
	m.followEpoch = m.reducer.StreamEpoch()
	m.follow = make(chan tea.Msg)
	m, _ = executeLine(t, m, "/diagnostic")
	m, startDiagnostic := updateModel(t, m, followDoneMsg{FollowID: 51, Mode: consoleapi.ModeNormal,
		Err: context.Canceled})
	diagnosticStarted := startDiagnostic().(followStartedMsg)
	m, _ = updateModel(t, m, diagnosticStarted)
	m, restoreCmd := updateModel(t, m, followDoneMsg{FollowID: diagnosticStarted.FollowID,
		Mode: consoleapi.ModeDiagnostic, Err: &consoleclient.APIError{StatusCode: 403, Code: openapi.ErrorCLIForbidden}})
	if restoreCmd == nil || !m.switching || m.switchTarget != consoleapi.ModeNormal || !m.switchFallback ||
		m.reducer.State().Console.Mode != consoleapi.ModeNormal || m.reducer.State().Console.Diagnostic != nil {
		t.Fatalf("diagnostic failure did not start safe fallback: switching=%v target=%s fallback=%v state=%+v",
			m.switching, m.switchTarget, m.switchFallback, m.reducer.State())
	}
	normalStarted := restoreCmd().(followStartedMsg)
	m, _ = updateModel(t, m, normalStarted)
	ack := make(chan error, 1)
	m, _ = updateModel(t, m, followSnapshotMsg{FollowID: normalStarted.FollowID, Snapshot: consoleapi.AttachResponse{
		AgentID: "quote", Mode: consoleapi.ModeNormal, WorkerInstanceID: "worker-current", Generation: 48,
		WorkerStatus: domain.WorkerStatusOnline, SnapshotSequence: 1204}, Ack: ack})
	if err := <-ack; err != nil || m.switching || m.mode != consoleapi.ModeNormal || len(actions.followModeCalls) != 2 {
		t.Fatalf("Normal fallback ack=%v switching=%v mode=%s calls=%v", err, m.switching, m.mode, actions.followModeCalls)
	}
}

func TestRejectedDiagnosticSnapshotDrainsFollowBeforeNormalFallback(t *testing.T) {
	actions := &fakeTUIActions{}
	m := attachedModel(t, actions)
	m.followID = 52
	m.followEpoch = m.reducer.StreamEpoch()
	m.follow = make(chan tea.Msg)
	m, _ = executeLine(t, m, "/diagnostic")
	m, startDiagnostic := updateModel(t, m, followDoneMsg{FollowID: 52, Mode: consoleapi.ModeNormal,
		Err: context.Canceled})
	diagnosticStarted := startDiagnostic().(followStartedMsg)
	m, _ = updateModel(t, m, diagnosticStarted)

	ack := make(chan error, 1)
	m, drainCmd := updateModel(t, m, followSnapshotMsg{FollowID: diagnosticStarted.FollowID,
		Snapshot: consoleapi.AttachResponse{AgentID: "another-agent", Mode: consoleapi.ModeDiagnostic,
			WorkerInstanceID: "worker-current", Generation: 48, WorkerStatus: domain.WorkerStatusOnline,
			SnapshotSequence: 1204}, Ack: ack})
	if err := <-ack; err == nil || drainCmd == nil || !m.switching || m.switchTarget != consoleapi.ModeNormal ||
		!m.switchFallback || len(actions.cancelCalls) != 2 {
		t.Fatalf("rejected snapshot err=%v cmd=%v switching=%v target=%s fallback=%v cancels=%v",
			err, drainCmd, m.switching, m.switchTarget, m.switchFallback, actions.cancelCalls)
	}

	m, startNormal := updateModel(t, m, followDoneMsg{FollowID: diagnosticStarted.FollowID,
		Mode: consoleapi.ModeDiagnostic, Err: context.Canceled})
	if startNormal == nil || len(actions.followModeCalls) != 2 || actions.followModeCalls[1] != consoleapi.ModeNormal {
		t.Fatalf("Normal fallback calls=%v cmd=%v", actions.followModeCalls, startNormal)
	}
}

func TestDiagnosticSessionExpiryClearsPrivilegedStateAndCancelsFollow(t *testing.T) {
	actions := &fakeTUIActions{}
	m := attachedModel(t, actions)
	epoch, err := m.reducer.BeginStream(consoleapi.ModeDiagnostic)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.reducer.ApplySnapshotForStream(epoch, consoleapi.AttachResponse{AgentID: "quote",
		Mode: consoleapi.ModeDiagnostic, WorkerInstanceID: "worker-current", Generation: 48,
		WorkerStatus: domain.WorkerStatusOnline, Diagnostic: &consoleapi.DiagnosticView{
			LastHeartbeatAt: fixedNow(), LeaseUntil: fixedNow().Add(time.Minute)}, SnapshotSequence: 1204}); err != nil {
		t.Fatal(err)
	}
	m.mode = consoleapi.ModeDiagnostic
	m.followID = 61
	m.followEpoch = epoch
	m.follow = make(chan tea.Msg)
	m.input.SetValue("draft survives")
	m.input.SetCursor(5)
	m, cmd := updateModel(t, m, tickMsg{Now: m.session.ExpiresAt})
	state := m.reducer.State()
	if cmd == nil || m.session.Authenticated || m.mode != consoleapi.ModeNormal ||
		state.Console.Mode != consoleapi.ModeNormal || state.Console.Diagnostic != nil ||
		state.Connection != consolemodel.ConnectionDisconnected || m.input.Value() != "draft survives" ||
		m.input.LineInfo().CharOffset != 5 || len(actions.cancelCalls) != 1 || actions.cancelCalls[0] != 61 {
		t.Fatalf("expiry state auth=%v mode=%s reducer=%+v draft=%q cursor=%d cancels=%v cmd=%v",
			m.session.Authenticated, m.mode, state, m.input.Value(), m.input.LineInfo().CharOffset,
			actions.cancelCalls, cmd)
	}
}

func TestEstablishedDiagnosticFollowTerminationClearsDiagnostic(t *testing.T) {
	m := attachedModel(t, &fakeTUIActions{})
	epoch, err := m.reducer.BeginStream(consoleapi.ModeDiagnostic)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.reducer.ApplySnapshotForStream(epoch, consoleapi.AttachResponse{AgentID: "quote",
		Mode: consoleapi.ModeDiagnostic, WorkerInstanceID: "worker-current", Generation: 48,
		WorkerStatus: domain.WorkerStatusOnline, Diagnostic: &consoleapi.DiagnosticView{
			LastHeartbeatAt: fixedNow(), LeaseUntil: fixedNow().Add(time.Minute)}, SnapshotSequence: 1204}); err != nil {
		t.Fatal(err)
	}
	m.mode = consoleapi.ModeDiagnostic
	m.followID = 62
	m.followEpoch = epoch
	m.follow = make(chan tea.Msg)
	m, _ = updateModel(t, m, followDoneMsg{FollowID: 62, Mode: consoleapi.ModeDiagnostic,
		Err: errors.New("network stopped")})
	state := m.reducer.State()
	if m.mode != consoleapi.ModeNormal || state.Console.Mode != consoleapi.ModeNormal || state.Console.Diagnostic != nil ||
		!strings.Contains(m.timeline.String(), "Normal-safe") {
		t.Fatalf("terminated Diagnostic state mode=%s reducer=%+v timeline=%q", m.mode, state, m.timeline.String())
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

func TestHelpOverlayIncludesFocusedControlsAndTimelineNavigation(t *testing.T) {
	m := attachedModel(t, &fakeTUIActions{})
	m.width, m.height = 100, 40
	m.overlay = overlayHelp
	view := ansi.Strip(m.overlayView())
	for _, expected := range []string{"/steer <content>", "/cancel", "/diagnostic", "/normal",
		"PgUp/PgDown/Home/End scroll Timeline"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("help overlay missing %q: %s", expected, view)
		}
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
