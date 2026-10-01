package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"sync"
	"testing"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

// All review tests use isolated SQLite fixtures; none constitute AGY E2E evidence.
func completedReviewFixture(t *testing.T, repository *Repository, suffix string, result openruntime.TurnResult) (repositoryFixture, *domain.Task, *domain.RunAttempt) {
	t.Helper()
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerQueued)
	worker, guard := registerMessageWorker(t, repository, fixture, descriptor)
	created, run := beginMessageRun(t, repository, fixture, worker, descriptor, suffix)
	if err := repository.FinishRun(context.Background(), guard, run.ID, 2, run.Version, result, nil, 0, nil,
		journalEvent("event-review-finish-task-"+suffix, "task.settled", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-review-finish-run-"+suffix, "run_attempt.finished", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	task, err := repository.GetTask(context.Background(), created.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	run, err = repository.GetRunAttempt(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, task, run
}

func resultReview(fixture repositoryFixture, task *domain.Task, run *domain.RunAttempt, id, decision string) domain.TaskReview {
	return domain.TaskReview{ID: id, TaskID: task.ID, RunID: run.ID, RunVersion: run.Version, Decision: decision, Note: "human checked output", ReviewedBy: fixture.ownerPrincipal}
}

func assertReviewKeepsExecution(t *testing.T, repository *Repository, beforeTask *domain.Task, beforeRun *domain.RunAttempt, version int64) {
	t.Helper()
	afterTask, err := repository.GetTask(context.Background(), beforeTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := *beforeTask
	want.Version = version
	want.UpdatedAt = afterTask.UpdatedAt
	if !reflect.DeepEqual(*afterTask, want) {
		t.Fatalf("review changed execution Task: before=%+v after=%+v", want, afterTask)
	}
	afterRun, err := repository.GetRunAttempt(context.Background(), beforeRun.ID)
	if err != nil || !reflect.DeepEqual(beforeRun, afterRun) {
		t.Fatalf("review changed Run: before=%+v after=%+v err=%v", beforeRun, afterRun, err)
	}
}

func TestTaskReviewPersistsDecisionWithoutRewritingExecution(t *testing.T) {
	for _, known := range []bool{false, true} {
		for _, decision := range []string{"accepted", "rejected"} {
			name := decision + "-uncertain-effects"
			if known {
				name = decision + "-known-effects"
			}
			t.Run(name, func(t *testing.T) {
				repository, _ := openTestRepository(t, nil)
				fixture, task, run := completedReviewFixture(t, repository, name, openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "completed artifact", FinalReply: true, SideEffectsKnown: known})
				review := resultReview(fixture, task, run, "review-"+name, decision)
				got, err := repository.ReviewTaskResult(context.Background(), task.Version, review)
				if err != nil {
					t.Fatal(err)
				}
				digest := sha256.Sum256([]byte(run.ResultJSON))
				if got.Decision != decision || got.ResultSHA256 != hex.EncodeToString(digest[:]) || got.Sequence <= 0 || got.TaskVersion != task.Version+1 || got.RunVersion != run.Version || got.CreatedAt.IsZero() {
					t.Fatalf("review=%+v", got)
				}
				persisted, err := repository.GetTaskReview(context.Background(), task.ID)
				if err != nil || !reflect.DeepEqual(got, persisted) {
					t.Fatalf("persisted review=%+v err=%v", persisted, err)
				}
				assertReviewKeepsExecution(t, repository, task, run, got.TaskVersion)
				assertCancelEventCount(t, repository, task.ID, "task.result_reviewed", 1)
				// Replay uses the original CAS and returns the same immutable receipt.
				replayed, err := repository.ReviewTaskResult(context.Background(), task.Version, review)
				if err != nil || !reflect.DeepEqual(got, replayed) {
					t.Fatalf("review replay=%+v err=%v", replayed, err)
				}
				assertReviewKeepsExecution(t, repository, task, run, got.TaskVersion)
				for _, field := range []string{"decision", "note", "task", "run", "run-version", "principal"} {
					conflict := review
					switch field {
					case "decision":
						conflict.Decision = "rejected"
						if decision == "rejected" {
							conflict.Decision = "accepted"
						}
					case "note":
						conflict.Note = "changed assessment"
					case "task":
						conflict.TaskID = "other-task"
					case "run":
						conflict.RunID = "other-run"
					case "run-version":
						conflict.RunVersion++
					case "principal":
						conflict.ReviewedBy = fixture.agentPrincipal
					}
					if _, err := repository.ReviewTaskResult(context.Background(), got.TaskVersion, conflict); !errors.Is(err, domain.ErrIdempotencyConflict) {
						t.Fatalf("%s conflict error=%v", field, err)
					}
				}
				assertCancelEventCount(t, repository, task.ID, "task.result_reviewed", 1)
			})
		}
	}
}

func TestTaskReviewRejectsStaleTaskAndRunVersions(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture, task, run := completedReviewFixture(t, repository, "stale-review", openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "done", SideEffectsKnown: true})
	review := resultReview(fixture, task, run, "review-stale", "accepted")
	for _, field := range []string{"task-version", "run-version", "run-id"} {
		candidate, version := review, task.Version
		switch field {
		case "task-version":
			version--
		case "run-version":
			candidate.RunVersion--
		case "run-id":
			candidate.RunID = "run-not-current"
		}
		if _, err := repository.ReviewTaskResult(context.Background(), version, candidate); !errors.Is(err, domain.ErrStaleVersion) {
			t.Fatalf("%s stale error=%v", field, err)
		}
	}
	assertReviewKeepsExecution(t, repository, task, run, task.Version)
	assertCancelEventCount(t, repository, task.ID, "task.result_reviewed", 0)
}

func TestTaskReviewRejectsUnconfirmedRuns(t *testing.T) {
	for _, state := range []string{"no-run", "active", "active-terminal-task", "uncertain", "failed", "canceled"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			repository, _ := openTestRepository(t, nil)
			var fixture repositoryFixture
			var task *domain.Task
			var run *domain.RunAttempt
			if state == "uncertain" || state == "failed" || state == "canceled" {
				result := openruntime.TurnResult{Status: openruntime.TurnResultStatus(state), Error: "unconfirmed execution"}
				fixture, task, run = completedReviewFixture(t, repository, state, result)
			} else {
				fixture = seedRepository(t, repository)
				if state == "no-run" {
					created := createTask(t, repository, fixture, state)
					var err error
					task, _, err = repository.RequestTaskCancel(ctx, created.Task.ID, created.Task.Version, fixture.ownerPrincipal, nil, journalEvent("cancel-no-run-review", "task.cancel_requested", fixture.ownerPrincipal, fixture.organizationID), nil)
					if err != nil {
						t.Fatal(err)
					}
					run = &domain.RunAttempt{ID: "missing-run", Version: 1}
				} else {
					descriptor := messageDescriptor(openruntime.SteerQueued)
					worker, _ := registerMessageWorker(t, repository, fixture, descriptor)
					created, active := beginMessageRun(t, repository, fixture, worker, descriptor, state)
					run = active
					var err error
					task, err = repository.GetTask(ctx, created.Task.ID)
					if err != nil {
						t.Fatal(err)
					}
					if state == "active-terminal-task" {
						// Isolated inconsistent-state fixture proves active Run checks do
						// not rely solely on the Task's terminal status.
						task, err = repository.TransitionTask(ctx, domain.TaskTransition{TaskID: task.ID, ExpectedVersion: task.Version, AllowedFrom: []domain.TaskStatus{domain.TaskStatusRunning}, To: domain.TaskStatusUncertain, Event: journalEvent("terminal-with-active", "task.uncertain", fixture.ownerPrincipal, fixture.organizationID)})
						if err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			_, err := repository.ReviewTaskResult(ctx, task.Version, resultReview(fixture, task, run, "review-unconfirmed", "accepted"))
			var failure *domain.DomainError
			if !errors.As(err, &failure) || failure.Code != "INVALID_INPUT" {
				t.Fatalf("unconfirmed %s should be domain rejection, got %v", state, err)
			}
			persisted, err := repository.GetTask(ctx, task.ID)
			if err != nil || !reflect.DeepEqual(task, persisted) {
				t.Fatalf("rejected review changed Task=%+v err=%v", persisted, err)
			}
			assertCancelEventCount(t, repository, task.ID, "task.result_reviewed", 0)
		})
	}
}

func TestTaskReviewRollsBackStateAndJournal(t *testing.T) {
	for _, point := range []FaultPoint{FaultAfterStateWrite, FaultBeforeCommit} {
		t.Run(string(point), func(t *testing.T) {
			fail := false
			injected := errors.New("review rollback fault")
			repository, _ := openTestRepository(t, func(at FaultPoint) error {
				if fail && at == point {
					return injected
				}
				return nil
			})
			fixture, task, run := completedReviewFixture(t, repository, "rollback-review", openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "done", SideEffectsKnown: true})
			review := resultReview(fixture, task, run, "review-rollback", "accepted")
			fail = true
			if _, err := repository.ReviewTaskResult(context.Background(), task.Version, review); !errors.Is(err, injected) {
				t.Fatalf("expected %s fault, got %v", point, err)
			}
			fail = false
			assertReviewKeepsExecution(t, repository, task, run, task.Version)
			assertCancelEventCount(t, repository, task.ID, "task.result_reviewed", 0)
			// A real Journal FK failure must also roll back the preceding Task write.
			bad := review
			bad.ReviewedBy = "missing-review-principal"
			if _, err := repository.ReviewTaskResult(context.Background(), task.Version, bad); err == nil {
				t.Fatal("missing Journal actor accepted")
			}
			assertReviewKeepsExecution(t, repository, task, run, task.Version)
			assertCancelEventCount(t, repository, task.ID, "task.result_reviewed", 0)
			if _, err := repository.ReviewTaskResult(context.Background(), task.Version, review); err != nil {
				t.Fatalf("review after rollback: %v", err)
			}
		})
	}
}

func TestTaskReviewConcurrentDuplicateIsOneAssessment(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture, task, run := completedReviewFixture(t, repository, "duplicate-review", openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "done", SideEffectsKnown: true})
	review := resultReview(fixture, task, run, "review-duplicate", "accepted")
	start := make(chan struct{})
	results := make(chan error, 12)
	var ready sync.WaitGroup
	ready.Add(12)
	for i := 0; i < 12; i++ {
		go func() {
			ready.Done()
			<-start
			_, err := repository.ReviewTaskResult(context.Background(), task.Version, review)
			results <- err
		}()
	}
	ready.Wait()
	close(start)
	for i := 0; i < 12; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	assertReviewKeepsExecution(t, repository, task, run, task.Version+1)
	assertCancelEventCount(t, repository, task.ID, "task.result_reviewed", 1)
}
