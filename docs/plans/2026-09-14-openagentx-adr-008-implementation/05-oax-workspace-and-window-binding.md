---
doc_type: implementation_task
status: pending
owner: openagentx
updated_at: 2026-09-14
---

# 任务 05：`OAX` workspace 与非破坏 window 绑定

## 目标

将旧 `agentx` workspace 修订为大小写敏感的 `OAX`，正确识别 pane `0` 和额外 pane，并实现用户显式 Attach 后的可审计、非破坏 window-to-Agent 绑定。

## 依赖

- 任务 04 已通过；Agent 选择可由经认证控制面证明。

## 实施步骤

1. 将产品目标 session 统一为精确 `OAX`；帮助、错误、manifest 示例和测试清除把 `agentx` 当作受管 session 的行为。
2. 扩展 tmux runner 使用结构化 format 查询 session/window/pane/option；pane 状态必须由 `list-panes` 按 window 检查，不能用 active pane 或 `list-windows` 的单一 pane 字段推断 pane `0`。
3. 定义并实现 window marker（managed 与 agent ID）的读写、合法值校验和冲突分类。tmux 名称和 marker 只是本机映射，不是权威 Agent 身份。
4. Attach 前验证：进程在 tmux 内、session 精确为 `OAX`、当前 pane 精确为 `0`、当前 window 可绑定；失败只返回明确切换提示，不自动创建/切换 session/window/pane。
5. 显式 Agent 选择完成且控制面授权后执行窄范围绑定：将**当前 window**重命名为精确 `agent_id`，写 marker，再验证读取结果。
6. 绑定前检测同名 window、其他 window 已绑定同 Agent、当前 window 已有不同有效 marker、非法 Agent 名、pane `0` 缺失等冲突；任何冲突均不 rename、kill、move 或覆盖。
7. 若多步 tmux 更新部分失败，返回可见的 partial failure 和可执行修复提示；不得删除用户 pane。实现尽可能可补偿，并测试每个失败点。
8. Fleet Reconcile 必须保留 pane `1+`、unmanaged window、未知进程和 orphaned 现场；只补缺，不扫描 Agent 目录。
9. 产品代码继续禁止 `send-keys`、`paste-buffer`、`capture-pane`；启动 Console pane 使用显式新 pane/window command，不注入活动终端。

## 测试策略

- 单元测试使用 fake runner 覆盖所有查询/rename/set-option 失败点；
- 集成测试使用 `tmux -L <unique>` 的隔离 server，在其中创建名为 `OAX` 的 session；
- 覆盖 pane `0+1+2` 保留、当前 pane 非 0、pane 0 被删、同名冲突、重复幂等绑定、marker/name 不一致、unmanaged/orphaned window；
- 测试结束只 kill 隔离 server，不触碰默认 tmux server。

## 验证

```bash
go test ./internal/fleet ./internal/cli/fleet ./internal/cli/console
bash scripts/check-legacy-control-paths.sh --release
git diff --check
```

另运行任务新增的隔离 tmux integration test，并将完整命令与 server 名写入 execution log。

## 退出条件

- 只有 `OAX` pane `0` 可进入绑定；
- 多 pane、冲突和 partial failure 均有非破坏测试；
- 受管 marker 可读取验证且不成为业务身份来源；
- legacy control scanner 通过；
- 阶段提交和 execution log 完成后暂停。
