# OAX 基础总览

`openagentx overview` 是只读终端总览。它显示已登记 Agent、当前已知任务、就绪或阻塞原因，并可跳转到已存在的受管终端。关闭总览不会停止 Worker、取消任务或关闭 Agent 终端。

```sh
openagentx overview
openagentx overview --socket /absolute/profile/run/openagentx.sock --credentials /absolute/profile/credentials.json --worker-dir /absolute/profile/workers
```

路径沿用 CLI 的优先级：显式参数、对应 `OPENAGENTX_*` 资源环境变量、`OPENAGENTX_HOME`、`~/.openagentx`。登录复用 Console 的 installation-bound 凭据；需要 viewer 或更高角色及 `console.read`。未登录时按界面提示在同一 profile 执行 `openagentx console login`，然后按 `r` 重试。总览不自动登录、轮换或删除凭据。

## 阅读与导航

| 操作 | 效果 |
| --- | --- |
| `↑` / `↓`，`j` / `k` | 在列表选择 Agent；进入详情后逐行滚动 |
| `Tab` | 列表与详情间切换 |
| `PgUp` / `PgDn`，`Home` / `End` | 按页滚动或到首尾 |
| `Esc` | 回到列表 |
| `Enter` | 跳转选中 Agent 的现有受管 pane 0 |
| `r` | 立即刷新，无重叠请求批次 |
| `q` / `Ctrl-C` | 退出总览 |

80×24 可同时阅读列表、选中详情和操作提示；更长内容在详情中滚动。状态按需刷新，通常在上一批结束后等待 5 秒，单批最多 4 个并发读取、15 秒总超时。刷新只调用现有授权 GET API，不调用模型，不发任务，不进行审批。

列表通过 Console 的分页接口读取 Agent，不依赖 overview API 最近 100 条任务。每个 Agent 通过 Attach 查询当前 Run 或建议处理的 Task，再读取该 Task 的安全详情。因此“待处理”是当前已知任务/审批提示，**不是全局待办总数，也不是完整历史列表**。协作咨询、答复和续办的全局摘要不属于本批。

连接异常时保留上次内容并标记旧数据，禁用导航；单个 Agent 读取失败时独立标记旧数据并禁止跳转该 Agent。恢复后重新确认登录身份并读取快照。窗口存在、终端正在显示和 Worker 可执行是不同事实；就绪依据控制面提供的 Worker、lease 和运行环境信息。Task 成功只显示“执行完成”，独立业务效果核验明确标记“未记录”。

## 跳转约束

只定位 `OAX` session 中唯一匹配的受管 Agent 窗口与活 pane 0，核对 `@openagentx_managed=1`、`@openagentx_agent_id`、窗口名和不可变 pane ID；不会按数字窗口索引或单独名称猜测。没有标记、重复身份、死亡 pane 或身份变化时拒绝跳转，不创建或修复终端。

跳转前检查现有前台进程来源。Console 必须带有与总览相同的显式 socket 和 credentials 参数。原生终端还须由当前 Worker 配置的 Agent/socket、持久化 thread ID、同一终端进程树中的前台 Codex `resume --remote` 及存在的 Unix socket 交叉确认。读取 `/proc/exe` 和完整进程环境不是前置条件；无法读取或无法确证来源时保守拒绝。`--worker-dir` 应与目标 Agent 的原生终端 profile 一致。

验证完成后重新核对窗口/pane 和进程身份，再切换到精确 pane ID。总览不会执行 `agent open`、`resume`、`respawn-pane`、Worker 生命周期命令或持久化标记写入。现有未受管 overview shell 的接管属于独立安装步骤，不由这个读界面静默完成。
