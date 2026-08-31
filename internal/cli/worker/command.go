package workercli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/google/uuid"
	workerclient "openagentx/internal/client/worker"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	"openagentx/internal/runtime/agy"
	"openagentx/internal/runtime/codebuddy"
	"openagentx/internal/runtime/fake"
	residentworker "openagentx/internal/worker"
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
	configDir := filepath.Dir(configPath)
	for _, backendConfig := range processConfig.RuntimeBackendConfig {
		adapter, err := assembleM1Adapter(backendConfig, configDir)
		if err != nil {
			return err
		}
		backends = append(backends, residentworker.RuntimeBackend{ID: backendConfig.BackendID, Adapter: adapter})
	}
	pool, err := residentworker.NewBackendPool(backends)
	if err != nil {
		return err
	}
	runner, err := residentworker.NewRunner(processConfig.RunnerConfig("worker-"+uuid.NewString()), client, pool, client)
	if err != nil {
		return err
	}
	return runner.Run(ctx)
}

func assembleM1Adapter(config residentworker.RuntimeBackendConfig, configDir string) (openruntime.AgentRuntimeAdapter, error) {
	if config.AdapterID == "agy-batch" {
		agyConfig, err := agyConfigFromOptions(config.Options, configDir)
		if err != nil {
			return nil, err
		}
		adapter, err := agy.NewAdapter(agyConfig)
		if err != nil {
			return nil, err
		}
		return adapter, nil
	}
	if config.AdapterID == "codebuddy-cli" {
		adapterConfig, err := codebuddyConfigFromOptions(config.Options, configDir)
		if err != nil {
			return nil, err
		}
		return codebuddy.NewAdapter(adapterConfig)
	}
	if config.AdapterID != "fake" {
		return nil, fmt.Errorf("Runtime Adapter %q is not assembled in the M1 Worker build", config.AdapterID)
	}
	fakeConfig, err := fakeConfigFromOptions(config.Options)
	if err != nil {
		return nil, err
	}
	descriptor := openruntime.AdapterDescriptor{
		AdapterID: "fake", BackendType: "fake", Version: "1", LaunchProtocol: "inproc",
		Models: []string{fakeConfig.model}, ReasoningModes: []domain.ReasoningMode{domain.ReasoningBackendDefault},
		SessionModes: []domain.SessionMode{domain.SessionModeNew, domain.SessionModeResume}, Steer: openruntime.SteerQueued,
		Approval: openruntime.ApprovalPreflight, Cancel: openruntime.CancelNative,
		Streams: true, BackendOptionsJSON: json.RawMessage(`{"type":"object"}`), MaxConcurrency: 1,
	}
	if len(fakeConfig.statusSequence) != 0 {
		results := make([]openruntime.TurnResult, 0, len(fakeConfig.statusSequence))
		for _, status := range fakeConfig.statusSequence {
			results = append(results, fakeTurnResult(status, fakeConfig))
		}
		return fake.NewScriptedAutoAdapter(descriptor, results)
	}
	return fake.NewAutoAdapter(descriptor, fakeTurnResult(fakeConfig.status, fakeConfig))
}

type fakeAdapterConfig struct {
	model             string
	result            string
	providerSessionID string
	status            openruntime.TurnResultStatus
	statusSequence    []openruntime.TurnResultStatus
}

var fakeAdapterOptionKeys = map[string]struct{}{
	"model": {}, "result": {}, "provider_session_id": {},
	"result_status": {}, "result_status_sequence": {},
}

func fakeConfigFromOptions(options map[string]any) (fakeAdapterConfig, error) {
	config := fakeAdapterConfig{
		model: "fake-model", result: "fake turn completed", status: openruntime.TurnResultSucceeded,
	}
	for key := range options {
		if _, supported := fakeAdapterOptionKeys[key]; !supported {
			return fakeAdapterConfig{}, domain.ErrInvalidInput("unsupported fake Runtime option " + key)
		}
	}
	if value, exists := options["model"]; exists {
		model, ok := value.(string)
		if !ok || strings.TrimSpace(model) == "" {
			return fakeAdapterConfig{}, domain.ErrInvalidInput("fake Runtime option model must be a non-empty string")
		}
		config.model = strings.TrimSpace(model)
	}
	if value, exists := options["result"]; exists {
		result, ok := value.(string)
		if !ok {
			return fakeAdapterConfig{}, domain.ErrInvalidInput("fake Runtime option result must be a string")
		}
		config.result = result
	}
	if value, exists := options["provider_session_id"]; exists {
		providerSessionID, ok := value.(string)
		if !ok {
			return fakeAdapterConfig{}, domain.ErrInvalidInput("fake Runtime option provider_session_id must be a non-empty string")
		}
		providerSessionID = strings.TrimSpace(providerSessionID)
		if err := domain.ValidateOpaqueID("fake Runtime option provider_session_id", providerSessionID); err != nil {
			return fakeAdapterConfig{}, err
		}
		config.providerSessionID = providerSessionID
	}
	status, hasStatus, err := fakeResultStatusOption(options, "result_status")
	if err != nil {
		return fakeAdapterConfig{}, err
	}
	sequence, hasSequence, err := fakeResultStatusSequenceOption(options)
	if err != nil {
		return fakeAdapterConfig{}, err
	}
	if hasStatus && hasSequence {
		return fakeAdapterConfig{}, domain.ErrInvalidInput("fake Runtime options result_status and result_status_sequence are mutually exclusive")
	}
	if hasStatus {
		config.status = status
	}
	if hasSequence {
		config.statusSequence = sequence
	}
	return config, nil
}

func fakeResultStatusOption(options map[string]any, key string) (openruntime.TurnResultStatus, bool, error) {
	value, exists := options[key]
	if !exists {
		return "", false, nil
	}
	statusText, ok := value.(string)
	status := openruntime.TurnResultStatus(strings.TrimSpace(statusText))
	if !ok || !status.Valid() {
		return "", false, domain.ErrInvalidInput("fake Runtime option " + key + " must be a supported non-empty turn result status")
	}
	return status, true, nil
}

func fakeResultStatusSequenceOption(options map[string]any) ([]openruntime.TurnResultStatus, bool, error) {
	value, exists := options["result_status_sequence"]
	if !exists {
		return nil, false, nil
	}
	entries, ok := value.([]any)
	if !ok || len(entries) == 0 {
		return nil, false, domain.ErrInvalidInput("fake Runtime option result_status_sequence must be a non-empty list of supported turn result statuses")
	}
	sequence := make([]openruntime.TurnResultStatus, 0, len(entries))
	for _, entry := range entries {
		statusText, isString := entry.(string)
		status := openruntime.TurnResultStatus(strings.TrimSpace(statusText))
		if !isString || !status.Valid() {
			return nil, false, domain.ErrInvalidInput("fake Runtime option result_status_sequence must be a non-empty list of supported turn result statuses")
		}
		sequence = append(sequence, status)
	}
	return sequence, true, nil
}

func fakeTurnResult(status openruntime.TurnResultStatus, config fakeAdapterConfig) openruntime.TurnResult {
	return openruntime.TurnResult{
		Status: status, ProviderSessionID: config.providerSessionID, Result: config.result,
		UsageJSON: json.RawMessage(`{"fixture":true}`), SideEffectsKnown: true,
	}
}

func agyConfigFromOptions(options map[string]any, configDir string) (agy.Config, error) {
	var config agy.Config
	for key := range options {
		if key != "binary" && key != "models" && key != "working_dir" {
			return config, domain.ErrInvalidInput("unsupported AGY option " + key)
		}
	}
	if value, exists := options["binary"]; exists {
		binary, ok := value.(string)
		if !ok || strings.TrimSpace(binary) == "" {
			return config, domain.ErrInvalidInput("AGY option binary must be a non-empty string")
		}
		config.Binary = strings.TrimSpace(binary)
	}
	if value, exists := options["working_dir"]; exists {
		workingDir, ok := value.(string)
		workingDir = strings.TrimSpace(workingDir)
		if !ok || workingDir == "" {
			return config, domain.ErrInvalidInput("AGY option working_dir must be a non-empty string")
		}
		if !filepath.IsAbs(workingDir) && configDir != "" {
			workingDir = filepath.Join(configDir, workingDir)
		}
		workingDir = filepath.Clean(workingDir)
		if info, statErr := os.Stat(workingDir); statErr != nil || !info.IsDir() {
			return config, domain.ErrInvalidInput("AGY working_dir must be an existing directory")
		}
		config.WorkingDir = workingDir
	}
	if value, exists := options["models"]; exists {
		entries, ok := value.([]any)
		if !ok || len(entries) == 0 {
			return config, domain.ErrInvalidInput("AGY option models must be a non-empty list of strings")
		}
		seen := make(map[string]struct{}, len(entries))
		for _, entry := range entries {
			model, ok := entry.(string)
			model = strings.TrimSpace(model)
			if !ok || model == "" {
				return config, domain.ErrInvalidInput("AGY option models must be a non-empty list of strings")
			}
			if _, duplicate := seen[model]; duplicate {
				return config, domain.ErrInvalidInput("AGY option models must not contain duplicates")
			}
			seen[model] = struct{}{}
			config.Models = append(config.Models, model)
		}
	}
	return config, nil
}

func printOpenAgentXUsage() {
	fmt.Fprintln(os.Stderr, `OpenAgentX - Agent Organization Control Plane

Usage:
  openagentx worker run --config <agent.yaml>`)
}

// defaultCodeBuddyModel is the explicit fallback model when a Worker config
// does not declare model or models for the codebuddy-cli Runtime Adapter.
// CodeBuddy 任务只允许使用免费模型，因此默认固定为 hy4-preview。
const defaultCodeBuddyModel = "hy4-preview"

var codeBuddyOptionKeys = map[string]struct{}{
	"binary": {}, "working_dir": {}, "model": {}, "models": {},
	"effort": {}, "permission_mode": {}, "max_turns": {},
}

// codebuddyConfigFromOptions translates Worker YAML options into a CodeBuddy
// Adapter Config. Relative working_dir values are resolved against the
// directory of the Worker config file; unset options fall back to the safe
// defaults enforced by codebuddy.NewAdapter.
func codebuddyConfigFromOptions(options map[string]any, configDir string) (codebuddy.Config, error) {
	var config codebuddy.Config
	for key := range options {
		if _, supported := codeBuddyOptionKeys[key]; !supported {
			return config, domain.ErrInvalidInput("unsupported CodeBuddy option " + key)
		}
	}
	binary, err := codebuddyStringOption(options, "binary")
	if err != nil {
		return config, err
	}
	config.Binary = binary
	workingDir, err := codebuddyStringOption(options, "working_dir")
	if err != nil {
		return config, err
	}
	if workingDir != "" {
		if !filepath.IsAbs(workingDir) && configDir != "" {
			workingDir = filepath.Join(configDir, workingDir)
		}
		if info, statErr := os.Stat(workingDir); statErr != nil || !info.IsDir() {
			return config, domain.ErrInvalidInput("CodeBuddy working_dir must be an existing directory")
		}
		config.WorkingDir = workingDir
	}
	models, err := codebuddyModelsOption(options)
	if err != nil {
		return config, err
	}
	config.Models = models
	effort, err := codebuddyStringOption(options, "effort")
	if err != nil {
		return config, err
	}
	config.DefaultEffort = effort
	permissionMode, err := codebuddyStringOption(options, "permission_mode")
	if err != nil {
		return config, err
	}
	config.PermissionMode = permissionMode
	maxTurns, err := codebuddyMaxTurnsOption(options)
	if err != nil {
		return config, err
	}
	config.MaxTurns = maxTurns
	return config, nil
}

func codebuddyStringOption(options map[string]any, key string) (string, error) {
	value, exists := options[key]
	if !exists || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", domain.ErrInvalidInput("CodeBuddy option " + key + " must be a string")
	}
	if strings.TrimSpace(text) == "" {
		return "", domain.ErrInvalidInput("CodeBuddy option " + key + " cannot be blank")
	}
	return strings.TrimSpace(text), nil
}

func codebuddyModelsOption(options map[string]any) ([]string, error) {
	single, err := codebuddyStringOption(options, "model")
	if err != nil {
		return nil, err
	}
	if list, exists := options["models"]; exists && list != nil {
		if single != "" {
			return nil, domain.ErrInvalidInput("CodeBuddy options model and models are mutually exclusive")
		}
		entries, ok := list.([]any)
		if !ok || len(entries) == 0 {
			return nil, domain.ErrInvalidInput("CodeBuddy option models must be a non-empty list of strings")
		}
		models := make([]string, 0, len(entries))
		for _, entry := range entries {
			text, isString := entry.(string)
			if !isString || strings.TrimSpace(text) == "" {
				return nil, domain.ErrInvalidInput("CodeBuddy option models must be a non-empty list of strings")
			}
			models = append(models, strings.TrimSpace(text))
		}
		return models, nil
	}
	if single != "" {
		return []string{single}, nil
	}
	return []string{defaultCodeBuddyModel}, nil
}

func codebuddyMaxTurnsOption(options map[string]any) (int, error) {
	value, exists := options["max_turns"]
	if !exists || value == nil {
		return 0, nil
	}
	turns, ok := value.(int)
	if !ok || turns <= 0 {
		return 0, domain.ErrInvalidInput("CodeBuddy option max_turns must be a positive integer")
	}
	return turns, nil
}
