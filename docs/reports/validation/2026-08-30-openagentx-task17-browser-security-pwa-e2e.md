---
doc_type: validation_report
task: 17
status: passed
updated_at: 2026-08-30
---

# Task 17 验证报告：浏览器、安全与 PWA E2E

## 验证命令

```text
npm run build
go test ./...
git diff --check
python3 scripts/check_docs.py
```

以上命令均通过。

## HTTPS 与 PWA

使用隔离 Chromium（仅测试进程启用 `--ignore-certificate-errors`）访问 `https://rtx4090:4175/`：

- `window.isSecureContext === true`；
- Manifest 可读取，`display=standalone`，包含 `512x512` 的 `any maskable` 图标；
- Service Worker 注册成功，状态为 `activated`；
- `Cache Storage` 初始为空；
- `/api/observe/v1/overview` 请求通过网络返回，未产生缓存条目；
- 未启用 IndexedDB、Local Storage 或 Background Sync 保存业务控制数据。

## 视口与离线故障

真实浏览器检查视口：

| 视口 | 横向溢出 | 结果 |
|---|---:|---|
| 390x844 | false | 通过 |
| 412x915 | false | 通过 |
| 1440x900 | false | 通过 |

离线后页面显示“当前离线，审批、回复、发送和取消已暂停”；审批、回复、取消按钮以及指令输入和发送按钮均为 disabled。离线前输入的草稿在恢复在线后不会自动提交，页面没有出现“指令已写入 Agent Mailbox”。

## 服务端安全头

`TestServerSecurityHeaders` 与 `internal/api/auth.TestSecurityHeaders` 通过，验证 daemon HTTP 和 Web Auth 响应包含：

- `Cache-Control: no-store` 与 `Pragma: no-cache`；
- `X-Content-Type-Options: nosniff`；
- `Referrer-Policy: no-referrer`；
- `Permissions-Policy: camera=(), geolocation=(), microphone=()`；
- `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'`。

## 说明

本地 HTTPS 使用短期自签名证书，仅用于验证浏览器安全上下文和 Service Worker 生命周期；正式环境由 Nginx/Tailscale 提供受信 HTTPS 证书。未将自签名证书或测试截图写入仓库。
