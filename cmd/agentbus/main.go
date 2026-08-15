package main

import (
	"os"

	"agentbus/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:]))
}
