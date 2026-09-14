package consolemodel

import (
	"fmt"
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
	state       consoleapi.AttachResponse
	initialized bool
	cursor      int64
}

func New(snapshot consoleapi.AttachResponse) (*Reducer, error) {
	r := &Reducer{}
	if err := r.ApplySnapshot(snapshot); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Reducer) ApplySnapshot(snapshot consoleapi.AttachResponse) error {
	if err := validateSnapshot(snapshot); err != nil {
		return err
	}
	if r.initialized {
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
	r.state = cloneSnapshot(snapshot)
	r.cursor = snapshot.SnapshotSequence
	r.initialized = true
	return nil
}

func (r *Reducer) Apply(event openapi.JournalEventReadModel) (ApplyResult, error) {
	if !r.initialized {
		return ApplyResult{}, fmt.Errorf("Console reducer is not initialized")
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
	if event.Worker != nil && event.Run != nil {
		return ApplyResult{}, fmt.Errorf("Console event cannot contain both Worker and Run projections")
	}
	if event.Worker != nil {
		if err := validateWorkerProjection(event); err != nil {
			if workerProjectionTargetsCurrent(event.Worker, r.state) {
				r.state.BackendHealth = nil
			}
			return ApplyResult{}, err
		}
	}
	if event.Run != nil {
		if err := validateRunProjection(event); err != nil {
			return ApplyResult{}, err
		}
	}

	result := ApplyResult{CursorAdvanced: true}
	r.cursor = event.Sequence
	if event.Run != nil {
		result.Timeline = cloneEvent(event)
		run := event.Run
		if run.AgentID != r.state.AgentID || r.state.WorkerInstanceID == "" ||
			run.WorkerInstanceID != r.state.WorkerInstanceID || *run.WorkerGeneration != r.state.Generation ||
			r.state.WorkerStatus == domain.WorkerStatusOffline {
			return result, nil
		}
		if run.Status.Active() {
			generation := *run.WorkerGeneration
			r.state.ActiveRun = &consoleapi.RunSnapshot{RunID: event.Run.ID, TaskID: event.Run.TaskID,
				Status: run.Status, WorkerInstanceID: run.WorkerInstanceID, WorkerGeneration: &generation,
				StartedAt: run.StartedAt, UpdatedAt: run.UpdatedAt}
		} else if r.state.ActiveRun != nil && r.state.ActiveRun.RunID == event.Run.ID {
			r.state.ActiveRun = nil
		}
		return result, nil
	}
	if event.Worker == nil {
		if event.EventType != "worker.heartbeat" {
			result.Timeline = cloneEvent(event)
		}
		return result, nil
	}
	worker := event.Worker
	if worker.AgentID != r.state.AgentID {
		return result, nil
	}
	if worker.Generation < r.state.Generation {
		return result, nil
	}
	if worker.Generation == r.state.Generation && r.state.WorkerInstanceID != "" && worker.WorkerInstanceID != r.state.WorkerInstanceID {
		return result, nil
	}

	replaced := worker.Generation > r.state.Generation || worker.WorkerInstanceID != r.state.WorkerInstanceID
	statusChanged := worker.Status != r.state.WorkerStatus
	leaseAnomaly := leaseMovedBackward(r.state.LeaseUntil, worker.LeaseUntil)
	backendHealthChanged := !sameBackendHealth(r.state.BackendHealth, worker.BackendHealth)
	if replaced || worker.Status == domain.WorkerStatusOffline {
		r.clearWorkerScopedState()
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
	if event.EventType != "worker.heartbeat" || replaced || statusChanged || leaseAnomaly || backendHealthChanged {
		result.Timeline = cloneEvent(event)
	}
	return result, nil
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

func validateRunProjection(event openapi.JournalEventReadModel) error {
	run := event.Run
	if event.AggregateType != "run_attempt" || event.AggregateID != run.ID {
		return fmt.Errorf("Run projection does not match its event aggregate")
	}
	if err := domain.ValidateOpaqueID("run_id", run.ID); err != nil {
		return err
	}
	if err := domain.ValidateOpaqueID("task_id", run.TaskID); err != nil {
		return err
	}
	if err := domain.ValidateIdentifier("agent_id", run.AgentID); err != nil {
		return err
	}
	if err := domain.ValidateOpaqueID("worker_instance_id", run.WorkerInstanceID); err != nil {
		return err
	}
	if !run.Status.Valid() || run.WorkerGeneration == nil || *run.WorkerGeneration <= 0 {
		return fmt.Errorf("invalid Run projection status or Worker generation")
	}
	return nil
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
		run := *event.Run
		if event.Run.WorkerGeneration != nil {
			generation := *event.Run.WorkerGeneration
			run.WorkerGeneration = &generation
		}
		clone.Run = &run
	}
	return &clone
}
