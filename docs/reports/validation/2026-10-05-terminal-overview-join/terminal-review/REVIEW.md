# 阶段①终端身份独立预审

- 时间：2026-10-05T02:19:26.534837+00:00
- 基线 HEAD：`33ab0cc264790bb07a6e1ab29318463cc9db5715`；检查对象为当时尚未提交的阶段①源码，具体文件 SHA-256 见 [source-sha256.txt](source-sha256.txt)。实现代理仍可能继续改动；最终交付须核对影响。
- 结论：本次检查范围内未发现阻断项；仅为源码独立预审和一项真实隔离 tmux/PTY 检查，不是完整真实模型 E2E。

## 已核对关键边界

1. `agent_native.go:openAgentNative` 仅在 TMUX 非空时检查精确 TMUX_PANE；先查询该 pane 的 session，非 OAX 跳过纳管。OAX 路径使用 `PreflightAttach`、`BindCurrent(confirm=false)`，保留 overview、pane 0、同名/异身份保护。native 桥接调用在身份成功后。
2. `agent.go` console 路径对 Reconcile 目标窗口操作，不绑定来源窗口；attach/switch 的目标增加 `.0`。隔离 PTY 中先选 pane 1，随后两种命令均实测切换到 pane 0；证据见 [target-pane-check.json](target-pane-check.json)。
3. `binding.go:stabilizeBinding/applyBinding/restoreBinding` 对 automatic-rename、allow-rename、remain-on-exit、marker 和名称失败采取补偿。捕获显式选项是否存在及其原值；即使命令返回错误也视为可能已执行。取消时使用独立有界上下文恢复；补偿失败保留 PartialFailureError。
4. `setPaneLabel` 是唯一新增告警后继续的位置，仅涉及 pane-border-status/format；身份和锁名相关错误仍返回失败。生产入口均设置 Warn 回调。
5. 同身份窗口不再提前返回，差异选项会修复并读回验证。Reconcile 修复已存在目标窗口时不应用请求启动模式的标签，避免把现存 native 活 pane 标成 Console。
6. 稳定身份路径不改 pane index、不 respawn；额外 pane 进程保留由代码路径及新增隔离集成断言覆盖。新建 provisioning/dead pane 的既有生命周期仍独立。此次未重新运行实现代理整套测试，集成测试最终结果由实现代理报告。
7. 新增设置均为 window 级 `-w -t <window-id>`；未见全局 tmux 配置或 hook 修改。

## 验证方法与限制

隔离检查通过 Python pty 创建 tmux 客户端；唯一 server 为 `oax-review-target-2083332`。建立 OAX:quote 两个 pane，先令 pane 1 active，分别执行 `tmux -L <isolated> attach-session -t OAX:quote.0` 与 `switch-client -c <isolated-client> -t OAX:quote.0`，使用 list-panes 的 pane_index/pane_active 核对。两次结果均为 `0:1, 1:0`，随后关闭该隔离 server 和客户端。执行 umask 为 022。

未触碰真实 OAX tmux、Worker、服务、安装、业务工程或冻结 ADR。未运行真实 Codex；native 集成测试中的 OpenNative 回调是 dispatch seam，不构成模型成功证据。既有 zsh 中普通调用 native 结束返回 shell，不代表 pane_dead，标记和 remain-on-exit 不保证 respawn-dead 自动恢复；本批未扩展进程托管。

## 下一步

1. 实现代理完成当前测试后，主代理将结果与本报告 SHA-256 做影响核对；实质逻辑变化再聚焦复审。
2. 按已批准计划由主代理安排真实 Codex E2E 与最终源码/安装分离举证。
