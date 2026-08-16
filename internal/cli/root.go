package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func PrintJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func PrintError(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
}

// ReorderArgs moves flags and their values before positional arguments
// so standard Go flag.FlagSet can parse flags regardless of their position.
func ReorderArgs(args []string) []string {
	var flags []string
	var positionals []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			if strings.Contains(arg, "=") || arg == "-h" || arg == "--help" || arg == "-v" || arg == "--version" {
				flags = append(flags, arg)
			} else {
				flags = append(flags, arg)
				if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					flags = append(flags, args[i+1])
					i++
				}
			}
		} else {
			positionals = append(positionals, arg)
		}
	}

	return append(flags, positionals...)
}

func Execute(args []string) int {
	if len(args) == 0 {
		printUsage()
		return 1
	}

	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "serve":
		return runServe(cmdArgs)
	case "agent":
		return runAgent(cmdArgs)
	case "session":
		return runSession(cmdArgs)
	case "task":
		return runTask(cmdArgs)
	case "runtime":
		return runRuntime(cmdArgs)
	case "help", "--help", "-h":
		printUsage()
		return 0
	case "version", "--version", "-v":
		fmt.Println("AgentBus Go V0.2 (1.22)")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printUsage()
		return 1
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `AgentBus - Heterogeneous Agent Runtime Communication & Control Plane (Go V0.2)

Usage:
  agentbus <command> [subcommand] [flags]

Commands:
  serve                 Start the AgentBus daemon server
  agent                 Manage registered agents (whoami, attach, bootstrap, launch, register, list, get)
  session               Manage agent sessions (ready, show)
  task                  Manage tasks (submit, get, list, ack, status, send, complete, fail, cancel, watch, wait)
  runtime               Runtime hooks and integrations (agy-hook)
  help                  Show help
  version               Show version information

Flags:
  各相关子命令均支持 --socket <path> 指定 Unix Domain Socket 路径（默认读取 $AGENTBUS_SOCKET 或解析 AgentBus 根目录下的 run/agentbus.sock）。

Examples:
  agentbus serve --db data/agentbus.db --socket run/agentbus.sock

  # Agent Bootstrap & Session Ready
  agentbus agent whoami --config agents/orchestrator/agent.yaml
  agentbus agent attach --config agents/orchestrator/agent.yaml --no-notify
  agentbus session ready --agent orchestrator --generation 1
  agentbus agent attach --config agents/quote-service/agent.yaml --address %%52
  agentbus session ready --agent quote-service --generation 1
  agentbus session show --agent quote-service
  agentbus agent bootstrap --id quote-service

  # Tasks
  agentbus task submit --from orchestrator --to quote-service --idempotency-key k1 --content "Inspect Quote Service"
  agentbus task get <task-id> --agent quote-service
  agentbus task list --agent quote-service
  agentbus task ack <task-id> --agent quote-service
  agentbus task status <task-id> --agent quote-service --message "Checking routes"
  agentbus task send <task-id> --from orchestrator --content "Also check cache"
  agentbus task complete <task-id> --agent quote-service --result "Inspection done"
  agentbus task fail <task-id> --agent quote-service --error "Route error"
  agentbus task cancel <task-id> --agent orchestrator
  agentbus task watch <task-id> --agent orchestrator --after 0 --timeout 30s
  agentbus task wait <task-id> --agent orchestrator --timeout 30m
`)
}
