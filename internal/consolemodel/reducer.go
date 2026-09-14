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
	if err := domain.ValidateIdentifier("agent_id", snapshot.AgentID); err != nil {
		return err
	}
	if snapshot.SnapshotSequence < 0 {
		return fmt.Errorf("Console snapshot sequence cannot be negative")
	}
	if snapshot.WorkerInstanceID == "" {
		if snapshot.Generation != 0 {
			return fmt.Errorf("Console snapshot generation requires a Worker identity")
		}
	} else {
		if err := domain.ValidateOpaqueID("worker_instance_id", snapshot.WorkerInstanceID); err != nil {
			return err
		}
		if snapshot.Generation <= 0 {
			return fmt.Errorf("Console Worker generation must be positive")
		}
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

	result := ApplyResult{CursorAdvanced: true}
	r.cursor = event.Sequence
	if event.Run != nil {
		if event.Run.AgentID != r.state.AgentID {
			return result, nil
		}
		if event.Run.Status.Active() {
			r.state.ActiveRun = &consoleapi.RunSnapshot{RunID: event.Run.ID, TaskID: event.Run.TaskID,
				Status: event.Run.Status, StartedAt: event.Run.StartedAt, UpdatedAt: event.Run.UpdatedAt}
		} else if r.state.ActiveRun != nil && r.state.ActiveRun.RunID == event.Run.ID {
			r.state.ActiveRun = nil
		}
		result.Timeline = cloneEvent(event)
		return result, nil
	}
	if event.Worker == nil {
		if event.EventType != "worker.heartbeat" {
			result.Timeline = cloneEvent(event)
		}
		return result, nil
	}
	worker := event.Worker
	if worker.AgentID != r.state.AgentID || worker.Generation <= 0 || worker.WorkerInstanceID == "" {
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
	r.state.WorkerInstanceID = worker.WorkerInstanceID
	r.state.Generation = worker.Generation
	r.state.WorkerStatus = worker.Status
	r.state.Capabilities = append([]string(nil), worker.Capabilities...)
	r.state.LastHeartbeatAt = worker.LastHeartbeatAt
	r.state.LeaseUntil = worker.LeaseUntil
	if event.EventType != "worker.heartbeat" || replaced || statusChanged || leaseAnomaly {
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

func leaseMovedBackward(previous, next time.Time) bool {
	return !previous.IsZero() && (next.IsZero() || next.Before(previous))
}

func cloneSnapshot(snapshot consoleapi.AttachResponse) consoleapi.AttachResponse {
	snapshot.Capabilities = append([]string(nil), snapshot.Capabilities...)
	if snapshot.BackendHealth != nil {
		backendHealth := make(map[string]openruntime.BackendHealth, len(snapshot.BackendHealth))
		for id, health := range snapshot.BackendHealth {
			backendHealth[id] = health
		}
		snapshot.BackendHealth = backendHealth
	}
	if snapshot.ActiveRun != nil {
		run := *snapshot.ActiveRun
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
		clone.Worker = &worker
	}
	if event.Output != nil {
		output := *event.Output
		clone.Output = &output
	}
	if event.Run != nil {
		run := *event.Run
		clone.Run = &run
	}
	return &clone
}
