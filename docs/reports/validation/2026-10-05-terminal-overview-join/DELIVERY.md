# 终端身份、基础 Overview 与接入短名交付

2026-10-05。本批三项已实现并安装；六个业务 Agent 持续运行。范围按[已确认计划](../../../plans/2026-10-05-terminal-overview-join.md)，不扩大为完整 ADR 或业务功能验收。

## 现在怎么用

- 在 tmux `OAX` 中选择 `overview`：`↑↓` 选择 Agent，`Tab` 进入详情并滚动，`Enter` 跳到已核对的现有领域终端，`q` 退出总览。也可在交互终端运行 `openagentx overview`。见[操作说明](../../../operations/overview.md)。
- 在需要加入的新 Codex 会话中单独输入 `$oax-join`。旧 `$openagentx-join` 继续可用。模型利用上下文，只补问缺项，并生成身份与交接资料；结果仍为 `local_prepared / ready=false`，自动接管另行验收。
- 后续在 OAX 的 pane 0 执行 `openagentx agent open <id> --native`，会固化当前窗口身份、名称、rename 锁及标题。身份冲突、目标不明、身份配置失败拒绝此次打开并恢复选项；只有边框显示失败可告警继续。Console 对实际目标窗口处理，不修改调用来源窗口。

Overview 普通程序约每五秒读取既有授权 API，不唤醒模型。列表展示当前已知任务、就绪与阻塞提示；不是全局待办计数，也不是完整协作记录。断线保留旧数据并明确禁用导航。

## 源码、安装和运行态

| 项目 | 实际交付 |
| --- | --- |
| 接入短名 | `6627755`，两名共用唯一 prepare helper |
| 窗口身份固化 | `562d463`，Native、Console、Fleet 复用绑定与标签逻辑 |
| 基础 Overview | `eb043ca`，无 daemon 协议修改 |
| 已安装 CLI | `eb043ca3012b40ca5515a97c83e5422f0598f839`，clean build |
| CLI SHA-256 | `9ff957ac0a6c043b9cd8e8a41ba6270e72e1d8dafe032bfac7f1b6950748144e` |
| 安装方式 | 原子替换 `~/.local/bin/openagentx`，旧工件备份于原始证据目录 |
| 正在运行的 daemon/六域 Worker/原生终端 | 保留原 `886ba7f` 工件与进程，不随新 CLI 安装重启；schema v5 |
| 总览窗口 | `OAX:overview.0`，window `@2` / pane `%2`；仅此旧 shell 被替换，核验时在线、列出 18 个登记 Agent |
| Skill 安装 | `~/.agents/skills/oax-join` 与旧长名均链接 canonical main；真实 `skills/list` 均可见、启用，旧链接 inode/mtime/target 不变 |

功能提交已快进合入 canonical `main`；本报告和证据随后独立提交。Git 提交/推送与运行态分开记录，最终 Git SHA 以交付提交及远端核验为准，不改写已安装工件来源。

来源见[固定构建](evidence/candidate-overview-build.json)、[原子安装](evidence/cli-installed.json)、[Skill 安装验收](evidence/skill-installed/verdict.json)。linked worktree 的 Go 首次构建误记录了外层工程 revision，已保留[失败记录](evidence/worktree-build-provenance-failure.json)；最终工件来自固定提交的干净独立 clone，错误来源工件未安装。

## 现场迁移与连续性

已安装旧二进制和 main 源码都使用 `@openagentx_managed` / `@openagentx_agent_id`；现场短名 `@oax-managed` / `@oax-agent-id` 是需要收敛的运行选项，不能据此反推哪个历史程序创建了窗口。

六域按真实 bridge Agent ID、原 thread、唯一 window/pane 与进程身份逐项核对后迁移，仅改目标窗口 options，读回成功才清除短名；保留准确的原人工标题。六窗口现仅有产品长名标记，不并存两套。见[迁移命令和前后证据](evidence/migration-applied/)、[独立脚本审查](evidence/migration-review/REVIEW.md)。

| Agent | window / pane 0 | 原 thread |
| --- | --- | --- |
| openagentx | `@8 / %11` | `01a0c98a-bef8-7730-9b2b-cbc2f3594559` |
| rhythm | `@28 / %55` | `01a0b4e3-7b2e-7822-8ae1-9f0e812115a9` |
| pay-service | `@27 / %54` | `01a0e016-d951-77d1-bc7e-d13662f4823c` |
| quote-service | `@3 / %3` | `01a1010a-c521-78b2-98f0-eaafa642c5fa` |
| identity-service | `@30 / %60` | `01a0e344-8d5f-7f30-a2a7-f5e217bf3f69` |
| oneaxe-voice | `@11 / %17` | `01a0ddcc-248f-7dd0-a263-67f1d53e0181` |

安装与总览切换前后核对：六域 Worker、后台 app-server、原生前台 PID/starttime、thread、instance/generation、配置、额外 pane，以及 daemon 和 Fleet 保持；用户 client 所在窗口不变。最终[独立核查](evidence/final-review/review.json)44 项满足，原始依据为[最终快照](evidence/final-review/final-independent.json)，不是仅结论 JSON。

限制：内核禁止读取部分旧进程及新 Overview 的 `/proc/PID/exe`；未声称取得这些运行 PID 的直接二进制哈希。对 Overview 独立核对了精确 argv、与安装后持续一致的 PID/starttime，以及启动路径所指磁盘工件的 SHA。旧进程连续性依据 PID/starttime、进程树、systemd、thread 及正式 API。

## 本批验证

| 验证 | 结果与证据 |
| --- | --- |
| 短指令真实模型准备 | PASS：输入精确 `$oax-join`，实际 `gpt-6-astra/high`，自动捕获当前 thread、隔离工作区不变、无 daemon/DB、副本准备未激活；[模型日志与判定](evidence/join-model-short/) |
| 真实 Native 与同 thread 重开 | PASS：独立 `gpt-6-astra/ultra` 一次只读 query，Task 成功且单 Run；退出释放终端锁，重开同 thread，不新增模型任务，额外 pane 保持；[真实协议与运行日志](evidence/runtime-e2e/INDEX.json) |
| Console 来源窗口与目标 | PASS：从 overview 和无关来源窗口分别执行正式命令，切到正确 pane 0；来源与额外 pane/PID/options 保持；[回执](evidence/runtime-e2e/run-02/console-verdict.json)及 tmux/PTY 原始记录 |
| Overview 真实交互 | PASS：160×45、80×24 列表/详情/准确跳转，超屏长详情首/尾/回首画面；自有 daemon 传输中断时旧数据/禁导航，恢复在线；[回执](evidence/runtime-e2e/run-02/overview-verdict.json)、[滚动证据](evidence/runtime-e2e/run-02/overview-scroll-verdict.json) |
| 旧 daemon 兼容与本机总览 | PASS：新候选实际读取运行中的旧 daemon；安装后总览在线，六域映射独立只读核对。真实 switch 在隔离环境验收，未跳动用户业务终端；[安装记录](evidence/overview-installed/verdict.json) |
| 必要集成/状态边界 | PASS：真实 tmux、选项失败恢复/冲突保护、103 Agent 分页、四并发、只读 GET、陈旧/重连；race、vet 和相关包回归；[窗口证据](evidence/terminal-impl/)、[总览独立审查](evidence/overview-review/review.json) |
| 两份 HTML | Chrome 实际渲染与交互、宽窄布局通过；受环境限制以原文注入自有空白页验证，file/HTTP 导航未验；[边界与截图](evidence/browser/README.md) |

Runtime 联合验收总计一个真实模型 Run，两个无 Worker 的排队测试 Task 均经正式 API 取消、0 Run。仅隔离 tmux/daemon/Worker/app-server 已清理，证据保留。各次夹具首败、修正和复验均保留，见[执行记录](EXECUTION-LOG.md)；没有重做模型掩盖首败。

## 保留边界

- 全局咨询/答复/续办摘要属于第二批；接入准备后的自动接管尚未纳入本批。
- 不代表支付、收费、数据迁移或六域业务功能验收；未发业务协作消息或实施业务仓库变更。
- Overview 是当前任务投影；列表可能含历史登记身份。没有虚构全局任务数、长期空闲账单结论或新增 30 分钟空闲验收。
- 真实模型 Native/Console 工件是 `562d463`，Overview 工件是最终 `eb043ca`；两者 Native/Console/Runtime 源码无变化，按影响核对复用证据，不另耗模型重跑。

原始证据长期保留：`~/.local/state/openagentx/validation/2026-10-05-terminal-overview-join/`。入库为脱敏副本，见 [SHA-256 清单](evidence/SHA256SUMS)；未包含真实凭据、环境文件或数据库。
