# 正式安装原生终端验收（I）

- 入口：`/home/sky/.local/bin/openagentx agent open codex-domain-e2e --native`，正式来源 commit `6b68eeb810c2b65d629cd8a817bad25a21483d5c`，入口运行二进制 SHA-256 `f9b74b7df378ee05a38d3b49dc465c9168ff3ad7c70a49f81430e21b7f6b3366`。没有裸 `codex resume`、tmux 注入、第二后台或服务重启。
- 首次打开真实回显迁移前旧历史、当前角色、正式 API 三轮和取消后成功查询；原生输入写文件，TUI 显示真实工具及最终回复 `INSTALLED-NATIVE01-DONE`。见 [原始终端脱敏副本](terminal.raw)、[可读 transcript](terminal-readable.txt)、[操作记录](pty-actions.jsonl)。
- 原生输入形成正式 Task `task-dfad9b9b-43f8-4e23-b452-8370c5ba3c27`、Run `run-bde3ff4b-3251-4de3-b479-c600898fbc69`。Run succeeded，Task uncertain；独立读取 `installed-native01.txt` 字节精确等于 `OAX-INSTALLED-NATIVE01\n`，未人工修改 Task 状态。见 [API/文件判定](../installed-native01-api/verdict.json)、[独立文件](../installed-native01-api/independent-file.json)。
- 首次 Ctrl-D 正常退出0后，在同 thread `01a0fb9b-4097-79d3-902f-6603a844fa15` 派正式 query，Task `task-64cf0c91-c634-4b9a-b1ea-dae18ce2a192` succeeded，reply=`INSTALLED-NATIVE-CLOSED-NEXT01`；重新打开真实终端回显原生写入及关闭期间 query。见 [重开 transcript](../installed-native02/terminal-readable.txt)。
- 两任务同 `worker-958389c6-d71a-4861-812e-40eafbecf1b3`、generation 2；前后 daemon/Worker PID、starttime、二进制SHA一致。后台PID2472259、前台PID2486580及重开PID2499373均从实际 `/proc/PID/exe --version` 得到 `codex-cli 0.160.0`；实际exe SHA均 `12eb3e81114588aca3b7998f4f19e8997b056aca08e57a7ca7c8a3ec8c652aad`。见 [前后台进程](live-processes.json)、[重开前台](../installed-native02/frontend-provenance.json)。
- 两次退出码均0；最终所有自有前台PID已消失，正式Agent仍ready，后台保持在线。见 [最终状态](../installed-native01-api/final-status.json)。本批不重复测试取消或竞争，不把文件核验改写为Runtime已知业务效果。

原始资料私密持久保存于 `/home/sky/.local/state/openagentx/validation/2026-10-02-codex-workflow/installed-native01`、`installed-native02`、`installed-native01-api`；同名目录为脱敏可复核副本，每目录均有 SHA256SUMS。真实终端操作窗口约08:44:33–08:49:03 UTC；后续仅整理证据。
