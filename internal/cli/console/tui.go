package console

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	consoleclient "openagentx/internal/client/console"
	"openagentx/internal/consolemodel"
	"openagentx/internal/credentialstore"
	"openagentx/internal/domain"
)

const (
	foregroundUnavailable = "Foreground Takeover（规划中，暂不可用）"
	maxTimelineEntries    = 256
	maxTimelineBytes      = 64 << 10
	maxTimelineEntryBytes = 2 << 10
)

type screenKind int

const (
	screenMenu screenKind = iota
	screenLogin
	screenLoading
	screenSelector
	screenAttach
)

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayStatus
	overlayHelp
	overlayDiagnostic
	overlayTasks
	overlayConfirmation
	overlayError
)

type initialAttach struct {
	AgentID string
	Mode    string
}

type sessionResultMsg struct {
	Status sessionStatus
	Err    error
}

type loginResultMsg struct {
	Status sessionStatus
	Err    error
}

type logoutResultMsg struct {
	Message string
	Err     error
}

type prepareAttachResultMsg struct {
	Preparation attachPreparation
	Err         error
}

type bindResultMsg struct {
	PreparationID        uint64
	AgentID              string
	Mode                 string
	Session              sessionStatus
	ConfirmationRequired bool
	Err                  error
}

type followStartedMsg struct {
	FollowID uint64
	Mode     string
	Updates  <-chan tea.Msg
}

type followSnapshotMsg struct {
	FollowID uint64
	Snapshot consoleapi.AttachResponse
	Ack      chan<- error
}

type followEventMsg struct {
	FollowID uint64
	Event    openapi.JournalEventReadModel
	Ack      chan<- error
}

type followConnectionMsg struct {
	FollowID uint64
	State    consoleclient.FollowState
}

type followDoneMsg struct {
	FollowID uint64
	Mode     string
	Err      error
}

type followCancelResultMsg struct {
	FollowID uint64
	Found    bool
}

type taskOptionsResultMsg struct {
	Options []openapi.ConsoleTaskOption
	Err     error
}

type taskSnapshotPurpose string

const (
	taskSnapshotInitial taskSnapshotPurpose = "initial"
	taskSnapshotFocus   taskSnapshotPurpose = "focus"
	taskSnapshotStale   taskSnapshotPurpose = "stale"
)

type taskSnapshotResultMsg struct {
	TaskID   string
	Snapshot openapi.ConsoleTaskSnapshot
	Purpose  taskSnapshotPurpose
	Err      error
}

type tickMsg struct{ Now time.Time }
type statusReadinessMsg struct {
	Snapshot consoleapi.AttachResponse
	Err      error
}

type controlKind string

const (
	controlContinue     controlKind = "continue"
	controlAcceptResult controlKind = "accept-result"
	controlRejectResult controlKind = "reject-result"
	controlDispatch     controlKind = "dispatch"
	controlSteer        controlKind = "steer"
	controlCancel       controlKind = "cancel"
	controlApprove      controlKind = "approve"
	controlReject       controlKind = "reject"
)

type controlRequest struct {
	ParentTaskID    string
	RunID           string
	RunVersion      int64
	Kind            controlKind
	AgentID         string
	Intent          domain.TaskIntent
	TargetID        string
	ExpectedVersion int64
	Content         string
}

type controlResultMsg struct {
	Kind    controlKind
	Request controlRequest
	Outcome controlOutcome
	Err     error
}

type controlOutcome struct {
	Review         *domain.TaskReview
	TaskID         string
	TaskVersion    int64
	TaskStatus     domain.TaskStatus
	MessageID      string
	MessageVersion int64
	ApprovalID     string
	DecisionID     string
	Decision       domain.ApprovalDecisionValue
	DecisionState  domain.ApprovalDecisionState
	Sequence       int64
}

type timelineEntry struct {
	normal     string
	diagnostic string
}

type timelineBuffer struct {
	entries         []timelineEntry
	bytes           int
	diagnosticBytes int
}

func (b *timelineBuffer) Add(value string) {
	b.AddVariants(value, value)
}

func (b *timelineBuffer) AddVariants(normal, diagnostic string) {
	normal = boundedSafeMultiline(normal, maxTimelineEntryBytes)
	diagnostic = boundedSafeMultiline(diagnostic, maxTimelineEntryBytes)
	if normal == "" && diagnostic == "" {
		return
	}
	if diagnostic == "" {
		diagnostic = normal
	}
	if len(b.entries) > 0 {
		b.bytes++
		b.diagnosticBytes++
	}
	b.entries = append(b.entries, timelineEntry{normal: normal, diagnostic: diagnostic})
	b.bytes += len(normal)
	b.diagnosticBytes += len(diagnostic)
	for len(b.entries) > maxTimelineEntries || b.bytes > maxTimelineBytes || b.diagnosticBytes > maxTimelineBytes {
		b.bytes -= len(b.entries[0].normal)
		b.diagnosticBytes -= len(b.entries[0].diagnostic)
		b.entries = b.entries[1:]
		if len(b.entries) > 0 {
			b.bytes--
			b.diagnosticBytes--
		}
	}
}

func (b timelineBuffer) String() string { return b.StringForMode(consoleapi.ModeNormal) }
func (b timelineBuffer) StringForMode(mode string) string {
	values := make([]string, len(b.entries))
	for index, entry := range b.entries {
		values[index] = entry.normal
		if mode == consoleapi.ModeDiagnostic {
			values[index] = entry.diagnostic
		}
	}
	return strings.Join(values, "\n")
}
func (b timelineBuffer) Len() int { return len(b.entries) }
func (b timelineBuffer) Bytes() int {
	return b.bytes
}
func (b timelineBuffer) BytesForMode(mode string) int {
	if mode == consoleapi.ModeDiagnostic {
		return b.diagnosticBytes
	}
	return b.bytes
}

type agentItem struct{ option domain.ConsoleAgentOption }

func (i agentItem) Title() string { return i.option.AgentID + "  " + i.option.DisplayName }
func (i agentItem) Description() string {
	if i.option.ReadinessReason != "" {
		return i.option.ReadinessReason + " " + i.option.NextAction
	}
	active := "idle"
	if i.option.ActiveRunStatus != "" {
		active = string(i.option.ActiveRunStatus)
	}
	return fmt.Sprintf("%s  generation %d  %s", i.option.WorkerStatus, i.option.Generation, active)
}
func (i agentItem) FilterValue() string { return i.option.AgentID + " " + i.option.DisplayName }

type taskItem struct{ task consolemodel.TaskState }

func (i taskItem) Title() string { return boundedSafeText(i.task.TaskID, 256) }
func (i taskItem) Description() string {
	return boundedSafeText(fmt.Sprintf("%s  version %d  %s", i.task.Status, i.task.Version, i.task.Summary), 2048)
}
func (i taskItem) FilterValue() string {
	return boundedSafeText(i.task.TaskID+" "+i.task.Summary, 2304)
}

type tuiModel struct {
	statusReadiness *openapi.AgentReadiness
	actions         tuiActions
	now             func() time.Time

	screen       screenKind
	overlay      overlayKind
	direct       bool
	directAttach *initialAttach
	fatal        bool
	width        int
	height       int
	menuCursor   int
	busy         bool
	notice       string
	session      sessionStatus

	username   textinput.Model
	password   textinput.Model
	loginFocus int
	agents     list.Model
	tasks      list.Model

	preparationID uint64
	selectedAgent string
	mode          string
	confirmReturn screenKind

	reducer        *consolemodel.Reducer
	connection     consoleclient.ConnectionState
	input          textarea.Model
	viewport       viewport.Model
	timeline       timelineBuffer
	pending        bool
	pendingDraft   string
	taskLoading    bool
	follow         <-chan tea.Msg
	followID       uint64
	followEpoch    uint64
	switching      bool
	switchTarget   string
	switchFallback bool
}

func newTUIModel(actions tuiActions, now func() time.Time, direct *initialAttach) tuiModel {
	username := textinput.New()
	username.Prompt = "Username: "
	username.CharLimit = 128
	password := textinput.New()
	password.Prompt = "Password: "
	password.EchoMode = textinput.EchoPassword
	password.EchoCharacter = '*'
	password.CharLimit = 4096
	input := textarea.New()
	input.Prompt = "> "
	input.Placeholder = "/help"
	input.ShowLineNumbers = false
	input.SetHeight(3)
	input.SetWidth(76)
	input.CharLimit = 64 << 10
	input.Blur()
	m := tuiModel{actions: actions, now: now, screen: screenMenu, directAttach: direct,
		username: username, password: password, input: input, viewport: viewport.New(80, 12),
		connection: consoleclient.ConnectionDisconnected}
	if direct != nil {
		m.direct = true
		m.screen = screenLoading
		m.mode = direct.Mode
	}
	return m
}

func (m tuiModel) Init() tea.Cmd {
	commands := []tea.Cmd{tickCommand(m.now)}
	if m.directAttach != nil {
		commands = append(commands, m.actions.prepareAttachCmd(m.directAttach.Mode, m.directAttach.AgentID))
	} else {
		commands = append(commands, m.actions.sessionCmd())
	}
	return tea.Batch(commands...)
}

func tickCommand(now func() time.Time) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{Now: now()} })
}

func waitFollow(followID uint64, updates <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-updates
		if !ok {
			return followDoneMsg{FollowID: followID}
		}
		return msg
	}
}

func (m tuiModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil
	case tickMsg:
		commands := []tea.Cmd{tickCommand(m.now)}
		if m.session.Authenticated && !msg.Now.UTC().Before(m.session.ExpiresAt.UTC()) {
			socketPath := m.session.SocketPath
			m.session = sessionStatus{SocketPath: socketPath}
			m.connection = consoleclient.ConnectionDisconnected
			if m.reducer != nil {
				if m.switching || m.reducer.Snapshot().Mode == consoleapi.ModeDiagnostic {
					_ = m.reducer.AbortStream(m.reducer.StreamEpoch())
				} else {
					_ = m.reducer.SetConnection(m.reducer.StreamEpoch(), consolemodel.ConnectionDisconnected)
				}
			}
			m.mode = consoleapi.ModeNormal
			m.switching = false
			m.switchTarget = ""
			m.switchFallback = false
			m.pending = false
			if m.input.Value() == "" && m.pendingDraft != "" {
				m.input.SetValue(m.pendingDraft)
				m.input.CursorEnd()
			}
			m.pendingDraft = ""
			m.notice = "CLI session expired; run openagentx console login"
			if m.screen == screenAttach {
				m.timeline.Add(m.notice)
				m.syncTimeline(false)
			}
			if m.followID != 0 {
				commands = append(commands, m.actions.cancelFollowCmd(m.followID))
			}
		}
		return m, tea.Batch(commands...)
	case sessionResultMsg:
		m.busy = false
		m.session = msg.Status
		if msg.Err != nil {
			m.notice = safeErrorSummary(msg.Err)
		}
		return m, nil
	case loginResultMsg:
		m.busy = false
		m.password.SetValue("")
		if msg.Err != nil {
			m.notice = "Login failed: " + safeErrorSummary(msg.Err)
			return m, nil
		}
		m.username.SetValue("")
		m.session = msg.Status
		m.notice = "CLI session established"
		m.screen = screenMenu
		return m, nil
	case logoutResultMsg:
		m.busy = false
		if msg.Err != nil {
			m.notice = "Logout failed: " + safeErrorSummary(msg.Err)
			return m, nil
		}
		m.session = sessionStatus{SocketPath: m.session.SocketPath}
		m.notice = msg.Message
		return m, nil
	case prepareAttachResultMsg:
		m.busy = false
		if msg.Err != nil {
			return m.failAttach("Attach preparation failed: " + safeErrorSummary(msg.Err))
		}
		m.preparationID = msg.Preparation.ID
		m.mode = msg.Preparation.Mode
		m.session = msg.Preparation.Session
		if msg.Preparation.SelectedAgent != "" {
			m.selectedAgent = msg.Preparation.SelectedAgent
			m.busy = true
			return m, m.actions.bindCmd(m.preparationID, m.selectedAgent, false)
		}
		items := make([]list.Item, 0, len(msg.Preparation.Agents))
		for _, option := range msg.Preparation.Agents {
			items = append(items, agentItem{option: option})
		}
		m.agents = list.New(items, list.NewDefaultDelegate(), max(20, m.width), max(5, m.height-2))
		m.agents.Title = "Select Agent"
		m.agents.SetShowStatusBar(true)
		m.agents.SetFilteringEnabled(true)
		m.screen = screenSelector
		return m, nil
	case bindResultMsg:
		m.busy = false
		if msg.Err != nil {
			return m.failAttach("Workspace binding failed: " + safeErrorSummary(msg.Err))
		}
		if msg.ConfirmationRequired {
			m.overlay = overlayConfirmation
			m.confirmReturn = m.screen
			m.selectedAgent = msg.AgentID
			return m, nil
		}
		m.preparationID = msg.PreparationID
		m.selectedAgent = msg.AgentID
		m.mode = msg.Mode
		m.session = msg.Session
		m.screen = screenAttach
		m.overlay = overlayNone
		m.connection = consoleclient.ConnectionConnecting
		m.input.Focus()
		m.timeline.Add("Attaching to " + m.selectedAgent)
		m.syncTimeline(true)
		return m, m.actions.startFollowCmd(m.preparationID, m.selectedAgent)
	case followStartedMsg:
		if m.switching && msg.Mode != m.switchTarget {
			return m, m.actions.cancelFollowCmd(msg.FollowID)
		}
		m.follow = msg.Updates
		m.followID = msg.FollowID
		m.followEpoch = 1
		if m.reducer != nil {
			m.followEpoch = m.reducer.StreamEpoch()
		}
		return m, waitFollow(m.followID, m.follow)
	case followCancelResultMsg:
		return m, nil
	case followConnectionMsg:
		if msg.FollowID != m.followID {
			return m, nil
		}
		if m.reducer != nil && m.followEpoch != m.reducer.StreamEpoch() {
			return m, waitFollow(m.followID, m.follow)
		}
		if m.session.Authenticated {
			m.connection = msg.State.State
		} else {
			m.connection = consoleclient.ConnectionDisconnected
		}
		if m.reducer != nil {
			if err := m.reducer.SetConnection(m.reducer.StreamEpoch(), reducerConnectionState(m.connection)); err != nil {
				m.connection = consoleclient.ConnectionDisconnected
				m.overlay = overlayError
				m.notice = "Connection state rejected: " + safeErrorSummary(err)
			}
		}
		command := waitFollow(m.followID, m.follow)
		if msg.State.State == consoleclient.ConnectionRetentionReattach {
			m.timeline.Add("Event history expired; refreshing the authoritative snapshot")
			m.syncTimeline(false)
		}
		return m, command
	case followSnapshotMsg:
		if msg.FollowID != m.followID || m.reducer != nil && m.followEpoch != m.reducer.StreamEpoch() {
			ackFollow(msg.Ack, context.Canceled)
			if msg.FollowID == m.followID {
				return m, waitFollow(m.followID, m.follow)
			}
			return m, nil
		}
		if !m.session.Authenticated {
			ackFollow(msg.Ack, errLoginRequired)
			return m, waitFollow(m.followID, m.follow)
		}
		var err error
		if m.reducer == nil {
			m.reducer, err = consolemodel.New(msg.Snapshot)
			m.followEpoch = m.reducerEpoch()
		} else {
			err = m.reducer.ApplySnapshotForStream(m.followEpoch, msg.Snapshot)
		}
		if err != nil {
			ackFollow(msg.Ack, err)
			if m.switching {
				return m.failModeSwitch("Mode switch snapshot rejected: "+safeErrorSummary(err), true)
			}
			m.connection = consoleclient.ConnectionDisconnected
			if m.reducer != nil {
				_ = m.reducer.SetConnection(m.reducer.StreamEpoch(), consolemodel.ConnectionDisconnected)
			}
			m.overlay = overlayError
			m.notice = "Snapshot rejected: " + safeErrorSummary(err)
			return m, waitFollow(m.followID, m.follow)
		}
		ackFollow(msg.Ack, nil)
		m.mode = msg.Snapshot.Mode
		if m.mode == "" {
			m.mode = consoleapi.ModeNormal
		}
		if m.switching {
			m.timeline.Add("Console mode switched to " + m.mode)
			m.switching = false
			m.switchTarget = ""
			m.switchFallback = false
			if m.mode == consoleapi.ModeDiagnostic {
				m.overlay = overlayDiagnostic
			} else if m.overlay == overlayDiagnostic {
				m.overlay = overlayNone
			}
		} else {
			m.timeline.Add(fmt.Sprintf("Snapshot applied at cursor %d", m.reducer.Cursor()))
		}
		m.syncTimeline(false)
		commands := []tea.Cmd{waitFollow(m.followID, m.follow)}
		state := m.reducer.State()
		if state.FocusedTask != nil && state.FocusedTask.Detail == nil && !m.taskLoading {
			m.taskLoading = true
			commands = append(commands, m.actions.taskSnapshotCmd(m.preparationID, m.selectedAgent,
				state.FocusedTask.TaskID, taskSnapshotInitial))
		}
		return m, tea.Batch(commands...)
	case followEventMsg:
		if msg.FollowID != m.followID || m.reducer != nil && m.followEpoch != m.reducer.StreamEpoch() {
			ackFollow(msg.Ack, context.Canceled)
			if msg.FollowID == m.followID {
				return m, waitFollow(m.followID, m.follow)
			}
			return m, nil
		}
		if !m.session.Authenticated {
			ackFollow(msg.Ack, errLoginRequired)
			return m, waitFollow(m.followID, m.follow)
		}
		if m.reducer == nil {
			err := fmt.Errorf("Event arrived before the authoritative snapshot")
			ackFollow(msg.Ack, err)
			m.overlay = overlayError
			m.notice = err.Error()
			return m, waitFollow(m.followID, m.follow)
		}
		result, err := m.reducer.ApplyForStream(m.followEpoch, msg.Event)
		if err != nil {
			ackFollow(msg.Ack, err)
			m.connection = consoleclient.ConnectionDisconnected
			_ = m.reducer.SetConnection(m.reducer.StreamEpoch(), consolemodel.ConnectionDisconnected)
			m.overlay = overlayError
			m.notice = "Event rejected: " + safeErrorSummary(err)
			return m, waitFollow(m.followID, m.follow)
		}
		ackFollow(msg.Ack, nil)
		if result.Timeline != nil {
			state := m.reducer.State()
			if len(state.Timeline) > 0 {
				item := state.Timeline[len(state.Timeline)-1]
				m.timeline.AddVariants(timelineItemSummary(item, consoleapi.ModeNormal),
					timelineItemSummary(item, consoleapi.ModeDiagnostic))
			}
			m.syncTimeline(false)
		}
		return m, waitFollow(m.followID, m.follow)
	case followDoneMsg:
		if msg.FollowID == 0 && m.switching {
			return m.failModeSwitch("Mode switch failed: "+safeErrorSummary(msg.Err), false)
		}
		if msg.FollowID != m.followID {
			return m, nil
		}
		completedEpoch := m.followEpoch
		m.follow = nil
		m.followID = 0
		m.connection = consoleclient.ConnectionDisconnected
		if m.switching && m.reducer != nil && completedEpoch != m.reducer.StreamEpoch() {
			return m, m.actions.startFollowModeCmd(m.preparationID, m.selectedAgent, m.switchTarget)
		}
		if m.switching {
			return m.failModeSwitch("Mode switch failed: "+safeErrorSummary(msg.Err), false)
		}
		if m.reducer != nil {
			if m.reducer.Snapshot().Mode == consoleapi.ModeDiagnostic {
				_ = m.reducer.AbortStream(m.reducer.StreamEpoch())
				m.mode = consoleapi.ModeNormal
				m.timeline.Add("Diagnostic Follow stopped; using Normal-safe state")
				m.syncTimeline(false)
			} else {
				_ = m.reducer.SetConnection(m.reducer.StreamEpoch(), consolemodel.ConnectionDisconnected)
			}
		}
		if msg.Err != nil && !errors.Is(msg.Err, context.Canceled) {
			m.timeline.Add("Connection stopped: " + safeErrorSummary(msg.Err))
			m.syncTimeline(false)
		}
		return m, nil
	case taskOptionsResultMsg:
		m.taskLoading = false
		if msg.Err != nil {
			m.overlay = overlayError
			m.notice = "Task list failed: " + safeErrorSummary(msg.Err)
			return m, nil
		}
		if m.reducer == nil {
			m.overlay = overlayError
			m.notice = "Task list rejected: Console snapshot is unavailable"
			return m, nil
		}
		if err := m.reducer.ReplaceTaskOptions(msg.Options); err != nil {
			m.connection = consoleclient.ConnectionDisconnected
			_ = m.reducer.SetConnection(m.reducer.StreamEpoch(), consolemodel.ConnectionDisconnected)
			m.overlay = overlayError
			m.notice = "Task list rejected: " + safeErrorSummary(err)
			return m, nil
		}
		m.setTaskItems(m.reducer.State())
		m.overlay = overlayTasks
		return m, nil
	case taskSnapshotResultMsg:
		m.taskLoading = false
		if msg.Err != nil {
			if msg.Purpose == taskSnapshotStale {
				m.timeline.Add("Task refresh after stale CAS failed: " + safeErrorSummary(msg.Err))
				m.syncTimeline(false)
				return m, nil
			}
			if msg.Purpose == taskSnapshotInitial {
				m.timeline.Add("Suggested Task detail failed: " + safeErrorSummary(msg.Err))
				m.syncTimeline(false)
				return m, nil
			}
			m.overlay = overlayError
			m.notice = "Task detail failed: " + safeErrorSummary(msg.Err)
			return m, nil
		}
		if m.reducer == nil {
			m.overlay = overlayError
			m.notice = "Task detail rejected: Console snapshot is unavailable"
			return m, nil
		}
		source := consolemodel.FocusAttachSuggestion
		if msg.Purpose == taskSnapshotFocus || msg.Purpose == taskSnapshotStale {
			source = consolemodel.FocusManual
		}
		if err := m.reducer.ApplyTaskSnapshot(msg.Snapshot, source); err != nil {
			m.connection = consoleclient.ConnectionDisconnected
			_ = m.reducer.SetConnection(m.reducer.StreamEpoch(), consolemodel.ConnectionDisconnected)
			m.overlay = overlayError
			m.notice = "Task detail rejected: " + safeErrorSummary(err)
			return m, nil
		}
		m.appendFocusedTaskSummary(msg.Purpose)
		if msg.Purpose == taskSnapshotStale {
			m.overlay = overlayStatus
		} else if msg.Purpose == taskSnapshotFocus {
			m.overlay = overlayNone
		}
		m.input.Focus()
		m.syncTimeline(msg.Purpose == taskSnapshotFocus || msg.Purpose == taskSnapshotStale)
		return m, nil
	case statusReadinessMsg:
		if msg.Err == nil && msg.Snapshot.AgentID == m.selectedAgent {
			m.statusReadiness = msg.Snapshot.Readiness
		}
		return m, nil
	case controlResultMsg:
		m.pending = false
		if msg.Err != nil {
			m.timeline.Add(string(msg.Kind) + " failed: " + safeErrorSummary(msg.Err))
			if m.input.Value() == "" && m.pendingDraft != "" {
				m.input.SetValue(m.pendingDraft)
				m.input.CursorEnd()
			}
			m.pendingDraft = ""
			m.syncTimeline(false)
			if staleControlError(msg.Err) && msg.Request.TargetID != "" {
				m.taskLoading = true
				m.timeline.Add("Task version is stale; refreshing authoritative state without retrying the command")
				m.syncTimeline(false)
				return m, m.actions.taskSnapshotCmd(m.preparationID, m.selectedAgent,
					msg.Request.TargetID, taskSnapshotStale)
			}
		} else {
			if m.reducer != nil && msg.Outcome.TaskID != "" {
				applyErr := m.reducer.ApplyControlTask(consolemodel.ControlTaskUpdate{
					AgentID: m.selectedAgent, TaskID: msg.Outcome.TaskID, Version: msg.Outcome.TaskVersion,
					Status: msg.Outcome.TaskStatus, Focus: msg.Kind == controlDispatch || msg.Kind == controlContinue,
				})
				if applyErr != nil {
					m.connection = consoleclient.ConnectionDisconnected
					_ = m.reducer.SetConnection(m.reducer.StreamEpoch(), consolemodel.ConnectionDisconnected)
					m.timeline.Add(string(msg.Kind) + " state rejected: " + safeErrorSummary(applyErr))
					if m.input.Value() == "" && m.pendingDraft != "" {
						m.input.SetValue(m.pendingDraft)
						m.input.CursorEnd()
					}
					m.pendingDraft = ""
					m.syncTimeline(false)
					return m, nil
				}
			}
			m.timeline.Add(msg.Outcome.summary(msg.Kind))
			m.pendingDraft = ""
			if msg.Outcome.Review != nil {
				m.taskLoading = true
				m.syncTimeline(false)
				return m, m.actions.taskSnapshotCmd(m.preparationID, m.selectedAgent, msg.Outcome.Review.TaskID, taskSnapshotFocus)
			}
		}
		m.syncTimeline(false)
		return m, nil
	}

	key, ok := message.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if key.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.overlay != overlayNone {
		return m.updateOverlay(key)
	}
	switch m.screen {
	case screenMenu:
		return m.updateMenu(key)
	case screenLogin:
		return m.updateLogin(key)
	case screenSelector:
		return m.updateSelector(key)
	case screenAttach:
		return m.updateAttach(key)
	default:
		return m, nil
	}
}

func reducerConnectionState(state consoleclient.ConnectionState) consolemodel.ConnectionState {
	switch state {
	case consoleclient.ConnectionConnecting:
		return consolemodel.ConnectionConnecting
	case consoleclient.ConnectionConnected:
		return consolemodel.ConnectionConnected
	case consoleclient.ConnectionReconnecting:
		return consolemodel.ConnectionReconnecting
	case consoleclient.ConnectionRetentionReattach:
		return consolemodel.ConnectionRetentionReattach
	default:
		return consolemodel.ConnectionDisconnected
	}
}

func (m tuiModel) updateMenu(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.menuItems()
	switch key.String() {
	case "up", "k":
		if m.menuCursor > 0 {
			m.menuCursor--
		}
	case "down", "j":
		if m.menuCursor+1 < len(items) {
			m.menuCursor++
		}
	case "enter":
		switch m.menuCursor {
		case 0:
			return m.startPrepare(consoleapi.ModeNormal, "")
		case 1:
			return m.startPrepare(consoleapi.ModeDiagnostic, "")
		case 2:
			m.screen = screenLogin
			m.loginFocus = 0
			m.username.SetValue(m.session.Username)
			m.username.Focus()
			m.password.Blur()
			m.notice = ""
		case 3:
			m.busy = true
			return m, m.actions.logoutCmd()
		case 4:
			m.notice = foregroundUnavailable
		case 5:
			return m, tea.Quit
		}
	case "q", "esc":
		return m, tea.Quit
	}
	return m, nil
}

func (m tuiModel) startPrepare(mode, agentID string) (tea.Model, tea.Cmd) {
	m.screen = screenLoading
	m.busy = true
	m.notice = ""
	m.mode = mode
	return m, m.actions.prepareAttachCmd(mode, agentID)
}

func (m tuiModel) updateLogin(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	switch key.String() {
	case "esc":
		m.password.SetValue("")
		m.username.SetValue("")
		m.screen = screenMenu
		return m, nil
	case "tab", "shift+tab":
		m.loginFocus = 1 - m.loginFocus
		m.focusLoginField()
		return m, nil
	case "enter":
		if m.loginFocus == 0 {
			m.loginFocus = 1
			m.focusLoginField()
			return m, nil
		}
		username := strings.TrimSpace(m.username.Value())
		password := m.password.Value()
		if username == "" || password == "" {
			m.notice = "Username and password are required"
			return m, nil
		}
		m.busy = true
		return m, m.actions.loginCmd(username, password)
	}
	var cmd tea.Cmd
	if m.loginFocus == 0 {
		m.username, cmd = m.username.Update(key)
	} else {
		m.password, cmd = m.password.Update(key)
	}
	return m, cmd
}

func (m *tuiModel) focusLoginField() {
	if m.loginFocus == 0 {
		m.username.Focus()
		m.password.Blur()
	} else {
		m.username.Blur()
		m.password.Focus()
	}
}

func (m tuiModel) updateSelector(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "esc" && m.agents.FilterState() != list.Filtering {
		if m.direct {
			return m, tea.Quit
		}
		m.screen = screenMenu
		return m, nil
	}
	if key.String() == "enter" && m.agents.FilterState() != list.Filtering {
		item, ok := m.agents.SelectedItem().(agentItem)
		if !ok {
			m.notice = "Select an Agent"
			return m, nil
		}
		m.selectedAgent = item.option.AgentID
		m.busy = true
		return m, m.actions.bindCmd(m.preparationID, m.selectedAgent, false)
	}
	var cmd tea.Cmd
	m.agents, cmd = m.agents.Update(key)
	return m, cmd
}

func (m tuiModel) updateOverlay(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.overlay == overlayTasks {
		if m.taskLoading {
			return m, nil
		}
		if key.String() == "esc" && m.tasks.FilterState() != list.Filtering {
			m.overlay = overlayNone
			m.input.Focus()
			return m, nil
		}
		if key.String() == "enter" && m.tasks.FilterState() != list.Filtering {
			item, ok := m.tasks.SelectedItem().(taskItem)
			if !ok {
				m.notice = "Select a Task"
				return m, nil
			}
			m.taskLoading = true
			return m, m.actions.taskSnapshotCmd(m.preparationID, m.selectedAgent,
				item.task.TaskID, taskSnapshotFocus)
		}
		var cmd tea.Cmd
		m.tasks, cmd = m.tasks.Update(key)
		return m, cmd
	}
	if m.overlay == overlayConfirmation {
		switch key.String() {
		case "y", "Y":
			m.overlay = overlayNone
			m.busy = true
			return m, m.actions.bindCmd(m.preparationID, m.selectedAgent, true)
		case "n", "N", "esc":
			m.overlay = overlayNone
			if m.direct {
				return m, tea.Quit
			}
			if m.confirmReturn == screenSelector {
				m.screen = screenSelector
			} else {
				m.screen = screenMenu
			}
			m.notice = "Rebinding canceled; tmux was not changed"
		}
		return m, nil
	}
	if key.String() == "esc" || key.String() == "enter" || key.String() == "q" {
		if m.overlay == overlayError && m.fatal {
			return m, tea.Quit
		}
		m.overlay = overlayNone
		if m.screen == screenAttach {
			m.input.Focus()
		}
	}
	return m, nil
}

func (m tuiModel) updateAttach(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "pgup", "pgdown", "home", "end":
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(key)
		return m, cmd
	case "enter":
		line := strings.TrimSpace(m.input.Value())
		if line == "" {
			return m, nil
		}
		return m.executeInput(line)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(key)
	return m, cmd
}

func (m tuiModel) executeInput(line string) (tea.Model, tea.Cmd) {
	state := consolemodel.State{}
	if m.reducer != nil {
		state = m.reducer.State()
	}
	command, err := parseControlInput(line, m.selectedAgent, state)
	switch {
	case errors.Is(err, errQuitCommand):
		return m, tea.Quit
	case errors.Is(err, errStatusCommand):
		m.input.Reset()
		m.overlay = overlayStatus
		m.statusReadiness = nil
		if actions, ok := m.actions.(interface{ statusReadinessCmd(uint64, string) tea.Cmd }); ok {
			return m, actions.statusReadinessCmd(m.preparationID, m.selectedAgent)
		}
		return m, nil
	case errors.Is(err, errHelpCommand):
		m.input.Reset()
		m.overlay = overlayHelp
		return m, nil
	case errors.Is(err, errDiagnosticCommand):
		return m.beginModeSwitch(consoleapi.ModeDiagnostic)
	case errors.Is(err, errNormalCommand):
		return m.beginModeSwitch(consoleapi.ModeNormal)
	case errors.Is(err, errTasksCommand):
		if m.switching {
			m.timeline.Add("Task list disabled while Console mode is switching")
			m.syncTimeline(true)
			return m, nil
		}
		if !m.session.Authenticated {
			m.timeline.Add("Task list disabled because the CLI session expired; run openagentx console login")
			m.syncTimeline(true)
			return m, nil
		}
		if m.connection != consoleclient.ConnectionConnected || m.reducer == nil {
			m.timeline.Add("Task list disabled while Console is disconnected")
			m.syncTimeline(true)
			return m, nil
		}
		if m.taskLoading {
			m.timeline.Add("A Task list or detail request is already pending")
			m.syncTimeline(true)
			return m, nil
		}
		m.input.Reset()
		m.taskLoading = true
		m.overlay = overlayTasks
		return m, m.actions.taskOptionsCmd(m.preparationID, m.selectedAgent)
	case errors.Is(err, errForegroundCommand):
		m.timeline.Add(foregroundUnavailable)
		m.input.Reset()
		m.syncTimeline(true)
		return m, nil
	case err != nil:
		m.timeline.Add("Command rejected: " + safeErrorSummary(err))
		m.syncTimeline(true)
		return m, nil
	}
	if !m.session.Authenticated {
		m.timeline.Add("Command disabled because the CLI session expired; run openagentx console login")
		m.syncTimeline(true)
		return m, nil
	}
	if m.switching {
		m.timeline.Add("Command disabled while Console mode is switching")
		m.syncTimeline(true)
		return m, nil
	}
	if m.connection != consoleclient.ConnectionConnected || m.reducer == nil {
		m.timeline.Add("Command disabled while Console is disconnected")
		m.syncTimeline(true)
		return m, nil
	}
	if m.taskLoading {
		m.timeline.Add("Command disabled while authoritative Task state is loading")
		m.syncTimeline(true)
		return m, nil
	}
	if m.pending {
		m.timeline.Add("A Console command is already pending")
		m.syncTimeline(true)
		return m, nil
	}
	m.pending = true
	m.pendingDraft = line
	m.input.Reset()
	m.timeline.Add(string(command.Kind) + " pending")
	m.syncTimeline(true)
	return m, m.actions.controlCmd(m.preparationID, command)
}

func (m tuiModel) beginModeSwitch(target string) (tea.Model, tea.Cmd) {
	if m.switching {
		m.timeline.Add("Console mode switch is already in progress")
		m.syncTimeline(true)
		return m, nil
	}
	if !m.session.Authenticated {
		m.timeline.Add("Mode switch disabled because the CLI session expired; run openagentx console login")
		m.syncTimeline(true)
		return m, nil
	}
	if m.connection != consoleclient.ConnectionConnected || m.reducer == nil {
		m.timeline.Add("Mode switch disabled while Console is disconnected")
		m.syncTimeline(true)
		return m, nil
	}
	if m.pending {
		m.timeline.Add("Mode switch disabled while a Console operation is pending")
		m.syncTimeline(true)
		return m, nil
	}
	current := m.reducer.Snapshot().Mode
	if current == "" {
		current = consoleapi.ModeNormal
	}
	if current == target {
		m.input.Reset()
		if target == consoleapi.ModeDiagnostic {
			m.overlay = overlayDiagnostic
		} else {
			m.timeline.Add("Console is already in Normal mode")
			m.syncTimeline(true)
		}
		return m, nil
	}
	if _, err := m.reducer.BeginStream(target); err != nil {
		m.timeline.Add("Mode switch rejected: " + safeErrorSummary(err))
		m.syncTimeline(true)
		return m, nil
	}
	m.switching = true
	m.switchTarget = target
	m.switchFallback = false
	m.mode = consoleapi.ModeNormal
	m.overlay = overlayNone
	m.input.Reset()
	m.timeline.Add("Switching Console mode to " + target)
	m.syncTimeline(false)
	if m.followID == 0 {
		return m, m.actions.startFollowModeCmd(m.preparationID, m.selectedAgent, target)
	}
	return m, m.actions.cancelFollowCmd(m.followID)
}

func (m tuiModel) failModeSwitch(message string, followActive bool) (tea.Model, tea.Cmd) {
	failedTarget := m.switchTarget
	wasFallback := m.switchFallback
	if m.reducer != nil {
		_ = m.reducer.AbortStream(m.reducer.StreamEpoch())
	}
	m.mode = consoleapi.ModeNormal
	m.connection = consoleclient.ConnectionDisconnected
	m.overlay = overlayNone
	if strings.TrimSpace(message) == "Mode switch failed:" {
		message = "Mode switch failed"
	}
	m.timeline.Add(message + "; using Normal-safe state")
	m.syncTimeline(false)

	if failedTarget == consoleapi.ModeDiagnostic && !wasFallback && m.session.Authenticated && m.reducer != nil {
		if _, err := m.reducer.BeginStream(consoleapi.ModeNormal); err == nil {
			m.switching = true
			m.switchTarget = consoleapi.ModeNormal
			m.switchFallback = true
			m.timeline.Add("Restoring Normal Console Follow")
			m.syncTimeline(false)
			if followActive && m.followID != 0 {
				return m, m.actions.cancelFollowCmd(m.followID)
			}
			return m, m.actions.startFollowModeCmd(m.preparationID, m.selectedAgent, consoleapi.ModeNormal)
		}
	}

	m.switching = false
	m.switchTarget = ""
	m.switchFallback = false
	if followActive && m.followID != 0 {
		return m, tea.Batch(m.actions.cancelFollowCmd(m.followID), waitFollow(m.followID, m.follow))
	}
	return m, nil
}

func (m tuiModel) reducerEpoch() uint64 {
	if m.reducer == nil {
		return 1
	}
	return m.reducer.StreamEpoch()
}

var (
	errQuitCommand       = errors.New("quit")
	errStatusCommand     = errors.New("status")
	errHelpCommand       = errors.New("help")
	errDiagnosticCommand = errors.New("diagnostic")
	errNormalCommand     = errors.New("normal")
	errTasksCommand      = errors.New("tasks")
	errForegroundCommand = errors.New("foreground")
)

func parseControlInput(line, agentID string, state consolemodel.State) (controlRequest, error) {
	command, remainder, _ := strings.Cut(strings.TrimSpace(line), " ")
	switch command {
	case "/quit":
		return controlRequest{}, errQuitCommand
	case "/status":
		return controlRequest{}, errStatusCommand
	case "/help":
		return controlRequest{}, errHelpCommand
	case "/diagnostic":
		if strings.TrimSpace(remainder) != "" {
			return controlRequest{}, fmt.Errorf("usage: /diagnostic")
		}
		return controlRequest{}, errDiagnosticCommand
	case "/normal":
		if strings.TrimSpace(remainder) != "" {
			return controlRequest{}, fmt.Errorf("usage: /normal")
		}
		return controlRequest{}, errNormalCommand
	case "/tasks":
		if strings.TrimSpace(remainder) != "" {
			return controlRequest{}, fmt.Errorf("usage: /tasks")
		}
		return controlRequest{}, errTasksCommand
	case "/foreground":
		return controlRequest{}, errForegroundCommand
	case "/dispatch":
		return parseDispatchRequest(remainder, agentID)
	case "/continue":
		task := state.FocusedTask
		if task == nil || !taskTerminal(task.Status) || task.Detail == nil {
			return controlRequest{}, userVisibleError{message: "请用 /tasks 选择已结束的工作并加载结果后继续"}
		}
		body := strings.TrimSpace(remainder)
		if !strings.HasPrefix(body, "--intent ") && task.Intent.Valid() {
			body = "--intent " + string(task.Intent) + " " + body
		}
		request, err := parseDispatchRequest(body, agentID)
		if err != nil {
			return controlRequest{}, userVisibleError{message: "用法: /continue [--intent query|mutation] <继续内容>"}
		}
		request.Kind = controlContinue
		request.ParentTaskID = task.TaskID
		return request, nil
	case "/accept", "/result-reject":
		task := state.FocusedTask
		if task == nil || !taskTerminal(task.Status) || task.Detail == nil || task.Detail.Version != task.Version || task.LatestRun == nil || task.LatestRun.Status != domain.RunAttemptSucceeded {
			return controlRequest{}, userVisibleError{message: "请用 /tasks 选择已有确认完成 Run 的工作；执行未确认停止时不能验收"}
		}
		if len(remainder) > 4096 {
			return controlRequest{}, userVisibleError{message: "验收备注不能超过4096字节"}
		}
		kind := controlAcceptResult
		if command == "/result-reject" {
			kind = controlRejectResult
		}
		return controlRequest{Kind: kind, AgentID: agentID, TargetID: task.TaskID, ExpectedVersion: task.Version, RunID: task.LatestRun.ID, RunVersion: task.LatestRun.Version, Content: strings.TrimSpace(remainder)}, nil
	case "/steer":
		return parseSteerRequest(strings.TrimSpace(remainder), agentID, state)
	case "/cancel":
		return parseCancelRequest(strings.TrimSpace(remainder), agentID, state)
	case "/approve", "/reject":
		parts := strings.Fields(remainder)
		if len(parts) != 2 {
			return controlRequest{}, fmt.Errorf("usage: %s <id> <expected-version>", command)
		}
		version, err := positiveVersion(parts[1])
		if err != nil {
			return controlRequest{}, err
		}
		kind := controlCancel
		if command == "/approve" {
			kind = controlApprove
		} else if command == "/reject" {
			kind = controlReject
		}
		return controlRequest{Kind: kind, AgentID: agentID, TargetID: parts[0], ExpectedVersion: version}, nil
	default:
		return controlRequest{}, fmt.Errorf("unknown command; use /help")
	}
}

func parseDispatchRequest(remainder, agentID string) (controlRequest, error) {
	const usage = "usage: /dispatch [--intent query|mutation] [--] <content>"
	content := strings.TrimSpace(remainder)
	intent := domain.TaskIntentMutation
	first, rest, _ := strings.Cut(content, " ")
	if first == "--intent" {
		value, body, _ := strings.Cut(strings.TrimSpace(rest), " ")
		intent = domain.TaskIntent(value)
		if !intent.Valid() {
			return controlRequest{}, fmt.Errorf("%s", usage)
		}
		content = strings.TrimSpace(body)
		first, rest, _ = strings.Cut(content, " ")
	}
	if first == "--" {
		content = strings.TrimSpace(rest)
	} else if strings.HasPrefix(first, "--") {
		return controlRequest{}, fmt.Errorf("%s", usage)
	}
	if content == "" {
		return controlRequest{}, fmt.Errorf("%s", usage)
	}
	return controlRequest{Kind: controlDispatch, AgentID: agentID, Intent: intent, Content: content}, nil
}

func parseSteerRequest(remainder, agentID string, state consolemodel.State) (controlRequest, error) {
	if remainder == "" {
		return controlRequest{}, fmt.Errorf("usage: /steer <content> or /steer --task <task-id> --version <n> <content>")
	}
	fields := strings.Fields(remainder)
	if fields[0] == "--task" {
		if len(fields) < 5 || fields[2] != "--version" {
			return controlRequest{}, fmt.Errorf("usage: /steer --task <task-id> --version <n> <content>")
		}
		version, err := positiveVersion(fields[3])
		if err != nil {
			return controlRequest{}, err
		}
		if err := validateExplicitTaskControl(state, fields[1]); err != nil {
			return controlRequest{}, err
		}
		return controlRequest{Kind: controlSteer, AgentID: agentID, TargetID: fields[1],
			ExpectedVersion: version, Content: strings.Join(fields[4:], " ")}, nil
	}
	if len(fields) >= 3 && (knownTask(state, fields[0]) || strings.HasPrefix(fields[0], "task-")) {
		version, err := positiveVersion(fields[1])
		if err != nil {
			return controlRequest{}, err
		}
		if err := validateExplicitTaskControl(state, fields[0]); err != nil {
			return controlRequest{}, err
		}
		return controlRequest{Kind: controlSteer, AgentID: agentID, TargetID: fields[0],
			ExpectedVersion: version, Content: strings.Join(fields[2:], " ")}, nil
	}
	task, err := focusedTaskForControl(state)
	if err != nil {
		return controlRequest{}, err
	}
	return controlRequest{Kind: controlSteer, AgentID: agentID, TargetID: task.TaskID,
		ExpectedVersion: task.Version, Content: remainder}, nil
}

func parseCancelRequest(remainder, agentID string, state consolemodel.State) (controlRequest, error) {
	if remainder == "" {
		task, err := focusedTaskForControl(state)
		if err != nil {
			return controlRequest{}, err
		}
		return controlRequest{Kind: controlCancel, AgentID: agentID, TargetID: task.TaskID,
			ExpectedVersion: task.Version}, nil
	}
	fields := strings.Fields(remainder)
	if len(fields) == 4 && fields[0] == "--task" && fields[2] == "--version" {
		version, err := positiveVersion(fields[3])
		if err != nil {
			return controlRequest{}, err
		}
		if err := validateExplicitTaskControl(state, fields[1]); err != nil {
			return controlRequest{}, err
		}
		return controlRequest{Kind: controlCancel, AgentID: agentID, TargetID: fields[1],
			ExpectedVersion: version}, nil
	}
	if len(fields) == 2 {
		version, err := positiveVersion(fields[1])
		if err != nil {
			return controlRequest{}, err
		}
		if err := validateExplicitTaskControl(state, fields[0]); err != nil {
			return controlRequest{}, err
		}
		return controlRequest{Kind: controlCancel, AgentID: agentID, TargetID: fields[0],
			ExpectedVersion: version}, nil
	}
	return controlRequest{}, fmt.Errorf("usage: /cancel or /cancel --task <task-id> --version <n>")
}

func focusedTaskForControl(state consolemodel.State) (*consolemodel.TaskState, error) {
	if state.FocusedTask == nil {
		return nil, userVisibleError{message: "no focused Task; use /tasks to select one"}
	}
	if state.FocusedTask.Version <= 0 || taskTerminal(state.FocusedTask.Status) {
		return nil, userVisibleError{message: "focused Task is terminal or has no usable version"}
	}
	return state.FocusedTask, nil
}

func validateExplicitTaskControl(state consolemodel.State, taskID string) error {
	if err := domain.ValidateOpaqueID("task_id", taskID); err != nil {
		return fmt.Errorf("invalid Task ID")
	}
	for _, task := range append(append([]consolemodel.TaskState(nil), state.ActiveTasks...), state.RecentTasks...) {
		if task.TaskID == taskID && taskTerminal(task.Status) {
			return userVisibleError{message: "selected Task is terminal"}
		}
	}
	return nil
}

func knownTask(state consolemodel.State, taskID string) bool {
	if state.FocusedTask != nil && state.FocusedTask.TaskID == taskID {
		return true
	}
	for _, task := range state.ActiveTasks {
		if task.TaskID == taskID {
			return true
		}
	}
	for _, task := range state.RecentTasks {
		if task.TaskID == taskID {
			return true
		}
	}
	return false
}

func taskTerminal(status domain.TaskStatus) bool {
	return status == domain.TaskStatusSucceeded || status == domain.TaskStatusFailed ||
		status == domain.TaskStatusCanceled || status == domain.TaskStatusUncertain
}

func positiveVersion(value string) (int64, error) {
	version, err := strconv.ParseInt(value, 10, 64)
	if err != nil || version <= 0 {
		return 0, fmt.Errorf("expected version must be a positive integer")
	}
	return version, nil
}

func (m *tuiModel) resize(width, height int) {
	m.width = max(width, 1)
	m.height = max(height, 1)
	contentWidth := max(1, m.width-2)
	inputHeight := 3
	if m.height < 8 {
		inputHeight = 1
	}
	m.input.SetWidth(contentWidth)
	m.input.SetHeight(inputHeight)
	m.viewport.Width = contentWidth
	m.viewport.Height = max(1, m.height-2-inputHeight)
	if m.screen == screenSelector {
		m.agents.SetSize(m.width, m.height)
	}
	if m.overlay == overlayTasks {
		m.tasks.SetSize(m.width, m.height)
	}
}

func (m *tuiModel) syncTimeline(forceBottom bool) {
	wasBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.timeline.StringForMode(m.displayMode()))
	if forceBottom || wasBottom {
		m.viewport.GotoBottom()
	}
}

func (m tuiModel) displayMode() string {
	if m.switching {
		return consoleapi.ModeNormal
	}
	if m.mode == consoleapi.ModeDiagnostic {
		return consoleapi.ModeDiagnostic
	}
	return consoleapi.ModeNormal
}

func (m *tuiModel) setTaskItems(state consolemodel.State) {
	items := make([]list.Item, 0, len(state.ActiveTasks)+len(state.RecentTasks))
	for _, task := range state.ActiveTasks {
		items = append(items, taskItem{task: task})
	}
	for _, task := range state.RecentTasks {
		items = append(items, taskItem{task: task})
	}
	m.tasks = list.New(items, list.NewDefaultDelegate(), max(1, m.width), max(1, m.height))
	m.tasks.Title = fmt.Sprintf("Tasks: %d active, %d recent (bounded)", len(state.ActiveTasks), len(state.RecentTasks))
	m.tasks.SetShowStatusBar(true)
	m.tasks.SetFilteringEnabled(true)
}

func (m *tuiModel) appendFocusedTaskSummary(purpose taskSnapshotPurpose) {
	state := m.reducer.State()
	task := state.FocusedTask
	if task == nil {
		return
	}
	label := "Focused Task"
	if purpose == taskSnapshotStale {
		label = "Task refreshed after stale CAS; review before retry"
	}
	m.timeline.Add(fmt.Sprintf("%s %s | version %d | status %s | stage %s",
		label, shortID(task.TaskID), task.Version, task.Status, taskStage(task)))
	for _, summary := range taskResultSummaries(task) {
		m.timeline.Add(summary)
	}
}

func (m tuiModel) View() string {
	width, height := m.renderDimensions()
	var body string
	switch m.screen {
	case screenLogin:
		body = m.loginView()
	case screenLoading:
		body = "OpenAgentX Console\n\nWorking..."
	case screenSelector:
		body = m.agents.View()
	case screenAttach:
		body = m.attachView()
	default:
		body = m.menuView()
	}
	if m.overlay != overlayNone {
		body = m.overlayView()
	}
	return fitTerminalView(body, width, height)
}

func (m tuiModel) renderDimensions() (int, int) {
	width, height := m.width, m.height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	return width, height
}

func fitTerminalView(body string, width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for index := range lines {
		lines[index] = ansi.Truncate(strings.TrimSuffix(lines[index], "\r"), width, "")
	}
	return strings.Join(lines, "\n")
}

func (m tuiModel) menuItems() []string {
	login := "Login"
	if m.session.Authenticated {
		login = "Replace Login"
	}
	return []string{"Normal Console Attach", "Diagnostic Attach", login, "Logout", foregroundUnavailable, "Exit"}
}

func (m tuiModel) menuView() string {
	status := "CLI session: not logged in"
	if m.session.Authenticated {
		status = fmt.Sprintf("CLI session: %s, expires %s", m.session.Username, m.session.ExpiresAt.UTC().Format(time.RFC3339))
	}
	lines := []string{"OpenAgentX Console", status, ""}
	for index, item := range m.menuItems() {
		prefix := "  "
		if index == m.menuCursor {
			prefix = "> "
		}
		lines = append(lines, prefix+item)
	}
	if m.notice != "" {
		lines = append(lines, "", boundedSafeText(m.notice, 512))
	}
	return strings.Join(lines, "\n")
}

func (m tuiModel) loginView() string {
	title := "Login"
	if m.session.Authenticated {
		title = "Replace Login"
	}
	lines := []string{"OpenAgentX Console / " + title, "", m.username.View(), m.password.View()}
	if m.busy {
		lines = append(lines, "", "Authenticating...")
	} else if m.notice != "" {
		lines = append(lines, "", boundedSafeText(m.notice, 512))
	}
	return strings.Join(lines, "\n")
}

func (m tuiModel) attachView() string {
	width, height := m.renderDimensions()
	snapshot := consoleapi.AttachResponse{AgentID: m.selectedAgent, Mode: m.mode, WorkerStatus: domain.WorkerStatusOffline}
	cursor := int64(0)
	state := consolemodel.State{}
	if m.reducer != nil {
		state = m.reducer.State()
		snapshot = state.Console
		cursor = m.reducer.Cursor()
	}
	header := fmt.Sprintf("Agent %s | mode %s | connection %s | cursor %d",
		snapshot.AgentID, snapshot.Mode, m.connection, cursor)
	status := fmt.Sprintf("Task none | Worker %s gen %d %s | timeline %d/%d bytes",
		shortID(snapshot.WorkerInstanceID), snapshot.Generation, snapshot.WorkerStatus, m.timeline.Len(),
		m.timeline.BytesForMode(m.displayMode()))
	if state.FocusedTask != nil {
		status = fmt.Sprintf("Task %s v%d %s (%s) | Worker %s gen %d %s",
			shortID(state.FocusedTask.TaskID), state.FocusedTask.Version, state.FocusedTask.Status,
			taskStage(state.FocusedTask), shortID(snapshot.WorkerInstanceID), snapshot.Generation, snapshot.WorkerStatus)
	}
	if m.pending {
		status += " | command pending"
	}
	if m.switching {
		header = fmt.Sprintf("Agent %s | mode switching to %s | connection switching | cursor %d",
			snapshot.AgentID, m.switchTarget, cursor)
		status += " | writes disabled"
	}
	header = boundedSafeText(header, width)
	status = boundedSafeText(status, width)
	switch height {
	case 1:
		return m.input.View()
	case 2:
		return strings.Join([]string{header, m.input.View()}, "\n")
	case 3:
		return strings.Join([]string{header, status, m.input.View()}, "\n")
	default:
		return strings.Join([]string{header, m.viewport.View(), status, m.input.View()}, "\n")
	}
}

func (m tuiModel) overlayView() string {
	width, height := m.renderDimensions()
	content := ""
	switch m.overlay {
	case overlayStatus:
		content = m.statusView()
	case overlayHelp:
		content = strings.Join([]string{"Console commands", "/status", "/tasks", "/dispatch [--intent query|mutation] <content>",
			"/continue [--intent query|mutation] <继续内容>", "/accept [验收备注]", "/result-reject [问题说明]",
			"/steer <content>（执行中补充，下轮处理）", "/cancel", "/steer --task <task-id> --version <n> <content>",
			"/cancel --task <task-id> --version <n>",
			"/approve <approval-id> <expected-version>", "/reject <approval-id> <expected-version>",
			"/diagnostic", "/normal", "/help", "/quit", "/foreground",
			"PgUp/PgDown/Home/End scroll Timeline"}, "\n")
	case overlayDiagnostic:
		if m.displayMode() != consoleapi.ModeDiagnostic {
			content = "Diagnostic view requires Diagnostic mode"
		} else if m.reducer == nil || m.reducer.Snapshot().Diagnostic == nil {
			content = "Diagnostic view is not available"
		} else {
			state := m.reducer.State()
			snapshot := state.Console
			d := snapshot.Diagnostic
			backends := make([]string, 0, len(snapshot.BackendHealth))
			for backendID, health := range snapshot.BackendHealth {
				backends = append(backends, backendID+"="+string(health))
			}
			sort.Strings(backends)
			runStatus := "none"
			if state.FocusedTask != nil && state.FocusedTask.LatestRun != nil {
				runStatus = string(state.FocusedTask.LatestRun.Status)
			} else if snapshot.ActiveRun != nil {
				runStatus = string(snapshot.ActiveRun.Status)
			}
			stage, outputStatus, diagnostic := "none", "none", "none"
			for index := len(state.Timeline) - 1; index >= 0; index-- {
				output := state.Timeline[index].Output
				if output == nil {
					continue
				}
				stage = valueOr(output.Stage, "none")
				outputStatus = valueOr(output.Status, "none")
				diagnostic = valueOr(output.Diagnostic, "none")
				break
			}
			content = strings.Join([]string{"Diagnostic", "mode diagnostic",
				"Worker " + valueOr(snapshot.WorkerInstanceID, "none"),
				fmt.Sprintf("generation %d", snapshot.Generation),
				"Worker status " + string(snapshot.WorkerStatus),
				"Backend health " + valueOr(strings.Join(backends, ", "), "none"),
				"Run/wait category " + runStatus,
				"Runtime stage " + stage,
				"Runtime status " + outputStatus,
				"heartbeat " + formatTimeOrNone(d.LastHeartbeatAt),
				"lease " + formatTimeOrNone(d.LeaseUntil),
				fmt.Sprintf("draining %t", d.Draining),
				"started " + formatTimeOrNone(d.StartedAt),
				"updated " + formatTimeOrNone(d.UpdatedAt),
				"diagnostic " + diagnostic}, "\n")
		}
	case overlayTasks:
		if m.taskLoading {
			content = "Loading authoritative Task state..."
		} else {
			return fitTerminalView(m.tasks.View(), width, height)
		}
	case overlayConfirmation:
		content = fmt.Sprintf("Rebind current OAX window to Agent %s?\n\ny confirm  n cancel", m.selectedAgent)
	case overlayError:
		content = m.notice
	}
	content = boundedSafeMultiline(content, 8<<10)
	if width < 20 || height < 6 {
		return fitTerminalView(content, width, height)
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2).
		Width(min(width-6, 78)).MaxHeight(max(1, height-4)).Render(content)
	return fitTerminalView(lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box), width, height)
}

func (m tuiModel) statusView() string {
	snapshot := consoleapi.AttachResponse{AgentID: m.selectedAgent, Mode: m.mode, WorkerStatus: domain.WorkerStatusOffline}
	cursor := int64(0)
	state := consolemodel.State{}
	if m.reducer != nil {
		state = m.reducer.State()
		snapshot = state.Console
		cursor = m.reducer.Cursor()
	}
	active := "none"
	if snapshot.ActiveRun != nil {
		active = fmt.Sprintf("%s (%s)", shortID(snapshot.ActiveRun.RunID), snapshot.ActiveRun.Status)
	}
	backends := make([]string, 0, len(snapshot.BackendHealth))
	for id, health := range snapshot.BackendHealth {
		backends = append(backends, id+"="+string(health))
	}
	sort.Strings(backends)
	heartbeatAge := age(m.now(), snapshot.LastHeartbeatAt)
	lease := "none"
	if !snapshot.LeaseUntil.IsZero() {
		lease = snapshot.LeaseUntil.UTC().Format(time.RFC3339)
	}
	drain := snapshot.WorkerStatus == domain.WorkerStatusDraining
	lines := []string{"Status",
		"CLI username: " + valueOr(m.session.Username, "not logged in"),
		"CLI expiry: " + formatTimeOrNone(m.session.ExpiresAt),
		"Socket: " + valueOr(m.session.SocketPath, "unresolved"),
		"Agent: " + snapshot.AgentID,
		"Worker: " + valueOr(snapshot.WorkerInstanceID, "none"),
		fmt.Sprintf("Generation: %d", snapshot.Generation),
		"Worker status: " + string(snapshot.WorkerStatus),
		"Backend health: " + valueOr(strings.Join(backends, ", "), "none"),
		"Active run: " + active,
		fmt.Sprintf("Draining: %t", drain),
		"Heartbeat age: " + heartbeatAge,
		"Lease until: " + lease,
		"Connection: " + string(m.connection),
		fmt.Sprintf("Event cursor: %d", cursor),
		"Mode: " + m.mode}
	if m.statusReadiness != nil {
		lines = append(lines, "就绪: "+m.statusReadiness.Reason)
		if m.statusReadiness.NextAction != "" {
			lines = append(lines, "下一步: "+m.statusReadiness.NextAction)
		}
	}
	if state.FocusedTask == nil {
		lines = append(lines, "", "Focused Task: none", "Use /tasks to select an active or recent Task")
		return strings.Join(lines, "\n")
	}
	task := state.FocusedTask
	lines = append(lines, "", "Focused Task",
		"Task ID: "+task.TaskID,
		fmt.Sprintf("Task version: %d", task.Version),
		"Task status: "+string(task.Status),
		"Task stage: "+taskStage(task))
	if task.Intent != "" {
		lines = append(lines, "Task intent: "+string(task.Intent))
	}
	if task.Detail != nil {
		lines = append(lines, "Task request: "+task.Detail.Content,
			"Task updated: "+task.Detail.UpdatedAt,
			"Task outcome state: "+task.Detail.OutcomeState)
		if task.Detail.ParentTaskID != nil {
			lines = append(lines, "继续自: "+*task.Detail.ParentTaskID)
		}
		if task.Detail.Review != nil {
			lines = append(lines, reviewSummary(task.Detail.Review))
		}
		if task.Detail.CompletionBasis != "" {
			lines = append(lines, "Completion basis: "+string(task.Detail.CompletionBasis))
			if task.Detail.CompletionBasis == domain.TaskCompletionQueryResultDelivered {
				lines = append(lines, "Complete reply delivered; side effects are not independently verified")
			}
		}
		if task.Detail.Result != nil {
			lines = append(lines, "Task result: "+*task.Detail.Result)
		}
		if task.Detail.Error != nil {
			lines = append(lines, "Task error: "+*task.Detail.Error)
		}
	}
	if task.WorkDelivery != nil {
		lines = append(lines, "", "Work delivery",
			"Mailbox ID: "+task.WorkDelivery.MailboxItemID,
			"Mailbox state: "+string(task.WorkDelivery.State))
	}
	if task.LatestRun != nil {
		run := task.LatestRun
		generation := int64(0)
		if run.WorkerGeneration != nil {
			generation = *run.WorkerGeneration
		}
		lines = append(lines, "", "RunAttempt",
			"Run ID: "+run.ID,
			fmt.Sprintf("Run version: %d", run.Version),
			"Run status: "+string(run.Status),
			"Run Worker: "+run.WorkerInstanceID,
			fmt.Sprintf("Run generation: %d", generation),
			"Runtime reply state: "+run.TurnResultState)
		if run.DeadlineAt != nil && !run.DeadlineAt.IsZero() {
			lines = append(lines, "运行截止: "+run.DeadlineAt.Local().Format(time.RFC3339))
		}
		if run.TurnResult != nil {
			lines = append(lines, "Runtime status: "+string(run.TurnResult.RuntimeStatus))
			if run.TurnResult.Body != "" {
				lines = append(lines, "Runtime reply: "+run.TurnResult.Body)
			}
			if run.TurnResult.Error != "" {
				lines = append(lines, "Runtime error: "+run.TurnResult.Error)
			}
			lines = append(lines, "Side effects source: "+run.TurnResult.SideEffectsSource,
				"Business verification: "+run.TurnResult.BusinessVerificationSource)
		}
	}
	if task.LatestMessage != nil {
		lines = append(lines, "", "Latest Message",
			"Message ID: "+task.LatestMessage.MessageID,
			fmt.Sprintf("Message version: %d", task.LatestMessage.Version))
	}
	if task.PendingApproval != nil {
		lines = append(lines, "", "Pending Approval",
			"Approval ID: "+task.PendingApproval.ApprovalRequestID,
			fmt.Sprintf("Expected Run version: %d", task.PendingApproval.ExpectedRunVersion))
	}
	return strings.Join(lines, "\n")
}

func taskStage(task *consolemodel.TaskState) string {
	if task == nil {
		return "none"
	}
	if task.LatestRun != nil {
		return "run " + string(task.LatestRun.Status)
	}
	if task.WorkDelivery != nil {
		return "mailbox " + string(task.WorkDelivery.State)
	}
	return string(task.Status)
}

func taskResultSummaries(task *consolemodel.TaskState) []string {
	if task == nil || !taskTerminal(task.Status) {
		return nil
	}
	result := make([]string, 0, 3)
	if task.Detail != nil && task.Detail.Review != nil {
		result = append(result, reviewSummary(task.Detail.Review))
	}
	result = append(result, "使用 /continue <内容> 关联继续；/accept [备注] 接受结果；/result-reject [备注] 记录问题")
	if task.Detail == nil {
		result = append(result, "Task is terminal, but authoritative outcome detail is not loaded")
	} else {
		switch task.Detail.OutcomeState {
		case "available", "truncated":
			if task.Detail.Result != nil && *task.Detail.Result != "" {
				result = append(result, "Task outcome result: "+*task.Detail.Result)
			}
			if task.Detail.Error != nil && *task.Detail.Error != "" {
				result = append(result, "Task outcome error: "+*task.Detail.Error)
			}
		case "not_recorded":
			result = append(result, "Task is terminal, but no safe Task outcome was recorded")
		default:
			result = append(result, "Task outcome is not yet available")
		}
	}
	if task.Status == domain.TaskStatusUncertain {
		result = append(result, "Task outcome remains uncertain; a Runtime reply is not proof of business success")
	}
	if task.LatestRun == nil {
		return result
	}
	run := task.LatestRun
	switch run.TurnResultState {
	case "available", "truncated":
		if run.TurnResult != nil && run.TurnResult.Body != "" {
			result = append(result, "Runtime reply: "+run.TurnResult.Body)
		}
		if run.TurnResult != nil && run.TurnResult.Error != "" {
			result = append(result, "Runtime error: "+run.TurnResult.Error)
		}
	case "empty":
		result = append(result, "Runtime completed without a safe displayable reply")
	case "invalid":
		result = append(result, "Runtime reply was rejected as invalid")
	case "not_recorded":
		result = append(result, "Runtime reply was not recorded")
	}
	return result
}

func timelineItemSummary(item consolemodel.TimelineItem, mode string) string {
	parts := []string{fmt.Sprintf("#%d %s", item.Sequence, boundedSafeText(item.EventType, 128))}
	details := make([]string, 0, 4)
	if item.Output != nil {
		values := []string{item.Output.Stage, item.Output.Status, item.Output.Text}
		if mode == consoleapi.ModeDiagnostic {
			values = append(values, item.Output.Diagnostic)
		}
		for _, value := range values {
			if value != "" {
				parts = append(parts, boundedSafeText(value, maxTimelineEntryBytes/2))
			}
		}
		return strings.Join(parts, " | ")
	}
	if item.TaskID != "" {
		parts = append(parts, "Task "+shortID(item.TaskID))
	}
	if item.TaskVersion > 0 {
		parts = append(parts, fmt.Sprintf("version %d", item.TaskVersion))
	}
	if item.TaskStatus != "" {
		parts = append(parts, "status "+string(item.TaskStatus))
	}
	if item.MailboxItemID != "" {
		parts = append(parts, string(item.MailboxKind)+"/"+string(item.MailboxLane)+" delivery "+string(item.MailboxState))
	}
	if item.RunID != "" {
		parts = append(parts, "Run "+shortID(item.RunID), fmt.Sprintf("run version %d", item.RunVersion),
			"run status "+string(item.RunStatus))
	}
	if item.MessageID != "" {
		parts = append(parts, "Message "+shortID(item.MessageID), fmt.Sprintf("message version %d", item.MessageVersion))
	}
	if item.ApprovalID != "" {
		parts = append(parts, "Approval "+shortID(item.ApprovalID), "approval "+string(item.ApprovalState))
	}
	if item.TaskOutcomeState == "not_recorded" && taskTerminal(item.TaskStatus) {
		parts = append(parts, "no safe Task outcome recorded")
	}
	if item.TaskResult != "" {
		details = append(details, "Task outcome result: "+boundedSafeText(item.TaskResult, maxTimelineEntryBytes/2))
	}
	if item.TaskError != "" {
		details = append(details, "Task outcome error: "+boundedSafeText(item.TaskError, maxTimelineEntryBytes/2))
	}
	if item.RuntimeReplyState != "" && item.RuntimeReplyState != "not_recorded" {
		parts = append(parts, "Runtime reply state "+item.RuntimeReplyState)
	}
	if !item.RunStatus.Active() {
		switch item.RuntimeReplyState {
		case "not_recorded":
			parts = append(parts, "Runtime reply was not recorded")
		case "empty":
			parts = append(parts, "Runtime completed without a safe displayable reply")
		case "invalid":
			parts = append(parts, "Runtime reply was rejected as invalid")
		}
	}
	if item.RuntimeReply != "" {
		details = append(details, "Runtime reply: "+boundedSafeText(item.RuntimeReply, maxTimelineEntryBytes/2))
	}
	if item.RuntimeError != "" {
		details = append(details, "Runtime error: "+boundedSafeText(item.RuntimeError, maxTimelineEntryBytes/2))
	}
	return strings.Join(append([]string{strings.Join(parts, " | ")}, details...), "\n")
}

func staleControlError(err error) bool {
	var apiErr *consoleclient.APIError
	return errors.As(err, &apiErr) && apiErr.Code == openapi.ErrorStaleVersion
}

func (m tuiModel) failAttach(message string) (tea.Model, tea.Cmd) {
	m.notice = message
	m.overlay = overlayError
	if m.direct {
		m.fatal = true
		m.screen = screenLoading
	} else {
		m.screen = screenMenu
	}
	return m, nil
}

func ackFollow(ack chan<- error, err error) {
	if ack != nil {
		ack <- err
	}
}

func eventSummary(event openapi.JournalEventReadModel, mode string) string {
	prefix := fmt.Sprintf("#%d %s", event.Sequence, boundedSafeText(event.EventType, 128))
	if event.Output != nil {
		parts := []string{prefix}
		values := []string{event.Output.Stage, event.Output.Status, event.Output.Text}
		if mode == consoleapi.ModeDiagnostic {
			values = append(values, event.Output.Diagnostic)
		}
		for _, value := range values {
			if value != "" {
				parts = append(parts, boundedSafeText(value, maxTimelineEntryBytes/2))
			}
		}
		return strings.Join(parts, " | ")
	}
	if event.Worker != nil {
		return fmt.Sprintf("%s | Worker %s generation %d", prefix, event.Worker.Status, event.Worker.Generation)
	}
	if event.Run != nil {
		return fmt.Sprintf("%s | Run %s %s", prefix, shortID(event.Run.ID), event.Run.Status)
	}
	return prefix
}

func (o controlOutcome) summary(kind controlKind) string {
	parts := []string{string(kind) + " succeeded"}
	switch kind {
	case controlDispatch, controlContinue, controlCancel:
		parts = append(parts, "task "+shortID(o.TaskID), fmt.Sprintf("version %d", o.TaskVersion), "status "+string(o.TaskStatus))
	case controlAcceptResult, controlRejectResult:
		if o.Review != nil {
			parts = append(parts, reviewSummary(o.Review))
		}
	case controlSteer:
		parts = append(parts, "task "+shortID(o.TaskID), fmt.Sprintf("task version %d", o.TaskVersion),
			"status "+string(o.TaskStatus), "message "+shortID(o.MessageID), fmt.Sprintf("message version %d", o.MessageVersion))
	case controlApprove, controlReject:
		parts = append(parts, "approval "+shortID(o.ApprovalID), "decision "+string(o.Decision),
			"decision id "+shortID(o.DecisionID), "status "+string(o.DecisionState))
	}
	if o.Sequence > 0 {
		parts = append(parts, fmt.Sprintf("sequence %d", o.Sequence))
	}
	return strings.Join(parts, " | ")
}

func safeErrorSummary(err error) string {
	if err == nil {
		return ""
	}
	var apiErr *consoleclient.APIError
	if errors.As(err, &apiErr) {
		if apiErr.Code != "" {
			return fmt.Sprintf("request failed (%s)", boundedSafeText(apiErr.Code, 64))
		}
		return fmt.Sprintf("request failed (HTTP %d)", apiErr.StatusCode)
	}
	var visible interface{ UserMessage() string }
	if errors.As(err, &visible) {
		return boundedSafeText(visible.UserMessage(), 1024)
	}
	if errors.Is(err, errLoginRequired) || errors.Is(err, credentialstore.ErrNotFound) {
		return "CLI session is not authenticated; run openagentx console login"
	}
	if strings.HasPrefix(err.Error(), "usage:") || strings.HasPrefix(err.Error(), "unknown command") ||
		strings.Contains(err.Error(), "positive integer") {
		return boundedSafeText(err.Error(), 256)
	}
	return "operation failed"
}

func boundedSafeText(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return strings.Repeat(".", max(0, limit))
	}
	value = value[:limit-3]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value + "..."
}

func boundedSafeMultiline(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
	if len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return strings.Repeat(".", max(0, limit))
	}
	value = value[:limit-3]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value + "..."
}

func shortID(value string) string {
	if value == "" {
		return "none"
	}
	if len(value) <= 18 {
		return value
	}
	return value[:15] + "..."
}

func age(now, then time.Time) string {
	if then.IsZero() {
		return "unknown"
	}
	delta := now.Sub(then)
	if delta < 0 {
		delta = 0
	}
	return delta.Round(time.Second).String()
}

func formatTimeOrNone(value time.Time) string {
	if value.IsZero() {
		return "none"
	}
	return value.UTC().Format(time.RFC3339)
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func reviewSummary(review *domain.TaskReview) string {
	label := "用户已验收"
	if review.Decision == "rejected" {
		label = "用户标记结果有问题"
	}
	text := fmt.Sprintf("%s | 验收人 %s | %s | Run %s v%d | 原执行状态保留", label, review.ReviewedBy, review.CreatedAt.Local().Format(time.RFC3339), shortID(review.RunID), review.RunVersion)
	if review.Note != "" {
		text += " | " + boundedSafeText(review.Note, 1024)
	}
	return text
}
