# 受管 Codex 模型切换修复交付

## 用户结果与范围

- 原生 `/model` 保存为当前 Agent 的持久偏好，后续 Run 使用选定模型及推理强度；已有 Run 的冻结规格不变。设置在 Run 规划读取时生效，排队任务取届时最新设置。
- Bridge 兼容 Codex `0.160.1` 的 `thread/settings/update`、模型相关 `config/batchWrite`，包括实际 TUI 附带的 `collaborationMode.settings`；不写全局 Codex config，不改变角色、目录或审批策略。
- 复用 schema v6 的 `execution_profiles` 和 `agent_profiles.default_execution_profile_id`，新增正式 Console 设置 API；SQLite 偏好、引用和审计同事务提交，CAS 防止覆盖并发修改。删除 Agent 时包含其独占执行设置。
- Worker 从真实 `model/list` 获取目录，菜单、控制面规划和 Adapter 校验一致；设置来源及版本进入 Run 的 resolved metadata。

## 当前交付状态

- 产品实现和真实模型切换已通过；正式工件已安装，daemon、六域 Worker 和原生 Bridge 已切换。首次部署失败后，经限定范围恢复，2026-10-07 11:43 CST 正式核验为 `PASS_AFTER_SCOPED_RECOVERY`，见 [最终结果](evidence/deployment/recovery02/result.json)与 [独立复核](evidence/deployment/recovery02/independent-review.json)。不能将该结果解释为所有 CLI 生命周期缺陷已修复。
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
| 正式安装与运行工件 | 恢复后通过 | 七个服务、六个前台 Bridge 的实际 `/proc/PID/exe` SHA 均与已验工件一致；服务 PID 已变、Worker generation 上升。见 [运行快照](evidence/deployment/recovery02/final-snapshot.json)。 |
| 正式六域与原终端 | 恢复后通过 | 六域 online/healthy/ready，原 thread、原 window/pane 不变；六域设置 API 与 Bridge 只读 `model/list`、`config/read` 成功。见 [运行检查](evidence/deployment/recovery02/final-runtime-checks.json)。真实模型选择及文件效果沿用同一工件的隔离 E2E，未重放正式业务任务。 |
| 操作者自动接续 | 正在同原 thread 执行 | 部署脚本回派 `task-ba0f9dac…` / `run-24d93238…`，恢复后的本 Agent 实际读取结果并执行网络/前台恢复与归档；快照时任务仍在运行，不将其记为业务完成。见 [自动接续](evidence/deployment/recovery02/automatic-verification-task.json)。 |
| 事务/版本/持久化 | 通过 | 真实 SQLite 集成验证偏好/profile/journal 失败回滚、CAS、重复同值、非法组合、重开仓库及真实 planner。见 [settings-boundary-tests.log](evidence/tests/settings-boundary-tests.log)。 |
| 受影响包回归 | 通过 | [changed-packages-final.log](evidence/tests/changed-packages-final.log)。 |
| 全套测试 | 未全部通过 | [首次全套结果](evidence/tests/go-test-all.log)：已有权限放宽与旧断言冲突，在 [main 基线复验](evidence/tests/baseline-main-tests.log) 同样失败；Console 首次刷新超时在 [单独复验](evidence/tests/console-workflow-recheck.log) 通过。首个新增持久化夹具 RuntimeIdentity 不匹配已修复，未把失败删除。 |

## 部署与恢复

用户明确授权修复后部署并重启。独立单元 `oax-native-model-rollout-20261007.service` 经正式 Worker stop 等正在运行的任务自然结束，于 11:35 CST 原子安装并重启 daemon、六域 Worker。操作者自身也被正常收尾，脚本随后回派核验任务，在原 thread 继续处理。本次未强杀活动 Run、未重试 uncertain 业务任务、未修改业务工程或直接写数据库。

首次脚本结果保留为 [FAILED](evidence/deployment/deployment01/result.json)，其 [操作记录](evidence/deployment/deployment01/operations.jsonl) 与 [自动恢复记录](evidence/deployment/deployment01/recovery.json) 未覆盖：

- Rhythm 的 `.registered` 摘要与当前 receipt/identity/worker/role 输入组合不符；无法仅凭摘要定位哪个输入改变。
- Pay 的旧 `joins/pay-service.json` 不是有效 JSON，且无 `.registered`。两域 `agent resume` 在网络准备前失败，online Worker 的网络仍属于旧 generation。
- 脚本误将 pane 进程未退出视为旧终端未退出；OpenAgentX pane 已回到原 zsh，其他五域则确实仍留着旧前台 bridge/TUI。shell 存活与 Bridge 存活需要分别核对。

后续在本次恢复授权内执行最小处置：两域原 `inherit` 网络经正式 mode test/publish 应用到当前 generation；保留接入收据和注册保护。精确核对 PID、启动时间、argv、thread 和 pane 归属后，只结束旧前台 bridge 并重开原 native 入口，不再重启 Worker。原始屏幕只留本机私密目录，未入 Git。执行脚本与 [恢复操作记录](evidence/deployment/recovery02/operations.jsonl) 作为当次证据保存。

最终七个服务与六个 Bridge 的实际可执行文件 SHA 均为上述已验工件；六域 generation 为 OpenAgentX 2、Rhythm 3、Pay 5、Quote 54、Identity 2、Voice 2。六个原 thread、原 `@window/%pane` 均保持，模型设置 API 和新 Bridge 只读 RPC 全部通过。独立子代理重新核实六域 ready 与准确前台归属；工件 SHA 和 RPC 核查另有 [最终运行检查](evidence/deployment/recovery02/final-runtime-checks.json) 举证。[正式证据清单](evidence/deployment/manifest.json) 包含首次失败及恢复后状态。

本批部署脚本是当次执行记录，不是已验通用升级器；后续复用前须先处理两项接入收据缺陷，并修正空闲 shell 与旧前台进程的辨别。`agent open --native` 当前可用；两域通用 `agent resume` 仍有上述阻断，不能将网络恢复当作该命令已修复。

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

该追加核查为只读诊断；其证据单独 [SHA-256 清单](evidence/interruption/manifest.json)保存。本次部署的自然收尾与原 thread 自动接续已经发生，不能与此前非预期硬截止混为一谈。
