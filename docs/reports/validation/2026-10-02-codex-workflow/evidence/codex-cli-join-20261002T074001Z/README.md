# 真实 PTY CLI 离线登记验收

- 范围：实际候选二进制的 `agent join --help`、`join --prepare`、相同 join 重复执行、文本和 JSON status；真实 PTY，五条命令退出码均为 0。
- 独立核验：首次与重复后的文件内容 hash/权限完全一致；receipt 为 `local_prepared`、`ready=false`；Fleet 禁用；数据库和 socket 不存在。
- `strace -f -e trace=process,connect` 显示每条命令只有候选 CLI 的一次 execve、零 connect；没有启动 Worker、Control、Codex 或其他子程序。Go 线程 clone 不视为子程序启动。
- 私密 `.env` 仅导出变量名、权限与 SHA-256，不导出明文。日志中的证据根目录替换为 `<RAW_DIR>`，用户目录替换为 `<USER_HOME>`；真实命令和原始资料留在本机私密目录。
- 原始目录：`/home/sky/.local/state/openagentx/evidence/codex-cli-join-20261002T074001Z`。候选来自记录的 HEAD 加工作区未提交改动，准确二进制 SHA-256 和构建信息见 provenance/build-info；没有替换已安装二进制。
- CLI 包 race 测试另见 `cli-race.log`。本证据只支持 CLI 入口与独立文件/进程核验，不支持真实模型、跨 turn Worker 或受管原生终端 E2E 结论。

下一步：由主代理统一验证 Runtime、原生桥接、Task/Run/Journal 与最终安装产物。
