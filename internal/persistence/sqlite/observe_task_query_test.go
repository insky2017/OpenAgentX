package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"openagentx/internal/domain"
)

func TestQueryTasksUsesStableKeysetAndServerFilters(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	for _, suffix := range []string{"a", "b", "c", "other"} {
		createTask(t, repository, fixture, suffix)
	}
	if _, err := repository.db.Exec(`UPDATE tasks SET updated_at = ?, status = CASE WHEN task_id = 'task-other' THEN 'failed' ELSE 'running' END`, formatTime(repositoryTestTime)); err != nil {
		t.Fatal(err)
	}

	first, err := repository.QueryTasks(context.Background(), fixture.agentID, domain.TaskStatusRunning, repositoryTestTime.Add(-time.Minute), repositoryTestTime.Add(time.Minute), "task", "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].ID != "task-c" || first[1].ID != "task-b" {
		t.Fatalf("first page=%+v", first)
	}
	second, err := repository.QueryTasks(context.Background(), fixture.agentID, domain.TaskStatusRunning, repositoryTestTime.Add(-time.Minute), repositoryTestTime.Add(time.Minute), "task", first[1].UpdatedAt, first[1].ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].ID != "task-a" {
		t.Fatalf("second page=%+v", second)
	}
}

func TestQueryTasksNormalizesRFC3339PrecisionAndTimezoneForRangeAndCursor(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	for _, suffix := range []string{"lower-overflow", "whole", "one-ns", "100us", "200us", "a-utc", "z-offset", "upper-overflow"} {
		createTask(t, repository, fixture, suffix)
	}
	timestamps := map[string]string{
		"task-lower-overflow": "0000-01-01T00:00:00+14:00",
		"task-whole":          "2026-09-05T12:00:00Z",
		"task-one-ns":         "2026-09-05T12:00:00.000000001Z",
		"task-100us":          "2026-09-05T12:00:00.0001Z",
		"task-200us":          "2026-09-05T12:00:00.000200000Z",
		"task-a-utc":          "2026-09-05T12:00:00.000400000Z",
		"task-z-offset":       "2026-09-05T20:00:00.0004+08:00",
		"task-upper-overflow": "9999-12-31T23:59:59-14:00",
	}
	for taskID, updatedAt := range timestamps {
		if _, err := repository.db.Exec(`UPDATE tasks SET updated_at = ? WHERE task_id = ?`, updatedAt, taskID); err != nil {
			t.Fatal(err)
		}
	}

	wantOrder := []string{"task-upper-overflow", "task-z-offset", "task-a-utc", "task-200us", "task-100us", "task-one-ns", "task-whole", "task-lower-overflow"}
	cursorUpdatedAt, cursorTaskID := "", ""
	var gotOrder []string
	for range wantOrder {
		page, err := repository.QueryTasks(context.Background(), fixture.agentID, "", time.Time{}, time.Time{}, "", cursorUpdatedAt, cursorTaskID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) != 1 {
			t.Fatalf("normalized page after %q/%q=%+v", cursorUpdatedAt, cursorTaskID, page)
		}
		gotOrder = append(gotOrder, page[0].ID)
		cursorUpdatedAt, cursorTaskID = page[0].UpdatedAt, page[0].ID
	}
	for index := range wantOrder {
		if gotOrder[index] != wantOrder[index] {
			t.Fatalf("normalized order=%v want=%v", gotOrder, wantOrder)
		}
	}

	after, err := time.Parse(time.RFC3339Nano, "2026-09-05T12:00:00.000000001Z")
	if err != nil {
		t.Fatal(err)
	}
	before, err := time.Parse(time.RFC3339Nano, "2026-09-05T12:00:00.0004Z")
	if err != nil {
		t.Fatal(err)
	}
	inRange, err := repository.QueryTasks(context.Background(), fixture.agentID, "", after, before, "", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	wantRange := []string{"task-200us", "task-100us", "task-one-ns"}
	if len(inRange) != len(wantRange) {
		t.Fatalf("normalized time range=%+v", inRange)
	}
	for index := range wantRange {
		if inRange[index].ID != wantRange[index] {
			t.Fatalf("normalized time range=%+v want=%v", inRange, wantRange)
		}
	}
}

func TestSQLiteRFC3339NanoKeyKeepsUTCOverflowYearsOrdered(t *testing.T) {
	inputs := []string{
		"0000-01-01T00:00:00+14:00",
		"2026-09-05T12:00:00.000000001Z",
		"9999-12-31T23:59:59-14:00",
	}
	want := []string{
		"00000-12-31T10:00:00.000000000Z",
		"02027-09-05T12:00:00.000000001Z",
		"10001-01-01T13:59:59.000000000Z",
	}
	keys := make([]string, 0, len(inputs))
	for index, input := range inputs {
		key, err := sqliteRFC3339NanoKey(input)
		if err != nil {
			t.Fatal(err)
		}
		if key != want[index] {
			t.Fatalf("time key for %q=%q want=%q", input, key, want[index])
		}
		keys = append(keys, key)
	}
	if !(keys[0] < keys[1] && keys[1] < keys[2]) {
		t.Fatalf("time keys are not ordered: %v", keys)
	}
}

func TestRepositoryRegistersRFC3339NanoKeyOnEveryConnectionAndReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "connections.db")
	repository, err := Open(ctx, path, Options{MaxOpenConns: 3})
	if err != nil {
		t.Fatal(err)
	}

	connections := make([]*sql.Conn, 0, 3)
	for index := 0; index < 3; index++ {
		connection, err := repository.db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, connection)
		var key string
		if err := connection.QueryRowContext(ctx, `SELECT openagentx_rfc3339nano_key(?)`, "2026-09-05T20:00:00.000000001+08:00").Scan(&key); err != nil {
			t.Fatalf("connection %d time key: %v", index, err)
		}
		if key != "02027-09-05T12:00:00.000000001Z" {
			t.Fatalf("connection %d time key=%q", index, key)
		}
	}
	for _, connection := range connections {
		if err := connection.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	var key string
	if err := reopened.db.QueryRowContext(ctx, `SELECT openagentx_rfc3339nano_key(?)`, "2026-09-05T12:00:00.0001Z").Scan(&key); err != nil {
		t.Fatal(err)
	}
	if key != "02027-09-05T12:00:00.000100000Z" {
		t.Fatalf("reopened time key=%q", key)
	}
}

func TestQueryTasksRejectsMalformedStoredTimestamp(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	createTask(t, repository, fixture, "malformed-time")
	if _, err := repository.db.Exec(`UPDATE tasks SET updated_at = 'not-a-time' WHERE task_id = 'task-malformed-time'`); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.QueryTasks(context.Background(), fixture.agentID, "", time.Time{}, time.Time{}, "", "", "", 10); err == nil {
		t.Fatal("QueryTasks accepted a malformed stored timestamp")
	}
}

func TestListTaskJournalBeforeReturnsNewestHistoryInChronologicalOrder(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	created := createTask(t, repository, fixture, "history")
	for index := 0; index < 4; index++ {
		event := journalEvent("event-history-extra-"+string(rune('a'+index)), "task.updated", fixture.ownerPrincipal, fixture.organizationID)
		event.AggregateType = "task"
		event.AggregateID = created.Task.ID
		if _, err := repository.db.Exec(`INSERT INTO event_journal (event_id, organization_id, aggregate_type, aggregate_id, event_type, actor_principal_id, payload_json, created_at) VALUES (?, ?, ?, ?, ?, ?, '{}', ?)`, event.ID, fixture.organizationID, event.AggregateType, event.AggregateID, event.EventType, event.ActorPrincipalID, formatTime(repositoryTestTime)); err != nil {
			t.Fatal(err)
		}
	}
	latest, err := repository.LatestJournalSequence(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	page, err := repository.ListTaskJournalBefore(context.Background(), created.Task.ID, latest+1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 3 || page[0].Sequence != latest-2 || page[2].Sequence != latest {
		t.Fatalf("history page=%+v latest=%d", page, latest)
	}
	rangePage, err := repository.ListTaskJournalRange(context.Background(), created.Task.ID, latest-2, latest-1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rangePage) != 1 || rangePage[0].Sequence != latest-1 {
		t.Fatalf("bounded live page=%+v latest=%d", rangePage, latest)
	}
}
