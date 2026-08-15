# Task 01：Core Daemon 与 SQLite Store

## 目标

建立 Go 1.22 工程、领域模型、SQLite schema、事务化状态转换和 HTTP-over-Unix-socket daemon。

## 实施范围

- `go.mod` 与 `cmd/agentbus`；
- `internal/domain`：Agent、Task、Message、Event；
- `internal/store`：migration、WAL、foreign keys、busy timeout；
- `internal/service`：注册、提交、读取和状态转换；
- `internal/server`：Unix socket HTTP API、JSON 错误和优雅退出；
- `log/slog` 结构化日志；
- runtime 数据目录 `.gitignore`。

## 必须满足

- 状态转换在事务内同时写 Task 和 Event；
- idempotency 冲突返回原 Task；
- 同一 target 的 active Task 约束无竞争窗口；
- Unix socket 启动时只移除已确认是 socket 的旧路径；
- SIGINT/SIGTERM 优雅关闭，不删除数据库。

## 验收

- Store 单元测试覆盖 migration、幂等、active task 和非法转换；
- Server 使用临时目录和 socket 完成集成测试；
- SQLite 重开后数据可读取。
