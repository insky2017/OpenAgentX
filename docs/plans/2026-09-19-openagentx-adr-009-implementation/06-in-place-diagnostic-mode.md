---
doc_type: implementation_task
status: completed
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

## 完成记录

- 实现提交：`de7eb3204132259e58f155c6ced9419d60c8f591`。
- `/diagnostic` 与 `/normal` 已在同一 Attach TUI 内使用正式 authenticated client 切换；每个 Follow 有独立
  context、opaque ID、registry 和 reducer stream epoch，旧流完成并移除后才启动新流。
- snapshot/event 必须经 reducer ack 后才推进 cursor；旧 epoch 输入安全取消。Diagnostic 403、网络或投影
  失败只进行一次 Normal-safe 回退，token 到期和 Diagnostic Follow 终止均立即清除 privileged state。
- Normal/Diagnostic Timeline 使用独立 bounded render，Normal 不回显历史 Diagnostic；overlay 只显示结构化、
  脱敏、限量的 Worker、Backend、Run、heartbeat/lease/drain 和 Runtime diagnostic。
- 五包普通/race、mode/Follow 重复测试、三次唯一 `tmux -L` + PTY Normal->Diagnostic->Normal->quit、全仓
  普通测试、vet、独立 build、Web observation、release scanner、whitespace 和冻结哈希检查通过。
- 本任务未实现 Task 07 的真实 daemon/Worker 用户闭环，未修改 API/schema/Runtime/Fleet/systemd/default paths，
  未 push、merge、安装、部署或操作真实状态。
