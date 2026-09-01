---
doc_type: validation_report
status: blocked
owner: openagentx
test_id: T09
validated_at: 2026-09-01
---

# OpenAgentX ADR-001 T09 生产指挥台端到端验证

## 结论

T09 的生产入口基础设施与任务控制 API 已通过抽检，但端到端手机指挥台整体阻塞。`https://agentx.oneaxe.cn/` 已验证 HTTPS/Nginx、认证、SSE、PWA 基础资源、生产 Task/Message 和 Worker 常驻；取消 API 在真实 `running` Task 上也完成了事务、Mailbox 和 Worker 结算闭环。

本轮不足以证明手机首次登录与安装 PWA 后可独立完成创建、回复、审批、取消、跟踪，也没有 PC 与手机两个独立 Session 观察同一持久事实的成对证据；认证后 `412x915` 浏览器交互和离线 fail-closed 也未独立闭环。另一个未通过子项是“真实长运行 AGY turn 被取消”：测试任务在执行预定的 `sleep 30` 之前，因 AGY eligibility 请求 DNS 失败而退出，故不能把这次结果解释为真实长运行进程中断成功。按测试计划，T09 保持 `BLOCKED`，不得进入 T10 的最终 GO 判定。

## 生产入口与浏览器结果

| 范围 | 结果 |
|---|---|
| HTTP → HTTPS、TLS、安全响应头 | 通过 |
| PWA Manifest、图标、Service Worker | 通过 |
| SSE 生产流、持续事件与 keepalive | 通过 |
| 未认证 Observe/Worker API 边界 | 按预期拒绝或不暴露 |
| owner 登录、Session、CSRF | 通过 |
| 登录与 Session 基础验证 | 通过；不记录密码、Cookie、CSRF 或 Session 值 |
| 生产 Task 创建、详情、最终状态与多轮 Message | 通过；接口、SSE 与持久事实已核对 |
| 认证后手机/PC 完整页面链路 | 证据不足；现有截图不能构成一次可审计的端到端交互记录 |
| 手机与 PC 两个独立 Session 共享持久事实 | 证据不足；未取得成对 Session/浏览器证据 |
| 认证后页面敏感信息隔离 | 证据不足；不可用未认证登录页或接口脱敏结果替代认证后页面审计 |
| 手机断网写操作 fail closed | 证据不足；没有本轮生产浏览器离线、禁用写操作及恢复后手工重试记录 |

归档目录 `docs/reports/validation/evidence/t09/` 现有 `390x844` 与 `1440x900` 截图；本轮 `412x915` 仅保存了登录页。它们可作为历史布局线索，不能替代本关所需的认证后完整交互、双 Session 和离线浏览器证据。

## 生产 API 多轮与持续 Worker

生产多轮任务 `task-ec41aea8-2a04-43f8-932b-711bec5b1fd4` 最终为 `succeeded`、`version=9`，包含 1 条 instruction、2 条 supplement、3 个 RunAttempt，并复用同一 provider session。Task 完成后 Worker 未退出，仍可继续接收后续任务。

## 取消实战证据

| 项目 | 结果 |
|---|---|
| Task | `task-89ff5a4b-0a8c-4d0b-ba3b-9b635a0a0b94` |
| 创建 | HTTP 200；观察到 `running`、version 2 |
| 取消 | HTTP 200；返回 `cancel_requested`、version 3、`expected_version=2` |
| Event/SSE | 包含 `task.created`、`task.running`、`run_attempt.started`、`task.cancel_requested`、`mailbox.cancel_created`、`mailbox.claimed`、`mailbox.accepted` |
| 最终事实 | Task `canceled`、version 4；RunAttempt `uncertain` |
| Worker | 取消后及约 3 分钟复核仍在线，实例未更换，持续 heartbeat |

`RunAttempt=uncertain` 的原因是 AGY eligibility DNS 失败并被终止；实际未执行到 `sleep 30`。这既证明系统在副作用未知时保持 fail-closed，也意味着本次不能证明真实长运行取消能力。

原始脱敏证据目录：`/run/user/1000/t09-cancel2.XK35cE`。报告不记录密码、Cookie、Session Token、代理凭据、私钥或完整 fencing token。

## 代码与运行基线

| 项目 | 结果 |
|---|---|
| 当前生产二进制 SHA-256 | `43a113903dff68b3303833152ea250d3819ae8261183ba6997fdbcd7e07a3a01` |
| daemon | `openagentx.service=active` |
| quote-service Worker | `openagentx-quote-service-worker.service=active` |
| ADR-001 | SHA-256 `587e9b8f27ddeb68aae8e5a507fefc10973739c8dd2c3def86e2c0bbc51db403`，未修改 |

T09 期间发现的 Panel Message 修复已完成最小回归、重新编译和部署：Panel 前端请求不含 `sender_principal_id` 时，服务端从认证 Session 注入发送方身份；生产 HTTPS 的已认证页面正常回复后得到 HTTP 200，隔离 Fake Worker 使 Task 从 `waiting_input` 续接至 `succeeded`。该修复不解除本报告列出的手机/PC 全旅程、离线与真实长运行取消阻塞。

## 解除阻塞条件

在保持当前取消状态机和 fail-closed 语义不变的前提下，先以独立手机与 PC Session 补齐登录、安装、创建、回复、审批、取消、跟踪、同一持久事实以及离线禁写/恢复后手动重试的浏览器记录；随后恢复 AGY eligibility 网络，使用无副作用且明确持续超过取消窗口的任务，重新取得：

1. Task 进入 `running` 且 RunAttempt 已启动；
2. 取消请求及 control mailbox 被当前 run 接收；
3. 目标 AGY 进程在长运行阶段被中断；
4. Task 最终 `canceled`，Worker 继续在线，Event Journal 与页面状态一致。

在取得上述 L3/L4 证据前，T09 保持 `BLOCKED`。
