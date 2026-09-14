---
doc_type: implementation_plan
status: active
owner: openagentx
updated_at: 2026-09-14
---

# OpenAgentX ADR-008 实施计划

## 1. 目标

按 [ADR-008](../decisions/ADR-008-oax-workspace-console-tui-and-cli-session.md) 分阶段完成以下能力，并以真实本地 UDS、隔离 tmux server 和用户级 systemd 场景验收：

- 统一 `~/.openagentx` 默认路径和配置优先级；
- 固定、大小写敏感的 `OAX` workspace，稳定使用 pane `0`，保留其他 pane；
- 显式且非破坏的 window-to-Agent 绑定；
- 一致 Attach 快照、Event Journal high-water cursor 和代际防回退 reducer；
- 可撤销、有期限、仅存摘要的 CLI Token 与权限为 `0600` 的本地 credential store；
- `openagentx console` 主菜单、Agent selector 和真正全屏 Attach TUI；
- Fleet 默认路径、显式清单和 user-systemd 默认宿主的一致集成。

本计划不实施 Foreground Takeover，不修改 ADR-006 的 Task intent 语义，不修改 ADR-007 的网络代际继承语义，也不把 tmux 引入身份、路由、lease 或业务控制路径。

## 2. 执行原则

1. 任务严格按 `01 -> 08` 顺序执行；前一任务退出条件和验证门禁未通过时不得开始下一任务。
2. 每次只实施一个任务。Codex 完成任务后更新[持续执行记录](2026-09-14-openagentx-adr-008-implementation/EXECUTION-LOG.md)、创建独立提交并暂停，等待监督者复核。
3. 每个提交必须同时包含该任务实现、定向测试和执行记录；不得以“后续补测试”越过门禁。
4. 生产服务、活动 socket、正式数据库、真实 `OAX` workspace 和 `~/.local/bin/openagentx` 在最终人工批准前均不得修改。
5. tmux 端到端测试使用独立 server/socket（例如 `tmux -L <unique-test-name>`），不得操作用户当前 tmux server。
6. 数据库测试只使用临时目录；不得打开或迁移 `~/.openagentx/data/openagentx.db`。
7. 发现计划未覆盖的新架构选择、安全降级或 ADR 冲突时立即停止，在执行记录中登记，不得自行扩大范围。

## 3. 受保护的目标主机现场

计划发布时，`rtx4090` 的 OpenAgentX 主工作树已有以下由先前任务产生的本地修改：

```text
 M README.md
M  deploy/systemd/openagentx-user.service
 M internal/worker/runner_test.go
?? docs/operations/openagentx-user-install-guide.md
```

这些文件是受保护基线，不属于计划文档提交。实施开始前必须由原执行 Codex 逐项复核：

- 若变更完整且符合其原任务，重新验证后作为**独立的前置提交**提交并推送；
- 若存在不确定、未完成或混合范围，立即暂停并请监督者决定；
- 禁止 reset、checkout、clean、stash 后遗忘、覆盖或将其混入 ADR-008 任务提交；
- `steadyflow` 父仓库不修改、不提交。

只有主工作树完成上述收口并干净后，才从最新 `origin/main` 创建独立 ADR-008 branch/worktree。执行记录必须保存前置提交 SHA、实现基线 SHA、worktree 路径和分支名。

## 4. 任务与依赖

| ID | 阶段 | 任务 | 依赖 | 状态 |
|---|---|---|---|---|
| 01 | G0 | [基线隔离、契约冻结与测试地图](2026-09-14-openagentx-adr-008-implementation/01-baseline-isolation-and-contract-freeze.md) | ADR-008、受保护现场收口 | pending |
| 02 | G1 | [默认路径与 CLI 表面](2026-09-14-openagentx-adr-008-implementation/02-default-paths-and-cli-surface.md) | 01 | pending |
| 03 | G2 | [一致 Attach cursor 与代际 reducer](2026-09-14-openagentx-adr-008-implementation/03-consistent-attach-cursor-and-reducer.md) | 02 | pending |
| 04 | G3 | [可撤销 CLI Token 会话](2026-09-14-openagentx-adr-008-implementation/04-revocable-cli-token-session.md) | 03 | pending |
| 05 | G4 | [`OAX` workspace 与非破坏绑定](2026-09-14-openagentx-adr-008-implementation/05-oax-workspace-and-window-binding.md) | 04 | pending |
| 06 | G5 | [Console 主菜单、Agent selector 与全屏 TUI](2026-09-14-openagentx-adr-008-implementation/06-console-menu-selector-and-fullscreen-tui.md) | 05 | pending |
| 07 | G6 | [Fleet、user-systemd 与默认 profile 集成](2026-09-14-openagentx-adr-008-implementation/07-fleet-user-systemd-and-profile-integration.md) | 06 | pending |
| 08 | GR | [集成审查、实机候选与发布门禁](2026-09-14-openagentx-adr-008-implementation/08-integration-review-and-release-gate.md) | 07 | pending |

## 5. 依赖流

```text
受保护现场独立收口
  -> 01 基线/契约
  -> 02 默认路径
  -> 03 快照/cursor/reducer
  -> 04 CLI Token
  -> 05 OAX workspace/绑定
  -> 06 Console TUI
  -> 07 Fleet/user-systemd 集成
  -> 08 全量验收与人工发布门禁
```

TUI 最后组合稳定的 path、cursor、auth 和 workspace 服务，避免以 UI 代码掩盖底层契约缺陷。Fleet 集成在 TUI 完成后执行，确保其生成的 pane 命令不再依赖明文密码或已删除的临时参数。

## 6. 冻结的不变量

### 6.1 Workspace 与绑定

- session 名精确为 `OAX`，不得接受 `oax`、`agentx` 等别名作为受管 workspace；
- 每个受管 Agent window 的稳定 Console pane 是 pane `0`；pane `1+` 必须保留；
- `console attach` 只能从 `OAX` session 当前 window 的 pane `0` 执行；不满足条件时 fail closed，只输出明确切换提示；
- 绑定必须来自用户显式 Attach 选择，成功后 window 名精确为 `agent_id`，并写 managed/Agent 标记；
- 同名 window、既有冲突标记、无 pane `0`、无法证明身份等情况不得猜测或破坏现场；
- 禁止产品使用 `send-keys`、`paste-buffer`、`capture-pane` 控制任务。

### 6.2 Console 与控制路径

- `openagentx console` 无子命令时进入主菜单；Attach 始终进入持续全屏 TUI；
- 删除 `console attach --once` 和独立 `console status`，详细状态位于 `/status` overlay；
- Agent 选择优先级固定为：显式 `--agent`、当前受管 window、经认证控制面列表；非交互环境缺少可唯一解析 Agent 时 fail closed；
- `steer/cancel/approval/dispatch` 只走正式 UDS/HTTP API、权限、CAS、审计和 Mailbox；
- Foreground Takeover 仅展示 `Foreground Takeover（规划中，暂不可用）`。

### 6.3 Event 与安全显示

- Attach 快照与 high-water cursor 必须属于同一一致读取边界；实时 follow 从该 cursor 之后开始；
- reconnect 只从最后确认 cursor 恢复，不从 sequence `0` 重放；
- 旧 generation、旧 WorkerInstance 和乱序事件不能覆盖当前状态；重复事件必须幂等；
- heartbeat 默认只折叠进内部状态和状态栏，只有状态转换进入 Timeline；
- Console 仅展示结构化、脱敏安全投影，不显示 Secret、隐藏推理、原始环境变量或未脱敏 stderr。

### 6.4 CLI 会话与默认路径

- 密码只由 `openagentx console login` 收集并仅发送给本地认证端点；不得写日志、argv、历史或 credential 文件；
- 本地仅保存 opaque Token 与非秘密元数据；credential 文件必须为 `0600`，原子更新，拒绝不安全权限/软链接降级；
- 服务端仅保存 Token 摘要；Token 可撤销、有绝对到期时间，默认最长 30 天，并绑定 canonical socket、installation ID、username 和 scope；
- logout 在服务可达时先撤销服务端 Token，并无论远端结果如何删除本地 Token；
- 路径优先级固定为：显式参数 > 对应环境变量/`OPENAGENTX_HOME` > `~/.openagentx`；
- 默认路径固定为 ADR-008 列出的 socket、database、fleet、workers 和 credentials 路径。

## 7. 提交与监督协议

- 分支建议：`codex/adr008-implementation`；worktree 必须位于主工作树之外。
- 每个任务一个可审查提交，建议提交主题：`feat(adr008): ...`、`test(adr008): ...` 或 `docs(adr008): ...`；不允许 amend 已被监督者验收的阶段提交。
- 每个阶段提交前必须执行 `git diff --check`、该任务定向测试和 execution log 自检。
- 提交后记录 `git status --short`、提交 SHA、测试命令与退出码，然后暂停；监督者明确放行后才能进入下一任务。
- 禁止直接 push `main`、force-push、rebase 已验收提交或修改父仓库。最终是否合并、推送、安装和重启由监督者另行批准。

## 8. 失败处理

- 测试失败：停在当前任务，只修复当前范围；不得用 skip、放宽断言或删除测试绕过。
- schema 升级失败：保留临时数据库和错误记录，不接触真实数据库，不继续 Auth/UI 阶段。
- tmux 冲突：返回结构化冲突，不 kill window/pane/session，不自动改名未知对象。
- credential 权限异常：拒绝加载/写入，给出修复提示，不放宽到 group/world 可读。
- API/TUI reconnect 异常：停止发送控制命令并明确显示断线；不得假装成功。
- 现有 dirty 文件发生变化：立即停止，记录 `git status` 和 diff，不自行恢复。

## 9. 最终验收层级

1. **纯单元层**：path resolver、CLI parser、cursor reducer、Token 生命周期、TUI model/update、tmux planner；
2. **package 集成层**：Console API/client、SQLite、Fleet、Auth、CLI；
3. **隔离进程层**：临时 home + 临时 DB + 临时 UDS + 隔离 tmux server；
4. **全仓静态与回归层**：Go test/race/vet/build、release scanner、Web build/test、文档与 diff 检查；
5. **只读目标主机层**：二进制候选 hash、现有服务健康、文件权限和计划外变更核对；
6. **人工批准后的实机层**：备份、安装候选、升级服务、真实 `OAX` 体验和 graceful drain。

第 6 层不属于 Codex 自动执行权限。第 1—5 层任一失败，发布结论均为 no-go。

## 10. 计划状态维护

- 任务开始时，仅将执行记录对应任务标记为 `active`；完成并经监督者确认后标记为 `completed`；
- 主计划状态仅在阶段提交中同步，不预写成功结果；
- 执行记录必须保留失败尝试、纠正过程和未决问题，不得只保留最终绿色结果；
- 最终验证另建 validation report，并链接每个任务提交及 execution log；
- ADR-008 在所有验收和人工体验完成前保持“已接受，尚未完成实施”。
