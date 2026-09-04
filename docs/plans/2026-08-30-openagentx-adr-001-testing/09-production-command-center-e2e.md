---
doc_type: test_task
status: blocked
owner: openagentx
test_id: T09
updated_at: 2026-09-05
---

# T09：手机与 PC 生产指挥台端到端

## 目标

通过 `https://agentx.oneaxe.cn/` 验证 OpenAgentX 指挥台既能监控，也能作为手机远程指挥入口。

## 用户旅程

1. 手机首次登录并安装 PWA；PC 使用另一 Session 登录。
2. 查看 Organization、Agent connectivity/availability/delivery readiness、Worker heartbeat、Backend health 和任务状态。
3. 选择逻辑 Agent，创建 Task A，查看时间线和实时状态。
4. 对 waiting_input 发送补充 Message。
5. 在待办中查看 Approval 的动作、范围、目标 run 和有效期，执行批准或拒绝。
6. 取消一个可取消 Task，确认页面、API 和 Event 一致。
7. 提交 Task B，确认 Agent 无需 tmux 自动接活。
8. 手机断网，验证所有写操作禁用且不进入后台队列；恢复联网后手工重试。
9. 退出、撤销 Session，并验证 PC/手机 Session 隔离和 idle/absolute timeout。

## 视口与质量

- 390x844、412x915、1440x900；
- 无文本裁切、控件重叠、布局跳动或不可达操作；
- SSE 长连接经 Nginx 不缓冲，HTTPS 证书、安全头和 Cookie 属性正确；
- 页面不得展示 Session Token、fencing token、密码或私密 Runtime 输出。

## 通过条件

手机能够独立完成发指令、回复、审批、取消和跟踪结果；PC 与手机看到同一持久事实，PWA 离线严格 fail closed。

## 验证结论

- 已确认生产 HTTPS/Nginx/SSE、PWA 基础资源、认证与 Session、生产 Task/Message/取消 API、事件流和 Worker 持续在线等基础链路；真实 AGY A/B/C 均有脱敏归档，A1 OAuth/EOF 失败后 A2/A3 成功，B 不可达代理 rc=2 且恢复后 wrapper 版本探针 rc=0。
- 真实长运行取消已通过：观察到 `sleep 30` 后发送取消，任务完成 `running → cancel_requested → canceled`，事件包含 control mailbox 接收、`runtime.agy.result` 和 `run_attempt.finished`；Worker 保持同一实例并能继续接单。
- `t09-final-pc` 与 `t09-final-mobile` 是两个独立生产浏览器 Session，均能创建/跟踪任务并读取持久事实；手机离线时写控件禁用，强制点击无请求，恢复后无自动重放。`beforeinstallprompt` 安装入口通过浏览器侧回归。
- Worker graceful release 重测通过：受控重启在 2 秒内完成新 generation 接管，日志无 `409 logical Agent already has a valid Active Worker`，旧 Worker 写入 `worker.released`、原子 offline 并推进 fencing；12 秒心跳、active run 和 quote-service mailbox 均正常。
- 2026-09-05 再次尝试 OS 级 PWA 安装：受控 Chrome 页面确认 manifest/Service Worker/installability，但当前自动化连接无法操作浏览器外壳安装菜单，`display-mode: standalone` 仍为 `false`；证据归档于 `docs/reports/validation/evidence/t09/browser-20260905-pwa/`，不伪造安装 PASS。
- 替代路径调查确认 DISPLAY=:1 的真实 Google Chrome 可打开生产页面，工具栏安装图标可见；但缺少浏览器外壳输入控制工具，自动化注入未触发安装对话框，仍无 standalone/installed 证据。需人工点击安装或提供等价浏览器外壳自动化能力。
- 隔离 `/tmp` profile + XTest 最终尝试因 Chrome `inotify_init() failed: Too many open files (24)` 未创建可操作窗口；未接触用户 profile，仍需人工点击或提供稳定 browser-shell 自动化能力。
- 受控清理匹配的 headless 资源后重试，隔离 Chrome 可创建窗口但代理报 `ERR_NO_SUPPORTED_PROXIES`，生产页面未加载，仍无安装证据；需支持该代理的稳定 GUI profile 或人工完成安装。
- 显式清除浏览器代理后的隔离 Chrome 可加载生产登录壳体，但 XTest 未获窗口焦点且未触发安装对话框，仍无 standalone/installed 证据；需人工或可用 browser-shell 自动化完成安装确认。
- T09 暂不标记整体 PASS；当前唯一阻塞是 OS 级 PWA 安装证据，需在真实受控浏览器中记录安装前后的 standalone/installed 状态。
