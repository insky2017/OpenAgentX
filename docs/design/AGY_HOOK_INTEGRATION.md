# AgentBus AGY Hook Integration (V0.2)

## 1. 架构与设计定位

在 AgentBus V0.2 中，为了打通异构 Agent Runtime（特别是 Antigravity / AGY CLI）的执行生命周期，AgentBus 提供了专用的 Runtime Lifecycle Ingress 入口：`agentbus runtime agy-hook`。

### 核心设计原则

1. **结构化协议驱动**：直接读取 AGY 传入 stdin 的标准 JSON Object，严禁通过正则解析 transcript 文本或截获 tmux pane 终端输出判断状态。
2. **生产推荐事件集**：生产环境推荐配置三种核心生命周期事件：`PreInvocation`（调用前观察）、`PostInvocation`（调用后观察）与 `Stop`（停机安全门禁）。不引入高频细粒度的 `ToolUse` 噪声事件。
3. **受管与未受管隔离 (Unmanaged Passthrough)**：项目级 `.agents/hooks.json` 可能被非 AgentBus 管理的普通 AGY 会话触发。若 Agent 未在 AgentBus 注册为受管身份，Hook 自动中性放行（输出 `{}` 并退出 0），不破坏普通开发体验。
4. **强受管身份与启动规范**：显式 `--agent` 或环境变量 `AGENTBUS_AGENT_ID` 是强受管身份；生产或长期运行的 Domain Agent 应通过 `agentbus agent launch` 或受管启动器注入 `AGENTBUS_AGENT_ID`。
5. **控制面超时预算 (Control Timeout Deadline)**：CLI 内置 `--control-timeout`（默认 5s），覆盖从身份解析、任务查询到事件写入的全流程。AGY `hooks.json` 中的 Hook timeout（默认 10s）必须大于该控制面超时，为输出 fail-closed 响应预留时间余量。
6. **安全 Stop 门禁 (Fail-Closed Gate)**：
   - 当 AgentBus Task 仍处于 `queued` 或 `running` 状态时，`Stop` Hook 强制返回 `{"decision": "continue", "reason": "..."}` 阻止 Agent 异常提前结束；
   - 当遇到控制面超时、网络或接口故障时，强受管 Agent 的 Stop 决策同样 fail closed 返回 `decision: continue`，并在 stderr 输出诊断；
   - 任务处于终态 (`succeeded`, `failed`, `canceled`) 或无 active Task 时，输出 `{}` 放行。
7. **任务终态事实源**：Hook 只观察仍可关联的 Task 生命周期并提供 Stop 门禁；`task complete`、`task fail`、`task cancel` 仍是任务终态的唯一事实源，不宣称 Hook 单独提供完整终态审计。
8. **纯净标准输出 (Clean Stdout)**：stdout 必须是且只能是单个合法 JSON Object，严禁夹带任何日志或诊断文本；所有诊断与告警信息统一输出至 stderr。

---

## 2. AGY 原生配置契约 (`.agents/hooks.json`)

基于 AGY 1.1.13 原生 Lifecycle Hook 规范，配置采用具名 Hook 对象，并在 `PreInvocation`、`PostInvocation` 和 `Stop` 下使用平铺的 Handler 数组：

```json
{
  "agentbus-lifecycle": {
    "PreInvocation": [
      {
        "type": "command",
        "command": "../AgentBus/bin/agentbus runtime agy-hook --event PreInvocation --control-timeout 5s",
        "timeout": 10
      }
    ],
    "PostInvocation": [
      {
        "type": "command",
        "command": "../AgentBus/bin/agentbus runtime agy-hook --event PostInvocation --control-timeout 5s",
        "timeout": 10
      }
    ],
    "Stop": [
      {
        "type": "command",
        "command": "../AgentBus/bin/agentbus runtime agy-hook --event Stop --control-timeout 5s",
        "timeout": 10
      }
    ]
  }
}
```

*说明与运维要求：*
- **执行目录**：Hook 执行时的工作目录为包含 `hooks.json` 的目录（即 `.agents/`），因此命令路径使用 `../AgentBus/bin/agentbus` 即可正确调用控制面 CLI。
- **超时保护**：AGY 的 `timeout: 10` 宽于 CLI 的 `--control-timeout 5s`，保证在控制面超时时 CLI 能完整输出 fail-closed JSON 而不被 AGY 提前 SIGKILL。
- **配置加载**：AGY CLI 在启动初始化时加载 Hook 配置；更新 `.agents/hooks.json` 后，必须重启正在运行的 AGY 进程才能生效。

---

## 3. CLI 契约与输入输出

```bash
agentbus runtime agy-hook \
  --event <PreToolUse|PostToolUse|PreInvocation|PostInvocation|Stop> \
  [--agent <agent-id|auto>] \
  [--task <task-id>] \
  [--socket <path>] \
  [--control-timeout <duration>]
```

### 输入约束与校验

- **Stdin**: 必须为单一有效 JSON Object，限制最大 1 MiB。空输入、非 Object（如 Array、Primitive 或 null）、尾随多余 JSON/字符（如 `{ } { }`、`{} trailing`）、超限输入均直接拒绝并退出非零。
- **`--event`**: 必填，严格限制为 AGY 当前 5 种事件类型（生产环境推荐使用 `PreInvocation`, `PostInvocation`, `Stop`）。
- **`--control-timeout`**: 默认 5s，必须 `> 0`。全流程使用该 Deadline。

---

## 4. 身份与任务解析流程

```mermaid
flowchart TD
    A[AGY Hook Triggered] --> B{解析 Agent 身份}
    B -->|显式 --agent 非 auto| C[采用指定 agentID, 强受管]
    B -->|AGENTBUS_AGENT_ID 存在| D[采用环境变量 agentID, 强受管]
    B -->|--agent auto 无环境变量| E{检查 TMUX_PANE}
    E -->|TMUX_PANE 为空 或 Daemon不可达/超时| UNM[未受管 Agent: stdout 输出 {} 并退出 0]
    E -->|TMUX_PANE 存在| F[查询 Daemon 匹配 ready Session]
    F -->|匹配数量 != 1| UNM
    F -->|唯一匹配| G[采用匹配 agentID, 受管]

    C --> H{解析 Active Task}
    D --> H
    G --> H

    H -->|显式 --task 指定| I[校验 TargetAgentID == agentID]
    I -->|不匹配或查询失败/超时| J{是否为 Stop 事件?}
    J -->|是| FAIL_STOP[Stop fail-closed: stdout 输出 continue, stderr 记录诊断]
    J -->|否| ERR[stderr 报错并退出非零]
    I -->|匹配成功| K[绑定该 Task]

    H -->|未指定 --task| L[查询 TargetAgentID == agentID 且 status in queued,running]
    L -->|查询失败/超时 或 >1 个活跃任务| J
    L -->|数量 == 0| M[无活跃任务: stdout 输出 {} 并退出 0]
    L -->|数量 == 1| K

    K --> N{Task 是否已处于终态?}
    N -->|是| M
    N -->|否| O[发送 POST /api/v1/tasks/:id/runtime-events]

    O -->|请求失败/超时| J
    O -->|请求成功| P{是否为 Stop 事件?}
    P -->|否| M
    P -->|是| Q{返回的最新 Task 状态是否仍 active?}
    Q -->|是| FAIL_STOP
    Q -->|否| M
```

---

## 5. Runtime Event Ingress API

### 端点

```http
POST /api/v1/tasks/{task-id}/runtime-events
```

### 请求结构

```json
{
  "agent": "quote-service",
  "runtime": "agy",
  "event": "Stop",
  "session_id": "conv-example-session-id",
  "payload": {
    "conversationId": "conv-example-session-id"
  }
}
```

### 服务端校验与处理

1. **Session 状态校验**：actor agent 必须处于 `ready` 会话状态。
2. **Runtime 匹配**：actor agent 的 Profile `runtime` 必须为 `agy`。
3. **目标权限匹配**：`task.TargetAgentID` 必须等于 actor agent。
4. **事件白名单**：`event` 必须为 5 种合法 AGY 事件之一。
5. **Payload 与 Body 严格校验**：请求 Body 及 `payload` 字段均必须为单个合法且非 null 的 JSON Object，拒绝 Array、Primitive、尾随多余 JSON 或畸变数据。
6. **事件存储与发布**：
   - 插入 `runtime.event_observed` 事件至 `task_events` 表；
   - 唤醒 `EventBroker`，通知正在 `task watch` 该任务的观察者。
7. **最新状态重查 (Anti-Stale Refetch)**：在 `AddEvent` 成功后重新查询 Task 实体，若查询失败直接返回错误（触发 CLI Stop fail-closed），严禁回退到陈旧状态。

---

## 6. 真实 E2E 验证结果

在生产联调与隔离测试中，AGY Hook V0.2 已通过完整端到端实测验证：

1. **生命周期事件链实测**：
   - 原生 AGY Hook 成功在任务执行期间连续记录 `PreInvocation`、`PostInvocation` 及 `Stop` 三类 `runtime.event_observed` 事件；
   - Event Stream 单调递增序列完整，Orchestrator 可实时通过 `task watch` 观测到 Agent 的执行进展。

2. **Stop 门禁与安全闭环验证**：
   - 在 Task 处于 `running` 状态时，AGY 尝试停机触发 Stop Hook，Hook 准确实时识别 active Task 并向 stdout 输出 `{"decision": "continue", "reason": "..."}`；
   - AGY 成功捕获该决策并在终端中展示系统拦截提示，阻止了会话的提前异常退出；
   - 在收到系统提示后，Agent 显式调用 `task complete` 上报结果，Task 成功流转至 `succeeded` 终态；随后再次触发 Stop Hook 时中性放行（输出 `{}` 并退出 0）。

3. **生产环境 Agent 启动与强受管身份实测**：
   - 正式 tmux pane 中的业务 Agent（如 `agentbus-agent` 与 `quote-service`）通过 `agentbus agent launch` 成功拉起，并准确注入了 `AGENTBUS_AGENT_ID` 强受管环境变量；
   - 针对慢启动 Runtime（如需网络初始化的大模型环境），建议使用 `--bootstrap-delay 10s`；若进程初始化耗时较长导致首个 Bootstrap 通知未被消费，在进程稳定后执行 `agentbus agent bootstrap --id <agent-id>` 即可无缝完成 Ready 握手；
   - 两大正式 Agent 均在生产控制面中顺利产生 Pre/Post Invocation 观察事件并完成了端到端协作验收。
