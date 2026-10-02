package sqlite

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"openagentx/internal/api"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
)

// These tests use only isolated SQLite fixtures and the production command
// service. They prove D-level transaction/routing invariants, not native E2E.
func nativeTaskCommand(t *testing.T, repository *Repository) *controlplane.CommandService {
	t.Helper()
	service, err := controlplane.NewCommandService(repository, nil, func() time.Time { return repositoryTestTime })
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func nativeTaskRequest(f repositoryFixture, key, thread string) api.CreateTaskRequest {
	return api.CreateTaskRequest{Meta: api.CommandMeta{IdempotencyKey: key},
		SenderPrincipalID: f.ownerPrincipal, TargetAgentID: f.agentID, OrganizationID: f.organizationID,
		DispatchMode: domain.DispatchModeDirect, Intent: domain.TaskIntentQuery, Content: "continue this work",
		RuntimeSession: &domain.RuntimeSessionReference{BackendID: "codex-local", ProviderSessionID: thread}}
}

func nativeTaskCounts(t *testing.T, repository *Repository) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, table := range []string{"tasks", "messages", "mailbox_items", "session_bindings", "event_journal"} {
		var count int
		if err := repository.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		counts[table] = count
	}
	return counts
}

func nativeTaskUnchanged(t *testing.T, repository *Repository, before map[string]int) {
	t.Helper()
	if after := nativeTaskCounts(t, repository); !reflect.DeepEqual(after, before) {
		t.Fatalf("failed/replayed command changed durable rows: before=%v after=%v", before, after)
	}
}

func nativeTaskErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	var failure *domain.DomainError
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("error=%v, want domain code %s", err, code)
	}
}

func TestNativeTaskAtomicBindingDeliveryAndReplay(t *testing.T) {
	ctx := context.Background()
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	commands := nativeTaskCommand(t, repository)
	before := nativeTaskCounts(t, repository)
	request := nativeTaskRequest(fixture, "native-idempotent", "private-native-thread-1")
	created, err := commands.CreateTask(ctx, fixture.ownerPrincipal, request)
	if err != nil {
		t.Fatal(err)
	}
	after := nativeTaskCounts(t, repository)
	for table, delta := range map[string]int{"tasks": 1, "messages": 1, "mailbox_items": 1, "session_bindings": 1, "event_journal": 2} {
		if after[table]-before[table] != delta {
			t.Fatalf("%s delta=%d, want %d", table, after[table]-before[table], delta)
		}
	}
	binding, err := repository.GetSessionBinding(ctx, created.TaskID, fixture.agentID, "codex-local")
	if err != nil || binding.ProviderSessionID != request.RuntimeSession.ProviderSessionID || binding.State != domain.SessionBindingActive {
		t.Fatalf("binding=%+v err=%v", binding, err)
	}
	var privateLeaks int
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM event_journal WHERE payload_json LIKE ?`, "%"+binding.ProviderSessionID+"%").Scan(&privateLeaks); err != nil || privateLeaks != 0 {
		t.Fatalf("native routing ID leaked into Journal: count=%d err=%v", privateLeaks, err)
	}
	replay, err := commands.CreateTask(ctx, fixture.ownerPrincipal, request)
	if err != nil || !reflect.DeepEqual(created, replay) {
		t.Fatalf("replay=%+v want=%+v err=%v", replay, created, err)
	}
	nativeTaskUnchanged(t, repository, after)
	for _, change := range []string{"thread", "backend", "remove-session", "content"} {
		t.Run(change, func(t *testing.T) {
			conflict := request
			ref := *request.RuntimeSession
			conflict.RuntimeSession = &ref
			switch change {
			case "thread":
				ref.ProviderSessionID = "private-native-thread-2"
			case "backend":
				ref.BackendID = "other-backend"
			case "remove-session":
				conflict.RuntimeSession = nil
			case "content":
				conflict.Content = "different work"
			}
			if _, err := commands.CreateTask(ctx, fixture.ownerPrincipal, conflict); !errors.Is(err, domain.ErrIdempotencyConflict) {
				t.Fatalf("changed %s error=%v", change, err)
			}
			nativeTaskUnchanged(t, repository, after)
		})
	}
}

func TestNativeTaskRollbackIncludesEarlyBindingAndBothJournalEvents(t *testing.T) {
	for _, point := range []FaultPoint{FaultAfterStateWrite, FaultAfterDelivery, FaultBeforeCommit} {
		t.Run(string(point), func(t *testing.T) {
			repository, _ := openTestRepository(t, nil)
			fixture := seedRepository(t, repository)
			before := nativeTaskCounts(t, repository)
			injected := errors.New("native atomicity fault")
			failing := newRepository(repository.db, Options{Now: func() time.Time { return repositoryTestTime },
				FaultInjector: func(actual FaultPoint) error {
					if actual == point {
						return injected
					}
					return nil
				}})
			request := nativeTaskRequest(fixture, "native-rollback", "native-rollback-thread")
			if _, err := nativeTaskCommand(t, failing).CreateTask(context.Background(), fixture.ownerPrincipal, request); !errors.Is(err, injected) {
				t.Fatalf("fault result=%v", err)
			}
			nativeTaskUnchanged(t, repository, before)
			// Retrying the same input after the fault commits one complete unit.
			if _, err := nativeTaskCommand(t, repository).CreateTask(context.Background(), fixture.ownerPrincipal, request); err != nil {
				t.Fatalf("retry after rollback: %v", err)
			}
			if after := nativeTaskCounts(t, repository); after["tasks"] != before["tasks"]+1 || after["session_bindings"] != before["session_bindings"]+1 || after["event_journal"] != before["event_journal"]+2 {
				t.Fatalf("incomplete recovery after rollback: before=%v after=%v", before, after)
			}
		})
	}
}

func TestNativeTaskRejectsThreadAlreadyOwnedByOtherAgent(t *testing.T) {
	ctx := context.Background()
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	commands := nativeTaskCommand(t, repository)
	request := nativeTaskRequest(fixture, "native-owner", "shared-native-thread")
	if _, err := commands.CreateTask(ctx, fixture.ownerPrincipal, request); err != nil {
		t.Fatal(err)
	}
	principal := &domain.Principal{ID: "agent-other-principal", Kind: domain.PrincipalAgent, DisplayName: "Other", Status: domain.IdentityActive}
	if err := repository.CreatePrincipal(ctx, principal, journalEvent("event-native-other-principal", "principal.created", fixture.ownerPrincipal, "")); err != nil {
		t.Fatal(err)
	}
	agent := &domain.AgentIdentity{ID: "other", PrincipalID: principal.ID, OrganizationID: fixture.organizationID, DisplayName: "Other", Status: domain.AgentIdentityActive, Version: 1}
	profile := &domain.AgentProfileRecord{AgentID: agent.ID, Version: 1, InstructionsPath: "/roles/other.md", WorkspaceRoot: "/workspace/other"}
	if err := repository.CreateAgent(ctx, agent, profile, journalEvent("event-native-other-agent", "agent.created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	before := nativeTaskCounts(t, repository)
	request.TargetAgentID = agent.ID
	request.Meta.IdempotencyKey = "native-other-owner"
	_, err := commands.CreateTask(ctx, fixture.ownerPrincipal, request)
	nativeTaskErrorCode(t, err, "CONFLICT")
	nativeTaskUnchanged(t, repository, before)
}

func TestNativeTaskSourceRequiresTerminalMatchingIdentityAndActiveBinding(t *testing.T) {
	for _, scenario := range []string{"active-source", "other-agent", "other-org", "missing-binding", "invalid-binding", "finished-source"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			repository, _ := openTestRepository(t, nil)
			fixture := seedRepository(t, repository)
			commands := nativeTaskCommand(t, repository)
			initial := nativeTaskRequest(fixture, "native-source", "source-native-thread")
			if scenario == "missing-binding" {
				initial.RuntimeSession = nil
			}
			source, err := commands.CreateTask(ctx, fixture.ownerPrincipal, initial)
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "active-source" {
				_, _, err = repository.RequestTaskCancel(ctx, source.TaskID, source.TaskVersion, fixture.ownerPrincipal, nil,
					journalEvent("event-native-source-canceled", "task.cancel_requested", fixture.ownerPrincipal, fixture.organizationID), nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "invalid-binding" {
				// Isolated fixture only: invalid persisted binding must not resume.
				if _, err := repository.db.Exec(`UPDATE session_bindings SET state='invalid' WHERE context_id=?`, source.TaskID); err != nil {
					t.Fatal(err)
				}
			}
			next := nativeTaskRequest(fixture, "native-source-next", "")
			next.RuntimeSession.SourceTaskID = source.TaskID
			if scenario == "other-agent" {
				next.TargetAgentID = "other-agent"
			}
			if scenario == "other-org" {
				next.OrganizationID = "other-org"
			}
			before := nativeTaskCounts(t, repository)
			created, err := commands.CreateTask(ctx, fixture.ownerPrincipal, next)
			switch scenario {
			case "active-source":
				nativeTaskErrorCode(t, err, "INVALID_INPUT")
			case "other-agent", "other-org":
				nativeTaskErrorCode(t, err, "FORBIDDEN")
			case "missing-binding":
				if err == nil {
					t.Fatal("missing binding was accepted")
				}
			case "invalid-binding":
				if !errors.Is(err, domain.ErrUnsupportedCapability) {
					t.Fatalf("invalid binding error=%v", err)
				}
			case "finished-source":
				if err != nil {
					t.Fatal(err)
				}
				task, err := repository.GetTask(ctx, created.TaskID)
				if err != nil || task.ParentTaskID == nil || *task.ParentTaskID != source.TaskID {
					t.Fatalf("continuation=%+v err=%v", task, err)
				}
				binding, err := repository.GetSessionBinding(ctx, task.ID, fixture.agentID, "codex-local")
				if err != nil || binding.ProviderSessionID != "source-native-thread" {
					t.Fatalf("continued binding=%+v err=%v", binding, err)
				}
				before = nativeTaskCounts(t, repository)
				replay, err := commands.CreateTask(ctx, fixture.ownerPrincipal, next)
				if err != nil || !reflect.DeepEqual(created, replay) {
					t.Fatalf("continuation replay=%+v err=%v", replay, err)
				}
			}
			nativeTaskUnchanged(t, repository, before)
		})
	}
}

func TestNativeTaskOrdinaryReplayCompatibility(t *testing.T) {
	for _, eventType := range []string{"task.created", "task.queued"} {
		t.Run(eventType, func(t *testing.T) {
			repository, _ := openTestRepository(t, nil)
			fixture := seedRepository(t, repository)
			task, message, mailbox, event := newTaskDelivery(fixture, "ordinary-replay")
			event.EventType = eventType
			created, err := repository.CreateTask(context.Background(), task, message, mailbox, event)
			if err != nil {
				t.Fatal(err)
			}
			before := nativeTaskCounts(t, repository)
			request := nativeTaskRequest(fixture, task.IdempotencyKey, "")
			request.RuntimeSession = nil
			request.Intent = task.Intent
			request.Content = task.Content
			replayed, err := nativeTaskCommand(t, repository).CreateTask(context.Background(), fixture.ownerPrincipal, request)
			if err != nil || replayed.TaskID != created.Task.ID || replayed.Sequence != created.MailboxItem.Sequence {
				t.Fatalf("ordinary replay of %s: response=%+v err=%v", eventType, replayed, err)
			}
			nativeTaskUnchanged(t, repository, before)
			request.RuntimeSession = &domain.RuntimeSessionReference{BackendID: "codex-local", ProviderSessionID: "new-native-thread"}
			if _, err := nativeTaskCommand(t, repository).CreateTask(context.Background(), fixture.ownerPrincipal, request); !errors.Is(err, domain.ErrIdempotencyConflict) {
				t.Fatalf("ordinary request gained native session: %v", err)
			}
			nativeTaskUnchanged(t, repository, before)
		})
	}
}

func TestNativeTaskReferenceValidationRejectsAmbiguousRouting(t *testing.T) {
	for _, ref := range []domain.RuntimeSessionReference{
		{BackendID: "codex-local"},
		{BackendID: "codex-local", ProviderSessionID: "thread", SourceTaskID: "task"},
		{ProviderSessionID: "thread"},
		{BackendID: "bad backend", ProviderSessionID: "thread"},
		{BackendID: "codex-local", ProviderSessionID: strings.Repeat("x", 1025)},
	} {
		request := nativeTaskRequest(repositoryFixture{ownerPrincipal: "owner", agentID: "agent", organizationID: "org"}, "validation", "")
		request.RuntimeSession = &ref
		if err := request.Validate(); err == nil {
			t.Fatalf("accepted ambiguous or invalid native reference: %+v", ref)
		}
	}
}
