---
doc_type: implementation_task
status: pending
owner: openagentx
updated_at: 2026-09-19
---

# 任务 06：同 pane Diagnostic 模式

## 目标

让 `/diagnostic` 和 `/normal` 在当前 Attach TUI 内安全切换 mode，复用同一身份、授权、snapshot/cursor、
reducer 和安全输出投影，不退出/重启 pane 或影响 Worker。

## 范围边界

- Diagnostic 继续要求 owner role 与 `console.diagnostic` scope；
- 不新增全局 auth fallback、Worker 私有协议、Runtime TTY 或 raw log endpoint；
- 不用 tmux respawn/send/paste/capture 完成模式切换；
- Fleet 启动命令保持普通 `console attach --agent`，模式由 TUI 内显式选择。

## 验收矩阵

| 类别 | 验收内容 |
|---|---|
| 正向 | Normal `/diagnostic` 成功后显示 mode、脱敏 diagnostic；`/normal` 回切；focused Task、draft 和安全历史按契约保留 |
| 状态/CAS/幂等 | 每次切换只有一个 active Follow；旧流 cancel+ack 后新 snapshot 生效；重复请求幂等或明确 busy |
| 失败 | viewer/operator 403、token expiry、network、snapshot/reducer 拒绝时保持/回到安全 Normal，不混入 Diagnostic |
| 竞态 | 旧流 event/followDone 与新 snapshot 乱序；切换中 resize/control/expiry；retention gap 与 mode switch 同时发生 |
| 安全/资源 | Normal 永远 strip Diagnostic；Diagnostic 仍 redaction/rate/byte cap；无 raw payload/stderr/secret |
| 证据格式 | mode 状态机 before/msg/after、authorizer route matrix、Follow goroutine/cancel/ack、fake clock expiry |

## 实施步骤

1. 在 application/TUI model 中加入显式 switching state 和 typed mode-switch Msg/Cmd。
2. 先停止并等待旧 Follow，不接受旧 mode 的迟到 event，再以目标 mode Attach 获取新 snapshot/cursor。
3. 成功时原子更新 mode/connection/snapshot；失败时恢复安全状态并保留输入 draft。
4. Diagnostic view 显示结构化 stage、Backend、等待/退出类别、heartbeat/lease/drain 聚合和脱敏诊断。
5. 主菜单的 Diagnostic Attach 与 TUI `/diagnostic` 复用同一 application service，不形成第二套逻辑。

## 验证

```bash
go test ./internal/cli/console ./internal/client/console ./internal/api/console ./internal/api/panel -count=1
go test -race ./internal/cli/console ./internal/client/console ./internal/api/console ./internal/api/panel -count=1
go test ./internal/cli/console -run 'Diagnostic|Mode|Follow|TTY' -count=3
go vet ./internal/cli/console ./internal/client/console ./internal/api/...
npm run test:observation
git diff --check
```

## 退出条件

- 同 pane Normal/Diagnostic 往返和授权失败路径可重复验证；
- 不存在双 Follow、旧 mode 污染、cursor 跨越或 goroutine 泄漏；
- Diagnostic 内容继续满足 safeoutput、限流和容量上限；
- 创建一个 Task 06 实现提交后停止等待监督 gate。
