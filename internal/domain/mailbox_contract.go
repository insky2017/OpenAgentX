package domain

import (
	"sort"
	"time"
)

type MailboxKind string

const (
	MailboxKindTask     MailboxKind = "task"
	MailboxKindMessage  MailboxKind = "message"
	MailboxKindApproval MailboxKind = "approval"
	MailboxKindCancel   MailboxKind = "cancel"
)

func (k MailboxKind) Valid() bool {
	return k == MailboxKindTask || k == MailboxKindMessage || k == MailboxKindApproval || k == MailboxKindCancel
}

type MailboxLane string

const (
	MailboxLaneControl MailboxLane = "control"
	MailboxLaneWork    MailboxLane = "work"
)

func (l MailboxLane) Valid() bool {
	return l == MailboxLaneControl || l == MailboxLaneWork
}

func (l MailboxLane) Priority() int {
	if l == MailboxLaneControl {
		return 0
	}
	return 1
}

type MailboxState string

const (
	MailboxStatePending    MailboxState = "pending"
	MailboxStateClaimed    MailboxState = "claimed"
	MailboxStateAccepted   MailboxState = "accepted"
	MailboxStateSuperseded MailboxState = "superseded"
	MailboxStateFailed     MailboxState = "failed"
)

func (s MailboxState) Valid() bool {
	switch s {
	case MailboxStatePending, MailboxStateClaimed, MailboxStateAccepted, MailboxStateSuperseded, MailboxStateFailed:
		return true
	default:
		return false
	}
}

type MailboxItem struct {
	Sequence           int64        `json:"sequence"`
	ID                 string       `json:"mailbox_item_id"`
	TargetAgentID      string       `json:"target_agent_id"`
	Kind               MailboxKind  `json:"kind"`
	Lane               MailboxLane  `json:"lane"`
	TaskID             string       `json:"task_id,omitempty"`
	MessageID          string       `json:"message_id,omitempty"`
	ApprovalRequestID  string       `json:"approval_request_id,omitempty"`
	ApprovalDecisionID string       `json:"approval_decision_id,omitempty"`
	TargetRunID        string       `json:"target_run_id,omitempty"`
	ExpectedRunVersion int64        `json:"expected_run_version,omitempty"`
	State              MailboxState `json:"state"`
	WorkerInstanceID   string       `json:"worker_instance_id,omitempty"`
	FencingToken       int64        `json:"fencing_token,omitempty"`
	LeaseUntil         *time.Time   `json:"lease_until,omitempty"`
	Attempts           int          `json:"attempts"`
	CreatedAt          time.Time    `json:"created_at"`
	AcceptedAt         *time.Time   `json:"accepted_at,omitempty"`
}

// MailboxPayload is the control payload resolved under a claimed mailbox
// item's Worker authority. Exactly one field is populated for supported kinds.
type MailboxPayload struct {
	Message          *Message
	ApprovalDecision *ApprovalDecision
}

func (m MailboxItem) Validate() error {
	if m.Sequence <= 0 {
		return ErrInvalidInput("mailbox sequence must be positive")
	}
	if err := ValidateOpaqueID("mailbox_item_id", m.ID); err != nil {
		return err
	}
	if err := ValidateIdentifier("target_agent_id", m.TargetAgentID); err != nil {
		return err
	}
	if !m.Kind.Valid() || !m.Lane.Valid() || !m.State.Valid() {
		return ErrInvalidInput("unsupported mailbox kind, lane, or state")
	}
	if m.Kind == MailboxKindTask && m.Lane != MailboxLaneWork {
		return ErrInvalidInput("task mailbox items must use work lane")
	}
	if m.Kind == MailboxKindCancel && m.Lane != MailboxLaneControl {
		return ErrInvalidInput("cancel mailbox items must use control lane")
	}
	if m.Lane == MailboxLaneControl {
		if err := ValidateOpaqueID("target_run_id", m.TargetRunID); err != nil {
			return err
		}
		if err := ValidatePositiveVersion("expected_run_version", m.ExpectedRunVersion); err != nil {
			return err
		}
	}
	if m.Attempts < 0 || m.FencingToken < 0 {
		return ErrInvalidInput("mailbox attempts and fencing token cannot be negative")
	}
	return nil
}

func (m MailboxItem) ValidateForInsert() error {
	copy := m
	copy.Sequence = 1
	return copy.Validate()
}

func SortMailboxItems(items []MailboxItem) {
	sort.SliceStable(items, func(i, j int) bool {
		leftPriority := items[i].Lane.Priority()
		rightPriority := items[j].Lane.Priority()
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return items[i].Sequence < items[j].Sequence
	})
}
