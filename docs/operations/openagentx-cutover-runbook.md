# OpenAgentX 部署与切换演练

## 范围

本手册用于隔离环境演练 OpenAgentX daemon、Worker、Nginx/Tailscale HTTPS、PWA 和目标 SQLite。默认执行 `--dry-run`，不会停止服务、覆盖数据库或重载 Nginx。

## 前置检查

1. 构建并校验唯一二进制：

   ```bash
   go build -o bin/openagentx ./cmd/openagentx
   ./scripts/check-legacy-control-paths.sh --release
   ```

2. 在 daemon 启动前，通过本机交互式 CLI 创建初始 owner 和 Organization；不得使用默认密码或将密码放入命令行：

   ```bash
   /opt/openagentx/bin/openagentx init --db /var/lib/openagentx/openagentx.db
   ```

3. 由 owner 使用 `openagentx agent apply` 应用所需逻辑 Agent 身份，重复 apply 必须为无事件幂等操作。
4. 准备 `/opt/openagentx/bin/openagentx`、`/etc/openagentx/workers/*.yaml` 和权限为 0600 的 Worker 凭据。
5. 由 Tailscale 签发 `openagentx.tailnet` 证书，写入 Nginx 配置声明的位置；不得把私钥放入仓库。
6. 确认 daemon 仅监听 tailnet 私有地址，Worker 仅出站连接。

## 演练步骤

```bash
./deploy/scripts/rehearse-cutover.sh --dry-run
```

执行人员确认输出的 release、旧库和归档路径后，才可在隔离环境运行：

```bash
OPENAGENTX_OLD_DB=/srv/previous/openagentx.db \
OPENAGENTX_TARGET_DB=/var/lib/openagentx/openagentx.db \
OPENAGENTX_ARCHIVE_DIR=/srv/archive/openagentx-$(date +%Y%m%d%H%M%S) \
./deploy/scripts/rehearse-cutover.sh --execute
```

脚本按以下顺序执行：旧库只读副本和 SHA-256、目标库 schema verify、停止/启动 daemon 与 Worker、检查 systemd 状态、校验 Nginx 配置并 reload。Worker ready 前不开放业务入口。

## 故障恢复

若任一检查失败：

1. 立即 `systemctl stop openagentx.service openagentx-worker@*.service` 并在 Nginx 上关闭入口；
2. 保留归档副本和脚本输出，不修改归档文件；
3. 修复证书、配置或 Worker binding 后从 `--dry-run` 重新开始；
4. 只有 owner、数据库、Worker lease、SSE 重连和移动 PWA 检查全部通过，才进入 go。

## Go/No-Go

- Go test、race、vet 和目标 schema 检查通过；
- daemon/Worker service 状态 active，Worker heartbeat 和 mailbox ready gate 正常；
- Nginx HTTPS、SSE `proxy_buffering off`、安全响应头和多入口 Session 共享通过；
- 旧库归档 hash、权限和只读打开通过；目标 daemon 不加载旧库；
- 手机登录、创建 Task、审批/回复/取消、离线 fail-closed 和 PWA 安装通过。
