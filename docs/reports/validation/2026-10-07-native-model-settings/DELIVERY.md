# 受管 Codex 模型切换修复交付

## 用户结果与范围

- 原生 `/model` 保存为当前 Agent 的持久偏好，后续 Run 使用选定模型及推理强度；已有 Run 的冻结规格不变。设置在 Run 规划读取时生效，排队任务取届时最新设置。
- Bridge 兼容 Codex `0.160.1` 的 `thread/settings/update`、模型相关 `config/batchWrite`，包括实际 TUI 附带的 `collaborationMode.settings`；不写全局 Codex config，不改变角色、目录或审批策略。
- 复用 schema v6 的 `execution_profiles` 和 `agent_profiles.default_execution_profile_id`，新增正式 Console 设置 API；SQLite 偏好、引用和审计同事务提交，CAS 防止覆盖并发修改。删除 Agent 时包含其独占执行设置。
- Worker 从真实 `model/list` 获取目录，菜单、控制面规划和 Adapter 校验一致；设置来源及版本进入 Run 的 resolved metadata。

## 当前交付状态

- 产品实现、真实模型切换和最终发布工件的隔离重启复验已通过；正式安装/重启交由独立切换单元执行，运行态最终结果待追加，不能将代码合入视为已部署。
- 已验发布源码 `4774f19103ff2b9fff7eaf2ce2527578f3d75a55`，二进制 SHA-256 `780adff7f44cc6c71f37e52e8afc1bcacdfde8436045b3f52d0bc2dfe3614dbe`；独立 Git clone、`vcs.modified=false`，见 [工件来源](evidence/release-artifact.json)。
- 开发工作树：`/home/sky/work/touzi/OneAxe/OpenAgentX-native-model-worktree`，分支 `codex/native-model-settings`，基线 `3e2a859`。
- 本机原始证据：`~/.local/state/openagentx/validation/2026-10-07-native-model-settings/`。关键脱敏副本、首次失败、独立判定和 SHA manifest 保存在本目录 evidence 中。

## 已验行为与失败记录

| 项目 | 结论 | 证据与边界 |
|---|---|---|
| 原生菜单切换 | 首次失败后修复通过 | 实际 TUI 把模型放在 collaborationMode.settings，首版白名单拒绝；读取真实 RPC 后补兼容并保留黄金样例。Astra high → Sol high 均无保存错误。 |
| 第一个真实模型任务 | Run 成功、独立文件效果成立 | `task-870f9cf7…` / `run-39d486da…`；Run 与 Codex rollout 均为 `gpt-6.1-sol / high`，`model-proof-before.txt` 精确为 `OAX_MODEL_BEFORE_6a451c\n`。 |
| Task 终态 | uncertain 与 Owner accepted 分开记录 | 两个原生 mutation Task 保留 `uncertain / business_effect_unverified`；独立核对文件内容/字节/SHA 后，分别通过正式 Owner review CAS 入口 accepted。未直接改数据库、未重复执行，未改结算机制。 |
| 隔离重启首次阻断 | 已定位并修正部署顺序 | 仅启动 Worker 会保留旧 generation 的 inherit 网络绑定，Worker online 不等于可接单；恢复必须经过正式 `agent resume --no-open` 网络握手，不直接改数据库。 |
| 最终工件重启后真实任务 | 通过 | 同一thread `01a11320-3816-7330-b3d9-506257c92737`、设置version3保留。原排队Task仅有一个Run `run-74349401-bdbc-4d78-96a8-31df2d758cbe`，`gpt-6.1-sol / high`，精确文件 `OAX_MODEL_AFTER_0cf391\n`。 |
| 真实RPC边界 | 通过 | 六类非法写均拒绝，设置/任务集合和全局config SHA不变；重开后的resume/config/read/model/list投影一致。 |
| 事务/版本/持久化 | 通过 | 真实 SQLite 集成验证偏好/profile/journal 失败回滚、CAS、重复同值、非法组合、重开仓库及真实 planner。见 [settings-boundary-tests.log](evidence/tests/settings-boundary-tests.log)。 |
| 受影响包回归 | 通过 | [changed-packages-final.log](evidence/tests/changed-packages-final.log)。 |
| 全套测试 | 未全部通过 | [首次全套结果](evidence/tests/go-test-all.log)：已有权限放宽与旧断言冲突，在 [main 基线复验](evidence/tests/baseline-main-tests.log) 同样失败；Console 首次刷新超时在 [单独复验](evidence/tests/console-workflow-recheck.log) 通过。首个新增持久化夹具 RuntimeIdentity 不匹配已修复，未把失败删除。 |

## 部署与恢复

用户明确授权修复后部署并重启。`scripts/validation/deploy_native_model_settings.py` 默认只读检查；`--apply` 才执行正式 Worker stop 命令，等正在运行的任务自然结束，再原子安装、重启 daemon、启动六域 Worker，再逐域运行正式 `agent resume --no-open` 确认当前 generation 的网络应用，最后只在原受管 pane 已退出时重开原生入口。

操作者自己位于 `openagentx` Worker，故切换由独立 systemd user 单元执行，不依赖将被重启的进程。结果和恢复步骤保存在持久目录；失败时尝试恢复服务及操作者并提交核验任务，不静默回滚、不强杀正在执行的 Run、不重试 uncertain 业务任务。成功后逐域核对原 thread、准确 window/pane、Worker generation、模型目录和 CLI 工件 SHA，再由原 Agent 完成最终核验与报告提交。

## 未扩展范围

- 不修复既有 mutation 结果复核机制，不声称支付或应用业务验收完成。
- 不支持在模型设置请求中替换角色、plan 模式指令、工作目录、沙箱或审批策略。
- 模型目录在 Worker 启动时发现；供应商改变目录后重启对应 Worker 刷新，暂不新增常驻模型调用或模型目录热更新平台。

## 追加核查：为何执行途中自动停止

- 最近一轮 `task-bad9a0de…` / `run-3e1356d3…` 的正式 Console API 显示：开始于 `2026-10-06T21:20:06.517731044Z`，截止于 `21:50:06.517731044Z`，在截止后约 56 毫秒结束，Task/Run 均为 `canceled`。见 [正式状态快照](evidence/interruption/task-bad9a0de-4c32-47f0-98e8-3fbf1149e2a0.json)。六域 Worker 均配置 `30m0s`，[配置投影](evidence/interruption/configured-timeouts.json)只保留 adapter 与 timeout。
- Codex 原始 rollout 显示最后一次工具输出距中断约 1.142 秒，随后 `turn_aborted reason=interrupted`，没有正常 final；压缩后仍继续工作，不能把上下文压缩当作任务结束。见 [独立结论](evidence/interruption/independent-assessment.json)和 [rollout 元数据](evidence/interruption/rollout-facts.json)。
- 源码链路：控制面冻结 `DeadlineAt` → Codex Adapter 建立 deadline context → 到点后 `turnHandle.collect` 调用 `RequestCancel` → 向 app-server 发送 `turn/interrupt` → canceled。该次是 OAX 的硬时限主动终止执行；证据不指向 TUI Bridge 断连。旧版没有在取消终态清楚区分超时来源，是可观察性缺陷。
- 最近另外两次 Run 分别约 60 秒、30 秒结束，均早于自己的 deadline；rollout 只显示 interrupted，现有证据不能识别取消来源，保持未知。不能将全部历史取消归为同一原因。见 [短轮一](evidence/interruption/task-56d86c42-f007-41df-8375-1bf6424ec605.json)、[短轮二](evidence/interruption/task-0081e465-24e9-4a5f-9466-43889298d407.json)。
- 后续建议：把长任务时限作为明确的 Agent 配置，终端显示截止时间和超时原因；需要跨轮续办时保存进度并从检查点恢复。不能对已发生副作用的取消任务自动重跑。本次模型切换发布不更改时限、取消语义或自动重试策略。

该追加核查为只读诊断；其证据单独 [SHA-256 清单](evidence/interruption/manifest.json)保存。部署会有一次有意的自然收尾与原 thread 接续，不能与本次非预期硬截止混为一谈。
