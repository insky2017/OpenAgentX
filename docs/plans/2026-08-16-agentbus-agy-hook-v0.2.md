# AgentBus AGY Hook Ingress V0.2 实施与验收计划

## 1. 背景与目标

AgentBus V0.2 旨在实现异构 Agent Runtime（以 AGY 为代表）与 AgentBus 控制面的双向生命周期闭环。通过 AGY Lifecycle Hook，自动将 Agent 的执行阶段（Invocation、Stop）接入 AgentBus 并在任务未完成时阻断 Stop。

---

## 2. 实施任务拆解

### 任务 1: Domain 与 Event 契约扩展
- [x] 新增 `domain.EventRuntimeObserved` (`runtime.event_observed`) 事件类型。
- [x] 定义 AGY 5 种合法事件常量与白名单校验函数 `IsValidAGYEvent`。

### 任务 2: Service & Server 端点实现与状态竞争消除
- [x] 在 `Service` 增加 `RecordRuntimeEvent` 方法，执行 Session Ready、Profile Runtime、Task Target 及事件白名单校验。
- [x] 严格校验 API Payload 与 HTTP Request Body：必须为单个非 null 的 JSON Object，拒绝 Array、Primitive 与尾随畸变数据。
- [x] 写入不可变 Event 并通过 `EventBroker` 唤醒 `task watch`。
- [x] 在 `AddEvent` 后重新读取 Task 实体以返回最新状态；若 re-fetch 失败直接报错，禁止回退到陈旧状态。
- [x] 在 `Server` 注册 `POST /api/v1/tasks/{id}/runtime-events` 端点并处理标准错误映射。

### 任务 3: Client API 扩展
- [x] 在 `Client` 增加 `RecordRuntimeEvent` 方法。

### 任务 4: CLI `runtime agy-hook` 命令实现、超时控制与降级语义
- [x] 实现 `agentbus runtime agy-hook` 子命令。
- [x] 增加 `--control-timeout` 选项（默认 5s，必须 `> 0`），控制全流程 Deadline，保证短于 AGY Hook timeout。
- [x] 严格限制 stdin 为单个非 null 的 JSON Object（最大 1 MiB），拒绝尾随多余 JSON。
- [x] 实现未受管 AGY 降级放行：`--agent auto` 且未匹配到受管会话时输出 `{}` 并退出 0。
- [x] 实现受管 Agent 身份解析（显式 `--agent` / `AGENTBUS_AGENT_ID` / 唯一 ready `TMUX_PANE`）。
- [x] 实现 Target 专属活跃任务解析（仅过滤 `TargetAgentID == agentID` 且状态为 `queued,running`）。
- [x] 实现 Stop 门禁与 Fail-closed 决策：任务未完或控制面故障/超时时，强受管 Agent 的 stdout 必须返回 `decision: continue`，所有诊断仅写 stderr。

### 任务 5: 集成模板与文档
- [x] 按照 AGY 1.1.13 原生规范创建 `integrations/agy/hooks.json.example`（`agentbus-lifecycle` 具名 Hook，配置 `PreInvocation`、`PostInvocation` 与 `Stop`，`timeout: 10` 覆盖 `--control-timeout 5s`）。
- [x] 编写 `docs/design/AGY_HOOK_INTEGRATION.md`，记录架构契约与真实 E2E 验证结果。
- [x] 更新 `README.md` 与 `docs/ARCHITECTURE.md`，记录 10s bootstrap 延迟运维建议与 Hook 配置加载时机。

### 任务 6: 低 Token 单次阻塞等待 (`agentbus task wait`)
- [x] 实现 `agentbus task wait <task-id> --agent <caller-agent-id> --timeout 30m` 子命令。
- [x] 内部基于 EventBroker / GetEvents 长轮询等待，静默无噪声事件流，终态时输出单一 `TaskDetail` JSON。
- [x] 明确退出码契约：`0` (succeeded), `1` (参数/鉴权错误), `2` (failed), `3` (canceled), `4` (timeout)。
- [x] 全量测试覆盖：已成功任务立返、running->succeeded 事件唤醒、failed/canceled 退出码、timeout 超时退出与非法参数。

---

## 3. 验收与实测记录

1. **自动化测试与静态检查**：
   - `go vet ./...`：✅ 通过。
   - `go test -timeout 60s -count=1 ./...`：✅ 全量单元测试 100% 通过（7/7 packages）。
   - `go test -race -timeout 120s -count=1 ./...`：✅ 全量并发竞态检测 100% 通过（零 Race）。
   - `git diff --check`：✅ 无多余空白与格式差异。

2. **原生 AGY Hook 隔离 E2E 实测**：
   - 观察到 `PreInvocation`、`PostInvocation`、`Stop` 三类真实事件记录。
   - 处于 `running` 状态的 Task 触发 Stop 时被成功拦截并返回 `decision: continue`。
   - Agent 收到系统提示后显式执行 `task complete` 顺利达到 `succeeded` 终态。

3. **生产 Tmux Pane Agent 实测**：
   - 正式 Agent (`agentbus-agent` 与 `quote-service`) 通过 `agentbus agent launch` 成功拉起并注入 `AGENTBUS_AGENT_ID` 强受管身份。
   - 生产环境中平稳产生 Pre/Post Invocation 观察事件，顺利完成无副作用端到端业务验收。
