package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const ContractVersion = "v1"

const (
	ErrorInvalidRequest        = "INVALID_REQUEST"
	ErrorUnauthorized          = "UNAUTHORIZED"
	ErrorForbidden             = "FORBIDDEN"
	ErrorNotFound              = "NOT_FOUND"
	ErrorConflict              = "CONFLICT"
	ErrorStaleVersion          = "STALE_VERSION"
	ErrorLeaseExpired          = "LEASE_EXPIRED"
	ErrorFencingRejected       = "FENCING_REJECTED"
	ErrorUnsupportedCapability = "UNSUPPORTED_CAPABILITY"
	ErrorBackendUnavailable    = "BACKEND_UNAVAILABLE"
	ErrorInternal              = "INTERNAL_ERROR"
)

type ErrorResponse struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	RequestID string         `json:"request_id,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

type CommandMeta struct {
	IdempotencyKey   string `json:"idempotency_key"`
	ExpectedVersion  int64  `json:"expected_version,omitempty"`
	ExpectedSequence int64  `json:"expected_sequence,omitempty"`
}

func (m CommandMeta) Validate(requireVersion bool) error {
	key := strings.TrimSpace(m.IdempotencyKey)
	if key == "" || len(key) > 128 || strings.ContainsAny(key, "\r\n\t") {
		return fmt.Errorf("invalid idempotency_key")
	}
	if requireVersion && m.ExpectedVersion <= 0 {
		return fmt.Errorf("expected_version must be positive")
	}
	if m.ExpectedVersion < 0 || m.ExpectedSequence < 0 {
		return fmt.Errorf("expected version and sequence cannot be negative")
	}
	return nil
}

func DecodeStrictJSON(reader io.Reader, destination any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("request body must contain exactly one JSON value")
		}
		return err
	}
	return nil
}

func ValidateContractVersion(version string) error {
	if version != ContractVersion {
		return fmt.Errorf("unsupported contract_version %q", version)
	}
	return nil
}
