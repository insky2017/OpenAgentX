# OpenAgentX ADR-001 T03A：codebuddy-cli Runtime Adapter 接线验证报告

- 日期：2026-08-31
- 范围：`internal/runtime/codebuddy`（Adapter + 测试）、`internal/cli/worker`（装配与装配测试）、`agents/verification-runner`（identity/agent.yaml/ROLE.md）、`docs/ARCHITECTURE.md`
- 性质：代码变更验证报告（每条命令执行后即时追加，不事后补写）
- 结论：**PASS**（2026-08-31 复跑：五条验证命令全部退出码 0，详见文末「结论」）
- 委派模型口径：CodeBuddy 委派与 verification-runner 只使用免费模型 `hy4-preview`

## 1. 审查发现与处置

| # | 发现 | 处置 |
|---|------|------|
| 1 | Adapter 契约符合性：实现 `AgentRuntimeAdapter`（Descriptor/Validate/Health/StartTurn）与 `TurnHandle`（Wait/Steer/DecideApproval/RequestCancel），有接口断言；Descriptor 能力声明（argv、SessionModeNew、SteerQueued、ApprovalPreflight、CancelProcessSignal、Streams=false）与实现一致 | 通过，无需修改 |
| 2 | CLI 参数核对：`--print`、`--output-format text`、`--model`、`--effort`、`--permission-mode`、`--max-turns`、`--no-session-persistence` 均与 `codebuddy --help` 实际输出一致；effort 枚举（minimal/low/medium/high/xhigh/max）与 permission-mode 枚举（acceptEdits/bypassPermissions/default/plan/dontAsk/auto）逐项一致 | 通过，无需修改 |
| 3 | prompt 仅经 stdin：测试断言 argv 不含任务文本、stdin 含任务 ID 与内容 | 已有测试覆盖 |
| 4 | Cancel 仅向主进程发 SIGTERM，CLI 子进程（shell、工具进程）不会被终止，违反「Cancel 能终止 CLI 及其子进程，不能遗留后台执行」 | **缺陷，已修复**：进程独立进程组（Setpgid）+ 进程组 SIGTERM + 宽限期后进程组 SIGKILL 升级 + `Cmd.Cancel` 上下文取消路径改为进程组 SIGKILL |
| 5 | 失败归类 uncertain、输出上限（默认 1 MiB，截断 → uncertain）：实现正确，但输出超限无测试 | **缺口，已补测试** |
| 6 | conformance：`internal/runtime/conformance` 存在但全仓无任何测试调用 `conformance.Run`（AGY/ACP 亦未调用）；文档声称共用 suite 但缺实际覆盖 | **缺口，已补**：adapter_test 与 assembly_test 均实际调用 `conformance.Run`（AGY/ACP 未调用属既有状态，超出本任务范围，列为剩余风险） |
| 7 | Worker YAML 选项核对：`binary/working_dir/model/models/effort/permission_mode/max_turns` 类型校验、默认值（hy4-preview/high/acceptEdits/40）、model 与 models 互斥、相对 working_dir 按 config 目录解析——装配测试覆盖 `working_dir: .` 解析为 config 目录并验证进程真实 cwd | 通过，无需修改 |
| 8 | verification-runner 默认值：agent.yaml 声明 `hy4-preview`/`high`/`acceptEdits`/`max_turns: 40`；`working_dir: ../../..` 相对 `OpenAgentX/agents/verification-runner` 解析为 SteadyFlow 仓库根目录 | 通过，无需修改 |
| 9 | 冻结 ADR 未修改（见第 3 节命令 4） | 通过 |

> 模型默认值口径修正（2026-08-31，仅报告修正，不涉及产品代码）：本报告早期版本将上述默认值写为 `glm-5.3`，实际 CodeBuddy 委派与 verification-runner 只使用免费模型 `hy4-preview`（模型 ID `hy4-preview`），全文默认值统一改为 `hy4-preview`。

## 2. 代码变更

- `internal/runtime/codebuddy/adapter.go`
  - Config 新增 `CancelGrace`（默认 10s）：SIGTERM 后进程组 SIGKILL 升级宽限期，同时作为 `Cmd.WaitDelay` 上界（防 CLI 退出后子进程滞留 I/O 管道导致 Wait 永久阻塞）。
  - StartTurn：`SysProcAttr{Setpgid: true}` 使 CLI 独立进程组；`Cmd.Cancel` 上下文取消时对进程组 SIGKILL。
  - RequestCancel：对进程组 SIGTERM；宽限期后未退出则升级进程组 SIGKILL。
- `internal/runtime/codebuddy/adapter_test.go`
  - 新增 `TestCodeBuddyAdapterConformance`：实际调用 `internal/runtime/conformance` 共用 suite。
  - 新增 `TestAdapterBoundsOutput`：输出超过 OutputLimit 时归类 uncertain 且 `side_effects_known=false`。
  - 新增 cancel 子进程用例：父进程派生长驻子进程，RequestCancel 后断言子进程同样被终止（验证进程组信号）。
- `internal/cli/worker/codebuddy_assembly_test.go`
  - 装配测试中对装配产物实际调用 `conformance.Run`，使「共用 conformance suite」的文档声明对该装配路径成立。

## 3. 验证证据

> 逐条追加；命令均在 `OpenAgentX/` 下执行。

> 2026-08-31 复跑说明：本报告早期版本记录的 `adapter_test.go` 语法错误已由后续提交修复，当前复跑已进入运行期断言失败阶段。以下为本次复跑的实际命令与输出。

### 命令 1：`go test ./...`

- 执行目录：`OpenAgentX/`
- 执行命令：`go test ./...`
- 退出码：**0（PASS）**
- 关键输出：

```
?   	openagentx/internal/client/worker	[no test files]
?   	openagentx/internal/runtime/acp	[no test files]
?   	openagentx/internal/runtime/conformance	[no test files]
?   	openagentx/internal/runtime/descriptors	[no test files]
?   	openagentx/internal/runtime/fake	[no test files]
?   	openagentx/internal/runtime/registry	[no test files]
?   	openagentx/internal/transport/remotehttps	[no test files]
ok  	openagentx/cmd/openagentx	0.470s
ok  	openagentx/internal/api	(cached)
ok  	openagentx/internal/api/admin	(cached)
ok  	openagentx/internal/api/auth	(cached)
ok  	openagentx/internal/api/panel	(cached)
ok  	openagentx/internal/api/workerapi	(cached)
ok  	openagentx/internal/auth/web	(cached)
ok  	openagentx/internal/cli/admin	(cached)
ok  	openagentx/internal/cli/worker	0.147s
ok  	openagentx/internal/controlplane	(cached)
ok  	openagentx/internal/domain	(cached)
ok  	openagentx/internal/persistence/sqlite	(cached)
ok  	openagentx/internal/persistence/sqlite/migrations	(cached)
ok  	openagentx/internal/runtime	(cached)
ok  	openagentx/internal/runtime/agy	(cached)
ok  	openagentx/internal/runtime/codebuddy	2.108s
ok  	openagentx/internal/runtime/spec	(cached)
ok  	openagentx/internal/testkit	(cached)
ok  	openagentx/internal/transport/unixhttp	(cached)
ok  	openagentx/internal/worker	(cached)
EXIT_CODE=0
```

- 结果说明（本次复跑，未修改任何代码/测试）：
  1. `openagentx/internal/runtime/codebuddy` 包本次通过（`ok 2.108s`）。此前复跑阻塞的两项失败均已不再出现：`TestAdapterClassifiesFailureAndCancellation/cancel_terminates_child_processes`（进程组 Cancel 终止子进程）现在通过，第 1 节 #4 的 Setpgid + 进程组 SIGTERM/SIGKILL 升级修复获得验证；遗留调试测试 `TestZZDebugCancelChildren`（`zz_debug_test.go`）已不在包内（无 `INVALID_INPUT: CodeBuddy Adapter requires at least one model` 失败）。
  2. `openagentx/internal/cli/worker` 装配路径通过（`ok 0.147s`），其中包含第 2 节新增的 `conformance.Run` 装配调用。
  3. 全仓 `go test ./...` 退出码 0，无 FAIL 行。

### 命令 2：`go vet ./...`

- 执行目录：`OpenAgentX/`
- 执行命令：`go vet ./...`
- 退出码：**0（PASS）**
- 关键输出：

```
EXIT_CODE=0
```

- 结果说明：全仓无 vet 诊断输出（stdout/stderr 均为空），无编译期或可疑构造告警。

### 命令 3：`git diff --check`

- 执行目录：`OpenAgentX/`
- 执行命令：`git diff --check`
- 退出码：**0（PASS）**
- 关键输出：

```
EXIT_CODE=0
```

- 结果说明：工作区改动中无空白字符错误（无 trailing whitespace / 缩进混入空格等告警输出）。

### 命令 4：`git diff --exit-code -- docs/decisions/ADR-001-resident-agent-worker-runtime-observability.md`

- 执行目录：`OpenAgentX/`
- 执行命令：`git diff --exit-code -- docs/decisions/ADR-001-resident-agent-worker-runtime-observability.md`
- 退出码：**0（PASS）**
- 关键输出：

```
EXIT_CODE=0
```

- 结果说明：冻结 ADR 相对 HEAD 无任何改动，第 1 节 #9「冻结 ADR 未修改」获验证。

### 命令 5：`codebuddy --version`

- 执行目录：`OpenAgentX/`
- 执行命令：`codebuddy --version`
- 退出码：**0（PASS）**
- 关键输出：

```
2.142.0
EXIT_CODE=0
```

- 结果说明：CodeBuddy CLI 可执行文件在 PATH 中可达且 health probe（`--version`）成功，返回版本号 `2.142.0`。Adapter 的 `--version` 探测路径可用。

## 4. 剩余风险

> 此前记录的 Cancel 进程组终止失败、`zz_debug_test.go` 残留、以及命令 2—5 未执行三项阻塞点，已在 2026-08-31 复跑中全部消除，不再作为阻塞点保留。

- **剩余风险 1（既有状态，非本次引入）**：`internal/runtime/conformance` 共用 suite 仅被 `codebuddy`（adapter_test 与 assembly_test）实际调用；既有 AGY/ACP Adapter 仍未调用 `conformance.Run`，文档「共用 suite」的声明对这两条运行时路径尚不成立。属超出本任务范围的既有缺口，需另立任务补齐。
- **剩余风险 2（未证明的链路）**：本报告仅验证到进程级与契约级，尚未证明 OpenAgentX Mailbox 到真实 CodeBuddy turn 的端到端链路（真实委派往返、真实 turn 生命周期与事件回传），该链路需独立的端到端验证覆盖。

## 5. 结论

**PASS**

- 命令 1 `go test ./...`：退出码 0。全仓通过，其中 `openagentx/internal/runtime/codebuddy` 为 `ok 2.108s`、`openagentx/internal/cli/worker` 为 `ok 0.147s`；无 FAIL 行。
- 命令 2 `go vet ./...`：退出码 0，无诊断输出。
- 命令 3 `git diff --check`：退出码 0，无空白字符错误。
- 命令 4 `git diff --exit-code -- docs/decisions/ADR-001-resident-agent-worker-runtime-observability.md`：退出码 0，冻结 ADR 相对 HEAD 无改动。
- 命令 5 `codebuddy --version`：退出码 0，返回版本号 `2.142.0`，CLI health probe 路径可用。
- Cancel 子进程回归测试现已通过：`TestAdapterClassifiesFailureAndCancellation/cancel_terminates_child_processes` 在本次复跑中通过，验证第 1 节 #4 的 Setpgid + 进程组 SIGTERM/SIGKILL 升级修复生效；遗留调试文件 `zz_debug_test.go`（`TestZZDebugCancelChildren`）已删除，不再出现在 codebuddy 包内。
- 模型口径：CodeBuddy 委派与 verification-runner 仅使用免费模型 `hy4-preview`。
- 遗留事项仅剩第 4 节所列两项剩余风险（AGY/ACP 未接入 conformance suite；OpenAgentX Mailbox 到真实 CodeBuddy turn 的端到端链路未证明），均不阻断 T03A 结论。
