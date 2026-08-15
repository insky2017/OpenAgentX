package domain

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound            = errors.New("not found")
	ErrAgentNotFound       = errors.New("agent not found")
	ErrTaskNotFound        = errors.New("task not found")
	ErrIdempotencyConflict = errors.New("idempotency conflict")
	ErrWorkerBusy          = errors.New("worker busy: target agent already has an active task")
	ErrInvalidState        = errors.New("invalid task state")
	ErrInvalidTransition   = errors.New("invalid state transition")
	ErrUnauthorized        = errors.New("unauthorized actor for this action")
	ErrInvalidAddress      = errors.New("invalid connector address")
	ErrTerminalState       = errors.New("task is already in terminal state")
)

type DomainError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *DomainError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
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
