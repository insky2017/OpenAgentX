# 终端身份、接入短名与基础 Overview

用户于本轮确认实施；基线 main `eebac37`。开发分支 `codex/terminal-overview-join`，最终分批合入并推送 main。代码交付与本机安装、运行态切换分别举证。

## 本轮结果与边界

1. `agent open --native` 在 OAX pane 0 使用准确当前 pane/window 绑定身份；Console 对实际目标窗口固化。复用 Fleet 的验证、恢复和标签，不改变其他 pane。身份冲突、目标不明或身份写入部分失败拒绝此次原地打开；仅边框显示失败允许告警继续。无 tmux/非 OAX 的 native 使用保持可用。
2. 使用源码权威标记 `@openagentx_managed` / `@openagentx_agent_id`。先核对已安装工件和短名标记差异，再逐窗迁移六个业务窗口的 `@oax-managed` / `@oax-agent-id`，核实后清除短名，保留进程及 thread。锁定自动重命名，补齐准确终端标签。
3. `$oax-join` 为标准短入口，`$openagentx-join` 为兼容入口；共用准备实现，仅收集缺失资料，保留当前 thread。范围止于离线准备，自动接管后置。
4. 基础 Overview 提供系统连接状态、Agent 列表、当前任务/阻塞与待处理提示、选中详情、准确终端跳转。用户已授权接管替换现有 `OAX:overview`；协作咨询/答复/续办的全局摘要及事件投影属于第二批，本批不实现。

## 运行边界

- 六个业务 Agent：openagentx、rhythm、pay-service、quote-service、identity-service、oneaxe-voice。Worker、原生终端和原 thread 必须保留；不为安装重启这些 Worker、后台引擎或业务 pane。
- 原子安装新 CLI 只影响后续启动；保留现有进程持有的旧工件，记录新旧工件差异。若确需重启/替换任何业务 pane，先报告原因并等待用户确认。overview 替换已单独获授权。
- 不改 tmux 全局配置/hook，不修改业务仓库、资金、回调或部署。测试使用隔离 tmux server/profile；普通状态刷新不调用模型。

## 顺序与验收

阶段 0：记录安装版本、SHA-256、main 与安装源码差异、两套窗口标记、Worker PID/启动时间及 instance/generation、pane PID 与 thread。证明六个业务身份的映射；不根据数字 window index 判断身份。

阶段 1：终端固化与 Skill 短名并行实施，独立检查后分项提交。真实 tmux 验证正确定位、同身份修复、重名/异身份/overview/pane 1 保护、额外 pane 保留、选项部分失败恢复及 native/console 入口。真实 Codex 检查短名/兼容名发现与准备；不重新声称自动接管通过。

阶段 2：基础 Overview，复用已有授权读 API，兼容当前已安装 daemon，不以增加后台协议为前置。真实 TTY 检查列表、详情、断线/恢复、正确 pane 跳转；显示状态依据真实 API，不以窗口存在代表 Worker 在线，不以 Task succeeded 代表业务验收。

阶段 3：受检新 CLI 原子安装，按已核实映射逐窗迁移临时标记，只替换 overview。前后对比六个 Worker、终端进程、thread、generation 以及非目标窗口不变。记录 source commit/二进制哈希/安装与实际运行来源，不重启后台。

原始证据：`~/.local/state/openagentx/validation/2026-10-05-terminal-overview-join/`；脱敏副本、SHA-256 清单及 DELIVERY/COVERAGE/EXECUTION-LOG 入库。保留首次失败和复验，不以 mock、退出码或截图替代真实运行证据。冻结 ADR 不改；每阶段报告提交、验收与是否安装。
