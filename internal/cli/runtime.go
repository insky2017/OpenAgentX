package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"agentbus/internal/client"
	"agentbus/internal/domain"
	"agentbus/internal/service"
)

func printNeutralHookResponse() int {
	fmt.Println("{}")
	return 0
}

func printStopFailClosed(taskID string, reason string) int {
	if reason == "" {
		if taskID != "" {
			reason = fmt.Sprintf("AgentBus runtime event recording failed for task %s; check daemon status before stopping.", taskID)
		} else {
			reason = "AgentBus daemon or task verification failed; check agent status before stopping."
		}
	}
	resp := map[string]string{
		"decision": "continue",
		"reason":   reason,
	}
	PrintJSON(resp)
	return 0
}

func runRuntime(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: agentbus runtime <agy-hook> [flags]\n")
		return 1
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "agy-hook":
		return runRuntimeAGYHook(subArgs)
	default:
		fmt.Fprintf(os.Stderr, "Unknown runtime subcommand: %s\n", sub)
		return 1
	}
}

func runRuntimeAGYHook(args []string) int {
	fs := flag.NewFlagSet("runtime agy-hook", flag.ContinueOnError)
	eventFlag := fs.String("event", "", "AGY event type (PreToolUse|PostToolUse|PreInvocation|PostInvocation|Stop) (required)")
	agentFlag := fs.String("agent", "auto", "Agent ID or 'auto' (default: auto)")
	taskFlag := fs.String("task", "", "Explicit task ID (optional)")
	socketPath := fs.String("socket", "", "Unix socket path")
	controlTimeoutFlag := fs.Duration("control-timeout", 5*time.Second, "Timeout for control plane operations (default: 5s)")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	// 1. Validate flags
	event := strings.TrimSpace(*eventFlag)
	if event == "" {
		PrintError(fmt.Errorf("flag --event is required"))
		return 1
	}
	if !domain.IsValidAGYEvent(event) {
		PrintError(fmt.Errorf("invalid --event '%s': must be one of PreToolUse, PostToolUse, PreInvocation, PostInvocation, Stop", event))
		return 1
	}
	if *controlTimeoutFlag <= 0 {
		PrintError(fmt.Errorf("--control-timeout must be greater than 0"))
		return 1
	}

	// 2. Read stdin JSON (max 1 MiB, single object only)
	limitReader := io.LimitReader(os.Stdin, 1024*1024+1)
	rawBytes, err := io.ReadAll(limitReader)
	if err != nil {
		PrintError(fmt.Errorf("failed to read stdin: %w", err))
		return 1
	}
	trimmedBytes := bytes.TrimSpace(rawBytes)
	if len(trimmedBytes) == 0 {
		PrintError(fmt.Errorf("stdin is empty: expected JSON object"))
		return 1
	}
	if len(rawBytes) > 1024*1024 {
		PrintError(fmt.Errorf("stdin exceeds maximum allowed size of 1 MiB"))
		return 1
	}

	var rawMap map[string]any
	dec := json.NewDecoder(bytes.NewReader(trimmedBytes))
	if err := dec.Decode(&rawMap); err != nil || rawMap == nil {
		PrintError(fmt.Errorf("stdin must be a single non-null JSON object: %v", err))
		return 1
	}
	if dec.More() {
		PrintError(fmt.Errorf("stdin contains multiple JSON values"))
		return 1
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		PrintError(fmt.Errorf("stdin contains trailing characters after JSON object"))
		return 1
	}

	// Extract session_id / conversationId if present
	var sessionID string
	for _, k := range []string{"session_id", "sessionId", "conversation_id", "conversationId"} {
		if v, ok := rawMap[k].(string); ok && strings.TrimSpace(v) != "" {
			sessionID = strings.TrimSpace(v)
			break
		}
	}

	// 3. Resolve Agent identity & Managed status within controlTimeout deadline
	resolvedSocket := client.ResolveSocketPath(*socketPath)
	c := client.NewClient(resolvedSocket)
	ctx, cancel := context.WithTimeout(context.Background(), *controlTimeoutFlag)
	defer cancel()

	var agentID string
	var isManaged bool
	explicitAgent := strings.TrimSpace(*agentFlag)

	if explicitAgent != "" && explicitAgent != "auto" {
		agentID = explicitAgent
		isManaged = true
	} else if envAgent := strings.TrimSpace(os.Getenv("AGENTBUS_AGENT_ID")); envAgent != "" {
		agentID = envAgent
		isManaged = true
	} else {
		// Auto resolution via TMUX_PANE
		tmuxPane := strings.TrimSpace(os.Getenv("TMUX_PANE"))
		if tmuxPane == "" {
			// Unmanaged agent outside tmux
			return printNeutralHookResponse()
		}

		agents, err := c.ListAgents(ctx)
		if err != nil {
			// Daemon unreachable in unmanaged mode -> neutral pass
			return printNeutralHookResponse()
		}

		var matched []string
		for _, a := range agents {
			sessResp, err := c.GetSession(ctx, a.ID)
			if err != nil {
				continue
			}
			if sessResp.Session.Status == domain.SessionStatusReady && (sessResp.Session.ResolvedPaneID == tmuxPane || a.Address == tmuxPane) {
				matched = append(matched, a.ID)
			}
		}
		if len(matched) != 1 {
			// 0 or >1 matches in auto mode -> unmanaged agent
			return printNeutralHookResponse()
		}
		agentID = matched[0]
		isManaged = true
	}

	if !isManaged {
		return printNeutralHookResponse()
	}

	// 4. Resolve Task for Managed Agent
	var activeTask *domain.Task
	explicitTaskID := strings.TrimSpace(*taskFlag)

	if explicitTaskID != "" {
		taskDetail, err := c.GetTask(ctx, explicitTaskID, agentID)
		if err != nil {
			PrintError(fmt.Errorf("failed to get task '%s': %w", explicitTaskID, err))
			if event == domain.AGYEventStop {
				return printStopFailClosed(explicitTaskID, "")
			}
			return 1
		}
		if taskDetail.Task.TargetAgentID != agentID {
			PrintError(fmt.Errorf("task '%s' target agent '%s' does not match resolved agent '%s'", explicitTaskID, taskDetail.Task.TargetAgentID, agentID))
			if event == domain.AGYEventStop {
				return printStopFailClosed(explicitTaskID, fmt.Sprintf("Task %s target mismatch with agent %s; cannot stop while task target is unverified.", explicitTaskID, agentID))
			}
			return 1
		}
		activeTask = taskDetail.Task
	} else {
		tasks, err := c.ListTasks(ctx, agentID, "")
		if err != nil {
			PrintError(fmt.Errorf("failed to list tasks for agent '%s': %w", agentID, err))
			if event == domain.AGYEventStop {
				return printStopFailClosed("", fmt.Sprintf("Failed to query tasks for agent %s: %v; check daemon status before stopping.", agentID, err))
			}
			return 1
		}
		var targetTasks []*domain.Task
		for _, t := range tasks {
			if t.TargetAgentID == agentID && (t.Status == domain.TaskStatusQueued || t.Status == domain.TaskStatusRunning) {
				targetTasks = append(targetTasks, t)
			}
		}
		if len(targetTasks) == 0 {
			// No active task: neutral response
			return printNeutralHookResponse()
		}
		if len(targetTasks) > 1 {
			PrintError(fmt.Errorf("multiple active tasks (%d) found for target agent '%s'", len(targetTasks), agentID))
			if event == domain.AGYEventStop {
				return printStopFailClosed("", fmt.Sprintf("Multiple active tasks found for agent %s; resolve pending tasks before stopping.", agentID))
			}
			return 1
		}
		activeTask = targetTasks[0]
	}

	// Terminal tasks do not need to be blocked
	if activeTask.IsTerminal() {
		return printNeutralHookResponse()
	}

	// 5. Record Runtime Event
	req := service.RecordRuntimeEventRequest{
		Agent:     agentID,
		Runtime:   "agy",
		Event:     event,
		SessionID: sessionID,
		Payload:   json.RawMessage(trimmedBytes),
	}

	recResp, err := c.RecordRuntimeEvent(ctx, activeTask.ID, req)
	if err != nil {
		PrintError(fmt.Errorf("failed to record runtime event: %w", err))
		if event == domain.AGYEventStop {
			return printStopFailClosed(activeTask.ID, "")
		}
		return 1
	}

	// 6. Stop Gate & Neutral output
	if event == domain.AGYEventStop {
		if recResp.Status == string(domain.TaskStatusQueued) || recResp.Status == string(domain.TaskStatusRunning) {
			return printStopFailClosed(activeTask.ID, fmt.Sprintf("AgentBus task %s is still active; acknowledge it if needed, then explicitly run task complete or task fail before stopping.", activeTask.ID))
		}
		return printNeutralHookResponse()
	}

	return printNeutralHookResponse()
}
