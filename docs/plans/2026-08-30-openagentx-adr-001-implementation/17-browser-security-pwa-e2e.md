---
doc_type: implementation_task
status: completed
owner: openagentx
updated_at: 2026-08-30
---

# 任务 17：浏览器、安全与 PWA E2E

## 目标

以真实浏览器、HTTPS 代理和移动网络故障场景验收指挥台，证明其既可远程指挥，又不会缓存、越权或离线重放敏感控制操作。

## 依赖与入口

- 依赖：任务 16；
- 入口：完整 Web 后端与 React PWA；
- 退出门槛：G5。

## 实施范围

- 建立 Playwright 浏览器 E2E 和 `chrome-devtools mcp` 人工可复查验收；
- 覆盖 390x844、412x915 和 1440x900 三个必测视口；
- 覆盖 owner/operator/viewer、Session 过期/撤销、CSRF、幂等和越权路径；
- 覆盖 SSE 经代理断线、sequence replay、重复事件和缓存更新；
- 覆盖 PWA 安装、版本更新、offline 和重新在线；
- 检查 Service Worker Cache Storage、IndexedDB、Local Storage 和网络请求；
- 检查敏感字段、日志、错误页和浏览器历史泄漏；
- 形成截图、DOM、交互和安全响应头验收报告。

## 质量与验证

- 手机核心页面无整页横向滚动、遮挡、hover-only 或过小触控目标；
- 离线时所有写操作禁用，重新在线后不自动提交旧命令；
- API/SSE/Auth 响应不进入 Service Worker cache；
- 多 Nginx 入口共享 daemon Session，不要求 sticky session；
- 浏览器请求 Worker API/Gateway 被路由和授权双重拒绝；
- 登录限速、Cookie、安全头和 forwarded header 信任边界通过负向测试。

## 退出条件

- 三视口 DOM、渲染、触控和业务流程全部通过；
- iOS Safari、Android Chrome 和桌面 Chromium 的安装验证有记录；
- 移动网络 SSE 重连无事件丢失或重复状态副作用；
- G5 验收报告完成，才允许进入 Release。

## 实施结果

- 前端监听 `online`/`offline` 事件；离线时显示全局状态并禁用审批、回复、发送、取消及指令输入，不使用 Background Sync，也不在恢复在线后自动提交旧指令。
- Service Worker 对 `/api/` 和 SSE 请求保持网络直通，仅允许静态应用壳使用网络失败回退；Manifest 增加 `any maskable` 图标。
- daemon HTTP 与 Web Auth 入口统一返回 `Cache-Control: no-store`、`X-Content-Type-Options`、`Referrer-Policy`、`Permissions-Policy` 和 frame-ancestors CSP。
- 增加服务端安全响应头回归测试。

## 验证

详见 [Task 17 验证报告](../../reports/validation/2026-08-30-openagentx-task17-browser-security-pwa-e2e.md)。
