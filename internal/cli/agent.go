package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"agentbus/internal/client"
	"agentbus/internal/domain"
)

func runAgent(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: agentbus agent <register|list|get> [flags]\n")
		return 1
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "register":
		return runAgentRegister(subArgs)
	case "list":
		return runAgentList(subArgs)
	case "get":
		return runAgentGet(subArgs)
	default:
		fmt.Fprintf(os.Stderr, "Unknown agent subcommand: %s\n", sub)
		return 1
	}
}

func runAgentRegister(args []string) int {
	fs := flag.NewFlagSet("agent register", flag.ContinueOnError)
	id := fs.String("id", "", "Agent ID (required)")
	role := fs.String("role", "", "Agent role (required)")
	conn := fs.String("connector", domain.ConnectorTmux, "Connector type ('tmux' or 'none')")
	address := fs.String("address", "", "Connector address (e.g. %50, %51, session:window.pane)")
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	if strings.TrimSpace(*id) == "" {
		PrintError(fmt.Errorf("flag --id is required"))
		return 1
	}
	if strings.TrimSpace(*role) == "" {
		PrintError(fmt.Errorf("flag --role is required"))
		return 1
	}
	if *conn == domain.ConnectorTmux && strings.TrimSpace(*address) == "" {
		PrintError(fmt.Errorf("flag --address is required when connector is 'tmux'"))
		return 1
	}

	c := client.NewClient(*socketPath)
	agent, err := c.RegisterAgent(context.Background(), &domain.Agent{
		ID:        *id,
		Role:      *role,
		Connector: *conn,
		Address:   *address,
	})
	if err != nil {
		PrintError(err)
		return 1
	}

	PrintJSON(agent)
	return 0
}

func runAgentList(args []string) int {
	fs := flag.NewFlagSet("agent list", flag.ContinueOnError)
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	c := client.NewClient(*socketPath)
	agents, err := c.ListAgents(context.Background())
	if err != nil {
		PrintError(err)
		return 1
	}

	PrintJSON(agents)
	return 0
}

func runAgentGet(args []string) int {
	fs := flag.NewFlagSet("agent get", flag.ContinueOnError)
	idFlag := fs.String("id", "", "Agent ID")
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	agentID := *idFlag
	if agentID == "" && len(fs.Args()) > 0 {
		agentID = fs.Args()[0]
	}

	if strings.TrimSpace(agentID) == "" {
		PrintError(fmt.Errorf("agent ID is required (e.g. agentbus agent get <agent-id> or --id <agent-id>)"))
		return 1
	}

	c := client.NewClient(*socketPath)
	agent, err := c.GetAgent(context.Background(), agentID)
	if err != nil {
		PrintError(err)
		return 1
	}

	PrintJSON(agent)
	return 0
}
