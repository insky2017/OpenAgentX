package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"agentbus/internal/client"
)

func runSession(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: agentbus session <ready|show> [flags]\n")
		return 1
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "ready":
		return runSessionReady(subArgs)
	case "show":
		return runSessionShow(subArgs)
	default:
		fmt.Fprintf(os.Stderr, "Unknown session subcommand: %s\n", sub)
		return 1
	}
}

func runSessionReady(args []string) int {
	fs := flag.NewFlagSet("session ready", flag.ContinueOnError)
	agentID := fs.String("agent", "", "Agent ID (required)")
	genStr := fs.String("generation", "", "Session generation (required)")
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	if strings.TrimSpace(*agentID) == "" {
		PrintError(fmt.Errorf("flag --agent is required"))
		return 1
	}
	if strings.TrimSpace(*genStr) == "" {
		PrintError(fmt.Errorf("flag --generation is required"))
		return 1
	}

	gen, err := strconv.ParseInt(*genStr, 10, 64)
	if err != nil || gen <= 0 {
		PrintError(fmt.Errorf("invalid --generation: must be a positive integer"))
		return 1
	}

	c := client.NewClient(*socketPath)
	session, err := c.ReadySession(context.Background(), *agentID, gen)
	if err != nil {
		PrintError(err)
		return 1
	}

	PrintJSON(session)
	return 0
}

func runSessionShow(args []string) int {
	fs := flag.NewFlagSet("session show", flag.ContinueOnError)
	agentID := fs.String("agent", "", "Agent ID (required)")
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	id := *agentID
	if id == "" && len(fs.Args()) > 0 {
		id = fs.Args()[0]
	}

	if strings.TrimSpace(id) == "" {
		PrintError(fmt.Errorf("flag --agent is required (e.g. agentbus session show --agent <id>)"))
		return 1
	}

	c := client.NewClient(*socketPath)
	resp, err := c.GetSession(context.Background(), id)
	if err != nil {
		PrintError(err)
		return 1
	}

	PrintJSON(resp)
	return 0
}
