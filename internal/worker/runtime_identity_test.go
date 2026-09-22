package worker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	"openagentx/internal/runtime/acp"
	"openagentx/internal/runtime/agy"
	"openagentx/internal/runtime/codebuddy"
	runtimenetwork "openagentx/internal/runtime/network"
	"openagentx/internal/safeoutput"
)

// Exercise both Worker preflight and the actual process Adapter. A changed
// native program must run exactly once; the immutable network snapshot stays old.
func TestRuntimeExecutableUpdateWarnsAndRuns(t *testing.T) {
	for _, adapterID := range []string{"agy-batch", "codebuddy-cli", "acp-test"} {
		for _, scenario := range []string{"unchanged", "updated", "warning sink fails", "updated process fails"} {
			t.Run(adapterID+"/"+scenario, func(t *testing.T) {
				dir := t.TempDir()
				write := func(name, text string) string {
					t.Helper()
					path := filepath.Join(dir, name)
					if err := os.WriteFile(path, []byte(text), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(path, 0o700); err != nil {
						t.Fatal(err)
					}
					return path
				}
				response := `{"event":"result","result":{"status":"SUCCESS","response":"fixture-ok"}}`
				if adapterID == "codebuddy-cli" {
					response = "fixture-ok"
				}
				if adapterID == "acp-test" {
					response = `{"status":"succeeded","result":"fixture-ok"}`
				}
				script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo fixture; exit 0; fi\ncat >/dev/null\nprintf 'started\\n' >> launches\nprintf '%s\\n' '" + response + "'\n"
				native := write("native", script)
				binary, helper := native, ""
				if adapterID == "agy-batch" {
					binary = write("wrapper", "#!/bin/sh\nexec ./native \"$@\"\n")
					helper = write("helper", "#!/bin/sh\necho helper-fixture-1\n")
				}
				identity, err := runtimenetwork.InspectRuntimeIdentity(context.Background(), adapterID, "1", binary, helper, native)
				if err != nil {
					t.Fatal(err)
				}
				launches := filepath.Join(dir, "launches")
				env := []string{"IDENTITY_PRIVATE=fixture-private-value"}
				var adapter openruntime.AgentRuntimeAdapter
				switch adapterID {
				case "agy-batch":
					adapter, err = agy.NewAdapter(agy.Config{Binary: binary, RealBinary: native, HelperBinary: helper,
						Models: []string{"model"}, WorkingDir: dir, Environment: env, RuntimeIdentity: identity})
				case "codebuddy-cli":
					adapter, err = codebuddy.NewAdapter(codebuddy.Config{Binary: binary, Models: []string{"model"},
						WorkingDir: dir, Environment: env, RuntimeIdentity: identity})
				default:
					adapter, err = acp.NewAdapter(acp.Config{Binary: binary, WorkingDir: dir, Environment: env, RuntimeIdentity: identity,
						Descriptor: openruntime.AdapterDescriptor{AdapterID: adapterID, BackendType: "acp", Version: "1", LaunchProtocol: "stdio",
							Models: []string{"model"}, ReasoningModes: []domain.ReasoningMode{domain.ReasoningBackendDefault},
							SessionModes: []domain.SessionMode{domain.SessionModeNew}, Steer: openruntime.SteerUnsupported,
							Approval: openruntime.ApprovalUnsupported, Cancel: openruntime.CancelProcessSignal, NetworkModes: []string{"inherit"}, MaxConcurrency: 1}})
				}
				if err != nil {
					t.Fatal(err)
				}
				pool, err := NewBackendPool([]RuntimeBackend{{ID: "primary", Adapter: adapter}})
				if err != nil {
					t.Fatal(err)
				}
				policy := domain.NetworkPolicy{Mode: domain.NetworkInherit, PolicyVersion: 13, BindingRevision: 6,
					ManifestDigest: strings.Repeat("a", 64), RuntimeIdentity: identity}
				request := openruntime.TurnRequest{Task: domain.Task{ID: "task-fixture", TargetAgentID: "quote", Content: "reply fixture-ok"},
					Execution: domain.ResolvedExecutionSpec{Version: 1, Spec: domain.ExecutionSpec{
						AdapterID: adapterID, BackendID: "primary", Model: "model", Timeout: 5 * time.Second,
						Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault},
						Session:   domain.SessionSpec{Mode: domain.SessionModeNew, ContextID: "context-fixture"}, Network: policy}}}
				before := request.Execution
				if scenario != "unchanged" {
					write("native", script+"# executable update\n")
				}
				if scenario == "updated process fails" {
					write("native", "#!/bin/sh\ncat >/dev/null\nprintf 'started\\n' >> launches\nexit 7\n")
				}
				// Publishing the same policy must also tolerate a native update.
				ack := pool.ProcessNetworkWork(context.Background(), &openapi.NetworkWorkEnvelope{Work: domain.NetworkWork{
					ID: "apply-fixture", Kind: domain.NetworkWorkApply, BackendID: "primary", Mode: policy.Mode,
					PolicyVersion: policy.PolicyVersion, BindingRevision: policy.BindingRevision, ManifestDigest: policy.ManifestDigest, RuntimeIdentity: identity}})
				if ack.State != "succeeded" || ack.Policy == nil || ack.Policy.RuntimeIdentity != identity {
					t.Fatalf("network apply rejected update or rewrote identity: state=%s diagnostic=%s", ack.State, ack.DiagnosticCode)
				}
				prepared, selected, err := pool.PrepareRunNetwork(context.Background(), request)
				if err != nil {
					t.Fatalf("Worker preflight rejected executable change: %v", err)
				}
				if !reflect.DeepEqual(prepared.Execution, before) {
					t.Fatal("preflight rewrote the frozen execution snapshot")
				}
				var events []openruntime.RuntimeEvent
				sinkErr := errors.New("fixture warning persistence failure")
				handle, err := selected.StartTurn(context.Background(), prepared, openruntime.EventSinkFunc(func(_ context.Context, event openruntime.RuntimeEvent) error {
					events = append(events, event)
					if scenario == "warning sink fails" {
						return sinkErr
					}
					return nil
				}))
				if scenario == "warning sink fails" {
					if !errors.Is(err, sinkErr) || handle != nil {
						t.Fatalf("sink failure did not stop launch: %v", err)
					}
					if _, err := os.Stat(launches); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("process started without persisting warning")
					}
					return
				}
				if err != nil {
					t.Fatalf("Adapter rejected executable update: %v", err)
				}
				result, waitErr := handle.Wait(context.Background())
				if scenario == "updated process fails" {
					if result.Status == openruntime.TurnResultSucceeded {
						t.Fatal("warning hid a real process failure")
					}
				} else if waitErr != nil || result.Status != openruntime.TurnResultSucceeded || result.Result != "fixture-ok" || result.SideEffectsKnown {
					t.Fatalf("unexpected turn result: status=%s result=%q error=%v", result.Status, result.Result, waitErr)
				}
				launched, err := os.ReadFile(launches)
				if err != nil || string(launched) != "started\n" {
					t.Fatalf("expected exactly one launch: %q %v", launched, err)
				}
				warnings := 0
				for index, event := range events {
					if event.Type != "identity.warning" {
						continue
					}
					warnings++
					if index != 0 || event.Validate() != nil {
						t.Fatal("warning must be the first valid public event")
					}
					payload, err := safeoutput.ProjectRuntimePayload(event.Payload)
					if err != nil {
						t.Fatal(err)
					}
					var projection safeoutput.RuntimeProjection
					if err := json.Unmarshal(payload, &projection); err != nil {
						t.Fatal(err)
					}
					if projection.Status != "warning" || !strings.Contains(projection.Text, "continuing") || projection.Diagnostic != "" || projection.HasError ||
						strings.Contains(string(payload), dir) || strings.Contains(string(payload), "fixture-private-value") {
						t.Fatal("invalid or unsafe warning projection")
					}
				}
				wantWarnings := 1
				if scenario == "unchanged" {
					wantWarnings = 0
				}
				if warnings != wantWarnings {
					t.Fatalf("warnings=%d want=%d", warnings, wantWarnings)
				}
			})
		}
	}
}
