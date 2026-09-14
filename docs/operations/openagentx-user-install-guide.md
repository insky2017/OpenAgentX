# OpenAgentX 用户级安装与启动指南

## 适用范围

本指南用于单机 Linux 用户级部署：

- 可执行文件：`~/.local/bin/openagentx`
- 配置与状态根目录：`~/.openagentx`
- systemd units：`~/.config/systemd/user/openagentx.service`、`openagentx-worker@.service`
- 管理方式：`systemctl --user`

该布局不依赖源码仓库持续存在。Fleet 默认使用用户级 Worker unit 和
`systemctl --user`。系统级 `deploy/systemd/openagentx-worker@.service` 继续供明确的
系统部署使用；不要与用户级模板混装。

## 目录布局

```text
~/.local/bin/openagentx
~/.openagentx/
  openagentx.env
  release.txt
  data/openagentx.db
  run/openagentx.sock
  web/
  workers/
  identities/
  fleet.yaml
  backups/
```

`openagentx.env` 只放非秘密的 daemon 参数。密码、Cookie、CSRF token、Runtime
凭据和私钥不得写入该文件或 Fleet 清单。

## 1. 构建发布产物

在 OpenAgentX 仓库根目录执行：

```bash
go test ./...
go vet ./...
go build -buildvcs=false -o /tmp/openagentx-release ./cmd/openagentx

cd web
npm install --no-audit --no-fund
npm run build
cd ..
```

OpenAgentX 位于父仓库的 submodule 中时，Go 1.22 可能记录父仓库而非 submodule
revision，因此这里关闭自动 VCS stamp，并单独记录精确源码 commit 和二进制摘要：

```bash
git rev-parse HEAD
sha256sum /tmp/openagentx-release
```

## 2. 创建安装目录

```bash
install -d -m 0755 ~/.local/bin ~/.config/systemd/user
install -d -m 0700 ~/.openagentx ~/.openagentx/data ~/.openagentx/run
install -d -m 0700 ~/.openagentx/backups ~/.openagentx/workers ~/.openagentx/identities
install -d -m 0755 ~/.openagentx/web
```

daemon 的非秘密参数保存在 `~/.openagentx/openagentx.env`：

```text
OPENAGENTX_HTTP_ADDR=0.0.0.0:18100
```

若只允许本机浏览器访问，将值改为 `127.0.0.1:18100`。

## 3. 初始化或迁移数据库

全新安装时，在启动 service 前创建 owner：

```bash
/tmp/openagentx-release init --db ~/.openagentx/data/openagentx.db
```

从现有部署迁移时，先使用 SQLite 在线备份，不直接复制可能带 WAL 的数据库：

```bash
sqlite3 /path/to/current/openagentx.db \
  ".backup '$HOME/.openagentx/data/openagentx.db'"
chmod 0600 ~/.openagentx/data/openagentx.db
/tmp/openagentx-release schema verify --db ~/.openagentx/data/openagentx.db
```

## 4. 安装二进制、Web 资源和 unit

```bash
install -m 0755 /tmp/openagentx-release ~/.local/bin/openagentx
cp -a web/dist/. ~/.openagentx/web/
install -m 0644 deploy/systemd/openagentx-user.service \
  ~/.config/systemd/user/openagentx.service
install -m 0644 deploy/systemd/openagentx-worker-user@.service \
  ~/.config/systemd/user/openagentx-worker@.service
```

写入本次发布证据，不在其中记录秘密：

```bash
{
  git rev-parse HEAD
  sha256sum ~/.local/bin/openagentx
  date --iso-8601=seconds
} > ~/.openagentx/release.txt
chmod 0600 ~/.openagentx/openagentx.env ~/.openagentx/release.txt
```

## 5. 启动并验证 daemon

```bash
systemctl --user daemon-reload
systemctl --user enable --now openagentx.service
systemctl --user status openagentx.service --no-pager
loginctl show-user "$(id -un)" -p Linger
```

需要在用户退出登录后继续运行时，`Linger` 必须为 `yes`；否则由系统管理员执行
`loginctl enable-linger <user>`。

验证 HTTP、schema 和实际运行二进制：

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:18100/api/observe/v1/health
~/.local/bin/openagentx schema verify --db ~/.openagentx/data/openagentx.db

service_pid=$(systemctl --user show -p MainPID --value openagentx.service)
sha256sum ~/.local/bin/openagentx "/proc/${service_pid}/exe"
```

健康响应应为 `{"status":"ok"}`，两个二进制摘要必须一致。

## 6. 准备 Agent 配置

Fleet 清单保存在 `~/.openagentx/fleet.yaml`，只显式列出要管理的 Agent：

```yaml
version: 1
session: OAX
agents:
  - agent_id: quote-service
    worker_config: /home/<user>/.openagentx/workers/quote-service.yaml
    enabled: true
```

Worker 配置放在 `~/.openagentx/workers/<agent-id>.yaml`。路径不会展开 `~` 或
环境变量，Unix socket 和 Runtime workspace 应使用绝对路径：

```yaml
version: 1
agent_id: quote-service
transport: unix
unix_socket: /home/<user>/.openagentx/run/openagentx.sock
capabilities:
  - quote-service-development
runtime_backends:
  - backend_id: primary
    adapter_id: agy-batch
    options:
      binary: agy-graft
      working_dir: /absolute/path/to/workspace
      models:
        - model-name
```

Fleet 不创建虚构 Runtime 配置，也不再通过 identity 直接写数据库。先通过正式 Agent
管理入口创建 Agent。若为兼容旧 manifest 保留 `identity_file`，它只会核对 Agent ID、
组织和显示名是否与经认证控制面列表一致；不一致时 fail closed。

## 7. Console 与 Fleet workspace

Attach 必须从大小写敏感的 `OAX` session 内、目标 window 的 pane `0` 执行。旧
`agentx` session 不会自动迁移或合并。先登录 CLI Token 并创建 `OAX` workspace，再进入
对应 Agent window 的 pane `0`：

```bash
~/.local/bin/openagentx console login
~/.local/bin/openagentx fleet init --agent quote-service
tmux attach-session -t OAX
```

主菜单可在 tmux 外启动，用于 Login/Replace Login、Logout 和进入 Attach 流程：

```bash
~/.local/bin/openagentx console \
  --socket ~/.openagentx/run/openagentx.sock
```

Attach 必须在 `OAX` 的 pane `0` 中运行。可显式指定 Agent，也可复用 compatible managed
window marker；未绑定 window 会显示经认证控制面的 Agent selector：

```bash
~/.local/bin/openagentx console attach \
  --socket ~/.openagentx/run/openagentx.sock \
  --agent quote-service
```

Console 仅提供全屏交互模式，不提供 `--once` 或连续 JSON fallback。非 TTY 自动化应调用
Observe API；退出 TUI 只 detach Console，不会停止或 drain Worker。

`fleet up` 会先验证 canonical `~/.local/bin/openagentx` 可执行，并核对已加载用户 unit 的实际
`ExecStart`、`WorkingDirectory` 和 `EnvironmentFiles` 精确指向用户 home、
`~/.openagentx/workers/<agent-id>.yaml` 与对应 `.env`，然后才协调 workspace 并执行
`systemctl --user start`。Worker unit 只 `Wants` daemon 并自行重连，daemon stop/restart 不会
通过 systemd 依赖关系强停 Worker。Fleet 只读取 Linger 状态并提示，不会自动 enable unit、daemon
或 linger。Worker 环境文件若存在，必须由当前用户持有且权限不宽于 `0600`。

Console pane 退出后，pane `0` 会以 remain-on-exit 保留为 dead。确认 window 名、两个 marker
和 pane `0` 均 compatible 后，可显式恢复；该命令不会碰 live pane 或 pane `1+`：

```bash
~/.local/bin/openagentx fleet workspace --respawn-dead
```

## 8. 更新

先完成 graceful drain，再替换 daemon：

```bash
~/.local/bin/openagentx fleet down \
  --file ~/.openagentx/fleet.yaml

sqlite3 ~/.openagentx/data/openagentx.db \
  ".backup '$HOME/.openagentx/backups/openagentx-before-update.db'"
systemctl --user stop openagentx.service
install -m 0755 /tmp/openagentx-release ~/.local/bin/openagentx
cp -a web/dist/. ~/.openagentx/web/
systemctl --user start openagentx.service
```

更新后重复第 5 节的全部验证。终端中断 `fleet down` 的观察不会撤销已经持久化的
graceful-stop intent。

强制停止不是 graceful 的替代写法。只有明确接受活动 RunAttempt 可能变为 uncertain 时，
才使用两个独立确认：

```bash
~/.local/bin/openagentx fleet force-stop \
  --confirm-force-stop \
  --confirm-active-run-uncertain
```

## 9. 回滚

安装前保留上一版二进制和 SQLite 在线备份。启动失败时先停止 service，再恢复
二进制；只有确认新版本改变了数据库且旧版本无法打开时，才在 service 停止状态下
恢复数据库备份：

```bash
systemctl --user stop openagentx.service
install -m 0755 /path/to/previous/openagentx ~/.local/bin/openagentx
systemctl --user start openagentx.service
journalctl --user -u openagentx.service -n 100 --no-pager
```

不要在 daemon 运行时覆盖数据库，也不要删除失败现场、日志或备份。
