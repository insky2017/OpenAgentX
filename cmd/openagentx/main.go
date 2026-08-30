package main

import (
	"os"

	workercli "openagentx/internal/cli/worker"
)

func main() {
	os.Exit(workercli.ExecuteOpenAgentX(os.Args[1:], workercli.RunWorkerProcess))
}
