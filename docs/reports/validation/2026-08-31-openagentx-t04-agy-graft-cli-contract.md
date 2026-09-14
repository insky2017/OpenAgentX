# T04 AGY Graft CLI 契约只读核对报告

- 日期：2026-08-31
- 角色：OpenAgentX `verification-runner` Domain Agent
- 模型：`hy4-preview`
- OpenAgentX Task ID：`task-9e9a822c-1585-464c-8cc0-b843d0c6c8c2`
- 性质：**只读核对**。未运行测试、未提交、未重启服务、未发起真实模型调用、未读取或输出任何密码/代理凭据/Token/Cookie/私钥。
- 允许产物：仅本文件。

## 0. 结论

**`CLI_CONTRACT_MISMATCH`**

Adapter（`OpenAgentX/internal/runtime/agy/adapter.go`）构造的命令行/输入/输出协议与本机 `agy-graft`（AGY `1.1.22`）**不一致**。差异分两类：

1. **已由 help 原文证实的契约缺口**（P0，是两次约 0.2 秒即 `uncertain` 的最可能直接原因）：Adapter 未传 `--input-format`，而 help 只在 `--input-format stream-json` 下承诺从 stdin 读取；且 Adapter 写入 stdin 的是纯文本而非 help 要求的 NDJSON。
2. **声明了但 CLI 上无对应参数 / 未接线**（P1）：`--model` 从不传递；`ReasoningBudgetTokens` 在 help 中无任何对应参数；`ApprovalPreflight` 声明但未传 `--dangerously-skip-permissions` 且 `DecideApproval` 返回 unsupported。

## 1. 核对对象

| 文件 | 说明 |
| --- | --- |
| `/home/sky/tools/bin/agy-graft` | bash 包装脚本。以 `exec mgraftcp "${proxy_flag[@]}" --select_proxy_mode "$SELECT_PROXY_MODE" "$REAL_AGY" "$@"` 结尾（`agy-graft:178-181`），**对参数只做透传，不改写 argv**；但会 `export GOMAXPROCS=1` 并 `unset` `http_proxy/https_proxy/all_proxy/HTTP_PROXY/HTTPS_PROXY/ALL_PROXY`（`agy-graft:166-176`）。因此 `agy-graft --help` / `--version` 的输出即为真实 AGY `1.1.22` 的契约。 |
| `OpenAgentX/internal/runtime/agy/adapter.go` | AGY Runtime Adapter，构造 argv 与 stdin。 |
| `OpenAgentX/internal/runtime/agy/stream.go` | stream-json 解析与终态归类。 |

## 2. 两条只读命令的实测证据

### 命令 1：`agy-graft --help`

```bash
/usr/bin/zsh -c 'source /home/sky/.zshrc >/dev/null 2>&1; set_proxy_server >/dev/null; agy-graft --help'
```

- 退出码：**0**
- **stdout：空**
- **stderr：完整 usage**（`Usage of agy:` + 参数列表 + `Available subcommands:`）

**关键实测事实：AGY 的用法/诊断信息写在 stderr，且退出码为 0。**

### 命令 2：`agy-graft --version`

```bash
/usr/bin/zsh -c 'source /home/sky/.zshrc >/dev/null 2>&1; set_proxy_server >/dev/null; agy-graft --version'
```

- 退出码：**0**
- stdout：`1.1.22`
- stderr：空

判定：`Adapter.Health`（`adapter.go:86`）使用 `--version` + `CombinedOutput()`，该路径**契约成立**，可解释「Backend health online」而真实 turn 却立刻失败的现象。

### help 原文摘录（逐字，仅列与 Adapter 相关的参数）

```
--input-format   Input format for print mode (text, stream-json). stream-json reads one
                 NDJSON message per line from stdin and runs a turn for each; it requires
                 --output-format stream-json (default text)
--output-format  Output format for print mode (text, json, stream-json) (default text)
-p               Short alias for --print
--print          Run a single prompt non-interactively and print the response
--prompt         Alias for --print
--model          Model for the current CLI session
--effort         Reasoning effort for the current CLI session (low|medium|high)
--conversation   Resume a previous conversation by ID
--dangerously-skip-permissions
                 Auto-approve all tool permission requests without prompting
--print-timeout  Timeout for print mode wait (default 5m0s)
--sandbox        Run in a sandbox with terminal restrictions enabled
--mode           Set the agent execution mode for this session (accept-edits, plan)
-c / --continue  Continue the most recent conversation
```

> help 全文**未出现任何位置参数（positional `<prompt>`）**的说明，只有 flag 与子命令。

## 3. 逐项契约比较表

| # | 核对项 | Adapter 实际行为（代码位置） | AGY `1.1.22` help 原文/实测 | 判定 |
| --- | --- | --- | --- | --- |
| 1 | `--print` | `args = []string{"--print", "--output-format", "stream-json"}`（`adapter.go:99`） | 存在 `--print`（`Run a single prompt non-interactively and print the response`），别名 `-p`、`--prompt` | **MATCH**（参数存在且语义一致） |
| 2 | `--output-format stream-json` | 同上（`adapter.go:99`） | `--output-format` 取值为 `text, json, stream-json`，`stream-json` 合法 | **MATCH**（取值合法） |
| 3 | **prompt 通过 stdin 传入** | `command.Stdin = strings.NewReader(prompt)`，prompt 为 `Task.Content` + `"\n\nFollow-up:\n"` + 各 message + `"\n"` 的**纯文本**（`adapter.go:106-107`、`adapter.go:125-136`） | help **仅**在 `--input-format stream-json` 项下写明 `reads one NDJSON message per line from stdin`；Adapter **未传 `--input-format`** → 默认 `text`，而 help 对 `text` 输入格式**未作任何 stdin 承诺**，且 help 中无任何位置参数可承载 prompt | **MISMATCH（P0）** |
| 4 | stdin 数据格式 | 纯文本（上） | 若走 `stream-json`，stdin 必须是 **NDJSON：每行一个 JSON 消息** | **MISMATCH（P0）**：即便补上 `--input-format stream-json`，当前 `buildPrompt` 输出的纯文本仍不是 NDJSON |
| 5 | `--model` | **从不传递**（`adapter.go:99-102` 只有 3~5 个参数）；`Descriptor` 声明 `Models: []string{"default"}`（`adapter.go:61`） | 存在 `--model`（`Model for the current CLI session`） | **MISMATCH（P1）**：模型选择完全未接线；`default` 是否为合法 model id 本次未验证（help 未列候选值，需 `agy models` 子命令，超出本次允许的两条命令） |
| 6 | reasoning | `Descriptor` 声明 `ReasoningBackendDefault / ReasoningEffort / ReasoningBudgetTokens`（`adapter.go:62`），但 `StartTurn` **不传任何 reasoning 参数** | CLI 只有 `--effort (low\|medium\|high)`；help 中**无任何 budget / token / thinking 相关参数** | **MISMATCH（P1）**：`ReasoningBudgetTokens` 在 CLI 上无落点（能力虚标）；`ReasoningEffort` 有 `--effort` 可映射但未实现 |
| 7 | session 参数 | resume 时追加 `--conversation <ProviderSessionID>`（`adapter.go:100-102`）；new 时不传 | 存在 `--conversation`（`Resume a previous conversation by ID`）；另有 `--continue` / `-c` | **MATCH**（参数名与语义一致） |
| 8 | 审批 / 权限 | `Descriptor` 声明 `Approval: ApprovalPreflight`（`adapter.go:64`），但 `DecideApproval` 恒返回 `ErrApprovalUnsupported`（`adapter.go:197-198`），且 argv 中**不传** `--dangerously-skip-permissions`、`--sandbox`、`--mode` | 存在 `--dangerously-skip-permissions`（`Auto-approve all tool permission requests without prompting`）与 `--sandbox` | **MISMATCH（P1）**：非交互 `--print` 模式下没有自动批准路径，工具权限请求无法满足 |
| 9 | stdout 约定 | 从 stdout 逐行解析 JSON，空 stdout → `AGY stream-json was empty` → `Status=Uncertain, SideEffectsKnown=false`（`stream.go:86-88`、`adapter.go:172-178`） | 实测 `--version` 走 stdout、`--help` 走 stderr | **部分 MISMATCH**：解析方向正确，但**终态判定过宽**——`stream.go:90-96` 在「stdout 非空且无 status 字段」时直接判 `Succeeded` 且 `SideEffectsKnown=true`，不满足 fail-closed |
| 10 | stderr 约定 | 读取 stderr 到 `stderrBytes`，**只用其长度做超限判断，内容从未写入 `result.Error` 或事件**（`adapter.go:152-158`）；`parseStderrLines`（`adapter.go:222-229`）为死代码 | 实测诊断信息写在 stderr（help 全文在 stderr，exit 0） | **MISMATCH（可观测性，P1）**：直接解释了 T04 Task A 诊断报告中「`result_json`/Event Journal/worker journal 均无原始 AGY stderr」的证据缺失 |
| 11 | 退出码约定 | 仅在 `Wait()` 非零时把结果降级为 `Uncertain`（`adapter.go:159-171`） | help 未文档化退出码；实测 `--help`=0、`--version`=0 | **UNVERIFIED + 高风险**：help 证明 AGY 存在「诊断信息进 stderr 但 exit 0」的路径；Adapter 在 exit 0 时只依赖 stdout 内容判定，见第 4 节 |
| 12 | 超时 | 未传 `--print-timeout` | `--print-timeout  Timeout for print mode wait (default 5m0s)` | **风险（P2）**：若 OpenAgentX 侧 `ExecutionSpec.Timeout` > 5m，AGY 会先被自身默认 5m 截断，双方超时不一致 |

## 4. 两次约 0.2 秒即 `uncertain` 的最可能不兼容点

已知失败特征（引自 `docs/reports/validation/2026-08-31-openagentx-t04-agy-task-a-failure-diagnosis.md`）：Task 与 RunAttempt 创建后约 0.2 秒即 `uncertain`，持久错误 `Runtime Backend ended without a verifiable result`，Worker 仍 online、`NRestarts=0`。

**最可能路径（按证据强度排序）：**

**P0 — 缺 `--input-format`，prompt 实际未以 AGY 认识的方式送达**

1. `StartTurn` 只发 `--print --output-format stream-json`（`adapter.go:99`），`--input-format` 保持默认值 `text`。
2. help 对 stdin 的唯一承诺挂在 `--input-format stream-json` 上：`stream-json reads one NDJSON message per line from stdin and runs a turn for each`。**`text` 输入格式下 help 未承诺读取 stdin，且 help 中不存在位置参数**。因此在默认 `text` 模式下，Adapter 经 stdin 传入的 prompt 很可能根本没有被消费。
3. 未被消费 prompt 时，进程没有可执行的一轮任务，在**未发起任何模型调用**的情况下立即结束——这与「0.2 秒即返回」「Worker 未重启」「无网络/鉴权痕迹」完全吻合。
4. 进程结束 → stdout 为空 → `stream.go:86-88` 返回 `AGY stream-json was empty` → `adapter.go:172-178` 置 `Status=Uncertain`、`SideEffectsKnown=false` → 上层持久化为 `Runtime Backend ended without a verifiable result`。
5. 与此同时 AGY 的提示/诊断写在 **stderr 且 exit 0**（`--help` 实测证据），而 `adapter.go:152-158` 丢弃了 stderr 内容，于是**连失败原因都没有留下**——这正是诊断报告第 1 节「缺失证据」的成因。

**P1 — 即便 prompt 被消费，也会在第一个工具权限请求处卡死或失败**

`Descriptor` 声明 `ApprovalPreflight`（`adapter.go:64`），但既不传 `--dangerously-skip-permissions`（help：`Auto-approve all tool permission requests without prompting`），`DecideApproval` 又恒返回 `ErrApprovalUnsupported`（`adapter.go:197-198`）。非交互 `--print` 模式下没有任何自动批准路径。

**P1 — 能力与 CLI 参数不对齐**

`ReasoningBudgetTokens` 在 help 中无对应参数（只有 `--effort (low|medium|high)`）；`--model` 从不传递而 `Models` 声明为 `default`。这两项不会直接导致 0.2 秒失败，但会让「模型/推理参数按 ExecutionSpec 生效」这一 T04 通过条件无法成立。

> 边界声明：以上为**基于 help 原文与代码路径的最可能归因**，本次任务禁止运行测试与真实模型调用，因此未对 `--input-format text` 的 stdin 行为做端到端实测；P2 项（`--print-timeout` 5m 默认值）与 `default` 是否为合法 model id 同样标记为 unverified，不得据猜测下结论。

## 5. 最小修复建议（本任务不改代码）

| # | 优先级 | 建议 | 依据 |
| --- | --- | --- | --- |
| 1 | P0 | `StartTurn` argv 增加 `--input-format stream-json`：`[]string{"--print", "--output-format", "stream-json", "--input-format", "stream-json"}` | help：`--input-format` 的 `stream-json` `requires --output-format stream-json`（后者已传），且是唯一承诺读 stdin 的输入格式 |
| 2 | P0 | `buildPrompt` 改为输出 **NDJSON**：把 `Task.Content` 与 `Messages` 合并为**单条** JSON 消息一行写入 stdin，而非纯文本 + `"\n\nFollow-up:\n"` 拼接 | help：`stream-json` `reads one NDJSON message per line from stdin and runs a turn for each`——**一行即一轮**，追加 Follow-up 段落会产生多个 turn，与「一次 `StartTurn` = 一个 turn」的语义冲突，并使 `ProviderSessionID` 归属混乱 |
| 3 | P0 | 把 AGY 的 stderr 内容接入 `result.Error` / Runtime Event（限制长度后），或删除死代码 `parseStderrLines` 并真正使用它 | `adapter.go:152-158` 当前只比较长度；实测诊断信息在 stderr |
| 4 | P1 | 传 `--model <spec.Model>`；`ReasoningEffort` 映射为 `--effort <low\|medium\|high>`，`ReasoningBackendDefault` 不传 `--effort`；**从 `Descriptor.ReasoningModes` 移除 `ReasoningBudgetTokens`**（CLI 无对应参数） | help：`--model`、`--effort (low\|medium\|high)`；help 中无 budget/token 参数 |
| 5 | P1 | 非交互路径显式传 `--dangerously-skip-permissions`（或 `--sandbox`），并让 `Descriptor.Approval` 与实际能力一致；若保留交互审批，则不应声明 `ApprovalPreflight` | help：`--dangerously-skip-permissions`；`adapter.go:64` vs `adapter.go:197-198` 自相矛盾 |
| 6 | P1 | `stream.go:90-96` 收紧终态判定：只有当流中出现终态记录（`status` 为 success/completed/done 或 `result` 非空）时才置 `Succeeded`；`exit 0 + 无终态` 必须判 `Uncertain`、`SideEffectsKnown=false` | 当前「stdout 非空即成功」不满足 AGENTS.md 的 fail-closed 要求；实测 AGY 存在 exit 0 的诊断路径 |
| 7 | P2 | 用 `ExecutionSpec.Timeout` 显式传 `--print-timeout`，避免被 AGY 自身默认 `5m0s` 截断 | help：`--print-timeout  Timeout for print mode wait (default 5m0s)` |
| 8 | P2 | 保留 `Health` 的 `--version` 探测，但改为校验 stdout 语义（如匹配版本号），而非 `CombinedOutput()` 无错即通过 | 实测 `--version` stdout=`1.1.22`、exit 0 |

## 6. 必须新增的回归测试（本任务不实现、不运行）

| # | 测试 | 断言 |
| --- | --- | --- |
| 1 | `TestStartTurnArgsGolden` | argv 精确快照，必须包含 `--print`、`--output-format stream-json`、`--input-format stream-json`；防止后续再次漂移 |
| 2 | `TestStartTurnWritesSingleNDJSONLineToStdin` | 捕获 stdin：每行必须 `json.Valid`；**恰好一条**消息；不再出现裸纯文本 prompt |
| 3 | `TestStartTurnPassesModelAndEffort` | `Spec.Model` → `--model`；`ReasoningEffort{high}` → `--effort high`；`ReasoningBackendDefault` → 不传 `--effort` |
| 4 | `TestValidateRejectsBudgetTokens` | `ReasoningBudgetTokens` 的 `Validate` 返回 `ErrUnsupportedCapability`（配合修复项 4 收窄 Descriptor） |
| 5 | `TestEmptyStdoutIsNotSucceeded` | exit 0 + 空 stdout → `Status=Uncertain`、`SideEffectsKnown=false`，且 `Error` 非空 |
| 6 | `TestExitZeroWithoutTerminalRecordStaysUncertain` | exit 0 + stdout 有非终态行 → 不得判 `Succeeded`（fail-closed 回归，对应修复项 6） |
| 7 | `TestStderrContentIsSurfaced` | AGY 在 stderr 输出诊断、`exit 0`、stdout 为空时，`result.Error` 必须包含该 stderr 文本（按 `StderrLimit` 截断） |
| 8 | `TestStartTurnPassesSkipPermissions` | 非交互路径必须传 `--dangerously-skip-permissions`（或显式 `--sandbox`），且与 `Descriptor.Approval` 一致 |
| 9 | `TestRealAgyHelpContract`（契约守卫，本机真 binary） | 运行 `agy-graft --help`（**不发起模型调用**），断言 help 文本仍含 `--print`、`--output-format`、`--input-format`、`--conversation`、`--model`、`--effort`、`--dangerously-skip-permissions` 七项参数名；任一消失即失败，用于捕获 AGY 版本升级导致的契约漂移 |
| 10 | `TestPrintTimeoutAlignsWithSpecTimeout` | argv 含 `--print-timeout`，取值与 `ExecutionSpec.Timeout` 对齐 |

## 7. 本报告未覆盖项

| # | 未覆盖项 | 原因 | 关闭方式 |
| --- | --- | --- | --- |
| 1 | `--input-format text` 下 AGY 是否真的忽略 stdin | 本任务仅允许两条只读命令，禁止真实模型调用 | 修复后以一次真实、明确、可逆的 Task A 取证（首次 turn 成功 + `result_json` 非空） |
| 2 | `default` 是否为合法 `--model` 取值 | help 未列候选值，需 `agy models` 子命令，超出本次授权 | 修复项 4 落地时以 `agy models` 输出校准 `Descriptor.Models` |
| 3 | AGY 各失败路径的退出码表 | help 未文档化，只能实测；本任务禁止额外命令 | 修复项 3（stderr 可观测）落地后，从真实失败 stderr + 退出码归纳 |
| 4 | 代理链路（`mgraftcp`）与 `AGY_GRAFT_*` 环境变量 | 属 wrapper 层而非 CLI 契约；本报告不读取任何代理凭据 | 由 T04 网络/代理专项核对覆盖 |
