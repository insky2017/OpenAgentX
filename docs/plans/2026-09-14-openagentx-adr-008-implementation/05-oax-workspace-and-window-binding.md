---
doc_type: implementation_task
status: completed
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

## 完成记录

- 主实现：`c9339bb8c8cfa35a0a2bbd74608d273eb00bd2f6`；产品 workspace 统一为
  大小写敏感的精确 `OAX`，稳定 Console 位置为 Agent window pane `0`，overview 为
  `OAX:overview.0`，旧 `agentx` 不迁移、不合并。
- tmux inventory 使用内部 window handle 和逐字段查询读取 name、pane index 与 window option，
  不依赖控制字符分隔、window index、pane ID、active pane 或进程名。受管 window 使用
  `@openagentx_managed=1` 与 `@openagentx_agent_id=<agent-id>`，名称、marker 和控制面授权 Agent
  必须一致。
- hardening：`e40e28bed11abc9789c143977363e601f067e4d3`；将 topology 校验收紧为
  target-aware preflight，保留无关 unmanaged/orphaned window 和 pane `1+`，并确保新 Console
  仅在 pane `0`、marker 和结构化重读验证完成后启动。
- binding 补证：`5468ffeb634ee5a4aed5577fbea5c1201a591cce`；真实隔离 tmux 覆盖
  `PreflightAttach -> BindCurrent` 的 rename、marker、pane `0/1/2` 保留、同 Agent 幂等、异
  Agent confirmation-required 无副作用及确认后重绑定。二次 preflight、TOCTOU 检查和各 mutation
  失败点补偿保持 fail closed，不 kill/move/读取既有或未知 pane。
- Ubuntu tmux 3.4 与独立 Termux tmux 3.4 的平台差异已由单字段查询协议消除；最终七组真实
  `tmux -L` integration、四包定向测试与 race、全仓 Go 测试、受影响 vet、全量 build、release
  scanner 和 diff check 通过，监督者结论为 Task 05 `GO`。
- Open Issue `T04-01`、`T05-01` 继续归属 Task 07/pending，不阻断本 gate；未开始 Task 06，
  未操作真实 `OAX`、default tmux、service、数据库、socket 或 installed binary。
