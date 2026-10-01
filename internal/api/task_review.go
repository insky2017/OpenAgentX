package api

import (
	"openagentx/internal/domain"
	"strings"
)

type ReviewTaskRequest struct {
	Meta       CommandMeta `json:"meta"`
	RunID      string      `json:"run_id"`
	RunVersion int64       `json:"run_version"`
	Decision   string      `json:"decision"`
	Note       string      `json:"note,omitempty"`
}

func (r ReviewTaskRequest) Validate() error {
	if err := r.Meta.Validate(true); err != nil {
		return err
	}
	if err := domain.ValidateOpaqueID("run_id", r.RunID); err != nil {
		return err
	}
	if r.RunVersion < 1 {
		return domain.ErrInvalidInput("run_version is required")
	}
	if r.Decision != "accepted" && r.Decision != "rejected" {
		return domain.ErrInvalidInput("decision must be accepted or rejected")
	}
	if len(r.Note) > 4096 || strings.ContainsRune(r.Note, '\x00') {
		return domain.ErrInvalidInput("review note must be at most 4096 bytes")
	}
	return nil
}
