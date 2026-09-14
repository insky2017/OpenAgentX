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
	Updates <-chan tea.Msg
}

type followSnapshotMsg struct {
	Snapshot consoleapi.AttachResponse
}

type followEventMsg struct {
	Event openapi.JournalEventReadModel
}

type followConnectionMsg struct {
	State consoleclient.FollowState
}

type followDoneMsg struct {
	Err error
}

type tickMsg struct{ Now time.Time }

type controlKind string

const (
	controlDispatch controlKind = "dispatch"
	controlSteer    controlKind = "steer"
	controlCancel   controlKind = "cancel"
	controlApprove  controlKind = "approve"
	controlReject   controlKind = "reject"
)

type controlRequest struct {
	Kind            controlKind
	AgentID         string
	TargetID        string
	ExpectedVersion int64
	Content         string
}

type controlResultMsg struct {
	Kind controlKind
	Err  error
}

type timelineBuffer struct {
	entries []string
	bytes   int
}

func (b *timelineBuffer) Add(value string) {
	value = boundedSafeText(value, maxTimelineEntryBytes)
	if value == "" {
		return
	}
	if len(b.entries) > 0 {
		b.bytes++
	}
	b.entries = append(b.entries, value)
	b.bytes += len(value)
	for len(b.entries) > maxTimelineEntries || b.bytes > maxTimelineBytes {
		b.bytes -= len(b.entries[0])
		b.entries = b.entries[1:]
		if len(b.entries) > 0 {
			b.bytes--
		}
	}
}

func (b timelineBuffer) String() string { return strings.Join(b.entries, "\n") }
func (b timelineBuffer) Len() int       { return len(b.entries) }
func (b timelineBuffer) Bytes() int     { return b.bytes }

type agentItem struct{ option domain.ConsoleAgentOption }

func (i agentItem) Title() string { return i.option.AgentID + "  " + i.option.DisplayName }
func (i agentItem) Description() string {
	active := "idle"
	if i.option.ActiveRunStatus != "" {
		active = string(i.option.ActiveRunStatus)
	}
	return fmt.Sprintf("%s  generation %d  %s", i.option.WorkerStatus, i.option.Generation, active)
}
func (i agentItem) FilterValue() string { return i.option.AgentID + " " + i.option.DisplayName }

type tuiModel struct {
	actions tuiActions
	now     func() time.Time

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

	preparationID uint64
	selectedAgent string
	mode          string
	confirmReturn screenKind

	reducer    *consolemodel.Reducer
	connection consoleclient.ConnectionState
	input      textarea.Model
	viewport   viewport.Model
	timeline   timelineBuffer
	pending    bool
	follow     <-chan tea.Msg
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

func waitFollow(updates <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-updates
		if !ok {
			return followDoneMsg{}
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
		return m, tickCommand(m.now)
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
		m.follow = msg.Updates
		return m, waitFollow(m.follow)
	case followConnectionMsg:
		m.connection = msg.State.State
		command := waitFollow(m.follow)
		if msg.State.State == consoleclient.ConnectionRetentionReattach {
			m.timeline.Add("Event history expired; refreshing the authoritative snapshot")
			m.syncTimeline(true)
		}
		return m, command
	case followSnapshotMsg:
		var err error
		if m.reducer == nil {
			m.reducer, err = consolemodel.New(msg.Snapshot)
		} else {
			err = m.reducer.ApplySnapshot(msg.Snapshot)
		}
		if err != nil {
			m.connection = consoleclient.ConnectionDisconnected
			m.overlay = overlayError
			m.notice = "Snapshot rejected: " + safeErrorSummary(err)
			return m, nil
		}
		m.timeline.Add(fmt.Sprintf("Snapshot applied at cursor %d", m.reducer.Cursor()))
		m.syncTimeline(true)
		return m, waitFollow(m.follow)
	case followEventMsg:
		if m.reducer == nil {
			m.overlay = overlayError
			m.notice = "Event arrived before the authoritative snapshot"
			return m, nil
		}
		result, err := m.reducer.Apply(msg.Event)
		if err != nil {
			m.connection = consoleclient.ConnectionDisconnected
			m.overlay = overlayError
			m.notice = "Event rejected: " + safeErrorSummary(err)
			return m, nil
		}
		if result.Timeline != nil {
			m.timeline.Add(eventSummary(*result.Timeline))
			m.syncTimeline(true)
		}
		return m, waitFollow(m.follow)
	case followDoneMsg:
		m.connection = consoleclient.ConnectionDisconnected
		if msg.Err != nil && !errors.Is(msg.Err, context.Canceled) {
			m.timeline.Add("Connection stopped: " + safeErrorSummary(msg.Err))
			m.syncTimeline(true)
		}
		return m, nil
	case controlResultMsg:
		m.pending = false
		if msg.Err != nil {
			m.timeline.Add(string(msg.Kind) + " failed: " + safeErrorSummary(msg.Err))
		} else {
			m.timeline.Add(string(msg.Kind) + " succeeded")
		}
		m.syncTimeline(true)
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
	command, err := parseControlInput(line, m.selectedAgent)
	switch {
	case errors.Is(err, errQuitCommand):
		return m, tea.Quit
	case errors.Is(err, errStatusCommand):
		m.overlay = overlayStatus
		return m, nil
	case errors.Is(err, errHelpCommand):
		m.overlay = overlayHelp
		return m, nil
	case errors.Is(err, errDiagnosticCommand):
		m.overlay = overlayDiagnostic
		return m, nil
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
	if m.connection != consoleclient.ConnectionConnected || m.reducer == nil {
		m.timeline.Add("Command disabled while Console is disconnected")
		m.syncTimeline(true)
		return m, nil
	}
	if m.pending {
		m.timeline.Add("A Console command is already pending")
		m.syncTimeline(true)
		return m, nil
	}
	m.pending = true
	m.input.Reset()
	m.timeline.Add(string(command.Kind) + " pending")
	m.syncTimeline(true)
	return m, m.actions.controlCmd(m.preparationID, command)
}

var (
	errQuitCommand       = errors.New("quit")
	errStatusCommand     = errors.New("status")
	errHelpCommand       = errors.New("help")
	errDiagnosticCommand = errors.New("diagnostic")
	errForegroundCommand = errors.New("foreground")
)

func parseControlInput(line, agentID string) (controlRequest, error) {
	command, remainder, _ := strings.Cut(strings.TrimSpace(line), " ")
	switch command {
	case "/quit":
		return controlRequest{}, errQuitCommand
	case "/status":
		return controlRequest{}, errStatusCommand
	case "/help":
		return controlRequest{}, errHelpCommand
	case "/diagnostic":
		return controlRequest{}, errDiagnosticCommand
	case "/foreground":
		return controlRequest{}, errForegroundCommand
	case "/dispatch":
		content := strings.TrimSpace(remainder)
		if content == "" {
			return controlRequest{}, fmt.Errorf("usage: /dispatch <content>")
		}
		return controlRequest{Kind: controlDispatch, AgentID: agentID, Content: content}, nil
	case "/steer":
		parts := strings.SplitN(strings.TrimSpace(remainder), " ", 3)
		if len(parts) != 3 || strings.TrimSpace(parts[2]) == "" {
			return controlRequest{}, fmt.Errorf("usage: /steer <task-id> <expected-version> <content>")
		}
		version, err := positiveVersion(parts[1])
		if err != nil {
			return controlRequest{}, err
		}
		return controlRequest{Kind: controlSteer, AgentID: agentID, TargetID: parts[0], ExpectedVersion: version, Content: strings.TrimSpace(parts[2])}, nil
	case "/cancel", "/approve", "/reject":
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

func positiveVersion(value string) (int64, error) {
	version, err := strconv.ParseInt(value, 10, 64)
	if err != nil || version <= 0 {
		return 0, fmt.Errorf("expected version must be a positive integer")
	}
	return version, nil
}

func (m *tuiModel) resize(width, height int) {
	m.width = max(width, 20)
	m.height = max(height, 8)
	m.input.SetWidth(max(10, m.width-2))
	m.input.SetHeight(3)
	m.viewport.Width = max(10, m.width-2)
	m.viewport.Height = max(1, m.height-8)
	if m.screen == screenSelector {
		m.agents.SetSize(m.width, max(5, m.height-2))
	}
}

func (m *tuiModel) syncTimeline(forceBottom bool) {
	wasBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.timeline.String())
	if forceBottom || wasBottom {
		m.viewport.GotoBottom()
	}
}

func (m tuiModel) View() string {
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
	return lipgloss.NewStyle().Width(max(20, m.width)).Height(max(8, m.height)).Render(body)
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
	snapshot := consoleapi.AttachResponse{AgentID: m.selectedAgent, Mode: m.mode, WorkerStatus: domain.WorkerStatusOffline}
	cursor := int64(0)
	if m.reducer != nil {
		snapshot = m.reducer.Snapshot()
		cursor = m.reducer.Cursor()
	}
	header := fmt.Sprintf("Agent %s  Mode %s  Worker %s  Generation %d  Connection %s",
		snapshot.AgentID, snapshot.Mode, shortID(snapshot.WorkerInstanceID), snapshot.Generation, m.connection)
	status := fmt.Sprintf("%s | cursor %d | timeline %d/%d bytes", snapshot.WorkerStatus, cursor, m.timeline.Len(), m.timeline.Bytes())
	if m.pending {
		status += " | command pending"
	}
	return strings.Join([]string{boundedSafeText(header, max(20, m.width)), m.viewport.View(), boundedSafeText(status, max(20, m.width)), m.input.View()}, "\n")
}

func (m tuiModel) overlayView() string {
	content := ""
	switch m.overlay {
	case overlayStatus:
		content = m.statusView()
	case overlayHelp:
		content = strings.Join([]string{"Console commands", "/status", "/dispatch <content>",
			"/steer <task-id> <expected-version> <content>", "/cancel <task-id> <expected-version>",
			"/approve <approval-id> <expected-version>", "/reject <approval-id> <expected-version>",
			"/diagnostic", "/help", "/quit", "/foreground"}, "\n")
	case overlayDiagnostic:
		if m.mode != consoleapi.ModeDiagnostic {
			content = "Diagnostic view requires Diagnostic Attach"
		} else if m.reducer == nil || m.reducer.Snapshot().Diagnostic == nil {
			content = "Diagnostic view is not available"
		} else {
			d := m.reducer.Snapshot().Diagnostic
			content = fmt.Sprintf("Diagnostic\nheartbeat %s\nlease %s\ndraining %t",
				d.LastHeartbeatAt.UTC().Format(time.RFC3339), d.LeaseUntil.UTC().Format(time.RFC3339), d.Draining)
		}
	case overlayConfirmation:
		content = fmt.Sprintf("Rebind current OAX window to Agent %s?\n\ny confirm  n cancel", m.selectedAgent)
	case overlayError:
		content = m.notice
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2).
		Width(min(max(20, m.width-6), 78)).Render(boundedSafeMultiline(content, 8<<10))
	return lipgloss.Place(max(20, m.width), max(8, m.height), lipgloss.Center, lipgloss.Center, box)
}

func (m tuiModel) statusView() string {
	snapshot := consoleapi.AttachResponse{AgentID: m.selectedAgent, Mode: m.mode, WorkerStatus: domain.WorkerStatusOffline}
	cursor := int64(0)
	if m.reducer != nil {
		snapshot = m.reducer.Snapshot()
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
	return strings.Join([]string{"Status",
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
		"Mode: " + m.mode}, "\n")
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

func eventSummary(event openapi.JournalEventReadModel) string {
	prefix := fmt.Sprintf("#%d %s", event.Sequence, boundedSafeText(event.EventType, 128))
	if event.Output != nil {
		parts := []string{prefix}
		for _, value := range []string{event.Output.Stage, event.Output.Status, event.Output.Text, event.Output.Diagnostic} {
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
