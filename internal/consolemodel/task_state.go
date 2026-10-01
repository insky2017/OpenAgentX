package consolemodel

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	"openagentx/internal/domain"
	"openagentx/internal/safeoutput"
)

const (
	maxActiveTasks        = 128
	maxRecentTasks        = 128
	maxTrackedTaskBytes   = 256 << 10
	maxTimelineEntries    = 256
	maxTimelineBytes      = 64 << 10
	maxTimelineEntryBytes = 2 << 10
	maxTimelineTextBytes  = 512
)

type ConnectionState string

const (
	ConnectionConnecting        ConnectionState = "connecting"
	ConnectionConnected         ConnectionState = "connected"
	ConnectionDisconnected      ConnectionState = "disconnected"
	ConnectionReconnecting      ConnectionState = "reconnecting"
	ConnectionRetentionReattach ConnectionState = "retention-reattach"
	ConnectionSwitching         ConnectionState = "switching"
)

func (s ConnectionState) Valid() bool {
	switch s {
	case ConnectionConnecting, ConnectionConnected, ConnectionDisconnected, ConnectionReconnecting,
		ConnectionRetentionReattach, ConnectionSwitching:
		return true
	default:
		return false
	}
}

type FocusSource string

const (
	FocusAttachSuggestion FocusSource = "attach_suggestion"
	FocusManual           FocusSource = "manual"
	FocusDispatch         FocusSource = "dispatch"
)

type ControlTaskUpdate struct {
	AgentID string
	TaskID  string
	Version int64
	Status  domain.TaskStatus
	Focus   bool
}

type TaskState struct {
	TaskID          string
	Version         int64
	Status          domain.TaskStatus
	Intent          domain.TaskIntent
	Summary         string
	UpdatedAt       string
	Detail          *openapi.ConsoleTaskReadModel
	WorkDelivery    *openapi.ConsoleMailboxReadModel
	LatestRun       *openapi.RunAttemptReadModel
	LatestMessage   *openapi.ConsoleMessageReadModel
	PendingApproval *openapi.ConsoleApprovalReadModel
	order           int64
}

type TimelineItem struct {
	Sequence              int64
	EventType             string
	TaskID                string
	TaskVersion           int64
	TaskStatus            domain.TaskStatus
	TaskOutcomeState      string
	TaskResult            string
	TaskResultTruncated   bool
	TaskError             string
	TaskErrorTruncated    bool
	MailboxItemID         string
	MailboxKind           domain.MailboxKind
	MailboxLane           domain.MailboxLane
	MailboxState          domain.MailboxState
	RunID                 string
	RunVersion            int64
	RunStatus             domain.RunAttemptStatus
	RuntimeReplyState     string
	RuntimeReply          string
	RuntimeReplyTruncated bool
	RuntimeError          string
	RuntimeErrorTruncated bool
	MessageID             string
	MessageVersion        int64
	ApprovalID            string
	ApprovalState         domain.ApprovalRequestState
	Output                *openapi.SafeOutputReadModel
	encodedSize           int
}

type State struct {
	Console       consoleapi.AttachResponse
	Connection    ConnectionState
	PendingMode   string
	FocusedTask   *TaskState
	FocusSource   FocusSource
	ActiveTasks   []TaskState
	RecentTasks   []TaskState
	TaskBytes     int
	Timeline      []TimelineItem
	TimelineBytes int
	StreamEpoch   uint64
}

type taskRecord struct {
	TaskState
	encodedSize int
}

func (r *Reducer) State() State {
	state := State{
		Console: cloneSnapshot(r.state), Connection: r.connection, PendingMode: r.pendingMode,
		FocusSource: r.focusSource, TaskBytes: r.taskBytes(), TimelineBytes: r.timelineBytes, StreamEpoch: r.streamEpoch,
	}
	if focused := r.tasks[r.focusedTaskID]; focused != nil {
		copy := cloneTaskState(focused.TaskState)
		state.FocusedTask = &copy
	}
	active, recent := r.taskCollections()
	state.ActiveTasks = active
	state.RecentTasks = recent
	state.Timeline = make([]TimelineItem, len(r.timeline))
	for index := range r.timeline {
		state.Timeline[index] = cloneTimelineItem(r.timeline[index])
	}
	return state
}

func (r *Reducer) StreamEpoch() uint64 { return r.streamEpoch }

func (r *Reducer) BeginStream(mode string) (uint64, error) {
	if mode != consoleapi.ModeNormal && mode != consoleapi.ModeDiagnostic {
		return r.streamEpoch, fmt.Errorf("unsupported Console stream mode")
	}
	r.streamEpoch++
	r.pendingMode = mode
	r.connection = ConnectionSwitching
	// A stream transition is always rendered from the Normal-safe projection
	// until the target snapshot has been validated and applied.
	r.state.Mode = consoleapi.ModeNormal
	r.state.Diagnostic = nil
	return r.streamEpoch, nil
}

func (r *Reducer) AbortStream(epoch uint64) error {
	if epoch != r.streamEpoch {
		return fmt.Errorf("stale Console stream epoch")
	}
	r.pendingMode = ""
	r.state.Mode = consoleapi.ModeNormal
	r.state.Diagnostic = nil
	r.connection = ConnectionDisconnected
	return nil
}

func (r *Reducer) SetConnection(epoch uint64, state ConnectionState) error {
	if epoch != r.streamEpoch {
		return fmt.Errorf("stale Console stream epoch")
	}
	if !state.Valid() {
		return fmt.Errorf("unsupported Console connection state")
	}
	r.connection = state
	return nil
}

func (r *Reducer) FocusTask(taskID string) error {
	if err := domain.ValidateOpaqueID("task_id", taskID); err != nil {
		return err
	}
	if r.tasks[taskID] == nil {
		return fmt.Errorf("cannot focus an untracked Console Task")
	}
	r.focusedTaskID = taskID
	r.focusSource = FocusManual
	r.trimTasks()
	return nil
}

func (r *Reducer) ApplyControlTask(update ControlTaskUpdate) error {
	if update.AgentID != r.state.AgentID {
		return fmt.Errorf("Console control Task belongs to another Agent")
	}
	if err := domain.ValidateOpaqueID("task_id", update.TaskID); err != nil {
		return err
	}
	if update.Version <= 0 || !update.Status.Valid() {
		return fmt.Errorf("invalid Console control Task version or status")
	}
	candidate := r.cloneShallow()
	candidate.cloneTaskRecord(update.TaskID)
	record := candidate.ensureTask(update.TaskID)
	if err := applyTaskReference(record, update.Version, update.Status); err != nil {
		return err
	}
	record.order = candidate.nextTaskOrder()
	if update.Focus {
		candidate.focusedTaskID = update.TaskID
		candidate.focusSource = FocusDispatch
	}
	candidate.trimTasks()
	*r = *candidate
	return nil
}

func (r *Reducer) ReplaceTaskOptions(options []openapi.ConsoleTaskOption) error {
	if len(options) > 10000 {
		return fmt.Errorf("Console Task options exceed the safe input bound")
	}
	for index := range options {
		if err := validateTaskOption(options[index]); err != nil {
			return err
		}
	}
	candidate := r.clone()
	lastOrder := candidate.taskOrder + int64(len(options))
	for index := range options {
		option := options[index]
		record := candidate.ensureTask(option.TaskID)
		if err := applyTaskReference(record, option.Version, option.Status); err != nil {
			return err
		}
		if record.Version == option.Version {
			intent := option.Intent
			if record.Intent != "" && record.Intent != intent {
				return fmt.Errorf("Console Task option intent cannot change")
			}
			if record.Summary != "" && record.Summary != option.Summary {
				return fmt.Errorf("Console Task option changed within version %d", option.Version)
			}
			if record.UpdatedAt != "" && record.UpdatedAt != option.UpdatedAt {
				return fmt.Errorf("Console Task option timestamp changed within version %d", option.Version)
			}
			record.Summary = option.Summary
			record.Intent = intent
			record.UpdatedAt = option.UpdatedAt
			record.encodedSize = 0
			record.order = lastOrder - int64(index)
		}
	}
	if len(options) > 0 {
		candidate.taskOrder = lastOrder
	}
	candidate.trimTasks()
	*r = *candidate
	return nil
}

func (r *Reducer) ApplyTaskSnapshot(snapshot openapi.ConsoleTaskSnapshot, source FocusSource) error {
	if source != FocusAttachSuggestion && source != FocusManual {
		return fmt.Errorf("unsupported Console Task snapshot focus source")
	}
	if err := validateTaskSnapshot(snapshot, r.state.AgentID); err != nil {
		return err
	}
	candidate := r.cloneShallow()
	candidate.cloneTaskRecord(snapshot.Task.TaskID)
	record := candidate.ensureTask(snapshot.Task.TaskID)
	changed, ignored, err := applyTaskProjection(record, snapshot.Task)
	if err != nil {
		return err
	}
	if !ignored {
		if changed || record.Detail != nil {
			record.WorkDelivery = cloneMailbox(snapshot.WorkDelivery)
			record.LatestRun = cloneRun(snapshot.LatestRun)
			if record.LatestRun != nil && record.LatestRun.Status.Active() && !candidate.runTargetsCurrentWorker(record.LatestRun) {
				record.LatestRun = nil
			}
			record.LatestMessage = cloneMessage(snapshot.LatestMessage)
			record.PendingApproval = cloneApproval(snapshot.PendingApproval)
			if record.PendingApproval != nil && record.PendingApproval.Mode == domain.ApprovalModeNative &&
				(record.LatestRun == nil || record.PendingApproval.TargetRunID != record.LatestRun.ID) {
				record.PendingApproval = nil
			}
			record.encodedSize = 0
		}
	}
	record.order = candidate.nextTaskOrder()
	if source == FocusManual || candidate.focusedTaskID == "" {
		candidate.focusedTaskID = snapshot.Task.TaskID
		candidate.focusSource = source
	}
	candidate.trimTasks()
	*r = *candidate
	return nil
}

func (r *Reducer) runTargetsCurrentWorker(run *openapi.RunAttemptReadModel) bool {
	return run != nil && run.AgentID == r.state.AgentID && r.state.WorkerInstanceID != "" &&
		run.WorkerInstanceID == r.state.WorkerInstanceID && run.WorkerGeneration != nil &&
		*run.WorkerGeneration == r.state.Generation && r.state.WorkerStatus != domain.WorkerStatusOffline
}

func (r *Reducer) ensureTask(taskID string) *taskRecord {
	if r.tasks == nil {
		r.tasks = make(map[string]*taskRecord)
	}
	if r.tasks[taskID] == nil {
		r.tasks[taskID] = &taskRecord{TaskState: TaskState{TaskID: taskID}}
	}
	return r.tasks[taskID]
}

func (r *Reducer) nextTaskOrder() int64 {
	r.taskOrder++
	return r.taskOrder
}

func (r *Reducer) taskCollections() ([]TaskState, []TaskState) {
	active := make([]TaskState, 0, len(r.tasks))
	recent := make([]TaskState, 0, len(r.tasks))
	for _, record := range r.tasks {
		copy := cloneTaskState(record.TaskState)
		if taskStatusTerminal(record.Status) {
			recent = append(recent, copy)
		} else {
			active = append(active, copy)
		}
	}
	sort.Slice(active, func(i, j int) bool { return taskStateAfter(active[i], active[j]) })
	sort.Slice(recent, func(i, j int) bool { return taskStateAfter(recent[i], recent[j]) })
	return active, recent
}

func taskStateAfter(left, right TaskState) bool {
	if left.order != right.order {
		return left.order > right.order
	}
	return left.TaskID > right.TaskID
}

func (r *Reducer) trimTasks() {
	for {
		active, recent := r.taskCollections()
		if len(active) <= maxActiveTasks && len(recent) <= maxRecentTasks && r.taskBytes() <= maxTrackedTaskBytes {
			return
		}
		var victim string
		var victimOrder int64 = int64(^uint64(0) >> 1)
		for taskID, record := range r.tasks {
			if taskID == r.focusedTaskID {
				continue
			}
			collectionOver := len(active) > maxActiveTasks && !taskStatusTerminal(record.Status) ||
				len(recent) > maxRecentTasks && taskStatusTerminal(record.Status)
			if (collectionOver || r.taskBytes() > maxTrackedTaskBytes) && record.order < victimOrder {
				victim, victimOrder = taskID, record.order
			}
		}
		if victim == "" {
			return
		}
		delete(r.tasks, victim)
	}
}

func (r *Reducer) taskBytes() int {
	total := 0
	for _, record := range r.tasks {
		if record.encodedSize == 0 {
			encoded, err := json.Marshal(record.TaskState)
			if err != nil {
				return maxTrackedTaskBytes + 1
			}
			record.encodedSize = len(encoded)
		}
		total += record.encodedSize
	}
	return total
}

func (r *Reducer) appendTimeline(item TimelineItem) {
	r.timeline = append(r.timeline, item)
	r.timelineBytes += item.encodedSize
	for len(r.timeline) > maxTimelineEntries || r.timelineBytes > maxTimelineBytes {
		r.timelineBytes -= r.timeline[0].encodedSize
		r.timeline = r.timeline[1:]
	}
}

func timelineItem(event openapi.JournalEventReadModel) (*TimelineItem, error) {
	item := TimelineItem{Sequence: event.Sequence, EventType: boundedText(event.EventType, 128)}
	if event.Task != nil {
		item.TaskID, item.TaskVersion, item.TaskStatus = event.Task.TaskID, event.Task.Version, event.Task.Status
		item.TaskOutcomeState = event.Task.OutcomeState
		if event.Task.OutcomeState != "pending" {
			if event.Task.Result != nil {
				item.TaskResult = boundedText(*event.Task.Result, maxTimelineTextBytes)
				item.TaskResultTruncated = event.Task.ResultTruncated || item.TaskResult != *event.Task.Result
			}
			if event.Task.Error != nil {
				item.TaskError = boundedText(*event.Task.Error, maxTimelineTextBytes)
				item.TaskErrorTruncated = event.Task.ErrorTruncated || item.TaskError != *event.Task.Error
			}
		}
	}
	if event.Mailbox != nil {
		item.MailboxItemID, item.MailboxKind, item.MailboxLane, item.MailboxState =
			event.Mailbox.MailboxItemID, event.Mailbox.Kind, event.Mailbox.Lane, event.Mailbox.State
	}
	if event.Run != nil {
		item.TaskID = event.Run.TaskID
		item.RunID, item.RunVersion, item.RunStatus = event.Run.ID, event.Run.Version, event.Run.Status
		item.RuntimeReplyState = event.Run.TurnResultState
		if event.Run.TurnResult != nil {
			item.RuntimeReply = boundedText(event.Run.TurnResult.Body, maxTimelineTextBytes)
			item.RuntimeReplyTruncated = event.Run.TurnResult.BodyTruncated || item.RuntimeReply != event.Run.TurnResult.Body
			item.RuntimeError = boundedText(event.Run.TurnResult.Error, maxTimelineTextBytes)
			item.RuntimeErrorTruncated = event.Run.TurnResult.ErrorTruncated || item.RuntimeError != event.Run.TurnResult.Error
		}
	}
	if event.Message != nil {
		item.MessageID, item.MessageVersion = event.Message.MessageID, event.Message.Version
	}
	if event.Approval != nil {
		item.ApprovalID, item.ApprovalState = event.Approval.ApprovalRequestID, event.Approval.State
	}
	if event.Output != nil {
		output := *event.Output
		output.Stage = boundedText(output.Stage, 128)
		output.Status = boundedText(output.Status, 128)
		output.Text = boundedText(output.Text, maxTimelineTextBytes)
		output.Diagnostic = boundedText(output.Diagnostic, maxTimelineTextBytes)
		output.TextTruncated = output.TextTruncated || output.Text != event.Output.Text
		output.DiagnosticTruncated = output.DiagnosticTruncated || output.Diagnostic != event.Output.Diagnostic
		item.Output = &output
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		return nil, fmt.Errorf("encode Console Timeline item: %w", err)
	}
	if len(encoded) > maxTimelineEntryBytes {
		if item.TaskResult != "" {
			item.TaskResultTruncated = true
		}
		if item.TaskError != "" {
			item.TaskErrorTruncated = true
		}
		if item.RuntimeReply != "" {
			item.RuntimeReplyTruncated = true
		}
		if item.RuntimeError != "" {
			item.RuntimeErrorTruncated = true
		}
		item.TaskResult, item.TaskError, item.RuntimeReply, item.RuntimeError = "", "", "", ""
		if item.Output != nil {
			item.Output.Text, item.Output.Diagnostic = "[TRUNCATED]", ""
			item.Output.TextTruncated, item.Output.DiagnosticTruncated = true, false
		}
		encoded, err = json.Marshal(item)
		if err != nil || len(encoded) > maxTimelineEntryBytes {
			return nil, fmt.Errorf("Console Timeline item exceeds the safe bound")
		}
	}
	item.encodedSize = len(encoded)
	return &item, nil
}

func applyTaskReference(record *taskRecord, version int64, status domain.TaskStatus) error {
	if record.Version == 0 {
		record.Version, record.Status = version, status
		record.encodedSize = 0
		return nil
	}
	if version < record.Version {
		return nil
	}
	if version == record.Version {
		if status != record.Status {
			return fmt.Errorf("Console Task status changed within version %d", version)
		}
		return nil
	}
	if taskStatusTerminal(record.Status) {
		if status != record.Status {
			return fmt.Errorf("terminal Console Task cannot change execution status")
		}
		record.Version = version
		record.encodedSize = 0
		return nil
	}
	record.Version, record.Status = version, status
	record.Detail = nil
	record.encodedSize = 0
	return nil
}

func applyTaskProjection(record *taskRecord, task openapi.ConsoleTaskReadModel) (changed, ignored bool, err error) {
	if record.Version > task.Version {
		return false, true, nil
	}
	intent := task.Intent
	if record.Intent != "" && record.Intent != intent {
		return false, false, fmt.Errorf("Console Task intent cannot change")
	}
	if record.Version > 0 && taskStatusTerminal(record.Status) {
		if record.Status != task.Status {
			return false, false, fmt.Errorf("terminal Console Task cannot change execution status")
		}
		if record.Detail != nil {
			before, after := *record.Detail, task
			before.Version, after.Version = 0, 0
			before.UpdatedAt, after.UpdatedAt = "", ""
			before.Review, after.Review = nil, nil
			if !reflect.DeepEqual(before, after) {
				return false, false, fmt.Errorf("terminal Console Task execution evidence cannot change")
			}
		}
	}
	if record.Version == task.Version {
		if record.Status != "" && record.Status != task.Status {
			return false, false, fmt.Errorf("Console Task status changed within version %d", task.Version)
		}
		if record.Detail != nil {
			if reflect.DeepEqual(*record.Detail, task) {
				return false, false, nil
			}
			previous, incoming := *record.Detail, task
			if previous.Version < incoming.Version && taskStatusTerminal(task.Status) {
				previous.Version = incoming.Version
				previous.UpdatedAt = incoming.UpdatedAt
			}
			previous.Review, incoming.Review = nil, nil
			if !reflect.DeepEqual(previous, incoming) {
				return false, false, fmt.Errorf("Console Task projection changed within version %d", task.Version)
			}
			if task.Review == nil {
				task.Review = record.Detail.Review
			}
		}
	}
	copy := cloneTask(&task)
	record.TaskID, record.Version, record.Status = task.TaskID, task.Version, task.Status
	record.Intent = intent
	record.UpdatedAt = task.UpdatedAt
	record.Summary = taskSummary(task)
	record.Detail = copy
	if taskStatusTerminal(task.Status) {
		record.PendingApproval = nil
		if record.LatestRun != nil && record.LatestRun.Status.Active() {
			record.LatestRun = nil
		}
	}
	record.encodedSize = 0
	return true, false, nil
}

func validateTaskSnapshot(snapshot openapi.ConsoleTaskSnapshot, agentID string) error {
	if snapshot.SnapshotSequence < 0 || snapshot.Task.AgentID != agentID {
		return fmt.Errorf("invalid Console Task snapshot identity or cursor")
	}
	if err := validateTaskProjection(snapshot.Task, agentID); err != nil {
		return err
	}
	if snapshot.WorkDelivery != nil {
		if err := validateMailboxProjection(*snapshot.WorkDelivery); err != nil ||
			snapshot.WorkDelivery.Kind != domain.MailboxKindTask || snapshot.WorkDelivery.Lane != domain.MailboxLaneWork {
			return fmt.Errorf("invalid Console Task work delivery projection")
		}
	}
	if snapshot.LatestRun != nil {
		if err := validateRun(*snapshot.LatestRun, agentID); err != nil || snapshot.LatestRun.TaskID != snapshot.Task.TaskID {
			return fmt.Errorf("invalid Console Task Run projection")
		}
	}
	if snapshot.LatestMessage != nil {
		if err := validateMessageProjection(*snapshot.LatestMessage); err != nil {
			return err
		}
	}
	if snapshot.PendingApproval != nil {
		if err := validateApprovalProjection(*snapshot.PendingApproval); err != nil || snapshot.PendingApproval.State != domain.ApprovalRequestPending {
			return fmt.Errorf("invalid Console Task Approval projection")
		}
	}
	return nil
}

func validateTaskOption(option openapi.ConsoleTaskOption) error {
	if err := domain.ValidateOpaqueID("task_id", option.TaskID); err != nil {
		return err
	}
	if !option.Intent.Valid() {
		return fmt.Errorf("invalid Console Task option intent")
	}
	if option.Version <= 0 || !option.Status.Valid() || strings.TrimSpace(option.Summary) == "" ||
		len(option.Summary) > 1<<10 || !utf8.ValidString(option.Summary) {
		return fmt.Errorf("invalid Console Task option")
	}
	if _, err := time.Parse(time.RFC3339Nano, option.UpdatedAt); err != nil {
		return fmt.Errorf("invalid Console Task option timestamp")
	}
	return nil
}

func validateTaskProjection(task openapi.ConsoleTaskReadModel, agentID string) error {
	if err := domain.ValidateOpaqueID("task_id", task.TaskID); err != nil {
		return err
	}
	if task.AgentID != agentID || domain.ValidateIdentifier("agent_id", task.AgentID) != nil || task.Version <= 0 || !task.Status.Valid() {
		return fmt.Errorf("invalid Console Task identity, version, or status")
	}
	if review := task.Review; review != nil {
		if review.TaskID != task.TaskID || review.TaskVersion > task.Version || review.TaskVersion <= 0 || review.RunVersion <= 0 || domain.ValidateOpaqueID("run_id", review.RunID) != nil || (review.Decision != "accepted" && review.Decision != "rejected") || !safeText(review.Note) {
			return fmt.Errorf("invalid Console result review")
		}
	}
	if !task.Intent.Valid() {
		return fmt.Errorf("invalid Console Task intent")
	}
	if !task.CompletionBasis.Valid() || task.CompletionBasis != "" &&
		(task.Status != domain.TaskStatusSucceeded ||
			task.CompletionBasis == domain.TaskCompletionQueryResultDelivered && task.Intent != domain.TaskIntentQuery ||
			task.CompletionBasis == domain.TaskCompletionMutationEffectsKnown && task.Intent != domain.TaskIntentMutation) {
		return fmt.Errorf("invalid Console Task completion basis")
	}
	if strings.TrimSpace(task.Content) == "" || !safeText(task.Content) || !safeOptionalText(task.Result) || !safeOptionalText(task.Error) {
		return fmt.Errorf("invalid Console Task safe text")
	}
	if !validTruncation(task.Result, task.ResultTruncated) || !validTruncation(task.Error, task.ErrorTruncated) {
		return fmt.Errorf("invalid Console Task truncation metadata")
	}
	if _, err := time.Parse(time.RFC3339Nano, task.CreatedAt); err != nil {
		return fmt.Errorf("invalid Console Task created_at")
	}
	if _, err := time.Parse(time.RFC3339Nano, task.UpdatedAt); err != nil {
		return fmt.Errorf("invalid Console Task updated_at")
	}
	terminal := taskStatusTerminal(task.Status)
	switch task.OutcomeState {
	case "pending":
		if terminal {
			return fmt.Errorf("terminal Console Task has a pending outcome")
		}
	case "not_recorded":
		if !terminal || nonEmpty(task.Result) || nonEmpty(task.Error) || task.ResultTruncated || task.ErrorTruncated {
			return fmt.Errorf("invalid not-recorded Console Task outcome")
		}
	case "available":
		if !terminal || !nonEmpty(task.Result) && !nonEmpty(task.Error) || task.ResultTruncated || task.ErrorTruncated {
			return fmt.Errorf("invalid available Console Task outcome")
		}
	case "truncated":
		if !terminal || !task.ResultTruncated && !task.ErrorTruncated {
			return fmt.Errorf("invalid truncated Console Task outcome")
		}
	default:
		return fmt.Errorf("invalid Console Task outcome state")
	}
	return nil
}

func validateMailboxProjection(mailbox openapi.ConsoleMailboxReadModel) error {
	if err := domain.ValidateOpaqueID("mailbox_item_id", mailbox.MailboxItemID); err != nil {
		return err
	}
	if !mailbox.Kind.Valid() || !mailbox.Lane.Valid() || !mailbox.State.Valid() || mailbox.Attempts < 0 || mailbox.CreatedAt.IsZero() ||
		mailbox.AcceptedAt != nil && mailbox.AcceptedAt.IsZero() || mailbox.LeaseUntil != nil && mailbox.LeaseUntil.IsZero() {
		return fmt.Errorf("invalid Console Mailbox projection")
	}
	if mailbox.Kind == domain.MailboxKindTask && mailbox.Lane != domain.MailboxLaneWork ||
		mailbox.Kind == domain.MailboxKindCancel && mailbox.Lane != domain.MailboxLaneControl {
		return fmt.Errorf("invalid Console Mailbox kind or lane")
	}
	if mailbox.WorkerInstanceID == "" && mailbox.LeaseUntil != nil || mailbox.WorkerInstanceID != "" && domain.ValidateOpaqueID("worker_instance_id", mailbox.WorkerInstanceID) != nil {
		return fmt.Errorf("invalid Console Mailbox Worker identity")
	}
	return nil
}

func validateMessageProjection(message openapi.ConsoleMessageReadModel) error {
	if domain.ValidateOpaqueID("message_id", message.MessageID) != nil || message.Version <= 0 || message.Sequence <= 0 ||
		!message.Kind.Valid() || strings.TrimSpace(message.Content) == "" || !safeText(message.Content) {
		return fmt.Errorf("invalid Console Message projection")
	}
	if _, err := time.Parse(time.RFC3339Nano, message.CreatedAt); err != nil {
		return fmt.Errorf("invalid Console Message timestamp")
	}
	return nil
}

func validateApprovalProjection(approval openapi.ConsoleApprovalReadModel) error {
	if domain.ValidateOpaqueID("approval_request_id", approval.ApprovalRequestID) != nil ||
		!approval.Mode.Valid() || !approval.State.Valid() || approval.ExpiresAt.IsZero() || approval.CreatedAt.IsZero() {
		return fmt.Errorf("invalid Console Approval projection")
	}
	if approval.Mode == domain.ApprovalModeNative {
		if domain.ValidateOpaqueID("target_run_id", approval.TargetRunID) != nil || approval.ExpectedRunVersion <= 0 {
			return fmt.Errorf("invalid native Console Approval target")
		}
	} else if approval.TargetRunID != "" || approval.ExpectedRunVersion != 0 {
		return fmt.Errorf("invalid preflight Console Approval target")
	}
	return nil
}

func validateSafeOutput(output openapi.SafeOutputReadModel) error {
	for _, value := range []string{output.Stage, output.Status, output.Text, output.Diagnostic} {
		if value != "" && !safeText(value) {
			return fmt.Errorf("invalid Console safe output")
		}
	}
	if !validTruncatedValue(output.Text, output.TextTruncated) ||
		!validTruncatedValue(output.Diagnostic, output.DiagnosticTruncated) ||
		output.Text != "" && !output.HasOutput || output.Diagnostic != "" && !output.HasError {
		return fmt.Errorf("invalid Console safe output metadata")
	}
	return nil
}

func validateRun(run openapi.RunAttemptReadModel, agentID string) error {
	if domain.ValidateOpaqueID("run_id", run.ID) != nil || domain.ValidateOpaqueID("task_id", run.TaskID) != nil ||
		run.AgentID != agentID || domain.ValidateIdentifier("agent_id", run.AgentID) != nil || run.Version <= 0 ||
		!run.Status.Valid() || domain.ValidateOpaqueID("worker_instance_id", run.WorkerInstanceID) != nil ||
		run.WorkerGeneration == nil || *run.WorkerGeneration <= 0 || run.StartedAt.IsZero() || run.UpdatedAt.IsZero() {
		return fmt.Errorf("invalid Console Run projection")
	}
	if run.FinishedAt != nil && run.FinishedAt.IsZero() {
		return fmt.Errorf("invalid Console Run finished_at")
	}
	switch run.TurnResultState {
	case "not_recorded", "invalid":
		if run.TurnResult != nil {
			return fmt.Errorf("invalid Console Runtime reply projection")
		}
	case "empty", "available", "truncated":
		if run.TurnResult == nil || !run.TurnResult.RuntimeStatus.Valid() || !safeText(run.TurnResult.Body) || !safeText(run.TurnResult.Error) {
			return fmt.Errorf("invalid Console Runtime reply projection")
		}
		if !validTruncatedValue(run.TurnResult.Body, run.TurnResult.BodyTruncated) ||
			!validTruncatedValue(run.TurnResult.Error, run.TurnResult.ErrorTruncated) {
			return fmt.Errorf("invalid Console Runtime reply truncation metadata")
		}
		if run.TurnResult.FinalReply && (run.TurnResult.RuntimeStatus != "succeeded" ||
			strings.TrimSpace(run.TurnResult.Body) == "" || run.TurnResult.BodyTruncated ||
			run.TurnResult.Error != "" || run.TurnResult.ErrorTruncated) {
			return fmt.Errorf("invalid Console final reply evidence")
		}
		if run.TurnResult.SideEffectsSource != "not_recorded" && run.TurnResult.SideEffectsSource != "runtime_reported" ||
			run.TurnResult.BusinessVerificationSource != "not_recorded" {
			return fmt.Errorf("invalid Console Runtime evidence source")
		}
		hasContent := run.TurnResult.Body != "" || run.TurnResult.Error != ""
		hasTruncation := run.TurnResult.BodyTruncated || run.TurnResult.ErrorTruncated
		if run.TurnResultState == "empty" && (hasContent || hasTruncation) ||
			run.TurnResultState == "available" && (!hasContent || hasTruncation) ||
			run.TurnResultState == "truncated" && !hasTruncation {
			return fmt.Errorf("Console Runtime reply does not match its state")
		}
	default:
		return fmt.Errorf("invalid Console Runtime reply state")
	}
	return nil
}

func safeText(value string) bool {
	return utf8.ValidString(value) && len(value) <= safeoutput.MaxTextBytes+len(safeoutput.TruncatedMarker)
}

func safeOptionalText(value *string) bool { return value == nil || safeText(*value) }
func nonEmpty(value *string) bool         { return value != nil && *value != "" }

func validTruncation(value *string, truncated bool) bool {
	if value == nil {
		return !truncated
	}
	return validTruncatedValue(*value, truncated)
}

func validTruncatedValue(value string, truncated bool) bool {
	hasMarker := strings.HasSuffix(value, safeoutput.TruncatedMarker)
	return truncated == hasMarker
}

func taskStatusTerminal(status domain.TaskStatus) bool {
	return status == domain.TaskStatusSucceeded || status == domain.TaskStatusFailed ||
		status == domain.TaskStatusCanceled || status == domain.TaskStatusUncertain
}

func taskSummary(task openapi.ConsoleTaskReadModel) string {
	summary := strings.TrimSpace(strings.SplitN(task.Content, "\n", 2)[0])
	if summary == "" {
		summary = task.TaskID
	}
	return boundedText(summary, 1<<10)
}

func boundedText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	marker := safeoutput.TruncatedMarker
	cut := limit - len(marker)
	if cut < 0 {
		cut = 0
	}
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut] + marker
}

func cloneTaskState(source TaskState) TaskState {
	clone := source
	clone.Detail = cloneTask(source.Detail)
	clone.WorkDelivery = cloneMailbox(source.WorkDelivery)
	clone.LatestRun = cloneRun(source.LatestRun)
	clone.LatestMessage = cloneMessage(source.LatestMessage)
	clone.PendingApproval = cloneApproval(source.PendingApproval)
	return clone
}

func cloneTask(source *openapi.ConsoleTaskReadModel) *openapi.ConsoleTaskReadModel {
	if source == nil {
		return nil
	}
	clone := *source
	clone.Result = cloneString(source.Result)
	clone.Error = cloneString(source.Error)
	clone.ParentTaskID = cloneString(source.ParentTaskID)
	if source.Review != nil {
		review := *source.Review
		clone.Review = &review
	}
	return &clone
}

func cloneMailbox(source *openapi.ConsoleMailboxReadModel) *openapi.ConsoleMailboxReadModel {
	if source == nil {
		return nil
	}
	clone := *source
	clone.LeaseUntil = cloneTime(source.LeaseUntil)
	clone.AcceptedAt = cloneTime(source.AcceptedAt)
	return &clone
}

func cloneRun(source *openapi.RunAttemptReadModel) *openapi.RunAttemptReadModel {
	if source == nil {
		return nil
	}
	clone := *source
	if source.WorkerGeneration != nil {
		generation := *source.WorkerGeneration
		clone.WorkerGeneration = &generation
	}
	clone.FinishedAt = cloneTime(source.FinishedAt)
	if source.TurnResult != nil {
		result := *source.TurnResult
		if source.TurnResult.RuntimeSideEffectsKnown != nil {
			known := *source.TurnResult.RuntimeSideEffectsKnown
			result.RuntimeSideEffectsKnown = &known
		}
		clone.TurnResult = &result
	}
	return &clone
}

func cloneMessage(source *openapi.ConsoleMessageReadModel) *openapi.ConsoleMessageReadModel {
	if source == nil {
		return nil
	}
	clone := *source
	return &clone
}

func cloneApproval(source *openapi.ConsoleApprovalReadModel) *openapi.ConsoleApprovalReadModel {
	if source == nil {
		return nil
	}
	clone := *source
	return &clone
}

func cloneString(source *string) *string {
	if source == nil {
		return nil
	}
	clone := *source
	return &clone
}

func cloneTime(source *time.Time) *time.Time {
	if source == nil {
		return nil
	}
	clone := *source
	return &clone
}

func cloneTimelineItem(source TimelineItem) TimelineItem {
	clone := source
	if source.Output != nil {
		output := *source.Output
		clone.Output = &output
	}
	return clone
}

func sameRun(left, right *openapi.RunAttemptReadModel) bool { return reflect.DeepEqual(left, right) }
