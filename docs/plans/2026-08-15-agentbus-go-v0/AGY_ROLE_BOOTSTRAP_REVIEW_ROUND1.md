# AgentBus V0.1 角色 Bootstrap：Codex 审核修正 Round 1

## 任务边界

请只修改 `AgentBus/`，完成本文件全部要求并运行验证。不要操作真实 `%50/%51/%52/%53` panes，不修改 SteadyFlow 父仓库，不执行 `git add`、`git commit` 或 push。

用户已明确：本轮不要求兼容旧版 V0 数据、API 或命令行为。不要为向后兼容增加分支；以 V0.1 新契约的正确性、fail-closed 与可验证性为准。

## 必须修正

### 1. Delivery failure 不得二次递增 generation

当前 `Service.AttachAgent` 在 Bootstrap 通知失败后再次调用 `store.AttachAgent`，`Service.BootstrapAgent` 再次调用 `store.BootstrapAgent`。这会让一次 attach/bootstrap 将 generation 增加两次，并可能让 API 返回的 generation 与数据库不一致。

要求：

- Store 增加“按 `agent_id + generation` CAS 更新当前 Session delivery 状态”的方法；该方法只能更新 `status/resolved_pane_id/delivery_error/updated_at`，不得修改 generation、started_at 或 ready_at（失败状态应清空 ready_at）。
- Service 通知失败时使用该方法把同一 generation 改为 `delivery_failed`。
- API response 的 `Session` 必须与数据库完全一致，`DeliveryError` 必须返回本次通知错误。
- attach 与 bootstrap 各增加 Service 测试：probe 成功但实际 delivery 失败；断言 generation 只增加一次、返回值与 `session show` 一致、状态为 `delivery_failed`、错误非空、该 generation 可以按既有契约 ready。

### 2. Task 写操作的 Ready Gate 必须与写入原子化

当前 Service 先调用 `IsAgentReady`，随后才进入 Store transaction。并发 re-bootstrap 可插入两者之间，导致 session 已回到 `bootstrapping` 后仍创建任务或写入状态。

要求：

- 保留 Service 的快速检查可以，但 Store 必须在执行相应写入的同一 transaction 内再次检查 session status。
- `SubmitTask` 在 transaction 内检查 sender 与 target 均为 `ready`。
- `AckTask`、`UpdateTaskStatus`、`CompleteTask`、`FailTask` 在各自 transaction 内检查 actor 为 `ready`。
- `SendMessage`、`CancelTask` 在各自 transaction 内检查 sender/actor 为 `ready`。
- 不 ready 时统一返回可被 `errors.Is(err, domain.ErrAgentNotReady)` 匹配的错误。
- 增加 Store 测试，证明绕过 Service 直接调用上述写入口时也会 fail closed；至少覆盖 submit、target 写动作、sender 写动作三类，并覆盖 re-bootstrap 后拒绝。

读操作的 gate 可以在线性化于 Service readiness 查询时，本轮不要求为纯读接口增加长事务。

### 3. `agent launch` 必须真正保证受管启动

当前实现有三个问题：无 `TMUX_PANE` 时会回退到 manifest 地址继续启动；pane 冲突未拒绝；后台 goroutine 静默忽略 attach/delivery 错误，短命子进程甚至可在 attach 前成功退出。

要求：

- `agent launch` 必须检测非空 `TMUX_PANE`，否则在启动子进程前拒绝。
- V0.1 `launch` 只接受 `connector: tmux`。
- `$TMUX_PANE`、manifest address（非空且非 `auto`）与 `--address`（若提供）必须一致；任一冲突在启动子进程前拒绝。
- 子进程启动后等待 bootstrap delay，再同步获得 attach 结果；attach API 错误或 disposition=`delivery_failed` 时终止子进程、等待回收并返回非零，不能留下裸 Agent。
- 子进程若在 attach 完成前退出，视为受管启动失败并返回非零。
- attach 成功后继续等待并原样返回子进程退出码；context cancel 必须终止并回收子进程。
- 继续 direct argv，禁止 shell 拼接。
- 重写 launch 测试：使用测试 helper 子进程或其他受控非 Agent 命令，不启动真实 coding Agent；覆盖非 tmux、地址冲突、attach 失败、成功 attach 后 session 存在，以及子进程退出码传播。删除当前会让 `echo` 在 attach 前退出仍 PASS 的无效断言。

### 4. Delivery disposition 必须诚实

`--no-notify`、`connector: none` 或没有执行投递时不能返回 `notified`。

要求：

- 增加明确的 disposition（推荐 `skipped`），用于没有尝试投递的路径。
- `notified` 只表示 TmuxConnector 实际完成投递。
- 增加对应 Service/CLI 断言。

### 5. V0.1 文档必须可直接运行

当前 README 仍只展示 `agent register` 后直接 submit，这在 ready gate 下必然失败。

要求：

- README 目录树加入 `agents/`，架构说明加入 Role Manifest/Profile/Session Generation/Ready Gate。
- 快速上手改为 `whoami → attach → session ready → task`；说明 Coordinator 用 `--no-notify` 后手工 ready，南向 Agent 从 Bootstrap 通知读取 generation 并 ready。
- 增加 `agent bootstrap`、`session show` 与 `agent launch` 的简短说明。
- 将 ROLE 中 `%50/%51/%52` 明确写成“当前默认部署 pane”，不能暗示 address override 后角色身份也改变。
- 不删除设计文档；不要修改其中的兼容策略文字，Codex 会按用户最新决定单独同步设计决策。

## 建议同时修正

- 在 daemon attach 边界加强 `AgentProfile` canonical path 校验：`workspace/config_path/instructions_path` 应为绝对 clean path，instructions 至少存在、为普通非空可读文件且不超过 1 MiB。若实现该项，同步更新测试 fixture，不得依赖不存在的 `/a.yaml`、`/r.md`。
- Manifest loader 对 ROLE 的“可读”要求应实际 open/read 验证，而不只 `os.Stat`。

## 必跑验证

```bash
cd AgentBus
gofmt -w .
go mod tidy
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
go build -o bin/agentbus ./cmd/agentbus
git diff --check
git status --short
```

完成后简要报告：修改点、关键新增测试、每条命令结果、仍有风险。不要提交。
