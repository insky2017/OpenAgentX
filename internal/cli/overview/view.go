package overview

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"openagentx/internal/domain"
)

func safeText(s string, limit int) string {
	s = ansi.Strip(s)
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	runes := []rune(s)
	if len(runes) > limit {
		s = string(runes[:limit]) + "…"
	}
	return s
}
func oneLine(s string) string { return strings.Join(strings.Fields(safeText(s, 160)), " ") }
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}
func taskStatus(s domain.TaskStatus) string {
	switch s {
	case domain.TaskStatusQueued:
		return "排队"
	case domain.TaskStatusDispatching:
		return "派发中"
	case domain.TaskStatusRunning:
		return "执行中"
	case domain.TaskStatusWaitingInput:
		return "等待输入"
	case domain.TaskStatusWaitingApproval:
		return "等待审批"
	case domain.TaskStatusCancelRequested:
		return "取消中"
	case domain.TaskStatusSucceeded:
		return "执行完成"
	case domain.TaskStatusFailed:
		return "执行失败"
	case domain.TaskStatusCanceled:
		return "已取消"
	case domain.TaskStatusUncertain:
		return "结果待核实"
	}
	return string(s)
}
func rowStatus(r row) string {
	if r.Error != "" {
		return "旧数据"
	}
	if r.Task != nil {
		return taskStatus(r.Task.Task.Status)
	}
	if r.Attached.Readiness != nil && r.Attached.Readiness.Ready {
		return "可工作"
	}
	return "未就绪"
}
func rowSummary(r row) string {
	if r.Error != "" {
		return r.Error
	}
	if r.Task != nil {
		return oneLine(r.Task.Task.Content)
	}
	if r.Attached.SuggestedTask != nil {
		return oneLine(r.Attached.SuggestedTask.Summary)
	}
	if r.Attached.Readiness != nil {
		return r.Attached.Readiness.Reason
	}
	return "就绪状态未知"
}
func (m model) View() string {
	m.clamp()
	if m.width < 36 || m.height < 12 {
		return ansi.Truncate("OAX 总览：请扩大至至少 36×12；q 退出。", m.width, "…")
	}
	lh, dh := m.heights()
	lines := []string{"OAX 总览  ·  观察与终端导航", m.stateLine(), "控制面：" + m.reader.socket}
	nameWidth := min(24, max(12, m.width/4))
	statusWidth := 12
	workWidth := max(1, m.width-nameWidth-statusWidth-4)
	lines = append(lines, "  "+fit("Agent", nameWidth)+" "+fit("状态", statusWidth)+" "+fit("当前工作 / 阻塞与待处理", workWidth))
	for i := 0; i < lh; i++ {
		index := m.listOffset + i
		if index >= len(m.rows) {
			line := ""
			if len(m.rows) == 0 && i == 0 {
				line = "  暂无可显示 Agent"
			}
			lines = append(lines, line)
			continue
		}
		r := m.rows[index]
		prefix := "  "
		if index == m.selected {
			prefix = "› "
		}
		name := r.Option.AgentID
		line := prefix + fit(name, nameWidth) + " " + fit(rowStatus(r), statusWidth) + " " + fit(rowSummary(r), workWidth)
		if index == m.selected {
			line = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).Render(line)
		}
		lines = append(lines, line)
	}
	focus := "Tab 进入详情"
	if m.detailFocus {
		focus = "详情滚动中 · Tab 返回列表"
	}
	lines = append(lines, fmt.Sprintf("── 选中详情 · %s · Agent %d/%d ──", focus, min(m.selected+1, len(m.rows)), len(m.rows)))
	detail := m.detailLines()
	for i := 0; i < dh; i++ {
		index := m.detailOffset + i
		line := ""
		if index < len(detail) {
			line = detail[index]
		}
		lines = append(lines, line)
	}
	lines = append(lines, "提示："+oneLine(m.notice), "↑↓/PgUp/PgDn 滚动 · Tab 列表/详情 · Enter 跳转 · r 刷新 · q 退出")
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], m.width, "…")
	}
	return strings.Join(lines, "\n")
}
func (m model) detailLines() []string {
	if len(m.rows) == 0 {
		return []string{"等待控制面状态。退出总览不会停止后台 Worker。"}
	}
	r := m.rows[min(m.selected, len(m.rows)-1)]
	lines := []string{r.Option.DisplayName + " (" + r.Option.AgentID + ")"}
	if !m.connected || r.Error != "" {
		lines = append(lines, "数据陈旧 · 暂停导航；请等待连接恢复。")
	}
	if r.Attached.Readiness != nil {
		lines = append(lines, "就绪："+r.Attached.Readiness.Reason)
		if r.Attached.Readiness.NextAction != "" {
			lines = append(lines, "建议操作："+r.Attached.Readiness.NextAction)
		}
	}
	lines = append(lines, fmt.Sprintf("Worker：%s · generation %d · 心跳 %s", r.Attached.WorkerStatus, r.Attached.Generation, age(r.Attached.LastHeartbeatAt)))
	if r.Task == nil {
		lines = append(lines, "当前没有已知活动任务。待处理提示不代表所有历史任务总数。")
	} else {
		t := r.Task
		lines = append(lines, "Task："+t.Task.TaskID+" · "+taskStatus(t.Task.Status), "任务："+t.Task.Content)
		if r.Attached.ActiveRun != nil {
			run := r.Attached.ActiveRun
			lines = append(lines, "Run："+run.RunID+" · "+string(run.Status)+" · 已运行 "+age(run.StartedAt))
		}
		if t.PendingApproval != nil {
			lines = append(lines, "待处理：需要审批（"+string(t.PendingApproval.State)+"）")
		}
		if t.Task.Status == domain.TaskStatusWaitingInput {
			lines = append(lines, "待处理：等待输入，请进入 Agent 终端处理。")
		}
		if r.Attached.SuggestedTask != nil && r.Attached.SuggestedTask.TaskID != t.Task.TaskID {
			lines = append(lines, "另有待处理："+taskStatus(r.Attached.SuggestedTask.Status)+" · "+r.Attached.SuggestedTask.Summary)
		}
		if t.WorkDelivery != nil {
			lines = append(lines, "投递："+string(t.WorkDelivery.State))
		}
		if t.Task.Result != nil {
			lines = append(lines, "结果："+*t.Task.Result)
		} else if t.LatestRun != nil && t.LatestRun.TurnResult != nil && t.LatestRun.TurnResult.Body != "" {
			lines = append(lines, "最近输出："+t.LatestRun.TurnResult.Body)
		}
		if t.Task.Error != nil {
			lines = append(lines, "错误："+*t.Task.Error)
		}
		if t.Task.CompletionBasis != "" {
			lines = append(lines, "完成依据："+string(t.Task.CompletionBasis))
		}
		lines = append(lines, "业务效果独立核验：未记录；执行完成不等于业务验收。")
	}
	if r.Error != "" {
		lines = append(lines, "读取问题："+r.Error)
	}
	lines = append(lines, "Enter 只跳转已验证的现有受管 pane 0，不启动或停止 Agent。")
	return strings.Split(ansi.Wrap(safeText(strings.Join(lines, "\n"), 32768), max(1, m.width), ""), "\n")
}
func age(t time.Time) string {
	if t.IsZero() {
		return "未知"
	}
	d := time.Since(t)
	if d < 0 {
		return "时钟偏差"
	}
	return d.Truncate(time.Second).String()
}
