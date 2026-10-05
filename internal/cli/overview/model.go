package overview

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type tickMsg struct{ Generation uint64 }
type navigationResult struct {
	Err        error
	AuthFailed bool
}

type model struct {
	tickGeneration                     uint64
	ctx                                context.Context
	reader                             *reader
	navigator                          *navigator
	rows                               []row
	selected, listOffset, detailOffset int
	detailFocus                        bool
	width, height                      int
	loading, navigating, connected     bool
	lastSync                           time.Time
	notice                             string
}

func newModel(ctx context.Context, reader *reader, navigator *navigator) model {
	return model{ctx: ctx, reader: reader, navigator: navigator, width: 80, height: 24, loading: true, notice: "正在连接控制面…"}
}
func (m model) Init() tea.Cmd    { return m.refresh() }
func (m model) refresh() tea.Cmd { return func() tea.Msg { return m.reader.refresh(m.ctx) } }
func (m *model) nextTick() tea.Cmd {
	m.tickGeneration++
	generation := m.tickGeneration
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return tickMsg{generation} })
}
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
		m.height = max(1, msg.Height)
	case tickMsg:
		if msg.Generation != m.tickGeneration {
			return m, nil
		}
		if !m.loading && !m.navigating {
			m.loading = true
			return m, m.refresh()
		}
		cmd := m.nextTick()
		return m, cmd
	case refreshResult:
		m.loading = false
		if msg.Err != nil {
			m.connected = false
			m.notice = msg.Err.Error()
			cmd := m.nextTick()
			return m, cmd
		}
		selected := ""
		if len(m.rows) > 0 {
			selected = m.rows[m.selected].Option.AgentID
		}
		old := map[string]row{}
		for _, r := range m.rows {
			old[r.Option.AgentID] = r
		}
		for i := range msg.Rows {
			if msg.Rows[i].Error != "" {
				if prior, ok := old[msg.Rows[i].Option.AgentID]; ok {
					prior.Error = msg.Rows[i].Error
					msg.Rows[i] = prior
				}
			}
		}
		m.rows = msg.Rows
		m.selected = 0
		for i, r := range m.rows {
			if r.Option.AgentID == selected {
				m.selected = i
			}
		}
		m.connected = true
		m.lastSync = msg.At
		m.notice = "只读观察 · 待处理为已知任务提示，不是全局总计"
		for _, r := range m.rows {
			if r.Error != "" {
				m.notice = "部分 Agent 状态读取失败；标记为旧数据并禁用其跳转"
				break
			}
		}
		cmd := m.nextTick()
		return m, cmd
	case navigationResult:
		m.navigating = false
		if msg.AuthFailed {
			m.connected = false
		}
		if msg.Err != nil {
			m.notice = msg.Err.Error()
		} else {
			m.notice = "已定位到受管终端；总览继续只读刷新"
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab", "shift+tab":
			m.detailFocus = !m.detailFocus
		case "esc":
			m.detailFocus = false
		case "r":
			if !m.loading && !m.navigating {
				m.loading = true
				m.tickGeneration++
				return m, m.refresh()
			}
		case "up", "k":
			if m.detailFocus {
				m.detailOffset = max(0, m.detailOffset-1)
			} else {
				m.selectRow(m.selected - 1)
			}
		case "down", "j":
			if m.detailFocus {
				m.detailOffset++
			} else {
				m.selectRow(m.selected + 1)
			}
		case "pgup":
			if m.detailFocus {
				m.detailOffset = max(0, m.detailOffset-8)
			} else {
				m.selectRow(m.selected - 8)
			}
		case "pgdown":
			if m.detailFocus {
				m.detailOffset += 8
			} else {
				m.selectRow(m.selected + 8)
			}
		case "home":
			if m.detailFocus {
				m.detailOffset = 0
			} else {
				m.selectRow(0)
			}
		case "end":
			if m.detailFocus {
				m.detailOffset = 1 << 20
			} else {
				m.selectRow(len(m.rows) - 1)
			}
		case "enter":
			if !m.connected || m.loading || m.navigating || len(m.rows) == 0 {
				m.notice = "等待控制面新鲜状态，暂不能跳转"
				break
			}
			r := m.rows[m.selected]
			if r.Error != "" || time.Since(r.UpdatedAt) > 20*time.Second {
				m.notice = "目标状态已陈旧；请刷新后再跳转"
				break
			}
			m.navigating = true
			id := r.Option.AgentID
			return m, func() tea.Msg {
				ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
				defer cancel()
				if err := m.reader.authorize(ctx); err != nil {
					return navigationResult{Err: err, AuthFailed: true}
				}
				return navigationResult{Err: m.navigator.jump(ctx, id)}
			}
		}
	}
	m.clamp()
	return m, nil
}
func (m *model) selectRow(i int) {
	if len(m.rows) == 0 {
		return
	}
	i = min(max(0, i), len(m.rows)-1)
	if i != m.selected {
		m.selected = i
		m.detailOffset = 0
	}
	m.detailFocus = false
}
func (m *model) clamp() {
	m.selected = max(0, min(m.selected, len(m.rows)-1))
	listHeight, _ := m.heights()
	if m.selected < m.listOffset {
		m.listOffset = m.selected
	}
	if m.selected >= m.listOffset+listHeight {
		m.listOffset = m.selected - listHeight + 1
	}
	m.listOffset = max(0, m.listOffset)
	_, detailHeight := m.heights()
	lines := m.detailLines()
	m.detailOffset = max(0, min(m.detailOffset, max(0, len(lines)-detailHeight)))
}
func (m model) heights() (int, int) {
	available := max(2, m.height-8)
	list := min(max(2, available/2), max(2, len(m.rows)))
	return list, max(1, available-list)
}
func (m model) stateLine() string {
	status := "连接中"
	if m.connected {
		status = "在线"
	} else if !m.lastSync.IsZero() {
		status = "离线 · 旧数据 · 禁止跳转"
	}
	if m.loading {
		status += " · 刷新中"
	}
	last := "尚未同步"
	if !m.lastSync.IsZero() {
		last = "上次同步 " + m.lastSync.Local().Format("15:04:05")
	}
	return fmt.Sprintf("%s  |  %s  |  %d 个 Agent", status, last, len(m.rows))
}
