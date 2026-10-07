package fleet

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	admincli "openagentx/internal/cli/admin"
	"openagentx/internal/domain"
	fleetmodel "openagentx/internal/fleet"
	workerconfig "openagentx/internal/worker"
)

func agentJoinUsage(w io.Writer) {
	fmt.Fprintln(w, `
自初始化（当前 CLI 可直接执行，无需人工粘贴终端输入）：
  先确认用户授权的职责和工作目录；只记录已知的 thread ID，不猜测 live 绑定。
  openagentx agent join --id research --name Research --workspace "$PWD" --role ./ROLE.md --handoff-file ./HANDOFF.md --prepare
  openagentx agent join --id research --name Research --workspace "$PWD" --role ./ROLE.md --thread-id <known-thread-id> --endpoint <managed-endpoint> --prepare
  当前轮结束后，由用户运行 openagentx agent resume research。

join 默认仅离线准备（--prepare=true）：记录本地身份、角色、目录、交接、私密环境和 receipt。
不启动 Worker 或服务，不领取任务，不宣称原 CLI 已绑定或 ready；重复相同输入幂等，冲突拒绝。
不要让当前 CLI 调用 resume 来接管自己；也不要通过模型反复 poll 维持监听。
角色由 Runtime adapter 注入；宿主负责总线监听。原生入口：openagentx agent open research --native。
--prepare=false 只适用于原 CLI 已结束且明确授权注册/启动的场景。`)
}

func agentRuntimeConfig(o agentOptions) ([]byte, error) {
	options := map[string]any{"binary": o.binary, "working_dir": o.workspace, "timeout": o.timeout.String()}
	if o.model != "" {
		options["models"] = []string{o.model}
	}
	backend, adapter := "agy", "agy-batch"
	if o.runtime == "codex" {
		backend, adapter = "codex", "codex-app-server"
		options["state_dir"] = filepath.Join(o.paths.workerDir, "codex", o.id)
		if o.threadID != "" {
			options["thread_id"] = o.threadID
		}
		if o.endpoint != "" {
			options["endpoint"] = o.endpoint
		}
		if o.handoffFile != "" {
			options["handoff_file"] = o.handoffFile
		}
	}
	return yaml.Marshal(workerconfig.ProcessConfig{Version: 1, AgentID: o.id, Transport: domain.WorkerTransportUnix, UnixSocket: o.paths.socket, RuntimeBackendConfig: []workerconfig.RuntimeBackendConfig{{BackendID: backend, AdapterID: adapter, Options: options, Network: domain.NetworkPolicy{Mode: domain.NetworkInherit}}}})
}

type joinReceipt struct {
	Version      int    `json:"version"`
	State        string `json:"state"`
	Ready        bool   `json:"ready"`
	AgentID      string `json:"agent_id"`
	Runtime      string `json:"runtime"`
	IdentityFile string `json:"identity_file"`
	Workspace    string `json:"workspace"`
	Role         string `json:"role_file"`
	ThreadID     string `json:"thread_id,omitempty"`
	Endpoint     string `json:"endpoint,omitempty"`
	HandoffFile  string `json:"handoff_file,omitempty"`
}

func joinReceiptPath(o agentOptions) string {
	return filepath.Join(o.paths.workerDir, "joins", o.id+".json")
}

func prepareAgentJoin(o *agentOptions, deps Dependencies) error {
	if o.workspace == "" && o.identity == "" {
		var err error
		o.workspace, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	if o.identity != "" {
		d, err := admincli.LoadAgentDefinition(o.identity)
		if err != nil {
			return err
		}
		if o.id != "" && o.id != d.AgentID {
			return fmt.Errorf("--id conflicts with imported identity")
		}
		o.id = d.AgentID
	}
	if o.id == "" {
		o.id = o.name
	}
	if err := domain.ValidateIdentifier("agent_id", o.id); err != nil {
		return fmt.Errorf("join requires an explicit valid --id: %w", err)
	}
	if err := validateAgentJoinTarget(*o); err != nil {
		return err
	}
	if o.prepareOnly {
		if _, err := os.Lstat(joinReceiptPath(*o) + ".registered"); err == nil {
			return fmt.Errorf("Agent handoff is already registered; use status/resume instead of preparing it again")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		manifest, err := fleetmodel.LoadFile(o.paths.manifest)
		if err == nil {
			for _, a := range manifest.Agents {
				if a.AgentID == o.id && a.Enabled {
					return fmt.Errorf("Agent is already enabled; pause and inspect before preparing a new handoff")
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if o.role != "" && o.identity == "" {
		source, err := filepath.Abs(o.role)
		if err != nil {
			return err
		}
		data, err := fleetmodel.ReadSecureFile(source, fleetmodel.SecureFileOptions{MaximumBytes: 1 << 20})
		if err != nil {
			return err
		}
		target := filepath.Join(o.paths.workerDir, "identities", o.id+".role.md")
		if _, err = fleetmodel.WriteExactFileAtomic(target, data, fleetmodel.AtomicFileOptions{}); err != nil {
			return err
		}
		o.role = target
	}
	if o.handoffFile != "" {
		source, err := filepath.Abs(o.handoffFile)
		if err != nil {
			return err
		}
		data, err := fleetmodel.ReadSecureFile(source, fleetmodel.SecureFileOptions{MaximumBytes: 1 << 20})
		if err != nil {
			return err
		}
		target := filepath.Join(o.paths.workerDir, "joins", o.id+".handoff.md")
		if _, err = fleetmodel.WriteExactFileAtomic(target, data, fleetmodel.AtomicFileOptions{}); err != nil {
			return err
		}
		o.handoffFile = target
	}
	return nil
}

func writeJoinReceipt(o agentOptions) error {
	data, err := json.MarshalIndent(joinReceipt{Version: 1, State: "local_prepared", Ready: false, AgentID: o.id, Runtime: o.runtime, IdentityFile: o.identity, Workspace: o.workspace, Role: o.role, ThreadID: o.threadID, Endpoint: o.endpoint, HandoffFile: o.handoffFile}, "", "  ")
	if err != nil {
		return err
	}
	_, err = fleetmodel.WriteExactFileAtomic(joinReceiptPath(o), append(data, '\n'), fleetmodel.AtomicFileOptions{})
	return err
}

func registerPreparedAgent(ctx context.Context, o *agentOptions, deps Dependencies) error {
	if err := domain.ValidateIdentifier("agent_id", o.id); err != nil {
		return err
	}
	registered, err := verifyRegisteredResume(ctx, *o, deps)
	if err != nil {
		return err
	}
	if registered {
		o.registeredResume = true
		return nil
	}
	data, err := fleetmodel.ReadSecureFile(joinReceiptPath(*o), fleetmodel.SecureFileOptions{MaximumBytes: 1 << 20, RequirePrivate: true})
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var receipt joinReceipt
	if err = json.Unmarshal(data, &receipt); err != nil {
		return err
	}
	if receipt.Version != 1 || receipt.AgentID != o.id {
		return fmt.Errorf("invalid join receipt")
	}
	// A local registration marker is written only after admin apply checked the
	// full identity. Merely finding the same ID in the selector is insufficient.
	identity, err := fleetmodel.ReadSecureFile(receipt.IdentityFile, fleetmodel.SecureFileOptions{MaximumBytes: 1 << 20, RequirePrivate: true})
	if err != nil {
		return err
	}
	worker, err := readWorkerSource(filepath.Join(o.paths.workerDir, o.id+".yaml"))
	if err != nil {
		return err
	}
	role, err := fleetmodel.ReadSecureFile(receipt.Role, fleetmodel.SecureFileOptions{MaximumBytes: 1 << 20})
	if err != nil {
		return err
	}
	digest := sha256.New()
	digest.Write(data)
	digest.Write([]byte{0})
	digest.Write(identity)
	digest.Write([]byte{0})
	digest.Write(worker)
	digest.Write([]byte{0})
	digest.Write(role)
	marker := []byte(fmt.Sprintf("%x\n", digest.Sum(nil)))
	markerPath := joinReceiptPath(*o) + ".registered"
	if saved, e := fleetmodel.ReadSecureFile(markerPath, fleetmodel.SecureFileOptions{MaximumBytes: 256, RequirePrivate: true}); e == nil {
		if string(saved) != string(marker) {
			return fmt.Errorf("registered handoff inputs changed; inspect identity/configuration before resume")
		}
		return nil
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}

	registration := *o
	registration.identity = receipt.IdentityFile
	registration.runtime = receipt.Runtime
	registration.workerSource = filepath.Join(o.paths.workerDir, o.id+".yaml")
	registration.prepareOnly = false
	registration.configureOnly = true
	if err = addAgent(ctx, &registration, deps); err != nil {
		return err
	}
	_, err = fleetmodel.WriteExactFileAtomic(markerPath, marker, fleetmodel.AtomicFileOptions{})
	return err
}

func validateAgentJoinTarget(o agentOptions) error {
	if o.endpoint != "" {
		u, err := url.Parse(o.endpoint)
		if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("endpoint must not contain credentials, query or fragment")
		}
		if u.Scheme != "ws" && u.Scheme != "unix" {
			return fmt.Errorf("endpoint must use ws or unix")
		}
		if u.Scheme == "ws" && u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1" {
			return fmt.Errorf("endpoint must be local")
		}
		if u.Scheme == "unix" && u.Path == "" {
			return fmt.Errorf("unix endpoint requires a path")
		}
	}
	if strings.ContainsAny(o.threadID, "\r\n\x00") {
		return fmt.Errorf("thread-id must be a single line")
	}
	return nil
}

// The receipt is a local preparation fact, not live Worker status.
func printPreparedAgentStatus(o agentOptions, deps Dependencies) (bool, error) {
	if err := domain.ValidateIdentifier("agent_id", o.id); err != nil {
		return false, err
	}
	if _, err := os.Lstat(joinReceiptPath(o) + ".registered"); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	data, err := fleetmodel.ReadSecureFile(joinReceiptPath(o), fleetmodel.SecureFileOptions{MaximumBytes: 1 << 20, RequirePrivate: true})
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var receipt joinReceipt
	if err = json.Unmarshal(data, &receipt); err != nil {
		return false, err
	}
	if receipt.Version != 1 || receipt.AgentID != o.id {
		return false, fmt.Errorf("invalid local join receipt")
	}
	if o.jsonOutput {
		return true, writePreparedStatusJSON(receipt, deps)
	}
	fmt.Fprintf(deps.Out, "%s · 本地交接已准备，尚未通过此入口激活；运行态未核实。\n当前轮结束后运行 openagentx agent resume %s。\n", o.id, o.id)
	return true, nil
}
func writePreparedStatusJSON(receipt joinReceipt, deps Dependencies) error {
	return json.NewEncoder(deps.Out).Encode(struct {
		joinReceipt
		RuntimeState string `json:"runtime_state"`
	}{receipt, "unverified"})
}
