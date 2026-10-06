package sqlite

import (
	"context"
	"errors"
	"testing"

	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

// Real SQLite transactions + the actual planner prove the persistent boundary,
// including faults between preference/profile and journal commit.
func TestAgentModelSettingsPersistencePlanningAndRollback(t *testing.T) {
	r, path := openTestRepository(t, nil)
	f := seedRepository(t, r)
	d := messageDescriptor(openruntime.SteerNative)
	d.AdapterID = "codex-app-server"
	d.RuntimeIdentity.AdapterID = d.AdapterID
	d.Models = []string{"first-model", "second-model"}
	d.ReasoningModes = []domain.ReasoningMode{domain.ReasoningBackendDefault, domain.ReasoningEffort}
	d.ModelReasoningEfforts = map[string][]string{"first-model": {"low"}, "second-model": {"medium", "high"}}
	w, _ := registerMessageWorker(t, r, f, d)
	ctx := context.Background()
	backends, err := r.ListWorkerBackends(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	backendID := backends[0].BackendID
	settings, err := r.GetAgentModelSettings(ctx, f.agentID, backendID)
	if err != nil || settings.Version != 0 || settings.Model != "first-model" {
		t.Fatalf("defaults=%+v err=%v", settings, err)
	}
	request := domain.AgentModelSettingsUpdate{BackendID: backendID, Model: "second-model", Effort: "high"}
	planner := controlplane.M1TurnPlanner{Bindings: r}
	task := createTask(t, r, f, "model-settings").Task
	before, err := planner.Plan(ctx, task, nil, backends)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []FaultPoint{FaultAfterStateWrite, FaultBeforeCommit} {
		r.faultInjector = func(p FaultPoint) error {
			if p == fault {
				return errors.New("injected settings rollback")
			}
			return nil
		}
		if _, err := r.SetAgentModelSettings(ctx, f.agentID, request, f.ownerPrincipal); err == nil {
			t.Fatal("fault accepted")
		}
		r.faultInjector = nil
		got, err := r.GetAgentModelSettings(ctx, f.agentID, backendID)
		if err != nil || got.Version != 0 {
			t.Fatalf("rollback=%+v %v", got, err)
		}
	}
	saved, err := r.SetAgentModelSettings(ctx, f.agentID, request, f.ownerPrincipal)
	if err != nil || saved.Version != 1 {
		t.Fatalf("save=%+v err=%v", saved, err)
	}
	if _, err := r.SetAgentModelSettings(ctx, f.agentID, request, f.ownerPrincipal); !errors.Is(err, domain.ErrStaleVersion) {
		t.Fatalf("stale write=%v", err)
	}
	request.ExpectedVersion = 1
	if replay, err := r.SetAgentModelSettings(ctx, f.agentID, request, f.ownerPrincipal); err != nil || replay.Version != 1 {
		t.Fatalf("same preference replay=%+v %v", replay, err)
	}
	request.Model = "unknown-model"
	if _, err := r.SetAgentModelSettings(ctx, f.agentID, request, f.ownerPrincipal); err == nil {
		t.Fatal("unsupported model accepted")
	}
	request.Model = "first-model"
	if _, err := r.SetAgentModelSettings(ctx, f.agentID, request, f.ownerPrincipal); err == nil {
		t.Fatal("unsupported model-effort pair accepted")
	}
	plan, err := planner.Plan(ctx, task, nil, backends)
	if err != nil || plan.Execution.Spec.Model != "second-model" || plan.Execution.Spec.Reasoning.Value != "high" || plan.Execution.Sources["model"] != "agent_model_settings:1" {
		t.Fatalf("plan=%+v %v", plan, err)
	}
	if before.Execution.Spec.Model != "first-model" || before.Execution.Spec.Reasoning.Mode != domain.ReasoningBackendDefault {
		t.Fatal("prior frozen plan changed")
	}
	var count int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM event_journal WHERE event_type='agent.model_settings.updated'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("audit count=%d %v", count, err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.GetAgentModelSettings(ctx, f.agentID, backendID)
	if err != nil || got.Model != "second-model" || got.Effort != "high" || got.Version != 1 {
		t.Fatalf("reopened=%+v %v", got, err)
	}
}

func TestAgentRemovalIncludesExclusiveModelPreference(t *testing.T) {
	r, _ := removalFixture(t)
	removalExec(t, r.db, `INSERT INTO execution_profiles VALUES('agent-model:test','org','agent-model:test','{"kind":"agent_model_settings_v1","model":"model"}',1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
 INSERT INTO agent_profiles VALUES('test',1,'/test/ROLE.md','/test','agent-model:test','[]','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');`)
	plan := removalPlan(t, r)
	if plan.Counts["execution_profiles"] != 1 || len(plan.Blockers) != 0 {
		t.Fatalf("preference cleanup plan=%+v", plan)
	}
	if _, err := r.ApplyAgentRemoval(context.Background(), "owner", plan.AgentIDs, plan.Digest); err != nil {
		t.Fatal(err)
	}
	if removalCount(t, r.db, "execution_profiles") != 0 {
		t.Fatal("exclusive model preference retained")
	}
}
