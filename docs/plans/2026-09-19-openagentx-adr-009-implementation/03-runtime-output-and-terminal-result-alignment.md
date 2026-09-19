---
doc_type: implementation_task
status: completed
owner: openagentx
updated_at: 2026-09-19
---

# 任务 03：Runtime 安全输出与终态结果对齐

## 目标

让 Console/Web 能在 Adapter 实际能力范围内获得一致的安全增量输出与最终 TurnResult，并明确区分
Task outcome、Runtime reply 和业务效果证据；不改变 ADR-006 的终态语义。

## 范围边界

- 仅处理已有 AGY/CodeBuddy 等正式 Adapter 的输出采集、持久化和安全投影；
- 不通过 tmux、Console 本地 TurnHandle 或进程旁路读取 stdout/stderr；
- 不制造 token stream、tool progress、thinking 或 Runtime 未承诺的 stage；
- `uncertain`、failed、canceled 等现有领域结论保持原义。

## 验收矩阵

| 类别 | 验收内容 |
|---|---|
| 正向 | 支持增量的 Adapter 产生 bounded safe output；不支持增量者显示 running 后给出安全最终 reply；Task/Runtime 两层结果均可读 |
| 状态/CAS/幂等 | Run/Task 归属与 terminal 持久化原子关系明确；重复 output/event 不重复最终 reply；终态不可被迟到片段回退 |
| 失败 | 空输出、畸形 envelope、仅 stderr、退出码与状态矛盾、截断、redaction 失败均 fail closed 并保留安全诊断 |
| 竞态 | 最后 output 与 terminal commit 先后、Worker generation replacement、cancel/exit 同时发生、重连重复片段 |
| 安全/资源 | Web/terminal 共用 safeoutput；单片、单任务和 Timeline 总量上限；无 raw stderr/secret/hidden reasoning |
| 证据格式 | 每个 Adapter 记录真实 argv/env/stdin/stdout/stderr/exit 契约和 fixture；业务副作用仅以正式可验证证据判定 |

## 实施步骤

1. 根据 Task 01 能力矩阵，为每个正式 Adapter 定义“增量可用/仅最终结果/不支持”的明确路径。
2. 统一 Runtime output 与 TurnResult body/error 的 `safeoutput` redaction、truncation 和类型标记。
3. 在持久化边界保证最终 Run/TurnResult 与对应 Journal event 的关系可测试；解析失败不推断成功。
4. API 明确返回 Task terminal 无结果、Runtime 无结果、结果被截断和结果无效的结构化原因。
5. Web 与 Console projection 测试使用同一 fixture，证明不会因客户端不同而泄露更多内容。

## 验证

```bash
go test ./internal/worker/... ./internal/safeoutput/... ./internal/api/panel ./internal/api/console -count=1
go test -race ./internal/worker/... ./internal/safeoutput/... ./internal/api/panel ./internal/api/console -count=1
go test ./... -count=1
go vet ./internal/worker/... ./internal/safeoutput/... ./internal/api/...
npm run test:observation
bash scripts/check-legacy-control-paths.sh --release
git diff --check
```

## 退出条件

- 各正式 Adapter 的可观察能力不超过实测契约；
- 最终回复与 Task outcome 分层，`uncertain` 不伪装成功；
- secret/raw stderr/hidden reasoning 和无界输出均有负向测试；
- 创建一个 Task 03 实现提交后停止等待监督 gate。

## 完成记录

- 实现提交：`e82ae373fd9a3bbadb7a4bee16ef3c5a8fc52f00`。
- AGY、CodeBuddy、ACP 与 Worker service 共用安全 TurnResult 投影；公开字段 4 KiB，Run 实时输出最多
  256 条，Adapter authoritative buffer 保持有界。
- Console/Web API 区分 Task outcome 与 Runtime reply，并结构化表示 pending、not recorded、invalid、empty、
  available 和 truncated；Normal 强制清除 Diagnostic 内容及 truncation metadata。
- `FinishRun` 原有 Run/Task/SessionBinding/Journal 单事务、CAS 和重复 finish 幂等未改变；ADR-006/007、
  reducer/TUI 和真实部署未修改。
- 定向普通/race、全仓普通测试、vet、build、Web observation、release scanner、whitespace 与 ADR hash
  检查通过；Task 04 及以后未提前实现。
