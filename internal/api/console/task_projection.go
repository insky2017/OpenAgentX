package console

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	openapi "openagentx/internal/api"
	"openagentx/internal/domain"
	"openagentx/internal/safeoutput"
)

const consoleTaskSummaryMaxBytes = 1 << 10

func ProjectConsoleTask(task domain.Task) (openapi.ConsoleTaskReadModel, error) {
	if err := task.ValidateTarget(); err != nil {
		return openapi.ConsoleTaskReadModel{}, err
	}
	if _, err := time.Parse(time.RFC3339Nano, task.CreatedAt); err != nil {
		return openapi.ConsoleTaskReadModel{}, fmt.Errorf("invalid Task created_at: %w", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, task.UpdatedAt); err != nil {
		return openapi.ConsoleTaskReadModel{}, fmt.Errorf("invalid Task updated_at: %w", err)
	}
	outcome := safeoutput.ProjectOutcome(task.IsTerminal(), task.Result, task.Error)
	return openapi.ConsoleTaskReadModel{
		CompletionBasis: task.CompletionBasis,
		TaskID:          task.ID, Version: task.Version, AgentID: task.TargetAgentID, Status: task.Status, Intent: task.Intent,
		Content: safeoutput.RedactText(task.Content), Result: outcome.Result, ResultTruncated: outcome.ResultTruncated,
		Error: outcome.Error, ErrorTruncated: outcome.ErrorTruncated, OutcomeState: outcome.State,
		CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
	}, nil
}

func projectConsoleTaskOption(task domain.Task) (openapi.ConsoleTaskOption, error) {
	projected, err := ProjectConsoleTask(task)
	if err != nil {
		return openapi.ConsoleTaskOption{}, err
	}
	summary := strings.TrimSpace(strings.SplitN(projected.Content, "\n", 2)[0])
	if summary == "" {
		summary = projected.TaskID
	}
	return openapi.ConsoleTaskOption{
		TaskID: projected.TaskID, Version: projected.Version, Status: projected.Status, Intent: projected.Intent,
		Summary: truncateUTF8(summary, consoleTaskSummaryMaxBytes), UpdatedAt: projected.UpdatedAt,
	}, nil
}

func ProjectConsoleMailbox(item domain.MailboxItem) (openapi.ConsoleMailboxReadModel, error) {
	if err := item.Validate(); err != nil {
		return openapi.ConsoleMailboxReadModel{}, err
	}
	if item.CreatedAt.IsZero() || item.AcceptedAt != nil && item.AcceptedAt.IsZero() ||
		item.LeaseUntil != nil && item.LeaseUntil.IsZero() {
		return openapi.ConsoleMailboxReadModel{}, fmt.Errorf("invalid Mailbox timestamps")
	}
	if item.WorkerInstanceID == "" {
		if item.FencingToken != 0 || item.LeaseUntil != nil {
			return openapi.ConsoleMailboxReadModel{}, fmt.Errorf("Mailbox lease requires a Worker identity")
		}
	} else if domain.ValidateOpaqueID("worker_instance_id", item.WorkerInstanceID) != nil || item.FencingToken <= 0 {
		return openapi.ConsoleMailboxReadModel{}, fmt.Errorf("invalid Mailbox Worker identity")
	}
	return openapi.ConsoleMailboxReadModel{
		MailboxItemID: item.ID, Kind: item.Kind, Lane: item.Lane, State: item.State, Attempts: item.Attempts,
		WorkerInstanceID: item.WorkerInstanceID, LeaseUntil: cloneTime(item.LeaseUntil),
		CreatedAt: item.CreatedAt, AcceptedAt: cloneTime(item.AcceptedAt),
	}, nil
}

func ProjectConsoleMessage(message domain.Message) (openapi.ConsoleMessageReadModel, error) {
	if err := message.ValidateTarget(); err != nil {
		return openapi.ConsoleMessageReadModel{}, err
	}
	if _, err := time.Parse(time.RFC3339Nano, message.CreatedAt); err != nil {
		return openapi.ConsoleMessageReadModel{}, fmt.Errorf("invalid Message created_at: %w", err)
	}
	return openapi.ConsoleMessageReadModel{
		MessageID: message.ID, Version: message.Version, Sequence: message.Sequence,
		Kind: message.Kind, Content: safeoutput.RedactText(message.Content), CreatedAt: message.CreatedAt,
	}, nil
}

func ProjectConsoleApproval(approval domain.ApprovalRequest) (openapi.ConsoleApprovalReadModel, error) {
	if err := approval.Validate(); err != nil {
		return openapi.ConsoleApprovalReadModel{}, err
	}
	if approval.CreatedAt.IsZero() {
		return openapi.ConsoleApprovalReadModel{}, fmt.Errorf("invalid Approval created_at")
	}
	return openapi.ConsoleApprovalReadModel{
		ApprovalRequestID: approval.ID, Mode: approval.Mode, State: approval.State,
		TargetRunID: approval.TargetRunID, ExpectedRunVersion: approval.ExpectedRunVersion,
		ExpiresAt: approval.ExpiresAt, CreatedAt: approval.CreatedAt,
	}, nil
}

func ProjectConsoleRun(run domain.RunAttempt, workerGeneration int64) (openapi.RunAttemptReadModel, error) {
	if err := run.Validate(); err != nil {
		return openapi.RunAttemptReadModel{}, err
	}
	if workerGeneration <= 0 || run.CreatedAt.IsZero() || run.UpdatedAt.IsZero() {
		return openapi.RunAttemptReadModel{}, fmt.Errorf("invalid Run Worker generation or timestamps")
	}
	policy := resolvedConsoleNetwork(run)
	model := openapi.RunAttemptReadModel{
		ID: run.ID, TaskID: run.TaskID, AgentID: run.AgentID, Version: run.Version,
		Status: run.Status, WorkerInstanceID: run.WorkerInstanceID,
		ExecutionSpecVersion: run.ExecutionSpecVersion, AdapterID: run.AdapterID,
		BackendID: run.BackendID, Model: run.Model, ReasoningMode: run.ReasoningMode,
		ReasoningValue: run.ReasoningValue, NetworkMode: policy.Mode, NetworkProfileID: policy.ProfileID,
		NetworkProfileVersion: policy.ProfileVersion, NetworkPolicyVersion: policy.PolicyVersion,
		NetworkBindingRevision: policy.BindingRevision, StartedAt: run.StartedAt,
		FinishedAt: cloneTime(run.FinishedAt), CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt,
	}
	generation := workerGeneration
	model.WorkerGeneration = &generation
	model.TurnResult, model.TurnResultState = consoleTurnResult(run.ResultJSON)
	return model, nil
}

func projectConsoleTaskSnapshot(snapshot domain.ConsoleTaskSnapshot, agentID, taskID string) (openapi.ConsoleTaskSnapshot, error) {
	if snapshot.SnapshotSequence < 0 || snapshot.Task.ID != taskID || snapshot.Task.TargetAgentID != agentID {
		return openapi.ConsoleTaskSnapshot{}, fmt.Errorf("invalid Console Task snapshot identity or cursor")
	}
	task, err := ProjectConsoleTask(snapshot.Task)
	if err != nil {
		return openapi.ConsoleTaskSnapshot{}, fmt.Errorf("project Console Task: %w", err)
	}
	result := openapi.ConsoleTaskSnapshot{Task: task, SnapshotSequence: snapshot.SnapshotSequence}
	if snapshot.WorkDelivery != nil {
		item := *snapshot.WorkDelivery
		if item.TaskID != taskID || item.TargetAgentID != agentID || item.Kind != domain.MailboxKindTask || item.Lane != domain.MailboxLaneWork {
			return openapi.ConsoleTaskSnapshot{}, fmt.Errorf("invalid Console Task work delivery ownership")
		}
		projected, err := ProjectConsoleMailbox(item)
		if err != nil {
			return openapi.ConsoleTaskSnapshot{}, fmt.Errorf("project Console Task work delivery: %w", err)
		}
		result.WorkDelivery = &projected
	}
	if snapshot.LatestRun != nil {
		run := *snapshot.LatestRun
		if run.TaskID != taskID || run.AgentID != agentID {
			return openapi.ConsoleTaskSnapshot{}, fmt.Errorf("invalid Console Task Run ownership")
		}
		projected, err := ProjectConsoleRun(run, snapshot.LatestRunWorkerGeneration)
		if err != nil {
			return openapi.ConsoleTaskSnapshot{}, fmt.Errorf("project Console Task Run: %w", err)
		}
		result.LatestRun = &projected
	} else if snapshot.LatestRunWorkerGeneration != 0 {
		return openapi.ConsoleTaskSnapshot{}, fmt.Errorf("Run generation requires a Run snapshot")
	}
	if snapshot.LatestMessage != nil {
		message := *snapshot.LatestMessage
		if message.TaskID != taskID || message.TargetAgentID != agentID {
			return openapi.ConsoleTaskSnapshot{}, fmt.Errorf("invalid Console Task Message ownership")
		}
		projected, err := ProjectConsoleMessage(message)
		if err != nil {
			return openapi.ConsoleTaskSnapshot{}, fmt.Errorf("project Console Task Message: %w", err)
		}
		result.LatestMessage = &projected
	}
	if snapshot.PendingApproval != nil {
		approval := *snapshot.PendingApproval
		if approval.TaskID != taskID || approval.State != domain.ApprovalRequestPending {
			return openapi.ConsoleTaskSnapshot{}, fmt.Errorf("invalid Console Task Approval ownership or state")
		}
		projected, err := ProjectConsoleApproval(approval)
		if err != nil {
			return openapi.ConsoleTaskSnapshot{}, fmt.Errorf("project Console Task Approval: %w", err)
		}
		result.PendingApproval = &projected
	}
	return result, nil
}

func consoleTurnResult(raw string) (*openapi.TurnResultReadModel, string) {
	result, state := safeoutput.DecodeTurnResult(raw)
	if result == nil {
		return nil, state
	}
	source := "not_recorded"
	if result.RuntimeSideEffectsKnown != nil {
		source = "runtime_reported"
	}
	return &openapi.TurnResultReadModel{
		FinalReply:    result.FinalReply,
		RuntimeStatus: result.Status, Body: result.Result, BodyTruncated: result.ResultTruncated,
		Error: result.Error, ErrorTruncated: result.ErrorTruncated, RuntimeSideEffectsKnown: result.RuntimeSideEffectsKnown,
		SideEffectsSource: source, BusinessVerificationSource: "not_recorded",
	}, state
}

func resolvedConsoleNetwork(run domain.RunAttempt) domain.NetworkPolicy {
	var resolved domain.ResolvedExecutionSpec
	if json.Unmarshal([]byte(run.ResolvedExecutionJSON), &resolved) != nil {
		return domain.NetworkPolicy{}
	}
	return resolved.Spec.Network
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func truncateUTF8(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	const marker = " [TRUNCATED]"
	cut := limit - len(marker)
	if cut < 0 {
		cut = 0
	}
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut] + marker
}
