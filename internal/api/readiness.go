package api

import (
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	"time"
)

type AgentReadiness struct {
	Ready       bool   `json:"ready"`
	CanStartNow bool   `json:"can_start_now"`
	Reason      string `json:"reason"`
	NextAction  string `json:"next_action,omitempty"`
}

// ProjectAgentReadiness consumes the latest Worker and generation-bound backend
// registrations returned by ListWorkerBackends, the same facts used to schedule.
func ProjectAgentReadiness(agentID string, worker *domain.WorkerInstance, tasks []domain.Task, backends []openruntime.BackendRegistration, now time.Time) AgentReadiness {
	state := AgentReadiness{Reason: "后台未运行", NextAction: "openagentx agent resume " + agentID}
	if worker == nil {
		return state
	}
	if worker.Status == domain.WorkerStatusDraining {
		state.Reason = "暂停中，当前工作结束后停止"
		return state
	}
	if worker.Status != domain.WorkerStatusOnline {
		return state
	}
	if !worker.LeaseUntil.After(now) {
		state.Reason = "后台连接已过期，请恢复"
		return state
	}
	state.Reason = "运行环境或网络尚未就绪"
	for _, backend := range backends {
		if backend.Health == openruntime.BackendHealthy {
			state = AgentReadiness{Ready: true, CanStartNow: true, Reason: "可开始工作"}
			break
		}
	}
	if state.Ready {
		for _, task := range tasks {
			if task.TargetAgentID == agentID && (task.Status == domain.TaskStatusRunning || task.Status == domain.TaskStatusWaitingApproval || task.Status == domain.TaskStatusCancelRequested) {
				state.CanStartNow = false
				state.Reason = "正在工作，新任务将排队"
				break
			}
		}
	}
	return state
}
