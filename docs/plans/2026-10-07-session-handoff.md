# 保留 Agent 身份的新会话交接

## 用户结果与范围

现有 Codex Agent 可通过正式入口 `openagentx agent new-session AGENT` 预览，再用 `--handoff-file FILE --apply` 将交接送到全新 thread。稳定身份、职责、工作目录、模型偏好和协作对象保持；旧 Task、Run、thread 历史保留。交接初始化仅确认收到上下文，不继续旧业务。`join` 仍仅承担首次接入。

用户已澄清 Overview 没有退出，而是窗口中多开了 pane；本批不修改 Overview 产品逻辑，也不关闭额外 shell。

## 实施约定

- 活动会话由控制面持久化并版本化；每个历史 Task 的 SessionBinding 不改写。首次登记从已有正式绑定核实来源，后续不以“最新 Task”猜活动会话。
- 默认命令只读预览。提交包含 expected thread、版本、稳定幂等键和交接摘要；忙碌、未完成咨询、冲突均明确拒绝。用户可在任务结束后从另一终端执行，无须取消原任务。
- 新 thread 初始化复用正式 query Task/Run、角色快照、模型偏好、事件和 Worker。冻结 ForceNew 选择，防止旧配置中的 thread ID 把任务带回原会话。
- 初始化期间阻止其他会话输入并发。成功后同事务发布新活动指针、更新已有 managed 协作上下文；失败保留原活动指针、保留失败证据，不自动重放。
- 旧原生 view 不得提交到已经退役的 thread；重开正式 `agent open --native` 从活动指针和当前后台 endpoint 重新连接。不开第二个 Runtime writer，不直接改 state.json 或生产数据库。
- 产品支持切换不等于立即替用户切当前执行者。正式切换对象和交接内容必须明确；本批先用隔离身份验证整个流程，不切其他业务 Agent。

## 验收

1. 真实隔离 daemon、Worker、Codex、tmux：预览无写入；交接 nonce 被新 thread 确认，身份/职责/workspace/model 保持；后续普通输入和重开终端落新 thread。
2. 原 thread 和历史账本保留；Worker 重启后不回流旧配置。已启用协作时验证 context 更新、peer/权限保留及后续消息路由。
3. 必要事务集成：忙时拒绝、CAS/幂等、失败回滚、旧 view/旧 Task 竞态拒绝、初始化失败不重放；历史 uncertain 保持原样。
4. 绑定候选 commit/二进制 SHA、命令、正式 API/Run/事件和 PTY 证据；首次失败保留。源码交付、安装和运行态分别记录。

## 非目标

任意历史会话切回、fork、跨 Agent thread 导入、自动摘要生成、自动重试未知业务、重构调度，以及尚未证因的 TUI exit0 退出不纳入本批。
