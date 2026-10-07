# 新工具独立验收

- 结论：隔离真实 HTTPS 客户端/API 集成通过，没有派发正式 Agent 或执行模型。
- 最终源码：`clients/oaxctl` SHA-256 `674e38a2618da69f07c0e2ebbf03377c9d7c711a88efa64ffe0f96e17fec893b`；`scripts/operations/oaxops` SHA-256 `1a7f7efbf9241d9682feddbfd1eee0ea52aee885700709d9f0493eec14b5a730`。
- 最终 [client-integration-06](client-integration-06/result.json) 使用以上客户端源码。保留 [manifest](client-integration-06/manifest.json)、[SQLite 只读核对](client-integration-06/database-crosscheck.json)和[隔离进程退出记录](client-integration-06/runtime.json)。通过登录、Secure Cookie 持久化、CSRF 轮换、会话脱敏、readiness、任务查询、派发与消息幂等、异内容同 key 拒绝、旧版本拒绝及等待超时。数据库为 1 个 queued Task、2 条 Message、0 Run、Task version 2。
- 夹具使用真实独立 daemon 和 Worker 注册，并经本地 TLS 通信；Worker 在派发前暂停，因此没有模型执行。这证明客户端/API 契约，不代表业务执行或外部 Cloudflare 链路通过。
- 01 为夹具误解析 JSON 数组；02 证实 say 需要支持 steer 的已注册 backend；03 的 /bin/false 健康检查失败；04 遇客户端公开 session 输出调整；05 已通过初版正向矩阵。06 由最终 readiness 文本调整及新增幂等/版本负向检查触发，最终脚本与客户端 SHA 和 manifest 一致。每次结果分别保留。
- oaxops 关键审查：配置的 Codex 路径解析为原生 ELF，open 和 exec resume 均用 pass_fds 传递已持有的 writer.lock；代码审查未发现描述符继承阻断问题。实际主机迁移、thread、systemd 与回执连续性由主代理和实现代理另行核验，不由本 HTTP 夹具证明。
- 已进行针对性秘密扫描；凭据和隔离数据库保留在 `~/.local/state/openagentx/validation/2026-10-08-oax-tools/` 私有目录，未入库。
