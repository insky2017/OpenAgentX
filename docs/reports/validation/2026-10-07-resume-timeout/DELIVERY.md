# Codex 持续执行与 Agent 恢复修复

## 当前结果

- Codex 默认不设总执行截止（`timeout: 0s`），任务正常执行到 Runtime 完成；显式正时限和人工取消继续有效。AGY 保持原约定。控制面冻结规格、Worker descriptor、CLI 配置和 Adapter 一致；不是把30分钟简单改成4小时。
- 已登记 Agent 的 `resume` 核对经认证安装中的正式身份、角色路径、workspace 和能力；不再依赖一次性接入回执的原始字节。未登记身份仍使用原注册保护。合法旧 Fleet 没有 `identity_file` 时，以正式档案核对，不伪造接入资料。
- 已在正式安装上用候选 CLI 验证 Rhythm、Pay、Quote 的 `resume --no-open` 成功，原 thread 保持，没有重跑业务任务。随后正式 Runtime 与六域原生 Bridge 均已升级，见下方部署核验。
- 真实长任务已通过只读复核：[结果](evidence/long01/recheck-result.json)。本地工具1860.005秒、正式Run1898.785秒、冻结timeout0/无deadline；仅一个Run succeeded。Task保留uncertain/business_effect_unverified，由独立文件证据核验本次测试效果。首次脚本列名错误保留为FAILED，没有重跑模型。
- 联合工件真实验收与正式部署均通过。2026-10-07 11:29 UTC 独立核验：七服务、六 Bridge 实际工件匹配，六域 online/healthy/ready，原 thread/window/pane 与模型偏好保留，六 Worker 均为 `timeout: 0s`；当前正式 Run 也已冻结为无总截止。

## 工件与正式部署

最终产品源码为 `c35d70260eeb39bec83c457ff1acf06269ddf2be`，由干净克隆构建（`vcs.modified=false`）；[工件记录](evidence/final-artifact.json)固定 SHA-256：

```text
259ffc489f5a85a369c5ab40577fbba762846b15d18cc2e7aeaed195cb833e69
```

产品及部署脚本已在 `c6e4a775dd61cddff9d3c5d7da697a135f1f0c67` 合入并推送 main。后续交付文档提交不改变已安装产品源码来源。安装位置为 `~/.local/bin/openagentx`，schema 仍为 v6，无数据库迁移。

本次唯一部署由 `/home/sky/docs` 的[独立指挥者](../../../operations/2026-10-07-oaxops-handoff.md)执行：10:58:11 UTC 正式请求六域 graceful stop，等待活动任务自然结束；11:22:38 全部旧 Worker 离线后安装，11:22:39 更新配置并重启 daemon，随后恢复六域 Worker 与原生终端。11:25:35 返回 [PASS](evidence/deployment01/result.json)。完整步骤见 [operations.jsonl](evidence/deployment01/operations.jsonl)。没有强杀活动 Run、重放不确定业务任务或直接修改数据库。此次存在授权的服务重启间隔，不能描述为进程全程不中断。

[独立现场核验](evidence/deployment01/independent-verification.json)直接读取运行进程工件、正式 API、配置和 tmux；七服务及六 Bridge 的 `/proc/PID/exe` 哈希全部一致。六域对照如下（原值见 [before](evidence/deployment01/before.json)，部署后见 [after](evidence/deployment01/after.json)）：

| Agent | generation 前 → 后 | 保持的原 thread | 保持的 window / pane |
| --- | --- | --- | --- |
| openagentx | 2 → 3 | `01a0c98a-bef8-7730-9b2b-cbc2f3594559` | `@8 / %11` |
| rhythm | 3 → 4 | `01a0b4e3-7b2e-7822-8ae1-9f0e812115a9` | `@28 / %55` |
| pay-service | 5 → 6 | `01a0e016-d951-77d1-bc7e-d13662f4823c` | `@27 / %54` |
| quote-service | 54 → 55 | `01a1010a-c521-78b2-98f0-eaafa642c5fa` | `@3 / %3` |
| identity-service | 2 → 3 | `01a0e344-8d5f-7f30-a2a7-f5e217bf3f69` | `@30 / %60` |
| oneaxe-voice | 2 → 3 | `01a0ddcc-248f-7dd0-a263-67f1d53e0181` | `@11 / %17` |

本轮正式 `task-c7a21368-74e1-4f26-b425-77b39aff94ea` / `run-69e12973-bde8-45a4-952d-46d3b3dfe73a` 的 Console API 无 deadline，持久冻结 `spec.timeout=0`。它在 OpenAgentX 原 thread 接续执行；部署脚本另将收尾核验 [task-7d8c2265-010a-4c45-b6e3-67b61041a497 排队](evidence/deployment01/verification-task.json)，该回执只证明排队，不冒称另一轮已执行。五个空闲域的本地 Runtime 元数据保留 `restored_unconfirmed`，正式 API 显示可接单；没有为改变该历史标记而重做业务。

## 最终工件真实联合验收

2026-10-07 10:57:55 UTC [真实联合结果 PASS](evidence/release01/release-result.json)：同一隔离原生 TUI 提交一次15秒工具，再提交一次补充输入，单 Run 完成两个精确文件；Worker/Bridge/Codex、正式 Task/Run/Journal、终端渲染和独立文件效果共同举证。

- [正式 Run](evidence/release01/release-final-run.json) succeeded；Task 的 `uncertain/business_effect_unverified` 原样保留，不改账本作成功包装。
- [冻结规格](evidence/release01/release-frozen-timeout.json)无总截止；[工具及文件](evidence/release01/tools/)独占创建，重复执行会失败；[最终画面](evidence/release01/release-final-screen.rendered.txt)包含两条工具执行与结果，补充输入不再留在待发送区。
- [running](evidence/release01/release-running-indicator.json) → [idle](evidence/release01/release-idle-indicator.json)，同 thread、原生终端持续存在、模型设置前后相同。原始 [PTY](evidence/release01/attached-client.pty.log)和 [API](evidence/release01/http/)一并保留。
- [联合回归](evidence/combined-tests.log)通过；隔离 fixture 已[关闭](evidence/release01/cleanup.json)，证据保留。

31分钟验收使用 `15f787f` 工件，最终工件为 `c35d702`。相关 Runtime/domain/planner 源码逐字节相同；差异及复用裁定见 [release-impact](evidence/release-impact.json)。最终工件另做上述真实联合验证，未为夹具错误或非时限代码差异重跑31分钟工具，也未把旧工件测量改记为新工件实跑。

## 为什么之前中断

旧 Worker 的30分钟配置进入冻结 Run 截止，Adapter 到点主动 `turn/interrupt`，即使仍在执行工具也会取消。现在默认没有这个总截止，只有显式配置才设置；显式超时结果标注 `deadline_exceeded`，不与人工取消混淆，也不自动重跑。

Rhythm 的旧 `.registered` 摘要与当前资料组合不符；Pay 的旧 receipt 不是合法 JSON。这些输入何时改变没有确证，不能归因于本次模型设置更新。日常恢复被错误地绑定到一次性引导资料，修复后使用正式登记身份；旧资料保持原样供审计。

## 验证边界

- 真实隔离31分钟任务使用独立 daemon、Worker、Codex app-server、正式 API；以原 Task/单 Run、模型真实工具日志、正式起终时间、文件效果和冻结规格共同判断，不能只信模型输出或文件里的自报时间。
- 初始验收脚本末尾把 SQLite 主键 `run_id` 写成 `id`。运行中的脚本不热改；保留首次夹具失败，再只读核验同一 Run，不重新执行31分钟工具动作。
- 真实 SQLite 集成覆盖损坏 receipt、合法 timeout/模型变化、旧 manifest，以及身份/角色路径/workspace/跨安装冲突。关键状态机覆盖显式超时和无限期任务人工取消；已有 AGY 正时限不放开。
- CLI 恢复与 Runtime 执行、Git 提交与实际安装、Run 成功与 mutation Task 的业务复核状态分别报告。全局默认无限不保证所有任务必然完成：显式用户取消、真实连接错误、宿主退出和不确定副作用仍按原契约处理。

## 相关并行事项

- [tmux 两态 spinner](../2026-10-07-tmux-work-spinner/DELIVERY.md)与[原生消息确认](../2026-10-07-native-pending-input/DELIVERY.md)随最终工件安装；各自真实验收与限制单独报告。
- 指挥者新 thread `01a115e8-e528-75f3-9e30-612d617cfb84` 在独立 user-systemd 运行，已读组织分工及 docs 索引，并实际执行本次部署。它从10:55到11:27跨越 OAX 重启，最后同 thread `turn.completed`、exit0；见[回执](evidence/commander-deployment01/result.json)和[回复](evidence/commander-deployment01/final.md)。它不在 OAX tmux 中自动创建窗口；`oaxops open` 才进入同一原生前台，`status` 可查最近事件。当前 writer 已释放，无空闲模型轮询，也尚未接入 OAX Mailbox 自动收件。
- 保留一个已证实、独立于输入确认的既有问题：取消后台工具缺失 OS PID 时，fallback 会重建 app-server；旧 Bridge 仍连接旧 endpoint，可能停在 Reconnecting，需同 thread 重开 view。本批真实补充输入确认已通过，但不宣称取消后的自动重连已修好。

## 证据

本机原始目录：`~/.local/state/openagentx/validation/2026-10-07-resume-timeout/`。脱敏副本、[原始/归档映射](evidence/archive-provenance.json)及 [SHA-256 manifest](evidence/manifest.json)已入库；私密配置备份、凭据和指挥者原始模型全文不入库。首次失败、只读补判和最终复验分别保留。
