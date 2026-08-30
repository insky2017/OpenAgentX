package domain

type CreateTaskResult struct {
	Task        Task
	MailboxItem MailboxItem
	Event       JournalEvent
	Replay      bool
}

type CreateMessageResult struct {
	Task        Task
	Message     Message
	MailboxItem MailboxItem
	Event       JournalEvent
}

type TaskTransition struct {
	TaskID            string
	ExpectedVersion   int64
	AllowedFrom       []TaskStatus
	To                TaskStatus
	Result            *string
	Error             *string
	CancelRequestedBy *string
	CancelRequestedAt *string
	Event             *JournalEvent
}
