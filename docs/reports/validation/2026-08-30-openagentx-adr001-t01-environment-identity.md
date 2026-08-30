---
doc_type: validation_report
status: passed
owner: openagentx
test_id: T01
validated_at: 2026-08-30
---

# OpenAgentX ADR-001 T01 测试准备与身份初始化验证

## 结论

T01 通过。OpenAgentX 已具备不依赖直接 SQLite 写入的正式初始化和 Agent 身份管理入口；daemon 使用持久化 owner 启动，合法 Resident Worker 可以取得当前 generation、lease 和 fencing token 并进入 online。

## 验证范围

- schema、daemon、UDS、本地 HTTP 和远程 HTTPS/TLS；
- 首个 owner、默认 Organization、daemon principal；
- `quote-service`、`test-fake-agent` 的 Principal、AgentIdentity 和 AgentProfile；
- 初始化和 Agent apply 的原子性、幂等、owner 认证及敏感字段边界；
- 未知 Agent 注册错误分类；
- Fake Worker 注册、心跳、lease、generation、fencing 和 Backend health。

## 证据

| 检查 | 结果 |
|---|---|
| 实现提交 | `c4bc5c7 feat: add formal OpenAgentX identity bootstrap` |
| 验证二进制 SHA-256 | `1cafb661bd3b99f00fc3323931ae73b53048e7c1f10e9499c1b08a56322b5fda` |
| 数据库变更前 SHA-256 | `75800ae2ce4b45548a41426d2f33531f0be8b53c3f18ba2513c798a222bbc2d6` |
| SQLite 在线备份 | `data/backups/openagentx-before-t01-20260830T141137.db`，SHA-256 `7b4cc40013ef377a54d730873218036b0e4b4506e42bf8e468c9391dcb50dbe2` |
| schema | `OpenAgentX schema v1 verified` |
| 初始化状态 | 1 个 Web owner、1 个 Organization、2 个逻辑 Agent、2 个 AgentProfile |
| 初始化 Event | owner/daemon/Organization/WebUser 共 4 条；两个 Agent 各 2 条，共 8 条 |
| 幂等 | 重复 apply `quote-service` 后 Event 仍为 8 |
| 敏感字段 | Event payload 中 Argon2id 摘要匹配数为 0 |
| daemon | `openagentx.service=active`，数据库 owner 加载成功，daemon 进程环境中 `OPENAGENTX_ADMIN_PASSWORD` 为 absent |
| UDS | `run/openagentx.sock` 为 socket，权限 `0600` |
| Web | 本地 HTTP 200；`https://agentx.oneaxe.cn/` HTTP 200、TLS verify result 0；owner 登录 HTTP 200 |
| 未知 Agent | Worker register 返回 HTTP 404，code `NOT_FOUND` |
| Fake Worker | 首次注册 generation 1/fencing 1；daemon 配置重启后旧实例 offline，重新启动的实例 online、generation 2/fencing 3、lease 持续续期 |
| Fake Backend | adapter `fake`、backend `primary`、health `healthy` |

数据库备份文件是本机运行证据，不纳入 Git。报告不记录密码、Cookie、Session Token、Worker Session Token 或私钥。

## 自动化验证

```bash
go test ./...
go test -race ./...
go vet ./...
python3 scripts/check_docs.py
git diff --check
```

覆盖的新增行为包括：

- 初始化事务在提交前故障时完整回滚；
- 初始化和 Agent apply 同内容重放不追加 Event；
- Agent apply 的 owner 密码错误时不创建身份；
- daemon 在无持久化 Web 用户时 fail closed；
- Worker 对未知 Agent 返回领域 `ErrAgentNotFound`，HTTP 映射为 404。

daemon 重启时活动 Worker 当前会因 UDS 连接失败退出，需要由进程管理器重新拉起；自动重连和完整恢复语义属于 T07，本报告只确认重新取得所有权后的 generation/fencing 单调前进与旧实例 offline。
