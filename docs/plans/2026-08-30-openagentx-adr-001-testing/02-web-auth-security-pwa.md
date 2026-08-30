---
doc_type: test_task
status: completed
owner: openagentx
test_id: T02
updated_at: 2026-08-30
---

# T02：Web 登录、安全、SSE、PWA 与离线

## 目标

验证指挥台只能由有效密码 Session 访问，RBAC、CSRF、SSE、Session 生命周期、PWA 和离线 fail-closed 符合 ADR。

## 测试矩阵

- 错误密码、无 Cookie、过期/撤销 Session 均拒绝访问 Observe/Control/SSE；
- viewer 不能写，operator 可发业务指令，owner 可执行管理操作；
- 缺失或错误 CSRF 的写请求返回 403；重复幂等键不重复创建事实；
- SSE 使用 sequence replay，断线重连无丢失、无重复业务状态；
- 390x844、412x915、1440x900 无遮挡、溢出和不可触达按钮；
- Manifest/Service Worker 激活，不缓存 `/api/`、Cookie、token 或审批数据；
- 离线时发送、回复、取消、审批全部禁用，恢复联网后不自动重放。

## 通过条件

真实浏览器、DOM、网络请求和服务端 Event 四类证据一致；移动端能够完成登录和导航，所有安全负向测试 fail closed。

## 验收结果

- Web Session 使用 SQLite 持久化，数据库只保存 Session/CSRF 摘要；daemon 重启后既有浏览器 Cookie 可恢复 Session，logout 立即撤销。
- 未认证、错误密码、缺失/错误 CSRF、RBAC 越权和 HTTP/body 幂等键不一致均 fail closed；相同幂等键重放不创建重复事实。
- SSE 支持 `after_sequence` 与 `Last-Event-ID`，经 Nginx 关闭 buffering 并可按 Event sequence 回放。
- 指挥台已接通 Task、Message、Cancel 和 Approval Decision 入口；Approval/finish 的竞态状态机留在 T06 验证。
- 390x844、412x915 与 1440x900 真实浏览器验收无横向溢出、控件越界或底部导航遮挡。
- PWA 静态应用壳可离线加载；Service Worker 不缓存 `/api/`，离线写操作禁用且恢复联网后不自动发送草稿。
- owner Worker health-check 经生产 HTTPS 创建并由 Resident Worker 一次领取、确认，状态从 `pending` 进入 `applied`。

证据见 [T02 验证报告](../../reports/validation/2026-08-30-openagentx-adr001-t02-web-security-pwa.md)。
