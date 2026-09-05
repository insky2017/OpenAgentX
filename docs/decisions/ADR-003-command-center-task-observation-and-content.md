---
doc_type: decision
status: accepted
canonical: true
owner: openagentx
updated_at: 2026-09-05
---

# ADR-003: 指挥台任务详情、运行观察与安全内容呈现

## 状态

已接受 (Accepted)

## 日期与决策者

- 日期：2026-09-05
- 决策者 / 所有者：OpenAgentX Team / OpenAgentX Maintainer

## 背景

ADR-001 已经确定指挥台通过认证后的 Observe、Control 和 Admin API 管理组织、Task、Approval 和 Worker。当前指挥台的任务页面把每个 Task 的完整正文直接平铺，Task 详情接口主要返回 Task 与 Message，SSE 事件只触发 overview 刷新。用户难以在手机和 PC 上同时浏览任务、跟踪 RunAttempt、判断 Runtime 是否仍在运行和核对最终结果。

Agent 产生的 Task、Message、Result、Runtime Event 和诊断内容是不可信输入，可能包含 Markdown、外链、秘密片段和超长输出。内容呈现必须在保留取证能力的同时维持 CSP、PWA 离线写保护和敏感信息边界。

## 决策

### 1. 任务使用列表到详情的工作台

任务页分为列表区和详情区。列表只显示短标题或首行摘要、Task ID、目标 Agent、状态、更新时间和待处理标记，并支持 Agent、状态、时间和文本筛选以及服务端分页。

桌面端使用左列表右详情；手机端使用列表到全屏详情的推送式导航。选中 Task 写入 URL 查询参数或 hash，SSE 刷新后保持选择；Task 被删除、终止或不再匹配筛选时显示明确提示并回到列表。回复、审批和取消仍在 Task 详情上下文中完成，写操作继续使用 CSRF、角色、Idempotency-Key 和 expected-version。

### 2. 详情提供可核对的运行观察

详情分为“内容、对话、运行、结果”视图：

- 内容：Task 正文、目标 Agent、执行约束和原始文本入口；
- 对话：Message、ApprovalRequest、Decision 和每次 turn 的输入摘要；
- 运行：Mailbox、RunAttempt、Worker、配置核对、Runtime 启动、公开输出、终态和 reconcile 时间线；
- 结果：结果正文、错误、usage、side-effects-known、产物链接和失败诊断。

运行时间线只呈现可证明事实，不保存或展示模型隐藏推理。Adapter 能力为 `Streams: false` 时显示启动、运行时长和最终结果，不伪造实时 token 或工具进度。输出事件按块合并并有总量上限，截断必须在界面上标明；状态、错误、取消、审批和结果事件不能被输出限量丢弃。

### 3. Observe API 提供快照、历史和实时增量

现有 Task、RunAttempt 和 Event 查询扩展为可分页、按 Task/Run/Agent 过滤的读取接口。Task 详情返回关联 Messages、RunAttempts、事件游标和脱敏诊断；Run 详情返回 ResolvedExecutionSpec 的非敏感字段、Worker generation、配置版本、状态变化和最终 TurnResult。

SSE 使用 Event Journal sequence 和 `Last-Event-ID` 继续回放。前端对事件去重，快照和游标不一致时重新加载详情；网络断开时显示连接状态，恢复后从最后游标补齐。页面刷新、浏览器窗口关闭或手机锁屏不取消 Runtime。

新增 Runtime 输出进入持久事件前执行字段白名单和敏感值脱敏；原始 stderr、凭据、Session Token、隐藏推理和完整环境变量不得进入 Journal、API 或浏览器缓存。需要取证的完整结果通过认证后的短期产物读取接口提供，并遵守大小、权限和过期策略。

### 4. Markdown 统一安全渲染

Task content、Task result、Message 和 Approval 描述使用同一个 React Markdown 组件，支持 CommonMark、GFM 表格、任务列表、引用、代码块和行内代码。保留“渲染 / 原文”切换和复制原文入口，解析失败回退纯文本。

渲染器不使用 `dangerouslySetInnerHTML` 或 `rehype-raw`；禁止脚本、事件属性、内联样式、`javascript:`、`data:` 和未经授权的外部资源。链接只允许安全协议；图片首版只允许认证后的同源产物或明确的安全占位，不自动加载外部图片。代码块和表格在移动端局部横向滚动，长内容按块渲染，不能造成页面整体横向溢出。

Markdown 解析、产物展示和复制操作不得改变现有 CSP，不得把敏感业务数据加入 Service Worker 缓存；离线时继续禁止写操作和控制命令排队。

### 5. 响应式、无障碍与阅读稳定性

关键页面在 390x844、412x915 和 1440x900 完成真实浏览器检查。列表使用明确的 `list`/`listitem` 语义，选中项提供 `aria-selected`，详情返回后恢复焦点。SSE 更新不能抢夺用户正在阅读的滚动位置；有新输出时显示可点击的“跳到最新”提示。

任务摘要、运行阶段、连接状态、错误和截断状态使用文本与可访问状态同时表达，不能只依赖颜色。空态、加载态、无匹配、权限不足、断线和历史回放失败必须有独立界面状态。

## 迁移与后果

- 当前平铺任务卡迁移为摘要列表，保留现有回复和取消入口；旧 URL 无选中 Task 时默认打开列表。
- 当前 overview 刷新机制继续作为快照兜底，新增详情接口和事件游标后逐步减少整页刷新。
- Markdown 渲染会增加前端依赖和长内容处理成本，但统一组件可以集中完成安全策略、原文切换和移动端布局。
- 运行观察只显示 Runtime Adapter 实际提供的能力；缺少流式协议时，界面明确显示“等待结果”，不推测内部过程。

## 验收矩阵

| 类别 | 验收断言 |
|---|---|
| 正向流程 | 任务列表进入详情，查看内容、对话、RunAttempt、结果并完成回复/审批/取消 |
| 状态不变量 | 详情选中态跨 SSE 刷新保持；显示的已应用配置和运行状态均有服务端事实支撑 |
| CAS/幂等 | 重复 SSE、重复写操作和详情重试不产生重复 Message、Decision 或控制命令 |
| 失败路径 | 断线、回放缺口、Markdown 解析失败、超长输出、权限不足均有安全降级 |
| 竞态场景 | 运行结束与取消/回复同时到达、Task 状态变化与当前详情不匹配时按服务端 version 处理 |
| 证据格式 | 三种视口的 DOM、渲染、触控、SSE 重连、原文复制、CSP、离线写保护和敏感信息检查均有报告 |

## 相关文档

- [ADR-001: OpenAgentX 组织控制面、Resident Worker 与移动指挥台决策](ADR-001-resident-agent-worker-runtime-observability.md)
- [ADR-002: Runtime 网络配置、应用与诊断](ADR-002-agent-cli-proxy-environment.md)
- `internal/api/panel/handler.go`
- `web/src/main.jsx`
- `web/public/sw.js`
