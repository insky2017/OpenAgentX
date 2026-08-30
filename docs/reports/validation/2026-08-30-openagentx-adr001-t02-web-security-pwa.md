---
doc_type: validation_report
status: passed
owner: openagentx
test_id: T02
validated_at: 2026-08-30
---

# OpenAgentX ADR-001 T02 Web 安全、SSE 与 PWA 验证

## 结论

T02 通过。OpenAgentX 指挥台已具备持久密码 Session、RBAC、CSRF、HTTP/body 双重幂等键、可恢复 SSE、移动优先 PWA 和离线 fail-closed；生产 HTTPS 入口可以安全创建 Worker Admin health-check，并由 Resident Worker 完成领取与确认。

## 验证范围

- 密码登录、持久 Session、CSRF 轮换、logout 撤销和 daemon 重启恢复；
- viewer/operator/owner RBAC、未认证访问、幂等重放和冲突请求；
- Observe/Control/Admin API、SSE sequence replay 与 Nginx 长连接；
- 指挥台 Agent/Worker/Task 字段映射、任务创建、回复、取消和审批入口；
- PWA Manifest、Service Worker、静态壳离线加载和敏感数据缓存边界；
- 390x844、412x915、1440x900 的真实 DOM、布局、交互和控制台；
- WorkerCommand `pending → claimed → applied` 持久化生命周期。

## 版本与运行证据

| 检查 | 结果 |
|---|---|
| 实现提交 | `b4fe7ed feat: harden OpenAgentX command center` |
| 验证二进制 SHA-256 | `6cf1966155269aef9654fb76aba7699d1c58448603e0506494bf7ca5486524c6` |
| schema | `schema_meta.version=1` |
| daemon / Worker | `openagentx.service=active`；测试 Worker active，`NRestarts=0` |
| UDS | `run/openagentx.sock` 权限 `0600` |
| HTTPS | `https://agentx.oneaxe.cn/` HTTP 200，TLS verify result 0 |
| 未认证 Observe | HTTP 401 |
| Nginx | `nginx -t` 成功，服务 active；HTTPS 终止、SSE `proxy_buffering off`、`proxy_read_timeout 1h` |
| Worker ownership | `test-fake-agent` generation 8、fencing token 14、status online |
| Admin health-check | `worker-command-1788089082005139519`：`applied`、attempts 1，结果为 Backend probe active |

报告不记录密码、Cookie、Session Token、CSRF Token、Worker Token、密码摘要或私钥。

## 安全与协议结果

| 场景 | 实际结果 |
|---|---|
| 错误密码 | HTTP 401，不签发 Cookie |
| 无 Cookie 的 Observe / SSE / Control | HTTP 401 |
| 缺失或错误 CSRF | HTTP 403 |
| viewer 写操作 / operator Admin | HTTP 403；owner 具有 Admin 权限 |
| `Idempotency-Key` 缺失或与 body 不一致 | HTTP 400 |
| 相同幂等键重复创建 Task | 两次响应返回同一 Task/sequence，数据库只有一份事实 |
| daemon 重启 | 既有 Cookie 恢复持久 Session，无需重新登录 |
| logout | 正确 CSRF 返回 204，随后 Session 立即返回 401 |
| SSE replay | 同时支持 `after_sequence` 和 `Last-Event-ID`；生产回放 baseline 1733，首条 replay ID 1733 |
| SSE 代理 | daemon 无固定 WriteTimeout；响应 `X-Accel-Buffering: no`，Nginx 禁用 buffering |

## PWA 与浏览器结果

- 真实 192x192、512x512 PNG 图标和 Manifest 可加载，Service Worker `openagentx-shell-v3` active。
- install 阶段预缓存 `/`、稳定命名 JS/CSS、Manifest 和图标；独立浏览器首次安装后离线重载仍显示应用壳。
- Cache Storage 中没有 `/api/`；Local Storage、Session Storage 和 IndexedDB 中没有业务状态或认证数据。
- 离线时发送、回复、取消、审批和退出不可用；恢复联网后草稿保留但不自动发送。
- 390x844、412x915、1440x900 均无横向溢出、交互控件越界或底部导航遮挡；任务、组织和指挥页面可触达。
- 生产页面控制台无 warning、error 或 issue；412x915 重载后正确显示 1 个有效 Worker，历史离线 Worker 未计入 connectivity。
- 移动指挥台创建的测试 Task 已由 Fake Worker 完成，页面状态与领域状态一致。

## 自动化验证

```bash
go test ./...
go test -race ./...
go vet ./...
npm --prefix web run build
python3 scripts/check_docs.py
git diff --check
```

新增回归覆盖持久 Session 跨 repository/manager 重启、CSRF 轮换与撤销、RBAC 层级、Panel 认证和幂等校验、SSE 恢复头、Admin owner 边界，以及 Worker Command 全生命周期的 SQLite 时间/NULL 解码。

T02 只确认审批入口、鉴权和持久化链路可用；native Approval、preflight Approval 与 turn finish 的竞态语义由 T06 验收。
