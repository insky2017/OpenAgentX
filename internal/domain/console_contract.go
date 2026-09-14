package domain

// ConsoleSnapshot is the transport-independent, transactionally consistent
// state used to attach an interactive Console to one logical Agent.
type ConsoleSnapshot struct {
	Agent            AgentIdentity
	Worker           *WorkerInstance
	BackendHealth    map[string]string
	ActiveRun        *RunAttempt
	SnapshotSequence int64
}

type JournalSequenceBounds struct {
	Earliest int64
	Latest   int64
}
