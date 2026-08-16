# AGY Review Round 2：收尾修正

请在当前同一 AGY 会话继续修正 AgentBus Go V0。只修改 `AgentBus/`，不暂存、不提交，不启动或操作真实 `%50/%51/%52/%53` pane。测试 TmuxConnector 时只能使用 mock 或独立临时 tmux session。

## 1. 修复 Unix socket 清理所有权竞态

当前 `internal/server/server.go` 的 `cleanupSocket()` 只判断路径当前为 socket 就删除。若当前 Server 创建的 socket 已被 unlink，随后另一个 daemon 在同一路径创建了新 socket，旧 Server 退出时会误删新 socket。

要求：

- `net.Listen("unix", ...)` 成功后记录本 Server 所创建 socket 文件的 identity；可使用 `os.Lstat` 得到的 `os.FileInfo`，清理时通过 `os.SameFile` 与当前路径重新 `Lstat` 的结果比较。
- `cleanupSocket()` 只在当前路径仍是 socket 且 identity 与本 Server 创建的 socket 相同的情况下删除。
- identity 缺失、路径不存在、类型不对或已被替换时一律不删除，fail closed。
- 增加回归测试：Server A 启动后 unlink 原路径并在同一路径建立 Server B/dummy Unix listener；停止 A，断言 B 的新 socket 路径仍存在且可连接。测试结束必须清理临时 listener/socket。

## 2. 补齐 HTTP E2E 的 supplemental message 回读断言

`internal/server/server_test.go` 的 E2E 在 `/send` 后再次调用 `GET /api/v1/tasks/{id}?agent=quote`，断言响应 `messages` 中存在 `kind=supplement` 且内容为发送值。已有 CLI 测试可以保留，但 Server HTTP 契约也要直接覆盖。

## 3. 修正 CLI 帮助文案

`internal/cli/root.go` 把 `--socket` 标为 `Global Flags`，但实现只支持每个叶子 subcommand 自己解析 `--socket`。V0 不必扩大解析器；将帮助改为准确的“各相关子命令支持 `--socket`”，不要暗示 `agentbus --socket X agent list` 可用。

同时把示例角色与真实映射统一：

```text
%50 orchestrator
%51 agentbus-agent（role=agentbus）
%52 quote-service（role=quote）
%53 daemon/log
```

## 4. 修正文档残留

- `README.md` 中 `%51` 注册示例改为 `--id agentbus-agent --role agentbus --address %51`；`%52` 使用 `--id quote-service --role quote --address %52`。
- `docs/ARCHITECTURE.md` 第 95 行附近不要把唯一 Worker 写成 `Quote Agent`，改成通用 Worker/目标 Agent 表述。
- `docs/ARCHITECTURE.md` V0 链路图同步为 Orchestrator、AgentBus Agent、Quote Service Agent 均经 AgentBus CLI/TmuxConnector 协作，避免残留 `%51` 是 Quote Agent 的旧模型。
- 历史指令文件 `AGY_IMPLEMENTATION_PROMPT.md` 不需要回写，它是当时委派记录。

## 5. 验证与汇报

完成后运行：

```bash
cd AgentBus
gofmt -w .
go mod tidy
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
go build -o bin/agentbus ./cmd/agentbus
```

最后只简要汇报：修改文件、关键修正、每条命令结果。不要提交或暂存。
