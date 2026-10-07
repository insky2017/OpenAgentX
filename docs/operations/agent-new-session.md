# 同一 Agent 交接到新 thread

`agent new-session` 保留稳定 Agent ID、角色、工作目录、模型偏好和协作对象，创建全新的 Codex thread。旧 thread 及其任务记录保留。它适合长会话收口后的接班；首次加入 OAX 仍用 `$oax-join`。

## 使用

先让当前 Agent 整理一个简短交接文件：当前职责、已完成事实、待办、重要路径、约束、未知业务结果。文件不应含凭据；它是背景资料，不自动授权重复执行旧任务。

在另一终端预览：

```sh
openagentx agent new-session openagentx --handoff-file /path/HANDOFF.md
```

输出当前 thread、会话版本、Worker/模型与可复制的提交命令。预览不创建 Task、不调用模型、不改会话。

等待当前任务和咨询结束，用预览返回的值提交：

```sh
openagentx agent new-session openagentx \
  --handoff-file /path/HANDOFF.md --apply \
  --expected-thread <预览中的thread> --expected-version <预览中的版本>
```

命令会创建一项只读交接初始化任务。新 thread 只确认身份、工作目录和交接标识，不执行交接中的业务。初始化成功后发布新活动会话，后续任务和协作采用它。忙碌、待处理咨询和版本冲突会明确拒绝。

看到成功回执后，退出旧原生 view，在原 Agent pane 中重新连接：

```sh
openagentx agent open openagentx --native
```

旧 view 的新输入被拒绝并提示重开，不会偷偷送到旧 thread。一个 Agent 仍只打开一个受管原生 view。终端重开不创建业务任务。

## 中断与失败

- `--wait` 默认 3 分钟，只控制命令等待，不限制模型执行。命令打印 Task ID 后即已有持久回执；等待中断不取消后台，也不自动重新提交。
- 同一 Agent、原 thread/版本、相同交接内容默认生成稳定幂等键。重发完全相同命令只取得同一 Task；也可提供 `--key`。不得为未知结果更换 key 反复提交。
- 初始化失败、取消或未知结果保留原活动指针和失败记录；不重放模型。Runtime 本身 unresolved 时须按原恢复流程处理，不能靠轮换绕过它。
- 原历史 mutation 的 `uncertain` 不会被改写为成功。未结束的 Task、排队输入、待答复咨询须先收口；本版不自动跨会话搬运未完成工作。
- Worker 重启后，正式活动指针继续决定新 Task 路由；不会因旧配置中的 `thread_id` 回流。原接入摘要也不会再次注入新会话。

## 实现与支持边界

控制面新增 schema v7 的 `agent_sessions`，记录活动 thread、上下文 Task、版本与待初始化 Task。初始化复用 Task/Run 和 Worker，冻结 `session.force_new`；候选绑定先于模型执行持久化，成功结算与活动指针、managed 协作上下文更新在同一事务提交。旧 Task 的 SessionBinding 不改写。

需要 daemon、CLI 与目标 Codex Worker 支持本批协议。Worker descriptor 必须声明 `session_handoff`；旧 Worker 明确拒绝新会话请求，不会把它误当旧会话续办。各进程实际安装情况以交付报告为准。

此入口只支持新 thread 接班，不提供任意旧历史切回、fork 或跨 Agent 导入。受管 TUI 的 `/resume` 仍不是跨会话切换入口。
