package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"agentbus/internal/client"
	"agentbus/internal/service"
)

func runTask(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: agentbus task <submit|get|list|ack|status|send|complete|fail|cancel|watch> [flags]\n")
		return 1
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "submit":
		return runTaskSubmit(subArgs)
	case "get":
		return runTaskGet(subArgs)
	case "list":
		return runTaskList(subArgs)
	case "ack":
		return runTaskAck(subArgs)
	case "status":
		return runTaskStatus(subArgs)
	case "send":
		return runTaskSend(subArgs)
	case "complete":
		return runTaskComplete(subArgs)
	case "fail":
		return runTaskFail(subArgs)
	case "cancel":
		return runTaskCancel(subArgs)
	case "watch":
		return runTaskWatch(subArgs)
	default:
		fmt.Fprintf(os.Stderr, "Unknown task subcommand: %s\n", sub)
		return 1
	}
}

func extractPositionalID(fs *flag.FlagSet, flagVal *string) string {
	if flagVal != nil && *flagVal != "" {
		return *flagVal
	}
	if len(fs.Args()) > 0 {
		return fs.Args()[0]
	}
	return ""
}

func runTaskSubmit(args []string) int {
	fs := flag.NewFlagSet("task submit", flag.ContinueOnError)
	from := fs.String("from", "", "Sender agent ID (required)")
	to := fs.String("to", "", "Target agent ID (required)")
	idempotencyKey := fs.String("idempotency-key", "", "Idempotency key (required)")
	content := fs.String("content", "", "Task content/instruction (required)")
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	if strings.TrimSpace(*from) == "" {
		PrintError(fmt.Errorf("flag --from is required"))
		return 1
	}
	if strings.TrimSpace(*to) == "" {
		PrintError(fmt.Errorf("flag --to is required"))
		return 1
	}
	if strings.TrimSpace(*idempotencyKey) == "" {
		PrintError(fmt.Errorf("flag --idempotency-key is required"))
		return 1
	}
	if strings.TrimSpace(*content) == "" {
		PrintError(fmt.Errorf("flag --content is required"))
		return 1
	}

	c := client.NewClient(*socketPath)
	resp, err := c.SubmitTask(context.Background(), service.SubmitTaskRequest{
		SenderAgentID:  *from,
		TargetAgentID:  *to,
		IdempotencyKey: *idempotencyKey,
		Content:        *content,
	})
	if err != nil {
		PrintError(err)
		return 1
	}

	PrintJSON(resp)
	return 0
}

func runTaskGet(args []string) int {
	fs := flag.NewFlagSet("task get", flag.ContinueOnError)
	idFlag := fs.String("id", "", "Task ID")
	agentID := fs.String("agent", "", "Caller agent ID (required)")
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	taskID := extractPositionalID(fs, idFlag)
	if strings.TrimSpace(taskID) == "" {
		PrintError(fmt.Errorf("task ID is required (e.g. agentbus task get <task-id> --agent <agent-id>)"))
		return 1
	}
	if strings.TrimSpace(*agentID) == "" {
		PrintError(fmt.Errorf("flag --agent is required to get task"))
		return 1
	}

	c := client.NewClient(*socketPath)
	taskDetail, err := c.GetTask(context.Background(), taskID, *agentID)
	if err != nil {
		PrintError(err)
		return 1
	}

	PrintJSON(taskDetail)
	return 0
}

func runTaskList(args []string) int {
	fs := flag.NewFlagSet("task list", flag.ContinueOnError)
	agentID := fs.String("agent", "", "Agent ID (required)")
	status := fs.String("status", "", "Filter by task status (queued, running, succeeded, failed, canceled)")
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	if strings.TrimSpace(*agentID) == "" {
		PrintError(fmt.Errorf("flag --agent is required to list tasks"))
		return 1
	}

	c := client.NewClient(*socketPath)
	tasks, err := c.ListTasks(context.Background(), *agentID, *status)
	if err != nil {
		PrintError(err)
		return 1
	}

	PrintJSON(tasks)
	return 0
}

func runTaskAck(args []string) int {
	fs := flag.NewFlagSet("task ack", flag.ContinueOnError)
	idFlag := fs.String("id", "", "Task ID")
	agentID := fs.String("agent", "", "Target agent ID (required)")
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	taskID := extractPositionalID(fs, idFlag)
	if strings.TrimSpace(taskID) == "" {
		PrintError(fmt.Errorf("task ID is required (e.g. agentbus task ack <task-id> --agent <agent-id>)"))
		return 1
	}
	if strings.TrimSpace(*agentID) == "" {
		PrintError(fmt.Errorf("flag --agent is required"))
		return 1
	}

	c := client.NewClient(*socketPath)
	task, err := c.AckTask(context.Background(), taskID, *agentID)
	if err != nil {
		PrintError(err)
		return 1
	}

	PrintJSON(task)
	return 0
}

func runTaskStatus(args []string) int {
	fs := flag.NewFlagSet("task status", flag.ContinueOnError)
	idFlag := fs.String("id", "", "Task ID")
	agentID := fs.String("agent", "", "Target agent ID (required)")
	message := fs.String("message", "", "Status message (required)")
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	taskID := extractPositionalID(fs, idFlag)
	if strings.TrimSpace(taskID) == "" {
		PrintError(fmt.Errorf("task ID is required (e.g. agentbus task status <task-id> --agent <agent-id> --message <msg>)"))
		return 1
	}
	if strings.TrimSpace(*agentID) == "" {
		PrintError(fmt.Errorf("flag --agent is required"))
		return 1
	}
	if strings.TrimSpace(*message) == "" {
		PrintError(fmt.Errorf("flag --message is required"))
		return 1
	}

	c := client.NewClient(*socketPath)
	if err := c.UpdateTaskStatus(context.Background(), taskID, *agentID, *message); err != nil {
		PrintError(err)
		return 1
	}

	PrintJSON(map[string]string{"status": "ok", "task_id": taskID})
	return 0
}

func runTaskSend(args []string) int {
	fs := flag.NewFlagSet("task send", flag.ContinueOnError)
	idFlag := fs.String("id", "", "Task ID")
	from := fs.String("from", "", "Sender agent ID (required)")
	content := fs.String("content", "", "Supplemental message content (required)")
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	taskID := extractPositionalID(fs, idFlag)
	if strings.TrimSpace(taskID) == "" {
		PrintError(fmt.Errorf("task ID is required (e.g. agentbus task send <task-id> --from <agent-id> --content <msg>)"))
		return 1
	}
	if strings.TrimSpace(*from) == "" {
		PrintError(fmt.Errorf("flag --from is required"))
		return 1
	}
	if strings.TrimSpace(*content) == "" {
		PrintError(fmt.Errorf("flag --content is required"))
		return 1
	}

	c := client.NewClient(*socketPath)
	if err := c.SendMessage(context.Background(), taskID, *from, *content); err != nil {
		PrintError(err)
		return 1
	}

	PrintJSON(map[string]string{"status": "ok", "task_id": taskID})
	return 0
}

func runTaskComplete(args []string) int {
	fs := flag.NewFlagSet("task complete", flag.ContinueOnError)
	idFlag := fs.String("id", "", "Task ID")
	agentID := fs.String("agent", "", "Target agent ID (required)")
	result := fs.String("result", "", "Task completion result text/json (required)")
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	taskID := extractPositionalID(fs, idFlag)
	if strings.TrimSpace(taskID) == "" {
		PrintError(fmt.Errorf("task ID is required (e.g. agentbus task complete <task-id> --agent <agent-id> --result <result>)"))
		return 1
	}
	if strings.TrimSpace(*agentID) == "" {
		PrintError(fmt.Errorf("flag --agent is required"))
		return 1
	}
	if strings.TrimSpace(*result) == "" {
		PrintError(fmt.Errorf("flag --result is required"))
		return 1
	}

	c := client.NewClient(*socketPath)
	task, err := c.CompleteTask(context.Background(), taskID, *agentID, *result)
	if err != nil {
		PrintError(err)
		return 1
	}

	PrintJSON(task)
	return 0
}

func runTaskFail(args []string) int {
	fs := flag.NewFlagSet("task fail", flag.ContinueOnError)
	idFlag := fs.String("id", "", "Task ID")
	agentID := fs.String("agent", "", "Target agent ID (required)")
	errStr := fs.String("error", "", "Error reason (required)")
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	taskID := extractPositionalID(fs, idFlag)
	if strings.TrimSpace(taskID) == "" {
		PrintError(fmt.Errorf("task ID is required (e.g. agentbus task fail <task-id> --agent <agent-id> --error <error>)"))
		return 1
	}
	if strings.TrimSpace(*agentID) == "" {
		PrintError(fmt.Errorf("flag --agent is required"))
		return 1
	}
	if strings.TrimSpace(*errStr) == "" {
		PrintError(fmt.Errorf("flag --error is required"))
		return 1
	}

	c := client.NewClient(*socketPath)
	task, err := c.FailTask(context.Background(), taskID, *agentID, *errStr)
	if err != nil {
		PrintError(err)
		return 1
	}

	PrintJSON(task)
	return 0
}

func runTaskCancel(args []string) int {
	fs := flag.NewFlagSet("task cancel", flag.ContinueOnError)
	idFlag := fs.String("id", "", "Task ID")
	agentID := fs.String("agent", "", "Sender agent ID (required)")
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	taskID := extractPositionalID(fs, idFlag)
	if strings.TrimSpace(taskID) == "" {
		PrintError(fmt.Errorf("task ID is required (e.g. agentbus task cancel <task-id> --agent <agent-id>)"))
		return 1
	}
	if strings.TrimSpace(*agentID) == "" {
		PrintError(fmt.Errorf("flag --agent is required"))
		return 1
	}

	c := client.NewClient(*socketPath)
	task, err := c.CancelTask(context.Background(), taskID, *agentID)
	if err != nil {
		PrintError(err)
		return 1
	}

	PrintJSON(task)
	return 0
}

func runTaskWatch(args []string) int {
	fs := flag.NewFlagSet("task watch", flag.ContinueOnError)
	idFlag := fs.String("id", "", "Task ID")
	agentID := fs.String("agent", "", "Caller agent ID (required)")
	afterSeqStr := fs.String("after", "0", "Sequence number to start watching after")
	timeoutStr := fs.String("timeout", "30s", "Watch polling timeout (e.g. 30s)")
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	taskID := extractPositionalID(fs, idFlag)
	if strings.TrimSpace(taskID) == "" {
		PrintError(fmt.Errorf("task ID is required (e.g. agentbus task watch <task-id> --agent <agent-id> --after 0 --timeout 30s)"))
		return 1
	}
	if strings.TrimSpace(*agentID) == "" {
		PrintError(fmt.Errorf("flag --agent is required to watch task"))
		return 1
	}

	afterSeq, err := strconv.ParseInt(*afterSeqStr, 10, 64)
	if err != nil {
		PrintError(fmt.Errorf("invalid --after sequence: %w", err))
		return 1
	}

	timeout, err := time.ParseDuration(*timeoutStr)
	if err != nil {
		PrintError(fmt.Errorf("invalid --timeout: %w", err))
		return 1
	}

	c := client.NewClient(*socketPath)
	currSeq := afterSeq

	deadline := time.Now().Add(timeout)

	for {
		remTimeout := time.Until(deadline)
		if remTimeout <= 0 {
			break
		}
		pollTimeout := remTimeout
		if pollTimeout > 5*time.Second {
			pollTimeout = 5 * time.Second
		}

		events, err := c.GetEvents(context.Background(), taskID, *agentID, currSeq, pollTimeout)
		if err != nil {
			PrintError(err)
			return 1
		}

		for _, evt := range events {
			PrintJSON(evt)
			if evt.Sequence > currSeq {
				currSeq = evt.Sequence
			}
		}

		// Check if task is finished
		detail, err := c.GetTask(context.Background(), taskID, *agentID)
		if err == nil && detail.Task != nil && detail.Task.IsTerminal() {
			// Fetch any last remaining events
			finalEvents, err := c.GetEvents(context.Background(), taskID, *agentID, currSeq, 0)
			if err == nil {
				for _, evt := range finalEvents {
					PrintJSON(evt)
				}
			}
			return 0
		}
	}

	return 0
}
