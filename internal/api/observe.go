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
	WorkerInstanceID string                               `json:"worker_instance_id"`
	AgentID          string                               `json:"agent_id"`
	Generation       int64                                `json:"generation"`
	Capabilities     []string                             `json:"capabilities,omitempty"`
	BackendHealth    map[string]openruntime.BackendHealth `json:"backend_health,omitempty"`
	Status           domain.WorkerStatus                  `json:"status"`
	LastHeartbeatAt  time.Time                            `json:"last_heartbeat_at"`
	LeaseUntil       time.Time                            `json:"lease_until"`
	StartedAt        time.Time                            `json:"started_at"`
	UpdatedAt        time.Time                            `json:"updated_at"`
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
	ID              string              `json:"id"`
	Version         int64               `json:"version"`
	TargetAgentID   string              `json:"target_agent_id"`
	OrganizationID  string              `json:"organization_id,omitempty"`
	DispatchMode    domain.DispatchMode `json:"dispatch_mode,omitempty"`
	Content         string              `json:"content"`
	Status          domain.TaskStatus   `json:"status"`
	Result          *string             `json:"result,omitempty"`
	ResultTruncated bool                `json:"result_truncated,omitempty"`
	Error           *string             `json:"error,omitempty"`
	ErrorTruncated  bool                `json:"error_truncated,omitempty"`
	OutcomeState    string              `json:"outcome_state"`
	CreatedAt       string              `json:"created_at"`
	UpdatedAt       string              `json:"updated_at"`
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
	BodyTruncated              bool                         `json:"body_truncated,omitempty"`
	Error                      string                       `json:"error,omitempty"`
	ErrorTruncated             bool                         `json:"error_truncated,omitempty"`
	RuntimeSideEffectsKnown    *bool                        `json:"runtime_side_effects_known,omitempty"`
	SideEffectsSource          string                       `json:"side_effects_source"`
	BusinessVerificationSource string                       `json:"business_verification_source"`
}

type JournalEventReadModel struct {
	Sequence      int64                     `json:"sequence"`
	ID            string                    `json:"event_id"`
	AggregateType string                    `json:"aggregate_type"`
	AggregateID   string                    `json:"aggregate_id"`
	EventType     string                    `json:"event_type"`
	CreatedAt     time.Time                 `json:"created_at"`
	Output        *SafeOutputReadModel      `json:"output,omitempty"`
	Worker        *WorkerReadModel          `json:"worker,omitempty"`
	Run           *RunAttemptReadModel      `json:"run,omitempty"`
	Task          *ConsoleTaskReadModel     `json:"task,omitempty"`
	Mailbox       *ConsoleMailboxReadModel  `json:"mailbox,omitempty"`
	Message       *ConsoleMessageReadModel  `json:"message,omitempty"`
	Approval      *ConsoleApprovalReadModel `json:"approval,omitempty"`
}

type ConsoleTaskOption struct {
	TaskID    string            `json:"task_id"`
	Version   int64             `json:"version"`
	Status    domain.TaskStatus `json:"status"`
	Summary   string            `json:"summary"`
	UpdatedAt string            `json:"updated_at"`
}

type ConsoleTaskPage struct {
	Tasks      []ConsoleTaskOption `json:"tasks"`
	NextCursor string              `json:"next_cursor,omitempty"`
	HasMore    bool                `json:"has_more"`
}

type ConsoleTaskSnapshot struct {
	Task             ConsoleTaskReadModel      `json:"task"`
	WorkDelivery     *ConsoleMailboxReadModel  `json:"work_delivery,omitempty"`
	LatestRun        *RunAttemptReadModel      `json:"latest_run,omitempty"`
	LatestMessage    *ConsoleMessageReadModel  `json:"latest_message,omitempty"`
	PendingApproval  *ConsoleApprovalReadModel `json:"pending_approval,omitempty"`
	SnapshotSequence int64                     `json:"snapshot_sequence"`
}

type ConsoleTaskReadModel struct {
	TaskID          string            `json:"task_id"`
	Version         int64             `json:"version"`
	AgentID         string            `json:"agent_id"`
	Status          domain.TaskStatus `json:"status"`
	Content         string            `json:"content"`
	Result          *string           `json:"result,omitempty"`
	ResultTruncated bool              `json:"result_truncated,omitempty"`
	Error           *string           `json:"error,omitempty"`
	ErrorTruncated  bool              `json:"error_truncated,omitempty"`
	OutcomeState    string            `json:"outcome_state"`
	CreatedAt       string            `json:"created_at"`
	UpdatedAt       string            `json:"updated_at"`
}

type ConsoleMailboxReadModel struct {
	MailboxItemID    string              `json:"mailbox_item_id"`
	Kind             domain.MailboxKind  `json:"kind"`
	Lane             domain.MailboxLane  `json:"lane"`
	State            domain.MailboxState `json:"state"`
	Attempts         int                 `json:"attempts"`
	WorkerInstanceID string              `json:"worker_instance_id,omitempty"`
	LeaseUntil       *time.Time          `json:"lease_until,omitempty"`
	CreatedAt        time.Time           `json:"created_at"`
	AcceptedAt       *time.Time          `json:"accepted_at,omitempty"`
}

type ConsoleMessageReadModel struct {
	MessageID string             `json:"message_id"`
	Version   int64              `json:"version"`
	Sequence  int64              `json:"sequence"`
	Kind      domain.MessageKind `json:"kind"`
	Content   string             `json:"content"`
	CreatedAt string             `json:"created_at"`
}

type ConsoleApprovalReadModel struct {
	ApprovalRequestID  string                      `json:"approval_request_id"`
	Mode               domain.ApprovalMode         `json:"mode"`
	State              domain.ApprovalRequestState `json:"state"`
	TargetRunID        string                      `json:"target_run_id,omitempty"`
	ExpectedRunVersion int64                       `json:"expected_run_version,omitempty"`
	ExpiresAt          time.Time                   `json:"expires_at"`
	CreatedAt          time.Time                   `json:"created_at"`
}

type SafeOutputReadModel struct {
	Stage               string `json:"stage,omitempty"`
	Status              string `json:"status,omitempty"`
	Text                string `json:"text,omitempty"`
	TextTruncated       bool   `json:"text_truncated,omitempty"`
	Diagnostic          string `json:"diagnostic,omitempty"`
	DiagnosticTruncated bool   `json:"diagnostic_truncated,omitempty"`
	HasOutput           bool   `json:"has_output,omitempty"`
	HasError            bool   `json:"has_error,omitempty"`
}

type EventPage struct {
	Events       []domain.JournalEvent `json:"events"`
	NextSequence int64                 `json:"next_sequence"`
}
