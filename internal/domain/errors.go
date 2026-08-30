package domain

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound                  = errors.New("resource not found")
	ErrAgentNotFound             = errors.New("agent not found")
	ErrAgentProfileNotFound      = errors.New("agent profile not found")
	ErrAgentSessionNotFound      = errors.New("agent session not found")
	ErrTaskNotFound              = errors.New("task not found")
	ErrWorkerBusy                = errors.New("worker already has an active task")
	ErrInvalidState              = errors.New("invalid state")
	ErrInvalidTransition         = errors.New("invalid state transition")
	ErrTerminalState             = errors.New("cannot transition from terminal state")
	ErrUnauthorized              = errors.New("unauthorized actor for this action")
	ErrIdempotencyConflict       = errors.New("idempotency key conflict with different payload")
	ErrInvalidAddress            = errors.New("invalid tmux address")
	ErrAgentNotReady             = errors.New("agent is not ready to participate in tasks")
	ErrSessionGenerationConflict = errors.New("session generation conflict")
	ErrInvalidManifest           = errors.New("invalid agent manifest")
	ErrUnsupportedCapability     = errors.New("unsupported capability")
	ErrStaleVersion              = errors.New("stale resource version")
	ErrLeaseExpired              = errors.New("lease expired")
	ErrFencingRejected           = errors.New("fencing token rejected")
	ErrTaskCancelRequested       = errors.New("task cancellation requested")
	ErrApprovalStale             = errors.New("approval request is stale")
)

type DomainError struct {
	Code    string
	Message string
	Err     error
}

func (e *DomainError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *DomainError) Unwrap() error {
	return e.Err
}

func ErrInvalidInput(msg string) error {
	return &DomainError{
		Code:    "INVALID_INPUT",
		Message: msg,
	}
}

func ErrForbidden(msg string) error {
	return &DomainError{
		Code:    "FORBIDDEN",
		Message: msg,
	}
}

func ErrConflict(msg string) error {
	return &DomainError{
		Code:    "CONFLICT",
		Message: msg,
	}
}
