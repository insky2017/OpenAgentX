---
doc_type: validation_report
status: passed
owner: openagentx
test_id: T09
validated_at: 2026-09-05
---

# OpenAgentX ADR-001 T09 生产指挥台端到端验证

## 结论

T09 `PASS`。生产入口、任务控制、真实 AGY 运行、取消、双 Session、移动离线 fail-closed 和 graceful Worker release/restart 均已取得可审计证据。A1 首次 OAuth/EOF 环境失败后按规则重试，A2/A3 成功；B 证明不可达代理以 rc=2 fail-closed，恢复后正式 wrapper 版本探针 rc=0；C 观察到真实 `sleep 30` 进程后发起取消，Task 从 `running` 经 `cancel_requested` 到 `canceled`，无残留，Worker 保持同一实例并继续接单。

OS 级 PWA 安装由产品 owner 在真实手机完成并确认。该人工验收的记录不虚构手机型号、操作系统、浏览器版本、截图或 standalone 检测值；既有自动化已验证 manifest `display=standalone`、Service Worker 和 `beforeinstallprompt` 安装入口。受控浏览器自动化无法操作浏览器外壳安装菜单的记录仅作为历史调查，不再构成 T09 阻塞。

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
| 认证后手机/PC 完整页面链路 | 通过；桌面与移动独立 session 均可进入认证后生产页面并创建/跟踪任务 |
| 手机与 PC 两个独立 Session 共享持久事实 | 通过；`t09-final-pc` 与 `t09-final-mobile` 均持有独立浏览器上下文并读取到持久任务事实 |
| 认证后页面敏感信息隔离 | 通过抽检；页面正文未暴露密码、Session Token、Cookie、CSRF 或 fencing token |
| 手机断网写操作 fail closed | 通过；离线写请求被明确拒绝，恢复在线后未发生自动补发 |

本轮证据已归档到 `docs/reports/validation/evidence/t09/browser-20260905/` 与 `docs/reports/validation/evidence/t09/browser-20260905-pwa/`；历史截图仍保留在 `docs/reports/validation/evidence/t09/`，不作为本轮双 Session 或离线结论的唯一依据。

## 本轮浏览器补充证据

本轮使用真实 Chrome DevTools MCP 登录生产站点并验证认证后页面。桌面认证后截图保存为 `docs/reports/validation/evidence/t09/browser-20260901/pc-authenticated-1440x900.png`；将同一真实会话切换到触控移动视口 `412x915` 后，认证首页 DOM 显示组织态势、`Quote Service` 状态、工作台入口和底部主导航，截图保存为 `docs/reports/validation/evidence/t09/browser-20260901/mobile-authenticated-412x915.png`。页面 DOM 中未观察到密码、Session Token、Cookie、CSRF、fencing token 或代理凭据等敏感字段。

本轮又补充了两个独立 `agent-browser` session：`t09-final-pc` 与 `t09-final-mobile`。两端均稳定访问 `https://agentx.oneaxe.cn/`，并在任务列表中读取到持久任务事实；PC 与 mobile 各自创建的任务均达到 `succeeded`，可证明独立 Session 的生产页面与任务读取链路。

同一组会话里又做了 PWA 可安装形态的浏览器侧复核：`link[rel=manifest]` 指向 `https://agentx.oneaxe.cn/manifest.webmanifest`，Service Worker 注册数为 1；`beforeinstallprompt` 可调度、安装入口可显示并在关闭后隐藏。但 `matchMedia('(display-mode: standalone)')` 仍为 `false`，`navigator.getInstalledRelatedApps()` 返回空数组，当前只能确认“可安装”，不能确认“已完成系统级安装”。

移动端离线验证使用 DevTools 将网络设为 Offline 后，`Quote Service` 工作台的输入框与发送按钮被禁用；脚本层强制点击也没有产生 fetch/API 请求，恢复网络后未观察到自动重放。对应证据保存于 `docs/reports/validation/evidence/t09/browser-20260905/` 的 offline 快照、点击结果和网络记录。

2026-09-05 的浏览器自动化复验确认 HTTPS、manifest、Service Worker 注册数为 1 和安装入口；当时 `display-mode: standalone` 为 `false`，因为该会话没有完成系统安装。受控连接不能操作浏览器外壳安装菜单，故相关截图、DOM 与调查记录不单独证明已安装。产品 owner 后续在真实手机完成 PWA 安装并确认，attestation 见 `docs/reports/validation/evidence/t09/browser-20260905-pwa/owner-mobile-pwa-install-attestation-20260905.txt`；该人工验收解除 OS 安装阻塞。

替代路径调查确认 DISPLAY=:1 上存在真实 Google Chrome 图形窗口，并可通过 `google-chrome --new-window` 打开生产页面；但本机缺少浏览器外壳输入控制工具，现有 DevTools 连接也不能操作 Chrome 菜单。工具栏安装图标可见但未能触发原生安装对话框，未读取或提交自动填充密码，未声称系统已安装。详见 `docs/reports/validation/evidence/t09/browser-20260905-pwa/gui-chrome-alternative-20260905.txt` 及对应截图。

最后一次隔离 profile + XTest 尝试在 `/tmp` 启动真实 Chrome，进程因 `inotify_init() failed: Too many open files (24)` 未创建可操作窗口；未接触用户 Chrome profile。现有 GUI 窗口的 XTest 菜单点击仍无效，故没有 OS 安装/启动证据。详见 `docs/reports/validation/evidence/t09/browser-20260905-pwa/isolated-profile-xtest-final-20260905.txt`。

在受控清理 63 个匹配的 headless DevTools/临时 profile 进程后，inotify 资源恢复；新的 `/tmp` 隔离 Chrome 可创建窗口，但通过 `set_proxy_server` 注入的代理在 Chrome 中报 `ERR_NO_SUPPORTED_PROXIES`，页面未加载，XTest 无法进入安装流程。未接触用户 Chrome profile，未读取凭据。详见 `docs/reports/validation/evidence/t09/browser-20260905-pwa/resource-cleanup-isolated-retry-20260905.txt`。

随后对新的 `/tmp` 隔离 profile 显式清除 HTTP(S)/ALL proxy 环境后直连，Chrome 页面可渲染生产登录壳体并显示安装图标；XTest 聚焦/点击未被窗口管理器接受（`_NET_ACTIVE_WINDOW=0x0`），没有原生安装对话框、standalone 或 installed-app 启动证据。详见 `docs/reports/validation/evidence/t09/browser-20260905-pwa/resource-cleanup-direct-no-proxy-20260905.txt`。

## 生产 API 多轮与持续 Worker

真实 AGY A/B/C 证据已归档于 `docs/reports/validation/evidence/t09/agy-graft-20260905/`。A1 `task-5b114f4e-9ecb-4326-8a46-0d89287efe98` 为一次 OAuth/EOF 环境失败；按规则重试的 A2 `task-bd2ffba7-477e-434e-be44-1489c9ebd452` 与 A3 `task-b4d7022a-7926-4ee5-b536-7d1b82a0098a` 均为 `succeeded`，Worker 同实例、generation 20 保持在线。A 事件含 `runtime.agy.init`、`runtime.agy.step_update`、`runtime.agy.result`、`run_attempt.finished` 和 `task.settled`。

B 的不可达代理探针明确 rc=2 并拒绝启动；代理恢复后正式 wrapper `agy-graft --version` rc=0，版本 `1.1.26`。这证明 wrapper、代理选择和恢复后的健康探针均可审计，不把一次短暂上游失败误写成 wrapper 永久故障。

后续任务 `task-3a10e0ab-76b0-4c4b-8c68-5e28c7b84cc9` 在取消后创建并达到 `succeeded`，证明 Worker 可继续接单。

## Worker graceful release/restart 重测

修复后由独立测试复验 `systemctl --user restart openagentx-quote-service-worker.service`：基线为 generation 24、fencing 47、online；重启后约 2 秒内服务恢复 active，`NRestarts=0`，日志扫描没有出现 `409 logical Agent already has a valid Active Worker`。旧 Worker 在退出前产生 `worker.released`，事务性写入 offline、`lease_until=updated_at` 并将 fencing 47 推进为 48；新 Worker 随后以 generation 25、fencing 49 注册并完成初始 heartbeat。整个接管耗时约 2.96 秒，无需等待旧 lease 自然过期。

接管后 12 秒复核保持 `NRestarts=0`，heartbeat 持续更新，active RunAttempt 为 0，quote-service mailbox 无 pending/claimed 项。脱敏命令、事件序列和状态快照见 `docs/reports/validation/evidence/t09/worker-graceful-release-20260905.txt`。Admin stop 页面操作未在本轮重复执行，因为需要新的 owner Session、CSRF 和幂等键；代码级 release-before-ack 顺序已由 UDS/mTLS/Runner 测试覆盖。

## 取消实战证据

| 项目 | 结果 |
|---|---|
| Task | `task-11759158-003f-4492-bdfe-af1633da8076` |
| 创建与运行 | HTTP 200；进入 `running`、version 2；进程摘要发现 `sleep 30` |
| 取消 | 返回 `cancel_requested`、version 3；control mailbox 被当前 run 接收 |
| Event/SSE | 包含 `task.cancel_requested`、`mailbox.cancel_created`、`mailbox.claimed`、`mailbox.accepted`、`runtime.agy.result`、`run_attempt.finished`、`task.settled` |
| 最终事实 | Task `canceled`、version 4；真实长任务无残留进程 |
| Worker | 同一 `instance_id`、generation 20 保持 online；取消后任务继续达到 `succeeded` |

取消链路已覆盖真实运行中的 AGY 进程；报告只引用脱敏状态、事件名和进程摘要，不记录密码、Cookie、Session Token、代理凭据、私钥或完整 fencing token。

## 代码与运行基线

| 项目 | 结果 |
|---|---|
| 当前生产二进制 SHA-256 | `03ea07333ffa8e59b4649387f4114c8e0ba62321cb85952c7a9675f15665457a` |
| daemon | `openagentx.service=active` |
| quote-service Worker | `openagentx-quote-service-worker.service=active` |
| ADR-001 | SHA-256 `587e9b8f27ddeb68aae8e5a507fefc10973739c8dd2c3def86e2c0bbc51db403`，未修改 |

T09 期间发现的 Panel Message 修复已完成最小回归、重新编译和部署：Panel 前端请求不含 `sender_principal_id` 时，服务端从认证 Session 注入发送方身份；生产 HTTPS 的已认证页面正常回复后得到 HTTP 200，隔离 Fake Worker 使 Task 从 `waiting_input` 续接至 `succeeded`。

## OS 安装验收

产品 owner 已在真实手机确认 PWA 安装完成。结合既有 manifest `display=standalone`、Service Worker 和安装入口自动化证据，T09 的 OS 安装条件已满足；浏览器外壳自动化限制不再阻塞 T09。

## 证据索引

- AGY A/B/C 与真实 `sleep 30` 取消：`docs/reports/validation/evidence/t09/agy-graft-20260905/`
- PC/mobile 双 Session、离线 fail-closed：`docs/reports/validation/evidence/t09/browser-20260905/`
- `beforeinstallprompt` 与 PWA runtime/installability：`docs/reports/validation/evidence/t09/browser-20260905-pwa/`
- Worker graceful release/restart：`docs/reports/validation/evidence/t09/worker-graceful-release-20260905.txt`
- 真实手机安装 owner attestation：`docs/reports/validation/evidence/t09/browser-20260905-pwa/owner-mobile-pwa-install-attestation-20260905.txt`
