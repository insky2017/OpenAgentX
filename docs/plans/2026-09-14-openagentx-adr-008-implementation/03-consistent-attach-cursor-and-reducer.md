---
doc_type: implementation_task
status: pending
owner: openagentx
updated_at: 2026-09-14
---

# 任务 03：一致 Attach cursor 与代际 reducer

## 目标

修复 Attach 从 sequence `0` 回放和旧 generation 覆盖新状态的问题。建立一致 snapshot/high-water 契约、断线续传 cursor 和可独立测试的状态 reducer。

## 依赖

- 任务 02 已通过；path/CLI 基础稳定。

## 实施步骤

1. 在 repository/service 的同一 SQLite 读取事务中取得 Agent snapshot 与 Event Journal high-water sequence。禁止以两个无事务查询拼接后宣称一致。
2. 扩展 Attach DTO 返回明确的 `snapshot_sequence`/`live_after_sequence`（最终命名以任务 01 冻结为准）；客户端 Follow 必须从该 cursor 之后开始。
3. 如果提供有限历史，历史窗口必须与 live cursor 分离，禁止把“显示最近 N 条”实现成从 `0` 读取全部历史。
4. reconnect 从最后**已应用** sequence 恢复；重复 sequence 幂等，gap/不可恢复 cursor 明确报错或重新 Attach，不静默重置为 `0`。
5. 建立纯 reducer，状态身份至少包含 `agent_id + worker_instance_id + generation`：
   - 旧 generation/旧 WorkerInstance 事件不得回退当前 Worker 状态；
   - 同 generation 乱序或重复事件行为确定；
   - 当前 generation 的状态转换更新状态栏；
   - heartbeat 默认合并，只在状态转换时产生 Timeline 项；
   - runtime 输出仅接受现有 safe-output 投影。
6. HTTP/UDS handler、client 和未来 TUI 共用同一 cursor/reducer 契约，禁止 CLI 自行解析原始 payload 得出另一套状态。
7. 保持 Normal/Diagnostic 共用 cursor 机制；Diagnostic 只增加授权、限流、脱敏字段，不改变事件排序。
8. 增加并发测试：在 Attach 读取期间插入新事件，证明不存在状态已跳到新值但 cursor 跳过对应事件的窗口。

## 必测场景

- snapshot generation 48 后存在 generation 42 heartbeat：UI 状态保持 48，旧 heartbeat 不进入 Timeline；
- snapshot cursor N，事件 N+1 正好并发提交：事件不丢且最多应用一次；
- reconnect 从 K 后返回 K+1，不重放 1..K；
- heartbeat burst 不无限增长 Timeline；
- Worker replacement、offline、draining 和 active run 切换；
- safe-output 中 Secret/环境变量/原始 stderr 不出现。

## 验证

```bash
go test ./internal/api/console ./internal/client/console ./internal/persistence/sqlite/... ./internal/safeoutput/...
go test -race ./internal/api/console ./internal/client/console ./internal/persistence/sqlite/...
git diff --check
```

## 退出条件

- Attach 一致性由事务测试证明；
- 所有 Follow/reconnect 路径不再默认 sequence `0`；
- reducer 对代际、乱序、重复和 heartbeat 有表驱动测试；
- 不改变正式控制命令路径；
- 阶段提交和 execution log 完成后暂停。
