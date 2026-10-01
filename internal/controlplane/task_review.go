package controlplane

import (
	"context"
	"openagentx/internal/api"
	"openagentx/internal/domain"
	"openagentx/internal/safeoutput"
)

type taskReviewState interface {
	ReviewTaskResult(context.Context, int64, domain.TaskReview) (*domain.TaskReview, error)
}

func (s *CommandService) ReviewTaskResult(ctx context.Context, principal, taskID string, req api.ReviewTaskRequest) (*domain.TaskReview, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	state, ok := s.state.(taskReviewState)
	if !ok {
		return nil, domain.ErrUnsupportedCapability
	}
	return state.ReviewTaskResult(ctx, req.Meta.ExpectedVersion, domain.TaskReview{
		ID: commandIdempotentID("task-review", principal, req.Meta.IdempotencyKey), TaskID: taskID,
		RunID: req.RunID, RunVersion: req.RunVersion, Decision: req.Decision,
		Note: safeoutput.RedactText(req.Note), ReviewedBy: principal,
	})
}
