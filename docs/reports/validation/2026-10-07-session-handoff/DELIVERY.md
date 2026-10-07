# Agent 新会话交接交付记录

## 当前状态

- **产品实现与隔离真实验收完成。** 产品提交 `e644511f2763ebe7df40dc31f6efaf7a43067b2b`，固定工件 SHA-256 为 `d3dd1a32311badf5f053fdee2305c49bb84f6cd4138a01fa7810e9cedac241a1`；收尾提交仅包含验证脚本、证据与文档。
- **已合入本地 main，远端推送受阻。** GitHub 持续返回服务端 `Internal Server Error`，远端仍为 `8d32583`；[提交及失败记录](PUBLICATION.md)。不将本地合入视为已推送。
- **未安装、未重启正式服务、未切换任何业务 Agent 的 thread。** 本批交付正式入口与已验证源码，生产启用仍需部署支持 schema v7/session_handoff 的 daemon、CLI 与目标 Worker。
- 2026-10-07 收尾读取的 `~/.local/bin/openagentx` SHA 仍为 `4c448f6e7cdf94ccc4ad1c0c6e5de61ee4a4c3f86d70affa12dda009d5166f39`，不是本批工件。已安装状态与 Git 交付分别记录。

## 已实现

- `agent new-session` 默认只读预览，显式 apply 携带原 thread/版本和稳定幂等键。
- schema v7 活动会话指针；正式 query Task 初始化全新 Codex thread，成功结算后原子发布；身份、模型、协作对象与旧历史保留。
- 事务内拒绝忙碌、未完成咨询、旧 view/旧 Task、过期版本；失败/取消/lease 恢复保留原指针，不自动重复初始化。
- 新 Runtime capability 防止旧 Worker 忽略 ForceNew；原接入摘要不会注入已轮换的新会话。
- 清理命令支持 v7 活动会话删除闭包；不改变已保留业务历史。

## 真实验收

独立 profile、daemon、单 Worker、Codex 0.160.1、原生 tmux/PTY，共 **6 个真实 `gpt-6-astra` Run**，全部 query 成功且各有单 Run。完整依据见[证据目录](evidence/README.md)、[Task/Run 账本](evidence/real01/task-accounting.json)和[独立复核](EVIDENCE-REVIEW.md)。

| 验收项 | 实际结果 |
| --- | --- |
| 只读预览、忙碌拒绝 | Task/Run/绑定不变；旧 thread 正在执行时交接请求被拒绝 |
| 新会话交接 | `01a116d0-a0ae-7760-9449-41cf66043de9` → `01a116d9-48fc-7ce0-8bf1-015006451a70`；版本 0→1；职责 SHA、目录、模型保持 |
| 交接内容有效 | 首轮确认身份/目录/nonce，无工具；后续未提供 nonce 的普通 query 精确回显交接 nonce |
| 幂等与旧终端 | 同 key 返回同 Task；旧 CAS 被拒；旧 native view 提交被拒且不增 Task；重开实际 argv 指向新 thread |
| 协作保持 | managed binding ID/身份/peer/凭据保持，generation 1→2；external peer inbox 可读新 thread 的关联精确答复，未验 peer ACK/自动续办 |
| Worker 重启 | 配置特意保留旧 thread；重启后普通 query 和原生 view 仍进入新 thread |
| 历史保全 | 旧 Task/Run/SessionBinding 逐行哈希不变，新旧 thread 绑定并存；workspace 无业务修改 |

所有隔离 Worker、daemon、tmux 与后台 Codex 已清理；原始证据仍保存在 `~/.local/state/openagentx/validation/2026-10-07-session-handoff/real01/`。入库副本已脱敏并附 [SHA256SUMS](evidence/SHA256SUMS)。这不是正式业务切换或生产部署验收。

## 回归检查与首次失败

- Runtime、Bridge、Console API/client、Fleet 相关包通过。
- SQLite/迁移、ControlPlane、Panel、domain/API 包通过；覆盖发布/回滚、CAS/幂等、取消/恢复、待咨询、旧会话保护、owner 权限和清理闭包。
- 初次 Fleet 权限用例在工作树及未修改 main 上均失败：shell `umask 077` 将夹具要求的 0644 文件变为 0600；以 `umask 022` 运行后通过。未修改产品权限策略或此既有测试。
- 首次编译遇到并行实施中间签名不一致、CLI 临时值调用指针方法；均已修复。事务测试首次夹具错误已修正；不作为产品通过证据。
- `umask 022; /home/sky/tools/go/bin/go test ./...` 全仓通过；相关包 `go vet`、`git diff --check` 通过，冻结 ADR 未变。[构建/检查记录](evidence/real01/checks/build-and-checks.json)。
- 真实验收最初两次 peer 登记失败来自夹具认证：非交互密码输入及该命令不支持 `--password-file`。改用已有安全 PTY 输入后复用同一初始化 Task、nonce 和 peer，不重跑已完成模型；首次失败、恢复记录和前后 harness SHA 均保留。[验收计划与执行说明](PLAN.md)。
- 工件内嵌 VCS 信息误取 linked worktree 外层仓库，不用它证明源码。已将 335 个产品源码/依赖文件逐一与 `e644511` 对照，并按原命令重建；重建工件与实际验收工件字节 SHA 完全一致。[源码/工件映射](evidence/real01/checks/source-rebuild-proof.json)。

## Overview 澄清

正式 `OAX:overview` 的 `%2` 仍运行总览并更新时间；同窗口额外 `%63` 为 shell。用户已确认是多开 pane，并非总览退出。本批不改总览产品逻辑、不关闭额外 shell、不操作业务终端。

## 边界

- 本批不提供任意历史 thread 切回/fork，不自动接续未知业务，不承诺修复原生 TUI 偶发 exit0 或取消 fallback 后旧 endpoint 重连；受管 TUI 的 `/resume` 仍不是跨会话切换入口。
- 未覆盖生产部署、peer 自动续办和全部取消/Runtime 故障组合；失败回滚、取消及 lease 恢复属于事务集成证据，不能扩大为对应真实模型全矩阵通过。
- 新 view 的实际 argv/屏幕证明重开到新 thread，旧 view 的键盘拒写已实测；切换后两次普通 nonce query 从正式 API 发起，未另做新 view 键盘提交的正向 Run。
- `Adapter.Health` 仍检查配置中的旧 thread；当前保留旧历史的真实流程已通过。以后旧 thread 不可读或被外部 writer 占用时，需调整健康检查来源，详见[实现审查](REVIEW.md)。

## 使用与下一步

1. 部署兼容的 daemon、CLI 和目标 Worker 后，按[操作说明](../../../operations/agent-new-session.md)执行 `openagentx agent new-session AGENT --handoff-file FILE` 只读预览。
2. 明确目标与交接内容，待 Agent 空闲后按预览提供的 thread/版本 apply；成功后退出旧 view，再 `agent open AGENT --native`。当前业务 Agent 不会因合入代码而自动换 thread。
