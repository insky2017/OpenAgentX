---
doc_type: decision
status: accepted
canonical: true
owner: openagentx
updated_at: 2026-09-05
---

# ADR-002: Runtime 网络配置、应用与诊断

## 状态

已接受 (Accepted)

## 日期与决策者

- 日期：2026-09-05
- 决策者 / 所有者：OpenAgentX Team / OpenAgentX Maintainer

## 背景

ADR-001 已经确定 Worker 通过 Runtime Adapter 启动 AGY、CodeBuddy 和 ACP 等外部 Runtime。不同 Backend 的网络要求可能不同：有的模型调用需要代理，有的内部服务必须直连，同一主机上还可能同时运行多套出口配置。

当前 Worker 配置中的 `runtime_backends[].options` 主要由部署文件提供，Adapter 内部已有 `Environment` 能力；AGY 使用正式 `agy-graft` wrapper，wrapper 会解析 `AGY_GRAFT_*`、受控配置文件和代理模式，并在启动下游进程前清除标准代理变量。代理配置不能继续依赖交互式 shell、临时 `export` 或不可观察的主机环境。

本 ADR 解决的是 Runtime 子进程的网络出口和配置应用问题。它不改变 Task、Mailbox、RunAttempt 的业务寻址语义，也不允许浏览器直接连接 Worker 或 Runtime。

## 决策

### 1. 控制面管理期望配置，Worker 报告实际配置

指挥台是日常配置入口，daemon 数据库保存代理方案、Backend 绑定关系和配置版本。`agent.yaml` 保留 Agent 身份、Worker 连接、Runtime 可执行文件和部署路径等启动条件；已有本地配置通过一次性导入进入控制面，导入结果必须可审计。

每个 Agent 的每个 Backend 有一个 `NetworkPolicy`：

| 字段 | 语义 |
|---|---|
| `mode` | `inherit`、`direct` 或 `named_profile` |
| `profile_id` | 命名代理方案 ID；仅 `named_profile` 使用 |
| `profile_version` | 期望的不可变版本 |
| `direct_destinations` | 必须直连的受控地址集合 |
| `secret_ref` | 代理认证秘密引用，不是秘密值 |
| `adapter_constraints` | 允许使用该策略的 Adapter/Backend 能力 |

`inherit` 是兼容现有部署环境的显式模式，不代表可以任意继承全部 Worker 环境；Worker 必须按 Adapter 白名单清理和合并环境。`direct` 只表示该 Runtime 不注入代理配置，不能声称绕过主机 TUN、系统路由或外部网络策略。

ProxyProfile 保存名称、协议、主机、端口、适用 Worker/Backend 范围、直连规则和版本。凭据通过 `secret_ref` 关联受控秘密存储，不进入 Agent YAML、Event Journal、浏览器响应、argv 或普通日志。

### 2. 配置采用草稿、测试、发布、回执四阶段

指挥台提供 Runtime 配置页和代理方案库。用户可以选择已有方案、创建草稿、替换秘密、执行目标 Worker 测试、发布版本和回退到旧版本。

每次变更生成新的配置版本，不原地修改已发布版本。版本状态至少包括：`draft`、`testing`、`ready`、`pending`、`applied`、`failed`、`stale`。页面必须同时显示期望版本和实际生效版本、目标 Agent/Backend/Worker generation、最近测试结论，以及当前 Run 是否仍固定使用旧版本。

配置写入、发布、测试和回退均使用现有 CSRF、角色、Idempotency-Key 和 expected-version/CAS 约束。重复提交同一命令不得生成重复生效记录；版本冲突必须提示用户刷新，而不是覆盖他人的修改。

### 3. 活动执行固定网络配置

Scheduler 创建 RunAttempt 时解析 `NetworkPolicy`，并把 `profile_id`、`profile_version`、策略摘要和脱敏 digest 写入 `ResolvedExecutionSpec` 与 RunAttempt。Turn 启动后不热修改其环境。

配置发布只影响新的 RunAttempt。运行中的 RunAttempt 完成、取消或进入 `uncertain` 后，下一次 turn 才使用新版本。配置、秘密或适用范围变化使 preflight 授权摘要失效时，必须重新检查授权条件。

Worker 收到配置后，在有效 Worker Session、generation 和 fencing token 下应用；实际应用回执必须带配置版本和运行时摘要。配置回退仍然是新版本，不能把旧版本直接改成当前版本。

### 4. Adapter 按能力接线，不使用任意 shell 命令

Adapter Descriptor 声明支持的网络模式，指挥台只显示已声明且已验证的选项。配置不得接受任意 shell 前缀、命令字符串或自由环境变量集合；wrapper 使用已注册的绝对路径、固定参数模板和发布时记录的 SHA-256。

AGY 的正式路径使用 `agy-graft` 的受控 `AGY_GRAFT_CONFIG` 或等价部署绑定。配置文件必须是 Worker 用户可读的普通 `0600` 文件；wrapper 的模式、端点优先级、凭据处理和清理标准代理变量保持其已验证契约。CodeBuddy、ACP 等其他 Adapter 分别声明自己真正支持的环境变量和直连规则。

环境合并必须去重并拒绝改变进程加载或解释行为的危险键，例如 `LD_PRELOAD`、`BASH_ENV`、`NODE_OPTIONS` 和未经声明的 `PATH`。配置不得修改 Worker 控制面连接环境。

### 5. 诊断在目标 Worker 上完成

“测试连接”不是成功任务的替代品。它由目标 Worker 使用目标 Adapter 的正式启动路径执行，分层报告配置解析、秘密可用性、代理端点、直连目标和 Runtime 健康检查；需要真实模型调用的试跑必须明确提示并按普通 Task 记录。

可恢复的代理或 Backend 配置错误应使 Backend 标记为 `unavailable` 或 `degraded`，Worker 保持控制连接并继续报告状态，不能因为某个 Backend 无法出网而丢失前端修复入口。身份认证、Worker 控制通道或租约失效仍按 ADR-001 的失败路径处理。

诊断和 Event Journal 记录 profile ID、版本、模式、适用 Backend、Worker generation、健康状态、耗时和脱敏错误；不记录代理密码、完整 URL 凭据、Session Token 或未脱敏 Runtime 输出。

### 6. 指挥台 API 与观察边界

新增配置 API 必须区分 Observe、Control 和 Admin 权限：读取可用方案和实际状态走 Observe；创建草稿、测试和发布走受授权的 Control；秘密管理和 Worker 级操作按 owner/Admin 权限执行。浏览器只调用 daemon API，Worker 配置通过现有 Worker Control Channel 传递。

API 需要提供 Backend 能力、ProxyProfile 脱敏列表、Agent/Backend 的期望与实际配置、测试结果、发布历史和失败诊断。任何“已生效”状态必须来自 Worker 回执，不能由前端提交成功推断。

## 迁移与后果

- 现有 `agent.yaml` 配置先以导入结果建立初始版本；没有代理配置的 Backend 保持显式 `inherit`，避免一次升级改变出网行为。
- 代理秘密迁移后只保留引用和脱敏摘要；真实凭据不写入 Git、YAML、Event Journal、PWA 缓存或截图。
- 代理配置变更增加了版本、测试和 Worker 回执的维护成本，但换来了可重复执行、可回退和可排障的运行环境。
- 健康页面同时显示“配置有效”“Worker 已应用”和“Runtime 最近可用”，不能合并成一个状态。

## 验收矩阵

| 类别 | 验收断言 |
|---|---|
| 正向流程 | 指挥台创建方案、目标 Worker 测试、发布并创建真实 Task；RunAttempt 使用同一 profile/version |
| 状态不变量 | 活动 Run 的网络配置不可变；`applied` 必须有 Worker 回执；敏感值不进入事件和报告 |
| CAS/幂等 | 并发发布产生版本冲突；重复发布、回执和回退不会重复生效 |
| 失败路径 | 错误端点、秘密缺失、文件权限错误、模式不匹配进入 `failed/unavailable`，不推断业务成功 |
| 竞态场景 | 发布与 StartTurn、回退与活动 Run、Worker 重启与旧回执按 version/generation/fencing 结算 |
| 证据格式 | 记录源码/二进制、配置版本、Worker instance/generation、Adapter/wrapper 版本、脱敏 argv/诊断和实际结果 |

## 相关文档

- [ADR-001: OpenAgentX 组织控制面、Resident Worker 与移动指挥台决策](ADR-001-resident-agent-worker-runtime-observability.md)
- [ADR-003: 指挥台任务详情、运行观察与安全内容呈现](ADR-003-command-center-task-observation-and-content.md)
- `deploy/agy/README.md`
- `deploy/agy/agy-graft`
- `docs/plans/2026-08-30-openagentx-adr-001-test-plan.md`
