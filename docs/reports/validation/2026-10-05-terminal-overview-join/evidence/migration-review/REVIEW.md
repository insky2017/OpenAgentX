# 六域窗口 marker 迁移独立审查

记录时间：2026-10-05T02:33:42.686558+00:00

结论：已完成的只读审查未发现本次迁移阻断项。本报告仅持久化此前结论，没有重新审查或执行迁移。

## 审查对象与源码 SHA-256

源码目录：`/home/sky/work/touzi/OneAxe/OpenAgentX-terminal-overview-worktree`。

- `scripts/validation/migrate_terminal_markers.py`：`08224fa695e40cb2aaceb602b28731fec234303c02feb0939673b190452d30ef`
- `scripts/validation/terminal_overview_snapshot.py`：`d61c7d85fa65621b14a3408296ee1d528d0cea5d305ce966bf2f70793ce9ba12`

以上哈希于报告持久化时读取；审查对象为相应迁移脚本与快照脚本。

已阅读证据：

- `../migration-dry-run/plan.json`
- `../migration-dry-run/preflight-continuity.json`

## 已核查要点

1. 六个目标窗口依据名称唯一性、真实 native bridge Agent ID、Codex resume thread 与 Worker state 一致性及 marker 冲突检查确定；非目标窗口重复使用目标 marker 会拒绝迁移。
2. 每窗写入前重新核查精确 pane ID 对应的 session、window ID、window name、pane index 与 pane PID。变更命令仅使用目标 window ID 设置或清除窗口级 options，不包含 respawn、进程停止、服务重启或全局 tmux 设置。
3. canonical marker 和终端选项写入逐项读回，旧 marker 清除后读回；异常按已触碰窗口逆序恢复原选项值以及原先是否显式设置。恢复失败记录在 verdict；异常路径整体保持 FAILED，不误报 PASS。
4. 原始预检记录显示六域 service、thread、config、terminal、Worker identity 全部连续，非 overview pane、daemon 与 fleet 检查也通过。
5. 脚本迁移后比较业务服务及其后端进程、终端进程与 native 子进程的 PID/starttime 等记录、thread、配置、Worker instance/generation、额外 pane 和 fleet；失败不会报告迁移 PASS。

## 边界与限制

- 此结论为源码及既有 dry-run 证据审查，不是已经实施迁移的验收。
- 本审查未运行 `--apply`，未操作真实 tmux、服务、安装或业务进程，未修改产品源码。
- 未独立运行迁移故障注入；恢复判断基于脚本异常路径和原选项快照。
- 未重新运行真实 Codex 或业务模型任务；主代理负责已授权迁移及其后验连续性记录。
