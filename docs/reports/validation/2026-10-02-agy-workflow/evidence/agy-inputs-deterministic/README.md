# AGY 角色与时限确定性证据（D）

- 正式 WorkerService.BeginAttempt 在 AGY 分支调用 GetAgent 取得 AgentProfileRecord，读取最多 1 MiB 的非空 UTF-8 职责文件，将路径、内容、SHA-256、profile_version、workspace_root 和截止时间存入既有 Run resolved_execution_json。
- AGY 使用快照内容组装 stream-json stdin，实际子进程 cwd 使用快照 workspace_root；不重新读取角色文件。正在运行的快照不漂移，下一轮读取最新文件，角色内容版本以 SHA-256 辨识。
- Worker YAML `runtime_backends[].options.timeout: 30m` 经 AGY descriptor.default_timeout 进入 resolved spec；省略时仍默认 30m。Run deadline_at = started_at + timeout，启动延迟与心跳不延后此时限。既有 cancel/timeout 后代清理继续复用。
- 不支持的 AGY approval_policy、非空 backend_options、backend_default 附带 reasoning.value 明确拒绝。Task API 非空 execution override 的拒绝由主代理实现并单独验证。
- 缺失/不可读/空职责文件、无效目录明确拒绝；文件错误使用 Domain INVALID_INPUT，确保 Worker API 不把原因降为 generic 500。失败前不接受 mailbox、不创建 Run。

## 测试与证据

| 文件 | 结果与边界 |
| --- | --- |
| 01-targeted.log | 首次失败：并行实施中的 task_review_repository.go 引用未定义 runColumns，控制面和CLI包编译失败；AGY包测试仍通过。保留首次失败，不覆盖。 |
| 02-targeted-retest.log | 主代理修复上述编译错误后，AGY/controlplane/worker CLI/domain/runtime 五包完整定向复验通过。 |
| 03-race.log | AGY/controlplane/worker CLI/runtime 四包 race 检查通过。 |
| 04-public-role-error-race.log | 最后将角色文件读错转为可公开 DomainError 后，三个受影响 AGY 控制面测试带 race 复验通过。 |
| metadata.json | 基线源码HEAD、Go版本、文件范围与证据边界。 |
| SHA256SUMS | 本目录证据文件内容校验。 |

覆盖角色注入、hash/profile_version/cwd、Run持久快照、文件更新下一轮生效、配置timeout、固定截止时间、空输出/缺终态/冲突终态/非零退出码、timeout与cancel后代进程清理。控制面测试以专用 GetAgent fixture 提供角色版本，Task/Run/Journal仍走 SQLite 事务；Adapter测试启动本地shell fixture检查实际stdin/cwd。真实Agent注册、真实AGY模型、正式服务和安装链未在本子任务执行，不能据此标记R/I通过。未修改CodeBuddy，未提交Git。
