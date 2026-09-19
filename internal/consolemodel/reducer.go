package consolemodel

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

type ApplyResult struct {
	CursorAdvanced bool
	Timeline       *openapi.JournalEventReadModel
}

// Reducer applies only browser-safe event projections. It owns the Console's
// current Worker identity and last successfully applied Event Journal cursor.
type Reducer struct {
	state         consoleapi.AttachResponse
	initialized   bool
	cursor        int64
	connection    ConnectionState
	pendingMode   string
	streamEpoch   uint64
	tasks         map[string]*taskRecord
	focusedTaskID string
	focusSource   FocusSource
	taskOrder     int64
	timeline      []TimelineItem
	timelineBytes int
}

func New(snapshot consoleapi.AttachResponse) (*Reducer, error) {
	r := &Reducer{connection: ConnectionConnecting, streamEpoch: 1, tasks: make(map[string]*taskRecord)}
	if err := r.ApplySnapshotForStream(r.streamEpoch, snapshot); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Reducer) ApplySnapshot(snapshot consoleapi.AttachResponse) error {
	return r.ApplySnapshotForStream(r.streamEpoch, snapshot)
}

func (r *Reducer) ApplySnapshotForStream(epoch uint64, snapshot consoleapi.AttachResponse) error {
	if epoch != r.streamEpoch {
		return fmt.Errorf("stale Console stream epoch")
	}
	if snapshot.Mode == "" {
		snapshot.Mode = consoleapi.ModeNormal
	}
	if err := validateSnapshot(snapshot); err != nil {
		return err
	}
	candidate := r.clone()
	if err := candidate.applySnapshot(snapshot); err != nil {
		return err
	}
	*r = *candidate
	return nil
}

func (r *Reducer) applySnapshot(snapshot consoleapi.AttachResponse) error {
	if r.initialized {
		expectedMode := r.state.Mode
		if r.pendingMode != "" {
			expectedMode = r.pendingMode
		}
		if snapshot.Mode != expectedMode {
			return fmt.Errorf("Console snapshot mode %q does not match stream mode %q", snapshot.Mode, expectedMode)
		}
		if snapshot.AgentID != r.state.AgentID {
			return fmt.Errorf("Console snapshot Agent changed from %q to %q", r.state.AgentID, snapshot.AgentID)
		}
		if snapshot.SnapshotSequence < r.cursor {
			return fmt.Errorf("Console snapshot sequence moved backward from %d to %d", r.cursor, snapshot.SnapshotSequence)
		}
		if snapshot.Generation < r.state.Generation {
			return fmt.Errorf("Console snapshot generation moved backward from %d to %d", r.state.Generation, snapshot.Generation)
		}
		if snapshot.Generation == r.state.Generation && snapshot.WorkerInstanceID != r.state.WorkerInstanceID {
			return fmt.Errorf("Console snapshot changed Worker identity within generation %d", snapshot.Generation)
		}
	}
	replaced := r.initialized && (snapshot.Generation > r.state.Generation || snapshot.WorkerInstanceID != r.state.WorkerInstanceID)
	r.state = cloneSnapshot(snapshot)
	r.cursor = snapshot.SnapshotSequence
	r.initialized = true
	r.pendingMode = ""
	if replaced || snapshot.WorkerStatus == domain.WorkerStatusOffline {
		r.clearTaskWorkerScopedState()
	}
	if snapshot.SuggestedTask != nil {
		option := *snapshot.SuggestedTask
		record := r.ensureTask(option.TaskID)
		previousVersion := record.Version
		if err := applyTaskReference(record, option.Version, option.Status); err != nil {
			return err
		}
		if previousVersion > option.Version {
			return fmt.Errorf("suggested Console Task version moved backward")
		}
		if record.Version == option.Version {
			if record.Summary != "" && record.Summary != option.Summary ||
				record.UpdatedAt != "" && record.UpdatedAt != option.UpdatedAt {
				return fmt.Errorf("suggested Console Task changed within version %d", option.Version)
			}
			record.Summary = option.Summary
			record.UpdatedAt = option.UpdatedAt
			record.encodedSize = 0
			record.order = r.nextTaskOrder()
		}
		if r.focusedTaskID == "" {
			r.focusedTaskID = option.TaskID
			r.focusSource = FocusAttachSuggestion
		}
	}
	r.trimTasks()
	return nil
}

func (r *Reducer) Apply(event openapi.JournalEventReadModel) (ApplyResult, error) {
	return r.ApplyForStream(r.streamEpoch, event)
}

func (r *Reducer) ApplyForStream(epoch uint64, event openapi.JournalEventReadModel) (ApplyResult, error) {
	if !r.initialized {
		return ApplyResult{}, fmt.Errorf("Console reducer is not initialized")
	}
	if epoch != r.streamEpoch {
		return ApplyResult{}, fmt.Errorf("stale Console stream epoch")
	}
	if event.Sequence <= 0 {
		return ApplyResult{}, fmt.Errorf("Console event sequence must be positive")
	}
	if event.Sequence < r.cursor {
		return ApplyResult{}, fmt.Errorf("Console event sequence moved backward from %d to %d", r.cursor, event.Sequence)
	}
	if event.Sequence == r.cursor {
		return ApplyResult{}, nil
	}
	if err := validateEvent(event, r.state.AgentID, r.state.Mode); err != nil {
		if event.Worker != nil && workerProjectionTargetsCurrent(event.Worker, r.state) {
			r.state.BackendHealth = nil
		}
		return ApplyResult{}, err
	}
	candidate := r.cloneForEvent(event)
	meaningful, err := candidate.applyEvent(event)
	if err != nil {
		return ApplyResult{}, err
	}
	result := ApplyResult{CursorAdvanced: true}
	if meaningful {
		item, itemErr := timelineItem(event)
		if itemErr != nil {
			return ApplyResult{}, itemErr
		}
		candidate.appendTimeline(*item)
		result.Timeline = cloneEvent(event)
	}
	candidate.cursor = event.Sequence
	*r = *candidate
	return result, nil
}

func (r *Reducer) applyEvent(event openapi.JournalEventReadModel) (bool, error) {
	switch event.AggregateType {
	case "worker_instance":
		return r.applyWorkerEvent(event)
	case "run_attempt":
		return r.applyRunEvent(event)
	case "task", "mailbox_item", "message", "approval_request":
		return r.applyTaskEvent(event)
	case "runtime":
		return event.Output != nil, nil
	default:
		return false, nil
	}
}

func (r *Reducer) applyWorkerEvent(event openapi.JournalEventReadModel) (bool, error) {
	worker := event.Worker
	if worker.AgentID != r.state.AgentID || worker.Generation < r.state.Generation ||
		worker.Generation == r.state.Generation && r.state.WorkerInstanceID != "" && worker.WorkerInstanceID != r.state.WorkerInstanceID {
		return false, nil
	}
	replaced := worker.Generation > r.state.Generation || worker.WorkerInstanceID != r.state.WorkerInstanceID
	statusChanged := worker.Status != r.state.WorkerStatus
	leaseAnomaly := leaseMovedBackward(r.state.LeaseUntil, worker.LeaseUntil)
	backendHealthChanged := !sameBackendHealth(r.state.BackendHealth, worker.BackendHealth)
	if replaced || worker.Status == domain.WorkerStatusOffline {
		r.clearWorkerScopedState()
		r.clearTaskWorkerScopedState()
	}
	r.state.WorkerInstanceID = worker.WorkerInstanceID
	r.state.Generation = worker.Generation
	r.state.WorkerStatus = worker.Status
	r.state.Capabilities = append([]string(nil), worker.Capabilities...)
	r.state.LastHeartbeatAt = worker.LastHeartbeatAt
	r.state.LeaseUntil = worker.LeaseUntil
	if worker.Status != domain.WorkerStatusOffline {
		r.state.BackendHealth = cloneBackendHealth(worker.BackendHealth)
	}
	if r.state.Mode == consoleapi.ModeDiagnostic && !replaced && worker.Status != domain.WorkerStatusOffline {
		r.state.Diagnostic = &consoleapi.DiagnosticView{LeaseUntil: worker.LeaseUntil,
			LastHeartbeatAt: worker.LastHeartbeatAt, StartedAt: worker.StartedAt,
			UpdatedAt: worker.UpdatedAt, Draining: worker.Status == domain.WorkerStatusDraining}
	}
	return event.EventType != "worker.heartbeat" || replaced || statusChanged || leaseAnomaly || backendHealthChanged, nil
}

func (r *Reducer) applyRunEvent(event openapi.JournalEventReadModel) (bool, error) {
	run := event.Run
	currentWorker := run.AgentID == r.state.AgentID && r.state.WorkerInstanceID != "" &&
		run.WorkerInstanceID == r.state.WorkerInstanceID && *run.WorkerGeneration == r.state.Generation &&
		r.state.WorkerStatus != domain.WorkerStatusOffline
	if !currentWorker {
		return true, nil
	}
	if active := r.state.ActiveRun; active != nil && active.RunID != run.ID && !runAfterSnapshot(*run, *active) {
		return true, nil
	}
	changed := false
	if record := r.tasks[run.TaskID]; record != nil {
		if taskStatusTerminal(record.Status) && run.Status.Active() {
			return true, nil
		}
		if record.LatestRun != nil && record.LatestRun.ID != run.ID && !runAfter(*run, *record.LatestRun) {
			return true, nil
		}
		if record.LatestRun == nil || runAfter(*run, *record.LatestRun) {
			record.LatestRun = cloneRun(run)
			record.encodedSize = 0
			record.order = r.nextTaskOrder()
			changed = true
		} else if record.LatestRun.ID == run.ID {
			if run.Version < record.LatestRun.Version {
				return false, nil
			}
			if run.Version == record.LatestRun.Version {
				if !sameRun(record.LatestRun, run) {
					return false, fmt.Errorf("Console Run projection changed within version %d", run.Version)
				}
				return false, nil
			}
			if !record.LatestRun.Status.Active() {
				return false, fmt.Errorf("terminal Console Run cannot be overwritten")
			}
			record.LatestRun = cloneRun(run)
			record.encodedSize = 0
			record.order = r.nextTaskOrder()
			changed = true
		}
	}
	if run.Status.Active() {
		generation := *run.WorkerGeneration
		r.state.ActiveRun = &consoleapi.RunSnapshot{RunID: run.ID, TaskID: run.TaskID, Status: run.Status,
			WorkerInstanceID: run.WorkerInstanceID, WorkerGeneration: &generation,
			StartedAt: run.StartedAt, UpdatedAt: run.UpdatedAt}
		changed = true
	} else if r.state.ActiveRun != nil {
		r.state.ActiveRun = nil
		changed = true
	}
	r.trimTasks()
	return changed, nil
}

func (r *Reducer) applyTaskEvent(event openapi.JournalEventReadModel) (bool, error) {
	task := *event.Task
	record := r.ensureTask(task.TaskID)
	changed, ignored, err := applyTaskProjection(record, task)
	if err != nil {
		return false, err
	}
	if ignored {
		return false, nil
	}
	secondaryChanged := false
	switch event.AggregateType {
	case "mailbox_item":
		if event.Mailbox.Kind == domain.MailboxKindTask && event.Mailbox.Lane == domain.MailboxLaneWork {
			if record.WorkDelivery != nil && record.WorkDelivery.MailboxItemID != event.Mailbox.MailboxItemID {
				return false, fmt.Errorf("Console Task work delivery identity changed")
			}
			if record.WorkDelivery == nil || !reflect.DeepEqual(*record.WorkDelivery, *event.Mailbox) {
				record.WorkDelivery = cloneMailbox(event.Mailbox)
				record.encodedSize = 0
				secondaryChanged = true
			}
		}
	case "message":
		if record.LatestMessage == nil || event.Message.Sequence > record.LatestMessage.Sequence {
			record.LatestMessage = cloneMessage(event.Message)
			record.encodedSize = 0
			secondaryChanged = true
		} else if event.Message.Sequence == record.LatestMessage.Sequence && !reflect.DeepEqual(*record.LatestMessage, *event.Message) {
			return false, fmt.Errorf("Console Message projection changed within sequence %d", event.Message.Sequence)
		}
	case "approval_request":
		if event.Approval.State == domain.ApprovalRequestPending {
			if record.PendingApproval != nil && event.Approval.ApprovalRequestID == record.PendingApproval.ApprovalRequestID {
				if !reflect.DeepEqual(*record.PendingApproval, *event.Approval) {
					return false, fmt.Errorf("pending Console Approval projection changed")
				}
			} else if record.PendingApproval == nil || event.Approval.CreatedAt.After(record.PendingApproval.CreatedAt) ||
				event.Approval.CreatedAt.Equal(record.PendingApproval.CreatedAt) &&
					event.Approval.ApprovalRequestID > record.PendingApproval.ApprovalRequestID {
				record.PendingApproval = cloneApproval(event.Approval)
				record.encodedSize = 0
				secondaryChanged = true
			}
		} else if record.PendingApproval != nil && record.PendingApproval.ApprovalRequestID == event.Approval.ApprovalRequestID {
			record.PendingApproval = nil
			record.encodedSize = 0
			secondaryChanged = true
		}
	}
	if taskStatusTerminal(task.Status) && r.state.ActiveRun != nil && r.state.ActiveRun.TaskID == task.TaskID {
		r.state.ActiveRun = nil
		secondaryChanged = true
	}
	if changed || secondaryChanged {
		record.order = r.nextTaskOrder()
	}
	r.trimTasks()
	return changed || secondaryChanged, nil
}

func runAfter(current, previous openapi.RunAttemptReadModel) bool {
	return current.StartedAt.After(previous.StartedAt) || current.StartedAt.Equal(previous.StartedAt) && current.ID > previous.ID
}

func runAfterSnapshot(current openapi.RunAttemptReadModel, previous consoleapi.RunSnapshot) bool {
	return current.StartedAt.After(previous.StartedAt) || current.StartedAt.Equal(previous.StartedAt) && current.ID > previous.RunID
}

func (r *Reducer) Snapshot() consoleapi.AttachResponse {
	return cloneSnapshot(r.state)
}

func (r *Reducer) Cursor() int64 {
	return r.cursor
}

func (r *Reducer) clearWorkerScopedState() {
	r.state.BackendHealth = nil
	r.state.Diagnostic = nil
	r.state.ActiveRun = nil
}

func (r *Reducer) clearTaskWorkerScopedState() {
	for _, record := range r.tasks {
		if record.LatestRun != nil && record.LatestRun.Status.Active() {
			record.LatestRun = nil
			record.encodedSize = 0
		}
		if record.PendingApproval != nil && record.PendingApproval.Mode == domain.ApprovalModeNative {
			record.PendingApproval = nil
			record.encodedSize = 0
		}
	}
}

func validateSnapshot(snapshot consoleapi.AttachResponse) error {
	if err := domain.ValidateIdentifier("agent_id", snapshot.AgentID); err != nil {
		return err
	}
	if snapshot.SnapshotSequence < 0 {
		return fmt.Errorf("Console snapshot sequence cannot be negative")
	}
	if snapshot.Mode != "" && snapshot.Mode != consoleapi.ModeNormal && snapshot.Mode != consoleapi.ModeDiagnostic {
		return fmt.Errorf("unsupported Console snapshot mode")
	}
	if snapshot.SuggestedTask != nil {
		if err := validateTaskOption(*snapshot.SuggestedTask); err != nil {
			return fmt.Errorf("invalid suggested Console Task: %w", err)
		}
		if taskStatusTerminal(snapshot.SuggestedTask.Status) {
			return fmt.Errorf("suggested Console Task must be active")
		}
	}
	if snapshot.WorkerInstanceID == "" {
		if snapshot.Generation != 0 || snapshot.WorkerStatus != domain.WorkerStatusOffline {
			return fmt.Errorf("offline Console snapshot cannot carry a Worker generation")
		}
		if len(snapshot.BackendHealth) != 0 || snapshot.Diagnostic != nil || snapshot.ActiveRun != nil {
			return fmt.Errorf("offline Console snapshot contains unbound Worker state")
		}
		return nil
	}
	if err := domain.ValidateOpaqueID("worker_instance_id", snapshot.WorkerInstanceID); err != nil {
		return err
	}
	if snapshot.Generation <= 0 || !snapshot.WorkerStatus.Valid() {
		return fmt.Errorf("invalid Console Worker generation or status")
	}
	for _, capability := range snapshot.Capabilities {
		if err := domain.ValidateIdentifier("capability", capability); err != nil {
			return err
		}
	}
	for backendID, health := range snapshot.BackendHealth {
		if err := domain.ValidateIdentifier("backend_id", backendID); err != nil {
			return err
		}
		if !health.Valid() {
			return fmt.Errorf("invalid Console Backend health")
		}
	}
	if snapshot.WorkerStatus == domain.WorkerStatusOffline &&
		(len(snapshot.BackendHealth) != 0 || snapshot.Diagnostic != nil || snapshot.ActiveRun != nil) {
		return fmt.Errorf("offline Console snapshot contains stale Worker state")
	}
	if snapshot.Diagnostic != nil && snapshot.Mode != consoleapi.ModeDiagnostic {
		return fmt.Errorf("Diagnostic state requires diagnostic Attach mode")
	}
	if snapshot.ActiveRun != nil {
		run := snapshot.ActiveRun
		if err := domain.ValidateOpaqueID("run_id", run.RunID); err != nil {
			return err
		}
		if err := domain.ValidateOpaqueID("task_id", run.TaskID); err != nil {
			return err
		}
		if err := domain.ValidateOpaqueID("worker_instance_id", run.WorkerInstanceID); err != nil {
			return err
		}
		if !run.Status.Active() || run.WorkerGeneration == nil || *run.WorkerGeneration <= 0 ||
			run.WorkerInstanceID != snapshot.WorkerInstanceID || *run.WorkerGeneration != snapshot.Generation {
			return fmt.Errorf("active RunAttempt is not fenced to the Console Worker")
		}
	}
	return nil
}

func validateEvent(event openapi.JournalEventReadModel, agentID, mode string) error {
	if domain.ValidateOpaqueID("event_id", event.ID) != nil || domain.ValidateOpaqueID("aggregate_id", event.AggregateID) != nil ||
		strings.TrimSpace(event.EventType) == "" || strings.ContainsAny(event.EventType, "\r\n\t") || len(event.EventType) > 128 {
		return fmt.Errorf("invalid Console event identity")
	}
	projectionCount := 0
	for _, present := range []bool{event.Output != nil, event.Worker != nil, event.Run != nil, event.Task != nil,
		event.Mailbox != nil, event.Message != nil, event.Approval != nil} {
		if present {
			projectionCount++
		}
	}
	switch event.AggregateType {
	case "worker_instance":
		if event.Worker == nil || projectionCount != 1 {
			return fmt.Errorf("Worker event requires exactly one Worker projection")
		}
		if err := validateWorkerProjection(event); err != nil {
			return err
		}
		if event.Worker.AgentID != agentID {
			return fmt.Errorf("Worker projection belongs to another Agent")
		}
	case "run_attempt":
		if event.Run == nil || projectionCount != 1 {
			return fmt.Errorf("Run event requires exactly one Run projection")
		}
		if err := validateRunProjection(event, agentID); err != nil {
			return err
		}
	case "runtime":
		if projectionCount > 1 || event.Output == nil && projectionCount != 0 {
			return fmt.Errorf("Runtime event has conflicting projections")
		}
		if event.Output != nil {
			if err := validateSafeOutput(*event.Output); err != nil {
				return err
			}
			if mode != consoleapi.ModeDiagnostic && (event.Output.Diagnostic != "" || event.Output.DiagnosticTruncated) {
				return fmt.Errorf("Normal Console event contains Diagnostic output")
			}
		}
	case "task":
		if event.Task == nil || projectionCount != 1 || event.AggregateID != event.Task.TaskID {
			return fmt.Errorf("Task event requires a matching Task projection")
		}
		if err := validateTaskProjection(*event.Task, agentID); err != nil {
			return err
		}
	case "mailbox_item":
		if event.Task == nil || event.Mailbox == nil || projectionCount != 2 || event.AggregateID != event.Mailbox.MailboxItemID {
			return fmt.Errorf("Mailbox event requires matching Task and Mailbox projections")
		}
		if err := validateTaskProjection(*event.Task, agentID); err != nil {
			return err
		}
		if err := validateMailboxProjection(*event.Mailbox); err != nil {
			return err
		}
	case "message":
		if event.Task == nil || event.Message == nil || projectionCount != 2 || event.AggregateID != event.Message.MessageID {
			return fmt.Errorf("Message event requires matching Task and Message projections")
		}
		if err := validateTaskProjection(*event.Task, agentID); err != nil {
			return err
		}
		if err := validateMessageProjection(*event.Message); err != nil {
			return err
		}
	case "approval_request":
		if event.Task == nil || event.Approval == nil || projectionCount != 2 || event.AggregateID != event.Approval.ApprovalRequestID {
			return fmt.Errorf("Approval event requires matching Task and Approval projections")
		}
		if err := validateTaskProjection(*event.Task, agentID); err != nil {
			return err
		}
		if err := validateApprovalProjection(*event.Approval); err != nil {
			return err
		}
	default:
		if projectionCount != 0 {
			return fmt.Errorf("unobservable Console event cannot carry a projection")
		}
	}
	return nil
}

func validateWorkerProjection(event openapi.JournalEventReadModel) error {
	worker := event.Worker
	if event.AggregateType != "worker_instance" || event.AggregateID != worker.WorkerInstanceID {
		return fmt.Errorf("Worker projection does not match its event aggregate")
	}
	if err := domain.ValidateOpaqueID("worker_instance_id", worker.WorkerInstanceID); err != nil {
		return err
	}
	if err := domain.ValidateIdentifier("agent_id", worker.AgentID); err != nil {
		return err
	}
	if worker.Generation <= 0 || !worker.Status.Valid() {
		return fmt.Errorf("invalid Worker projection generation or status")
	}
	for _, capability := range worker.Capabilities {
		if err := domain.ValidateIdentifier("capability", capability); err != nil {
			return err
		}
	}
	for backendID, health := range worker.BackendHealth {
		if err := domain.ValidateIdentifier("backend_id", backendID); err != nil {
			return err
		}
		if !health.Valid() {
			return fmt.Errorf("invalid Worker Backend health projection")
		}
	}
	return nil
}

func workerProjectionTargetsCurrent(worker *openapi.WorkerReadModel, state consoleapi.AttachResponse) bool {
	return worker != nil && worker.AgentID == state.AgentID && worker.WorkerInstanceID == state.WorkerInstanceID &&
		worker.Generation == state.Generation
}

func sameBackendHealth(left, right map[string]openruntime.BackendHealth) bool {
	if len(left) != len(right) {
		return false
	}
	for backendID, health := range left {
		if right[backendID] != health {
			return false
		}
	}
	return true
}

func cloneBackendHealth(source map[string]openruntime.BackendHealth) map[string]openruntime.BackendHealth {
	if source == nil {
		return nil
	}
	clone := make(map[string]openruntime.BackendHealth, len(source))
	for backendID, health := range source {
		clone[backendID] = health
	}
	return clone
}

func validateRunProjection(event openapi.JournalEventReadModel, agentID string) error {
	run := event.Run
	if event.AggregateType != "run_attempt" || event.AggregateID != run.ID {
		return fmt.Errorf("Run projection does not match its event aggregate")
	}
	return validateRun(*run, agentID)
}

func leaseMovedBackward(previous, next time.Time) bool {
	return !previous.IsZero() && (next.IsZero() || next.Before(previous))
}

func cloneSnapshot(snapshot consoleapi.AttachResponse) consoleapi.AttachResponse {
	snapshot.Capabilities = append([]string(nil), snapshot.Capabilities...)
	snapshot.BackendHealth = cloneBackendHealth(snapshot.BackendHealth)
	if snapshot.ActiveRun != nil {
		run := *snapshot.ActiveRun
		if snapshot.ActiveRun.WorkerGeneration != nil {
			generation := *snapshot.ActiveRun.WorkerGeneration
			run.WorkerGeneration = &generation
		}
		snapshot.ActiveRun = &run
	}
	if snapshot.Diagnostic != nil {
		diagnostic := *snapshot.Diagnostic
		snapshot.Diagnostic = &diagnostic
	}
	if snapshot.SuggestedTask != nil {
		task := *snapshot.SuggestedTask
		snapshot.SuggestedTask = &task
	}
	return snapshot
}

func cloneEvent(event openapi.JournalEventReadModel) *openapi.JournalEventReadModel {
	clone := event
	if event.Worker != nil {
		worker := *event.Worker
		worker.Capabilities = append([]string(nil), worker.Capabilities...)
		worker.BackendHealth = cloneBackendHealth(worker.BackendHealth)
		clone.Worker = &worker
	}
	if event.Output != nil {
		output := *event.Output
		clone.Output = &output
	}
	if event.Run != nil {
		clone.Run = cloneRun(event.Run)
	}
	clone.Task = cloneTask(event.Task)
	clone.Mailbox = cloneMailbox(event.Mailbox)
	clone.Message = cloneMessage(event.Message)
	clone.Approval = cloneApproval(event.Approval)
	return &clone
}

func (r *Reducer) clone() *Reducer {
	clone := r.cloneShallow()
	clone.cloneAllTaskRecords()
	return clone
}

func (r *Reducer) cloneForEvent(event openapi.JournalEventReadModel) *Reducer {
	clone := r.cloneShallow()
	switch event.AggregateType {
	case "task", "mailbox_item", "message", "approval_request":
		if event.Task != nil {
			clone.cloneTaskRecord(event.Task.TaskID)
		}
	case "run_attempt":
		if event.Run != nil {
			clone.cloneTaskRecord(event.Run.TaskID)
		}
	case "worker_instance":
		if event.Worker != nil && (event.Worker.Generation > r.state.Generation ||
			event.Worker.WorkerInstanceID != r.state.WorkerInstanceID || event.Worker.Status == domain.WorkerStatusOffline) {
			clone.cloneAllTaskRecords()
		}
	}
	return clone
}

func (r *Reducer) cloneShallow() *Reducer {
	clone := &Reducer{
		state: cloneSnapshot(r.state), initialized: r.initialized, cursor: r.cursor,
		connection: r.connection, pendingMode: r.pendingMode, streamEpoch: r.streamEpoch,
		focusedTaskID: r.focusedTaskID, focusSource: r.focusSource, taskOrder: r.taskOrder,
		timelineBytes: r.timelineBytes, tasks: make(map[string]*taskRecord, len(r.tasks)),
		timeline: append([]TimelineItem(nil), r.timeline...),
	}
	for taskID, record := range r.tasks {
		clone.tasks[taskID] = record
	}
	return clone
}

func (r *Reducer) cloneTaskRecord(taskID string) {
	record := r.tasks[taskID]
	if record == nil {
		return
	}
	copy := cloneTaskState(record.TaskState)
	r.tasks[taskID] = &taskRecord{TaskState: copy, encodedSize: record.encodedSize}
}

func (r *Reducer) cloneAllTaskRecords() {
	for taskID := range r.tasks {
		r.cloneTaskRecord(taskID)
	}
}
