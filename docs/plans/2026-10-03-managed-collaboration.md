# 原生终端与事件协作实施、验收计划

2026-10-03。用户已审阅 HTML 第 05 节并明确要求“提交相关的文件，然后开始落实方案”。阅读文档提交：`fea3561`（阅读仓库）。本计划授权范围是产品实现和独立测试身份的真实验收；原 Rhythm / Pay 的执行权切换仍须呈现具体交接结果后由用户确认。

## 用户结果

1. Fleet 中 Codex Agent 的固定 window / pane 0 默认打开原生交互 TUI，能输入、看过程、追问和中断；overview 与显式 Console 保留状态查看职责。
2. 无工作时由程序等待，不启动模型。咨询进入目标 Agent 的持久待办，当前工作完成后继续处理；答复回到请求方后自动安排下一轮。
3. 前台关闭后 Worker 仍执行和记录；重新打开仍在同一领域会话中看到历史与后续工作。
4. 领域归属、消息关联、Task / Run / Journal 与独立实际结果可核对。不能以 mock 或仅收到消息证明自动执行成功。

## 最小实施契约

- 复用现有通信绑定、消息、职责目录、Task、Mailbox、SessionBinding 和 Journal。通信绑定显式区分 `external` 与 `managed`，旧数据默认 external；同一 Agent 不同时拥有两种执行宿主，不创建影子 Agent 绕过原互斥。
- managed 通信绑定引用该 Agent 已建立的真实会话及 backend / context Task。线程身份从既有 SessionBinding 验证取得。通信 token 与 Worker/owner 凭据分离，原有消息 API 继续按 token 确认发件身份。
- 第一批 managed 自动入口只接 `consultation`，生成 `intent=query` 的任务。managed 行动请求明确返回不支持；external 的手动 request 行为保持。query 是结果结算语义，不能当作文件写权限隔离；角色与测试范围明确只读。
- 保存咨询与登记目标 Task / Mailbox / SessionBinding / 关联记录在同一事务中完成。同一消息重试复用原任务，不启动第二份工作。
- 对应 Task 正常结束时，在结算事务内保存唯一关联答复；若请求方为 managed，再建立同一 thread 的 query 续办任务。该任务标识为“消费答复”，结束后不自动回复 result，避免无限往返。
- 失败、取消、未知结果不冒充有效答复，不自动重做未知效果；保留可查询的原因与需要核对的状态。职责在投递、接单和答复时再次校验。
- 持久任务是恢复依据；内存通知只负责及时叫醒等待程序。没有轮询模型、tmux 按键投递或另一层 LLM 调度。
- Fleet 新建与显式复活死 pane 使用现有 Native Bridge，并等到对应后台就绪再打开。现有活 pane 不被强制替换。AGY 原生前台尚无适配，本批明确报告现状，不把状态 Console 当作已交付交互 TUI。

## 验收矩阵与强证据

| 用例 | 必须观察到的结果 | 证据 |
|---|---|---|
| M01 旧库升级与外部行为 | 旧 external 绑定、消息、职责保留；原互斥仍有效 | 升级前后只读记录摘要、schema、真实 CLI/API，原有回归 |
| M02 新建 Codex pane | 后台就绪后 pane 0 出现真正 Codex TUI，输入进入正式 Task | 命令与版本、tmux pane/PID、PTY、Task/Run、独立产物 |
| M03 咨询自动闭环 | A 发咨询，B 空闲后处理并关联答复，A 自动消费答复并继续 | 真实两个 Codex 后台、消息 ID、各 Task/turn/Run/Journal、只读答案证据 |
| M04 忙时与连续工作 | 同一 Agent 不并行启动两个 turn；当前任务结束后继续；两 Agent 可并行 | 时间线、任务队列、运行进程和事件 |
| M05 重试与重启 | 同 key 消息仍一个任务；待办重启后保留；未知执行不自动重做 | 重复请求回执、持久关联、重启前后状态与进程 |
| M06 职责与失败边界 | 错 scope / owner 拒绝；职责变化不静默越界；failed/uncertain 可追踪 | 真实协议拒绝、针对性事务测试、终态记录 |
| M07 前台退出和重开 | 关闭 TUI 后后台继续接单、上报；重开看到同一会话后续结果 | 前后台进程身份、PTY、API、SessionBinding |
| M08 空闲成本 | 完成工作后连续 30 分钟没有新模型执行请求 | 观察起止时间、Task/Run 数、app-server turn 请求和 Runtime 事件/会话计数；不能只看模型自述 |

真实验收使用隔离 profile、独立数据库/socket、独立测试 Agent 与临时工作目录，使用现有 Codex 凭据和默认代理。模型保持至少 gpt-6-astra，推理按当前用户约定；不读取或操作业务仓库、支付、回调和部署。

证据目录：本机 `~/.local/state/openagentx/validation/2026-10-03-managed-collaboration/<run-id>/`；脱敏副本与 SHA256SUMS 入 `docs/reports/validation/2026-10-03-managed-collaboration/`。首次失败与复验分开，记录源码 commit、构建 SHA、CLI/模型版本、实际命令、进程身份和每个用例的边界。源代码通过、隔离真实通过、正式安装通过分别报告。

## 交付顺序

1. 提交本计划和规则，实施 Fleet 与 managed 消息事务，运行必要单元/集成/竞态回归。
2. 提交可验候选，执行隔离真实双 Agent / PTY / 空闲验收；修复后保存独立复验。同步 DELIVERY、COVERAGE、EXECUTION-LOG 与阅读入口。
3. 汇总可运行命令、证据与缺口，完成候选代码提交。正式业务会话切换作为可审阅的下一步，不能把隔离通过报告为原 Desktop 自动协作通过。

不扩大到全部 CLI 的统一 TUI、完整原始事件归档、精确仅目标工具取消或未知终态一键恢复；这些既有缺口保留，阻断本次主链时才做有界处置。
