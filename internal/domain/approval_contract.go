package domain

import "time"

type ApprovalMode string

const (
	ApprovalModeNative    ApprovalMode = "native"
	ApprovalModePreflight ApprovalMode = "preflight"
)

func (m ApprovalMode) Valid() bool {
	return m == ApprovalModeNative || m == ApprovalModePreflight
}

type ApprovalRequestState string

const (
	ApprovalRequestPending  ApprovalRequestState = "pending"
	ApprovalRequestApproved ApprovalRequestState = "approved"
	ApprovalRequestRejected ApprovalRequestState = "rejected"
	ApprovalRequestStale    ApprovalRequestState = "stale"
	ApprovalRequestConsumed ApprovalRequestState = "consumed"
	ApprovalRequestExpired  ApprovalRequestState = "expired"
)

func (s ApprovalRequestState) Valid() bool {
	switch s {
	case ApprovalRequestPending, ApprovalRequestApproved, ApprovalRequestRejected,
		ApprovalRequestStale, ApprovalRequestConsumed, ApprovalRequestExpired:
		return true
	default:
		return false
	}
}

type ApprovalDecisionValue string

const (
	ApprovalDecisionApprove ApprovalDecisionValue = "approve"
	ApprovalDecisionReject  ApprovalDecisionValue = "reject"
)

func (d ApprovalDecisionValue) Valid() bool {
	return d == ApprovalDecisionApprove || d == ApprovalDecisionReject
}

type ApprovalDecisionState string

const (
	ApprovalDecisionPersisted  ApprovalDecisionState = "persisted"
	ApprovalDecisionApplied    ApprovalDecisionState = "applied"
	ApprovalDecisionSuperseded ApprovalDecisionState = "superseded"
)

func (s ApprovalDecisionState) Valid() bool {
	return s == ApprovalDecisionPersisted || s == ApprovalDecisionApplied || s == ApprovalDecisionSuperseded
}

type ApprovalRequest struct {
	ID                 string               `json:"approval_request_id"`
	TaskID             string               `json:"task_id"`
	Mode               ApprovalMode         `json:"mode"`
	TargetRunID        string               `json:"target_run_id,omitempty"`
	ExpectedRunVersion int64                `json:"expected_run_version,omitempty"`
	ScopeDigest        string               `json:"scope_digest"`
	State              ApprovalRequestState `json:"state"`
	ExpiresAt          time.Time            `json:"expires_at"`
	CreatedAt          time.Time            `json:"created_at"`
}

func (r ApprovalRequest) Validate() error {
	if err := ValidateOpaqueID("approval_request_id", r.ID); err != nil {
		return err
	}
	if err := ValidateOpaqueID("task_id", r.TaskID); err != nil {
		return err
	}
	if !r.Mode.Valid() || !r.State.Valid() {
		return ErrInvalidInput("unsupported approval mode or request state")
	}
	if err := ValidateOpaqueID("scope_digest", r.ScopeDigest); err != nil {
		return err
	}
	if r.ExpiresAt.IsZero() {
		return ErrInvalidInput("approval expires_at is required")
	}
	if r.Mode == ApprovalModeNative {
		if err := ValidateOpaqueID("target_run_id", r.TargetRunID); err != nil {
			return err
		}
		if err := ValidatePositiveVersion("expected_run_version", r.ExpectedRunVersion); err != nil {
			return err
		}
	} else if r.TargetRunID != "" || r.ExpectedRunVersion != 0 {
		return ErrInvalidInput("preflight approval cannot bind an active run")
	}
	return nil
}

type ApprovalDecision struct {
	ID                string                `json:"approval_decision_id"`
	ApprovalRequestID string                `json:"approval_request_id"`
	DecidedBy         string                `json:"decided_by"`
	Decision          ApprovalDecisionValue `json:"decision"`
	State             ApprovalDecisionState `json:"state"`
	IdempotencyKey    string                `json:"idempotency_key"`
	CreatedAt         time.Time             `json:"created_at"`
}

func (d ApprovalDecision) Validate() error {
	if err := ValidateOpaqueID("approval_decision_id", d.ID); err != nil {
		return err
	}
	if err := ValidateOpaqueID("approval_request_id", d.ApprovalRequestID); err != nil {
		return err
	}
	if err := ValidateOpaqueID("decided_by", d.DecidedBy); err != nil {
		return err
	}
	if !d.Decision.Valid() || !d.State.Valid() {
		return ErrInvalidInput("unsupported approval decision or state")
	}
	return ValidateOpaqueID("idempotency_key", d.IdempotencyKey)
}
