# 受管 Codex 模型切换修复交付

## 用户结果与范围

- 原生 `/model` 保存为当前 Agent 的持久偏好，后续 Run 使用选定模型及推理强度；已有 Run 的冻结规格不变。设置在 Run 规划读取时生效，排队任务取届时最新设置。
- Bridge 兼容 Codex `0.160.1` 的 `thread/settings/update`、模型相关 `config/batchWrite`，包括实际 TUI 附带的 `collaborationMode.settings`；不写全局 Codex config，不改变角色、目录或审批策略。
- 复用 schema v6 的 `execution_profiles` 和 `agent_profiles.default_execution_profile_id`，新增正式 Console 设置 API；SQLite 偏好、引用和审计同事务提交，CAS 防止覆盖并发修改。删除 Agent 时包含其独占执行设置。
- Worker 从真实 `model/list` 获取目录，菜单、控制面规划和 Adapter 校验一致；设置来源及版本进入 Run 的 resolved metadata。

## 当前交付状态

- 产品实现及第一轮真实模型切换已完成；最终构建的隔离重启复验、正式安装/重启尚待完成，不能将本记录理解为已部署。
- 开发工作树：`/home/sky/work/touzi/OneAxe/OpenAgentX-native-model-worktree`，分支 `codex/native-model-settings`，基线 `3e2a859`。
- 本机原始证据：`~/.local/state/openagentx/validation/2026-10-07-native-model-settings/`。正式交付副本及 SHA manifest 随验收更新。

## 已验行为与失败记录

| 项目 | 结论 | 证据与边界 |
|---|---|---|
| 原生菜单切换 | 首次失败后修复通过 | 实际 TUI 把模型放在 collaborationMode.settings，首版白名单拒绝；读取真实 RPC 后补兼容并保留黄金样例。Astra high → Sol high 均无保存错误。 |
| 第一个真实模型任务 | Run 成功、独立文件效果成立 | `task-870f9cf7…` / `run-39d486da…`；Run 与 Codex rollout 均为 `gpt-6.1-sol / high`，`model-proof-before.txt` 精确为 `OAX_MODEL_BEFORE_6a451c\n`。 |
| Task 终态 | 保留原有 uncertain | 原生 mutation 默认要求业务结果复核，Task 为 `uncertain / business_effect_unverified`；不得把 Run succeeded 写成 Task succeeded。本轮文件效果另行核实，未更改结算机制。 |
| 事务/版本/持久化 | 通过 | 真实 SQLite 集成验证偏好/profile/journal 失败回滚、CAS、重复同值、非法组合、重开仓库及真实 planner。见 [settings-boundary-tests.log](evidence/tests/settings-boundary-tests.log)。 |
| 受影响包回归 | 通过 | [changed-packages-final.log](evidence/tests/changed-packages-final.log)。 |
| 全套测试 | 未全部通过 | [首次全套结果](evidence/tests/go-test-all.log)：已有权限放宽与旧断言冲突，在 [main 基线复验](evidence/tests/baseline-main-tests.log) 同样失败；Console 首次刷新超时在 [单独复验](evidence/tests/console-workflow-recheck.log) 通过。首个新增持久化夹具 RuntimeIdentity 不匹配已修复，未把失败删除。 |

## 部署与恢复

用户明确授权修复后部署并重启。`scripts/validation/deploy_native_model_settings.py` 默认只读检查；`--apply` 才执行正式 Worker stop 命令，等正在运行的任务自然结束，再原子安装、重启 daemon、启动六域 Worker，并只在原受管 pane 已退出时重开原生入口。

操作者自己位于 `openagentx` Worker，故切换由独立 systemd user 单元执行，不依赖将被重启的进程。结果和恢复步骤保存在持久目录；失败时尝试恢复服务及操作者并提交核验任务，不静默回滚、不强杀正在执行的 Run、不重试 uncertain 业务任务。成功后逐域核对原 thread、准确 window/pane、Worker generation、模型目录和 CLI 工件 SHA，再由原 Agent 完成最终核验与报告提交。

## 未扩展范围

- 不修复既有 mutation 结果复核机制，不声称支付或应用业务验收完成。
- 不支持在模型设置请求中替换角色、plan 模式指令、工作目录、沙箱或审批策略。
- 模型目录在 Worker 启动时发现；供应商改变目录后重启对应 Worker 刷新，暂不新增常驻模型调用或模型目录热更新平台。
