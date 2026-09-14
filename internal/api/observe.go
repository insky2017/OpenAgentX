package api

import (
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

const (
	ObserveOverviewPath         = "/api/observe/v1/overview"
	ObserveOrganizationPath     = "/api/observe/v1/organization"
	ObserveAgentsPath           = "/api/observe/v1/agents"
	ObserveTasksPath            = "/api/observe/v1/tasks"
	ObserveMailboxesPath        = "/api/observe/v1/mailboxes"
	ObserveRunAttemptPath       = "/api/observe/v1/run-attempts/{run-id}"
	ObserveExecutionOptionsPath = "/api/observe/v1/execution-options"
	ObserveNetworkProfilesPath  = "/api/observe/v1/network-profiles"
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

type WorkerReadModel struct {
	WorkerInstanceID string              `json:"worker_instance_id"`
	AgentID          string              `json:"agent_id"`
	Generation       int64               `json:"generation"`
	Capabilities     []string            `json:"capabilities,omitempty"`
	Status           domain.WorkerStatus `json:"status"`
	LastHeartbeatAt  time.Time           `json:"last_heartbeat_at"`
	LeaseUntil       time.Time           `json:"lease_until"`
	StartedAt        time.Time           `json:"started_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
}

type TaskReadModel struct {
	Task                  TaskReadModelTask       `json:"task"`
	Messages              []domain.Message        `json:"messages"`
	RunAttempts           []RunAttemptReadModel   `json:"run_attempts"`
	Events                []JournalEventReadModel `json:"events"`
	SnapshotSequence      int64                   `json:"snapshot_sequence"`
	LiveAfterSequence     int64                   `json:"live_after_sequence"`
	HistoryBeforeSequence int64                   `json:"history_before_sequence,omitempty"`
	HasOlderEvents        bool                    `json:"has_older_events"`
	HasMoreLiveEvents     bool                    `json:"has_more_live_events"`
}

// TaskListItem contains only the fields needed to browse and select a Task.
// Full content and result data remain on the authenticated detail endpoint.
type TaskListItem struct {
	ID            string            `json:"id"`
	TargetAgentID string            `json:"target_agent_id"`
	Status        domain.TaskStatus `json:"status"`
	Summary       string            `json:"summary"`
	CreatedAt     string            `json:"created_at"`
	UpdatedAt     string            `json:"updated_at"`
}

type TaskListPage struct {
	Tasks      []TaskListItem `json:"tasks"`
	NextCursor string         `json:"next_cursor,omitempty"`
	HasMore    bool           `json:"has_more"`
}

// TaskReadModelTask is the browser-safe subset of a Task. Idempotency keys,
// sender principals and cancellation actors remain control-plane data.
type TaskReadModelTask struct {
	ID             string              `json:"id"`
	Version        int64               `json:"version"`
	TargetAgentID  string              `json:"target_agent_id"`
	OrganizationID string              `json:"organization_id,omitempty"`
	DispatchMode   domain.DispatchMode `json:"dispatch_mode,omitempty"`
	Content        string              `json:"content"`
	Status         domain.TaskStatus   `json:"status"`
	Result         *string             `json:"result,omitempty"`
	Error          *string             `json:"error,omitempty"`
	CreatedAt      string              `json:"created_at"`
	UpdatedAt      string              `json:"updated_at"`
}

// RunAttemptReadModel deliberately omits execution JSON and fencing material.
// Those fields are control-plane evidence, not browser-facing observation data.
type RunAttemptReadModel struct {
	ID                     string                  `json:"run_id"`
	TaskID                 string                  `json:"task_id"`
	AgentID                string                  `json:"agent_id"`
	Version                int64                   `json:"version"`
	Status                 domain.RunAttemptStatus `json:"status"`
	WorkerInstanceID       string                  `json:"worker_instance_id"`
	WorkerGeneration       *int64                  `json:"worker_generation,omitempty"`
	ExecutionSpecVersion   int64                   `json:"execution_spec_version"`
	AdapterID              string                  `json:"adapter_id"`
	BackendID              string                  `json:"backend_id"`
	Model                  string                  `json:"model"`
	ReasoningMode          domain.ReasoningMode    `json:"reasoning_mode"`
	ReasoningValue         string                  `json:"reasoning_value,omitempty"`
	NetworkMode            domain.NetworkMode      `json:"network_mode,omitempty"`
	NetworkProfileID       string                  `json:"network_profile_id,omitempty"`
	NetworkProfileVersion  int64                   `json:"network_profile_version,omitempty"`
	NetworkPolicyVersion   int64                   `json:"network_policy_version,omitempty"`
	NetworkBindingRevision int64                   `json:"network_binding_revision,omitempty"`
	TurnResult             *TurnResultReadModel    `json:"turn_result,omitempty"`
	TurnResultState        string                  `json:"turn_result_state"`
	StartedAt              time.Time               `json:"started_at"`
	FinishedAt             *time.Time              `json:"finished_at,omitempty"`
	CreatedAt              time.Time               `json:"created_at"`
	UpdatedAt              time.Time               `json:"updated_at"`
}

type TurnResultReadModel struct {
	RuntimeStatus              openruntime.TurnResultStatus `json:"runtime_status"`
	Body                       string                       `json:"body,omitempty"`
	Error                      string                       `json:"error,omitempty"`
	RuntimeSideEffectsKnown    *bool                        `json:"runtime_side_effects_known,omitempty"`
	SideEffectsSource          string                       `json:"side_effects_source"`
	BusinessVerificationSource string                       `json:"business_verification_source"`
}

type JournalEventReadModel struct {
	Sequence      int64                `json:"sequence"`
	ID            string               `json:"event_id"`
	AggregateType string               `json:"aggregate_type"`
	AggregateID   string               `json:"aggregate_id"`
	EventType     string               `json:"event_type"`
	CreatedAt     time.Time            `json:"created_at"`
	Output        *SafeOutputReadModel `json:"output,omitempty"`
	Worker        *WorkerReadModel     `json:"worker,omitempty"`
}

type SafeOutputReadModel struct {
	Stage      string `json:"stage,omitempty"`
	Status     string `json:"status,omitempty"`
	Text       string `json:"text,omitempty"`
	Diagnostic string `json:"diagnostic,omitempty"`
	HasOutput  bool   `json:"has_output,omitempty"`
	HasError   bool   `json:"has_error,omitempty"`
}

type EventPage struct {
	Events       []domain.JournalEvent `json:"events"`
	NextSequence int64                 `json:"next_sequence"`
}
