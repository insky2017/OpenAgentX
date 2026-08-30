package api

import (
	"openagentx/internal/domain"
)

const (
	ObserveOverviewPath         = "/api/observe/v1/overview"
	ObserveOrganizationPath     = "/api/observe/v1/organization"
	ObserveAgentsPath           = "/api/observe/v1/agents"
	ObserveTasksPath            = "/api/observe/v1/tasks"
	ObserveMailboxesPath        = "/api/observe/v1/mailboxes"
	ObserveRunAttemptPath       = "/api/observe/v1/run-attempts/{run-id}"
	ObserveExecutionOptionsPath = "/api/observe/v1/execution-options"
	ObserveEventsStreamPath     = "/api/observe/v1/events/stream"
	ObserveHealthPath           = "/api/observe/v1/health"
)

type AgentReadModel struct {
	AgentID             string                   `json:"agent_id"`
	PositionID          string                   `json:"position_id,omitempty"`
	Connectivity        domain.Connectivity      `json:"connectivity"`
	Availability        domain.Availability      `json:"availability"`
	DeliveryReadiness   domain.DeliveryReadiness `json:"delivery_readiness"`
	Worker              *domain.WorkerInstance   `json:"worker,omitempty"`
	ActiveRun           *domain.RunAttempt       `json:"active_run,omitempty"`
	PendingMailboxItems int                      `json:"pending_mailbox_items"`
}

type TaskReadModel struct {
	Task         domain.Task         `json:"task"`
	Messages     []domain.Message    `json:"messages"`
	RunAttempts  []domain.RunAttempt `json:"run_attempts"`
	LastSequence int64               `json:"last_sequence"`
}

type EventPage struct {
	Events       []domain.JournalEvent `json:"events"`
	NextSequence int64                 `json:"next_sequence"`
}
