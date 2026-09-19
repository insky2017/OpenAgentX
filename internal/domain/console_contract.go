package domain

import "time"

// ConsoleSnapshot is the transport-independent, transactionally consistent
// state used to attach an interactive Console to one logical Agent.
type ConsoleSnapshot struct {
	Agent                     AgentIdentity
	Worker                    *WorkerInstance
	BackendHealth             map[string]string
	ActiveRun                 *RunAttempt
	ActiveRunWorkerGeneration int64
	SuggestedTask             *Task
	SnapshotSequence          int64
}

// ConsoleTaskCursor is the transport-independent keyset used to page Tasks in
// deterministic updated_at DESC, task_id DESC order.
type ConsoleTaskCursor struct {
	UpdatedAt time.Time
	TaskID    string
}

// ConsoleTaskSnapshot is the storage-facing aggregate read under one SQLite
// transaction. Transport layers must validate ownership and apply the shared
// safe-output projection before exposing any field.
type ConsoleTaskSnapshot struct {
	Task                      Task
	WorkDelivery              *MailboxItem
	LatestRun                 *RunAttempt
	LatestRunWorkerGeneration int64
	LatestMessage             *Message
	PendingApproval           *ApprovalRequest
	SnapshotSequence          int64
}

type JournalSequenceBounds struct {
	Earliest int64
	Latest   int64
}
