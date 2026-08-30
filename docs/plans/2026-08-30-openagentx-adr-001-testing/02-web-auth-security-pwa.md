---
doc_type: test_task
status: pending
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
