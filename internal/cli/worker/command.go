package workercli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	workerclient "agentbus/internal/client/worker"
	"agentbus/internal/domain"
	openruntime "agentbus/internal/runtime"
	"agentbus/internal/runtime/agy"
	"agentbus/internal/runtime/fake"
	residentworker "agentbus/internal/worker"
	"github.com/google/uuid"
)

type WorkerRunFunc func(context.Context, string) error

func ExecuteOpenAgentX(args []string, runWorker WorkerRunFunc) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printOpenAgentXUsage()
		return 0
	}
	if args[0] != "worker" || len(args) < 2 || args[1] != "run" {
		fmt.Fprintln(os.Stderr, "Usage: openagentx worker run --config <agent.yaml>")
		return 1
	}
	flags := flag.NewFlagSet("worker run", flag.ContinueOnError)
	configPath := flags.String("config", "", "Path to target Worker agent.yaml")
	if err := flags.Parse(args[2:]); err != nil {
		return 1
	}
	if strings.TrimSpace(*configPath) == "" {
		fmt.Fprintln(os.Stderr, "Error: flag --config is required")
		return 1
	}
	if runWorker == nil {
		fmt.Fprintln(os.Stderr, "Error: no Agent Runtime Adapter is assembled in this build")
		return 1
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := runWorker(ctx, *configPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: Worker stopped abnormally: %v\n", err)
		return 1
	}
	return 0
}

func RunWorkerProcess(ctx context.Context, configPath string) error {
	processConfig, err := residentworker.LoadProcessConfig(configPath)
	if err != nil {
		return err
	}
	client, err := workerclient.NewUnixHTTPWorkerClient(processConfig.UnixSocket)
	if err != nil {
		return err
	}
	defer client.Close()
	backends := make([]residentworker.RuntimeBackend, 0, len(processConfig.RuntimeBackendConfig))
	for _, backendConfig := range processConfig.RuntimeBackendConfig {
		adapter, err := assembleM1Adapter(backendConfig)
		if err != nil {
			return err
		}
		backends = append(backends, residentworker.RuntimeBackend{ID: backendConfig.BackendID, Adapter: adapter})
	}
	pool, err := residentworker.NewBackendPool(backends)
	if err != nil {
		return err
	}
	runner, err := residentworker.NewRunner(processConfig.RunnerConfig("worker-"+uuid.NewString()), client, pool, nil)
	if err != nil {
		return err
	}
	return runner.Run(ctx)
}

func assembleM1Adapter(config residentworker.RuntimeBackendConfig) (openruntime.AgentRuntimeAdapter, error) {
	if config.AdapterID == "agy-batch" {
		binary, _ := config.Options["binary"].(string)
		adapter, err := agy.NewAdapter(agy.Config{Binary: binary})
		if err != nil {
			return nil, err
		}
		return adapter, nil
	}
	if config.AdapterID != "fake" {
		return nil, fmt.Errorf("Runtime Adapter %q is not assembled in the M1 Worker build", config.AdapterID)
	}
	model := "fake-model"
	resultText := "fake turn completed"
	if configured, ok := config.Options["model"].(string); ok && strings.TrimSpace(configured) != "" {
		model = configured
	}
	if configured, ok := config.Options["result"].(string); ok {
		resultText = configured
	}
	descriptor := openruntime.AdapterDescriptor{
		AdapterID: "fake", BackendType: "fake", Version: "1", LaunchProtocol: "inproc",
		Models: []string{model}, ReasoningModes: []domain.ReasoningMode{domain.ReasoningBackendDefault},
		SessionModes: []domain.SessionMode{domain.SessionModeNew}, Steer: openruntime.SteerQueued,
		Approval: openruntime.ApprovalPreflight, Cancel: openruntime.CancelNative,
		Streams: true, BackendOptionsJSON: json.RawMessage(`{"type":"object"}`), MaxConcurrency: 1,
	}
	return fake.NewAutoAdapter(descriptor, openruntime.TurnResult{
		Status: openruntime.TurnResultSucceeded, Result: resultText,
		UsageJSON: json.RawMessage(`{"fixture":true}`), SideEffectsKnown: true,
	})
}

func printOpenAgentXUsage() {
	fmt.Fprintln(os.Stderr, `OpenAgentX - Agent Organization Control Plane

Usage:
  openagentx worker run --config <agent.yaml>`)
}
