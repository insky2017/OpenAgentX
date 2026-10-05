# 测试 Agent 清理交付

已删除用户确认的 11 个测试身份及其专属 OAX 历史、12 个原生会话和 8 个测试目录。`OAX:overview.0` 已自动刷新为 7 个保留身份：六个业务领域及旧 `orchestrator`。本报告只判定本次清理，不代表业务功能或完整 ADR 验收。

要点：

- 清理功能源码 `3b6989eb65150e9360e9c734ebf7e1f9e3d7c60c` 已合入并推送 `main`，CLI 已原子安装。安装工件 SHA-256 为 `ddfaff6bd4370639b40bb6c2760e02e9245d948a2c62d149a4b2328a7a9479a7`；后续报告提交不改变安装来源。
- 正式数据库在线升级到 schema v6；没有重启 daemon、六域 Worker 或原生终端。清理结束快照中六域 PID/starttime、thread、Worker instance/generation、配置及终端都与安装前一致，保留历史集合没有减少。
- 首次 11 身份整批删除触发 2 秒截止并完整回滚。核对实际数据库副本后，按相互引用关系分为 8 批，用同一正式 CLI 完成；没有放宽时限、关闭外键或修改产品代码。正式每批数据库阶段为 272–1149 ms。
- 后续 turn 中断间隔出现独立运行态变化：OpenAgentX 自身 backend 的新进程始于 **05:39:15 UTC**，native/CLI 新进程始于 **07:47:04/05 UTC**，额外 pane 1 消失；其 Worker 和 thread 未变，其他五域未变。清理后快照时间为 **05:38:16 UTC**。变化原因尚未确认，不能把 07:49 的跨时段对照写成全部未变；详细差异保留。
- 两条包含业务角色的组织目录历史快照完整保留。共享日志、业务目录、Git 历史报告、公共配置与本批验收证据不在清理范围；AGY 原生删除留下的 11 个空 presence 文件不含会话内容、没有持锁者。

## 删除与保留

| 对象 | 本次结果 |
| --- | --- |
| 测试身份 / principal / profile | 各 11 个删除 |
| Task / Run | 33 / 35 条删除 |
| Task 消息 / Mailbox / 外部协作消息 | 36 / 38 / 6 条删除 |
| SessionBinding / external binding | 21 / 6 条删除 |
| Worker / backend 历史登记 | 各 35 条删除；候选服务实例停用 |
| 专属 Event Journal | 67,275 条删除；事件序号不重排 |
| 原生会话 | Codex 1 个 thread、AGY 11 个 conversation，正式接口删除并独立复核 |
| 专属工作目录 | 8 个目录、30 个文件删除；共享工作目录保留 |
| 保留身份 | `openagentx`、`rhythm`、`pay-service`、`quote-service`、`identity-service`、`oneaxe-voice`、`orchestrator` |

精确名单见[实施计划](../../../plans/2026-10-05-agent-cleanup.md)。数据库计数、分批顺序、每批耗时和连续采样见[正式结果汇总](evidence/production/production-summary.json)；它链接各批真实命令和逐步骤输出。外部目录单独归属核对并删除，见[目录删除回执](evidence/production/owned-workspaces-deletion.json)。不是仅隐藏总览条目。

## 现在的使用方法

```sh
# 默认只预览；先核对身份、历史数量、保留路径和阻断项
openagentx agent remove test-example --purge-history --dry-run

# 明确执行；停用目标 Worker、删除身份及其所属 OAX 历史和专属本机配置
openagentx agent remove test-example --purge-history --yes
```

相互通信的测试身份一起传入。大批历史超过时限时，按经过预览验证的独立关联组拆批；不能拆开有跨组引用的身份。原生会话和外部 workspace 需要先核对归属，不能把命令的 OAX 清理完成等同于全部外部历史删除。详见[操作文档](../../../operations/agent-removal.md)。

## 验收分层与真实证据

| 层次 | 已确认结果 | 证据及限制 |
| --- | --- | --- |
| 源码 / 集成 D | `go test ./...`、`go vet ./...` 通过；身份、共享引用、事务回滚、授权无残留、重放回执及目录边界检查 | [完整测试日志](evidence/production/go-test-final.log)；[首次失败日志](evidence/production/go-test-all.log)保留。首次失败为新夹具使用了错误的 not-found 类型，修正后通过；不以这些测试代替真实运行 |
| 真实隔离 R | 旧 daemon/Worker/native 在真实 Codex 工具执行期间在线迁移和删除另一身份，原终端继续执行下一任务，进程/thread 不变，精确文件正确 | [隔离报告](ISOLATED-VALIDATION.md)，含 Task/Run/Journal、PTY 命令、文件效果及锁测量。两条 mutation Task 的权威终态均为 `uncertain`，单 Run 成功，不改写为 Task 成功；最终工件复验边界另列 |
| 正式安装 I | 新 CLI 干净构建并安装，旧服务进程保持；v5→v6 18 ms，8 批清理全部成功 | [构建来源](evidence/production/release-build.json)、[安装回执](evidence/production/installation.json)、[分批回执汇总](evidence/production/production-summary.json) |
| 正式清理后核对 I | 只余 7 身份；候选任务/运行/绑定/Fleet 清空，外键有效，保留业务集合不减少；六域全部运行态对照通过 | [安装前](evidence/production/before-install.json)、[清理后](evidence/production/after-cleanup.json)、[真实总览 capture](evidence/production/overview-after.txt)、[独立核查](evidence/production/independent-final-review.json) |
| 原生历史 I | 12 个原生对象精确定位、正式删除、其他原生会话保留 | [原生历史报告](NATIVE-HISTORY.md)，版本、UUID、操作、索引/文件复核与原文 SHA 均列明 |
| 后续状态 | 07:49 对照发现自身域 backend/native 更换；不覆盖清理时证据，也不推断原因 | [后续快照](evidence/production/final-continuity.json)、[差异时刻](evidence/production/post-cleanup-runtime-difference.json) |

普通程序通过只读 Observe 采样，覆盖 05:20:54–05:39:13 UTC（两段重叠）；1009 个样本未出现 API 失败、六域离线或 instance/generation 变化，最大请求耗时 1349.96 ms。后段采样在 turn 中断时结束，此后没有连续监测证据。采样不调用模型；它验证 Worker 在线状态，不能代替 backend/native 进程快照或业务任务验收。

## 首次失败、分批恢复与边界

首次正式命令在停用测试实例、完成 18 ms schema 迁移后，删除 principal 阶段到达截止时间（2015 ms）并回滚。数据库历史没有部分删除，授权表与回执均为空，六域进程和 thread 不变。见[首轮命令](evidence/production/production-apply-command.json)、[步骤输出](evidence/production/production-apply.stdout.jsonl)、[错误原文](evidence/production/production-apply.stderr.txt)、[回滚核对](evidence/production/first-attempt-rollback-verification.json)。schema 升级和 systemd 停用已生效，未把跨资源操作误称为整体回滚。

只读在线备份副本定位到 principal 外键检查反复扫描约 37 万条 Journal。相同产品函数按 8 个关联组验证，全部保留行逐 rowid 内容 SHA 一致，外键有效、授权无残留；正式环境随后使用相同顺序，每批执行前检查六域 Worker 在线且 instance/generation 一致。副本和诊断过程中生成的数据库已删除，只保留程序、元数据与日志：[副本验证](evidence/production/batches-integrity.log)、[分批测量](evidence/production/bounded-results.json)、[副本删除回执](evidence/production/private-copy-deletion-receipt.json)。

没有达到“任意规模一次性删除”的保证；1 秒是软目标，正式 AGY 批为 1149 ms，仍在 2 秒硬上限内。未来若独立关联组本身超过预算，应另行优化查询/索引并重新验证，不提高截止时间碰运气。旧 Worker 对控制面断连仍缺少容错，本批未修改该行为；今后的 daemon 重启仍须独立安排，不能据本次在线清理通过宣称可无损重启。

## 原文与交付边界

原始日志、API 记录和 PTY 保存在本机 `/home/sky/.local/state/openagentx/validation/2026-10-05-agent-cleanup/`。Git 内保留脱敏副本及 SHA-256 清单：[总清单](evidence/SHA256SUMS)、[正式操作来源映射](evidence/production/source-manifest.json)、隔离与原生历史各自的来源 manifest。未复制凭据、完整 fencing token、环境变量内容或未脱敏原生 PTY。

下一步：

- 日常可继续使用六个业务 Domain Agent；本次没有修改其身份、职责、配置或 thread。
- 后续测试优先隔离 profile/数据库；必须进入正式总览的测试身份，在当批结束前按正式清理命令回收。
- 自身域后续 runtime/native 更换原因及旧 Worker 断连恢复是独立事项；本次只记录证据，不扩大为服务重启或运行时重构。
