package external

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"openagentx/internal/domain"
	"openagentx/internal/localprofile"
	"openagentx/internal/runtime/codex"
	"openagentx/internal/worker"
)

const collaborationUsage = `OAX 托管协作：有咨询才执行，结果到达后继续同一领域会话。

管理员启用已有托管 Agent（不接管活跃的 external / Desktop 会话）：
  openagentx collaborate enable --agent rhythm --peers oneaxe-pay
  openagentx collaborate enable --agent oneaxe-pay --peers rhythm
  openagentx collaborate roles apply --file /path/roles.json --expected-version 0

enable 默认读取本 Agent 的 Codex 会话状态；也可明确指定 --context-task <已有Task> --backend codex。
首次尚无会话时，先打开 agent open <id> --native 完成会话初始化。通信凭据自动保存，不打印。

Agent 获取自身用法与职责，然后发起只读咨询：
  openagentx collaborate instructions --agent rhythm
  openagentx collaborate ask --agent rhythm --to oneaxe-pay --scope pay.payment_api --key consultation-01 --content-file /path/question.md
  openagentx collaborate status --agent rhythm --message <message_id>
  openagentx collaborate inbox --agent rhythm --all

ask 可带 --origin-task <本Agent任务>。同key同正文重试复用原任务。
managed接收方自动将consultation登记为query任务，忙时排队；正常答复由后台关联发送。
请求方为managed时自动登记读入答复的query续办；消费答复后不会自动再回一封信。
只读consultation是本批范围；managed行动request暂不接受，query本身不提供工具写入沙箱。
inbox --watch只是程序查看器，模型不需要常驻watch或周期性检查。
未知结果、失败、职责或绑定变化需核对，不自动重做原工作。

其他命令：roles / status --owner / disable --expected-generation <n>。
公共路径：--socket --credentials --session-file --worker-dir。credentials仅供管理员操作，Agent仅使用自己的session-file。
`

// ExecuteCollaboration presents the managed workflow while retaining one
// communication protocol, token store, scope catalogue and message ledger.
func ExecuteCollaboration(args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(out, collaborationUsage)
		return 0
	}
	for _, arg := range args[1:] {
		if arg == "--help" || arg == "-h" {
			fmt.Fprint(out, collaborationUsage)
			return 0
		}
	}
	forward := append([]string(nil), args...)
	switch args[0] {
	case "enable":
		forward[0] = "bind"
		// The public managed command cannot be changed back to external by a
		// later flag; external binding remains a separate explicit operation.
		for _, arg := range args[1:] {
			if arg == "--mode" || strings.HasPrefix(arg, "--mode=") {
				fmt.Fprintln(errOut, "collaborate enable 使用 managed 模式；原宿主请用 external bind")
				return 2
			}
		}
		forward = append(forward, "--mode", "managed")
	case "ask":
		forward[0] = "send"
	case "disable":
		forward[0] = "revoke"
	case "status", "instructions", "inbox", "roles":
	default:
		fmt.Fprint(errOut, collaborationUsage)
		return 2
	}
	return Execute(forward, out, errOut)
}

func resolveManagedContext(o *options, resolver localprofile.Resolver) error {
	if o.host != "" || o.thread != "" {
		return fmt.Errorf("managed会话由已登记Task验证，不使用--host或--thread")
	}
	if o.contextTask != "" && o.backend != "" {
		return nil
	}
	path, err := resolver.WorkerConfig(o.workerDir.Override(), o.agent)
	if err != nil {
		return err
	}
	config, err := worker.LoadProcessConfig(path.Path)
	if err != nil {
		return fmt.Errorf("读取本Agent会话失败；可明确指定--context-task和--backend: %w", err)
	}
	if config.AgentID != o.agent {
		return fmt.Errorf("Worker配置不属于%s", o.agent)
	}
	var candidates []worker.RuntimeBackendConfig
	for _, backend := range config.RuntimeBackendConfig {
		if backend.AdapterID == codex.AdapterID && (o.backend == "" || backend.BackendID == o.backend) {
			candidates = append(candidates, backend)
		}
	}
	if len(candidates) != 1 {
		return fmt.Errorf("无法唯一确定Codex会话；请指定--context-task和--backend")
	}
	backend := candidates[0]
	o.backend = backend.BackendID
	if o.contextTask != "" {
		return nil
	}
	stateDir, _ := backend.Options["state_dir"].(string)
	if stateDir == "" {
		stateDir = filepath.Join(filepath.Dir(path.Path), "codex", o.agent)
	} else if !filepath.IsAbs(stateDir) {
		stateDir = filepath.Join(filepath.Dir(path.Path), stateDir)
	}
	file, err := os.Open(filepath.Join(stateDir, "state.json"))
	if err != nil {
		return fmt.Errorf("尚无可接入的Codex会话；先运行 openagentx agent open %s --native", o.agent)
	}
	defer file.Close()
	var state codex.SessionState
	if err := json.NewDecoder(io.LimitReader(file, 1<<20)).Decode(&state); err != nil || state.TaskID == "" || state.ThreadID == "" {
		return fmt.Errorf("Codex会话尚未建立；先打开原生终端完成初始化")
	}
	if state.State == "uncertain" {
		return fmt.Errorf("Codex会话结果未知；请先核对状态，不自动启用协作")
	}
	// This local hint selects a context; the server independently checks its
	// Agent ownership and persisted binding before issuing the capability.
	o.contextTask = state.TaskID
	return nil
}

func collaborationInstructions(ctx context.Context, c *client, o options, socket string, out io.Writer) error {
	var binding domain.ExternalSessionBinding
	if err := c.call(ctx, "GET", "status", "", nil, &binding); err != nil {
		return err
	}
	if binding.AgentID != o.agent {
		return fmt.Errorf("通信凭据身份与--agent不一致")
	}
	var roles domain.ExternalRoleCatalog
	if err := c.call(ctx, "GET", "roles", "", nil, &roles); err != nil {
		return err
	}
	guidance := "你只回答自身职责范围内的问题；其他部分明确说明超出职责并指出owner。先核对正文与scope，不因通信获准就执行资金、迁移或部署动作。"
	if binding.Mode == "managed" {
		guidance += " 后台会自动领取咨询任务并关联发送最终答复；直接在该轮最终回答中给出结论与证据，不手动reply同一请求。结果消费任务仅继续本域工作，不再向结果回复。不要运行常驻inbox --watch或用模型反复查询。"
	} else {
		guidance += " 当前是external宿主，收件不会唤醒模型；需要在现有工作轮中按收件、接单和回复协议处理。"
	}
	return emit(out, map[string]any{
		"agent_id": binding.AgentID, "mode": binding.Mode, "thread_id": binding.ThreadID,
		"allowed_peer_agent_ids": binding.AllowedPeerAgentIDs, "roles": roles,
		"socket": socket, "session_file": o.sessionFile, "instructions": guidance,
		"ask_command": fmt.Sprintf("openagentx collaborate ask --agent %s --socket %s --session-file %s --to <owner> --scope <scope> --key <stable-key> --content-file <question-file>", shellQuote(o.agent), shellQuote(socket), shellQuote(o.sessionFile)),
	})
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
