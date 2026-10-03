# 执行记录：托管协作与原生前台

## 规则、实现与源码验证

- 阅读仓库 `fea3561` 提交已审核 HTML；产品仓库 `84a0c8c` 提交实施规则和 M01–M08。
- `e22e4a8` 提交 managed 消息/Task/结果事务、schema v5、固定会话校验、Worker 通知、CLI 与 Fleet Codex 默认原生入口。候选阶段没有切换 Rhythm / Pay 的原 Desktop 宿主。
- 独立审查发现 planner 可在固定 backend 失效时回退到新会话；已在 Begin 持久事务中检查实际 Run backend、resolved backend、resume/context，不符隔离为 needs_review、0 Run，Worker 继续其他工作。
- Console 三 pane 状态行被截断，原断言不变，将状态放在 ID/version 前修复。中间一次持久证据目录过长超过 Unix socket 限制，改用短持久路径；失败和复验分别保留，见 [checks01](evidence/checks01/README.md)。
- 全量 `go test ./...`、`go vet ./...`、SQLite/Worker/Control/Fleet 五包 race 通过。该层为源码/集成证据，不替代真实模型。

## 工件与隔离真实执行

本机原始证据根目录：`/home/sky/.local/state/openagentx/validation/2026-10-03-managed-collaboration/`。

1. 初次在嵌套 submodule worktree 构建，Go 内嵌 VCS 错指外层 revision、modified=true。该工件未用于正式安装，原始 SHA 与首次初始化证据保留。随后独立干净 clone 检出 `e22e4a8` 重建，内嵌 revision 正确且 modified=false；SHA `14f4221694eedce95a7ce81eefae52b14f58393d8d7368b3057407d10437f2d9`。见本机 `release01/manifest.json`。
2. `live-e22e4a8-01` 使用正式旧 binary 建 v4 隔离库，候选 daemon 升 v5；42 个旧表的全部原有列 rowset 摘要不变。初次 CLI 回执被 ANSI 控制符干扰，验证器解析失败；真实已建立的 thread/binding 从正式状态恢复，没有重复初始化模型。
3. 第一组真实闭环成功后发现上述工件来源问题，停止继续投递，等待已发 native 任务自然结束。用正式 pause 与精确 owned-process 身份回收，只更换隔离 daemon/Worker；沿用真实原 thread，未直接改数据库或 state 文件。
4. 干净工件复验使用新咨询 key。首次验证器选到历史 reply，独立 fact 断言正确报错；改为按当前 key 找 request、再按 reply_to_message_id 找 reply。复用已执行 origin，不重复调用模型发问。`evidence-chain-20261003T180128-1865` 中新闭环严格通过，消息、双方 Task/Run、同 thread 和独立事实分别保存。
5. 原生入口使用既有 mutation 契约：Run 成功不自动消除 Task 的 business_effect_unverified。原生输入与效果核验单独举证，不能借此声称 query succeeded，不能手工改成成功。

6. 原生终端实际输入创建 `a472591c` 开头的 Task，Run 成功且独立 marker 字节匹配，Task 保留 uncertain。首次“关闭前台”用两次 Ctrl-C，TUI 按原生语义取消了当时后台 query；该 canceled 记录保留，不能算后台续跑。随后改为只断开前台 PTY 进程组，新 query `7d6b7e0f` 开头的 Task 正常成功，重开显示结果并保留同 thread。见本机 `evidence-native-continuation-20261003T180548-58be/native-result.json`。

## 正式安装与原协议回归

2026-10-03 18:06（北京时间），核对没有活动 Run、两演示 Agent 无待办后，仅停止两个演示 Worker 和 OAX daemon。备份原工件与 SQLite，再安装干净 `e22e4a8`、启动 daemon 完成 schema v5 迁移、启动原两 Worker。三服务实际 `/proc/<pid>/exe` SHA 均与候选一致。

[安装记录](evidence/installation01/result.json)及[迁移后记录](evidence/installation01/after-data.json)证明：旧6条消息、6项绑定和职责目录的原有列摘要保持；Rhythm / Pay 仍为 codex-desktop / external / active / generation 1。未执行 Desktop 生命周期或业务工程操作。备份仍在本机私密 installation01 目录，未入库。

[正式原协议复验](evidence/installed-protocol01/result.json)通过：仅使用随机测试身份和独立 scope；同 key、scope 拒绝、ACK/accepted/recover/result、清理后原规则完整保持分别核对。测试期间三服务 PID/starttime/SHA 未变；未创建模型任务。这项只证明安装后的旧通信行为，不替代 managed 双 Agent 模型证据。

## 用户补充测试原则

真实行为优先，不追求数量或覆盖率，已写入两处 AGENTS.md 并分别提交 `fb4bb10` / `1e21008`。后续只完成关键用户流程；不再追加重复模型调用、取消矩阵或琐碎单元。脱敏真实回执、独立随机 fact 的预期与实际结果、历史失败一并保存，可作为后续黄金回归依据。

HTML 同步本轮实现状态与证据入口。静态解析确认锚点、唯一ID及本地链接有效；内置浏览器的 URL 安全策略拒绝本地 file 预览，未尝试绕过。本轮仅改说明和状态文本，不能声称已重新完成视觉验收。

空闲窗口、Fleet 和恢复补验的完成结果依次追加；未执行项不写 PASS。

## 空闲窗口

2026-10-03 18:06:43–18:36:43（北京时间），实际1800.107508秒。`evidence-native-continuation-20261003T180548-58be/idle-result.json`为PASS：Task清单无变化、两个精确Codex thread新增模型轮次0、工具调用0；before/after的真实rollout与SHA保留。这是两个隔离Agent的程序等待证据，不推算账户费用或其他会话用量。

## Fleet 固定原生 pane

正式 `fleet workspace --respawn-dead` 使用与 canonical 相同 SHA，在独立 tmux server 中创建 overview 及两个测试 Agent 的 pane 0。首次自动输入紧接 Enter 被 Codex 的粘贴检测留在编辑区，120 秒未创建 Task；保留超时、编辑器 capture 与零新增 Task 证据。仅补一次 Enter，没有重发提示词，唯一 Task `task-6ef2c283-822d-4047-a9d2-f1284756ee07` / Run `run-8673d5cf-851a-49de-ba70-9b9fb29e132c` 随后完成。

独立随机文件内容 `OAX_FLEET_NATIVE_2524cc3621114458` 与工具读取、最终答案、pane capture 一致，Run succeeded；原生 mutation Task 仍为 uncertain/business_effect_unverified。夹具在文本与 Enter 之间加入一秒粘贴稳定时间，产品代码未修改。观察与收证复用这个唯一任务，未再次调用模型。见[Fleet 成功记录](evidence/live-e22e4a8-01/evidence-fleet-observe-20261003T184336-73b0/fleet-native-result.json)及[首次超时目录](evidence/live-e22e4a8-01/evidence-fleet-20261003T183859-0ffd/)。

18:43:37 仅关闭隔离 tmux server，daemon/Workers 留给恢复补验。这里验证的是正式 Fleet workspace 入口与原生交互；不声称 `fleet up` / systemd 首启整链已做真实验证。

## 忙时、离线与职责补验

本批 `evidence-recovery-20261003T192604-3f72` 使用同一隔离 profile、干净 e22 工件与原 thread。未修改业务项目或正式服务，不重跑已通过的主闭环、Fleet 或空闲窗口。

- M06：正式外部 UDS API 对未知 scope 返回 UNKNOWN_SCOPE，对错误负责人返回 OUT_OF_SCOPE，对 managed 行动 request 返回 INVALID_INPUT；每次拒绝后 Task 集合不变。
- M04：B 的真实 `sleep 75` 进程 PID/starttime 与 Worker 父子关系可核对；37 次观察期间咨询 queued / 0 Run。原忙任务 `task-b915e174-43c3-4b12-a3f7-ac36f9bef451`、B 咨询 `managed-task-e5c31acd958179aecad073c18f03db74`、A 消费 `managed-task-24434ba4235c94955a0058aaf845410c` 均一 Run 成功，B 前后 Run 时间不重叠，答复与独立 facts.txt 一致。
- M05：仅 SIGTERM 精确 B Worker；离线两次提交相同 key，均返回同一消息和 `managed-task-b729d594b243f21b5f80ffeef69975cd`，持久排队且 0 Run。相同配置重启 B 到 generation 3 后，发现 Worker online 但 Runtime readiness=false。夹具遗漏主验收已有的正式 `agent resume --no-open --wait 3m` 就绪步骤；保留只读状态和排队证据，补该步骤后原 Task 得到执行，未重发。
- B 答复 Run `run-c2e19eed-b09b-4b23-b3cc-a12ee1e09421`、A 消费 Run `run-8829d386-116d-4dd9-adb7-6fa3c0480cde` 均 succeeded，Task 均 query_result_delivered，独立事实与原 thread 保持。此项验证的是“重启 Worker、Runtime 正式就绪、持久待办续办”，不是仅凭 Worker online 推断自动恢复。

补验夹具开跑前还修正了 idle 门禁：最新已完成 Fleet 不是新的空闲观测，不应遮住此前完整 30 分钟 PASS；仍校验最新相关 idle phase 的时间、结果和 SHA，任何活动任务禁止重叠补验。

所有真实任务完成后，最终进程采集还引用已退出的旧 B PID，原批次以 `fixture process already exited before provenance capture` 保留 FAIL。夹具改为另存 retired Worker 记录，只对当前活进程做 residency 核对；`--finish-existing` 从已完成 M04/M05/M06 读取并核对原 Task/Run/事实，未派新任务，补采批次 `evidence-recovery-20261003T193650-574a` PASS。两版脚本快照、错误、就绪纠正 `evidence-recovery-resume-20261003T193523-a743`、实际API与最终结论均保存，未修改产品源码或强行改状态。

本轮不运行可选取消补验，不追加模拟或琐碎单元测试。真实 fact/回执/拒绝样例及首次失败保留为后续黄金回归依据。

## 收尾

19:38 的 `evidence-recovery-cleanup-20261003T193815-8229` 先读取正式 Task/overview，确认 18 项测试任务全为终态，再保存 owned PID/starttime、工件 SHA 和专属子进程身份，只停止隔离 B/A Worker 与 daemon。无专属活子进程残留；正式 daemon 与两演示 Worker 的 PID/starttime/SHA 前后一致。profile、原始失败和成功证据保留，没有清空数据库。

公共证据按源路径、源 SHA、脱敏 SHA 导出。完整 rollout 只在本机保留，入库的是可逐行重算模型/工具事件数的元数据投影，具体行为由 Task/Run/Journal、PTY、工具结果和独立事实文件支撑；源 SHA 清单与脱敏副本清单分开。导出前后扫描已知实际秘密值和常见凭据模式，未将凭据、原始 HTTP、数据库或二进制入库。

阅读 HTML 保留架构布局和交互，更新实现、安装、验收及下一步交接状态。最终静态记录为 `html-status02`；旧 `html-status01` 保留。受本地 file URL 浏览器策略限制，未绕过或伪报本轮视觉复验。最终提交只新增验收脚本修正、证据和文档；产品执行源码仍等同已安装的 `e22e4a8`，冻结 ADR 不变。
