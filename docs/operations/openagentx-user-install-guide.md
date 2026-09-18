# OpenAgentX 用户级安装与启动指南

## 适用范围

本指南用于单机 Linux 用户级部署，目标是从一个已验证的 OpenAgentX 发布产物快速进入可用的
Worker 和全屏 Console：

- 可执行文件：`~/.local/bin/openagentx`
- 配置与状态根目录：`~/.openagentx`
- daemon unit：`~/.config/systemd/user/openagentx.service`
- Worker unit：`~/.config/systemd/user/openagentx-worker@.service`
- tmux workspace：大小写敏感的 `OAX`
- 管理方式：`systemctl --user`

默认 Worker 宿主是用户级 systemd。系统级
`deploy/systemd/openagentx-worker@.service` 仅供明确的系统部署使用，不要与本指南的用户级模板混装。

主机必须提供可用的 user-systemd manager 和 `tmux`。从源码构建时还需要仓库声明的 Go、Node.js 与
npm 版本；使用预构建发布产物时不需要编译工具。安装后确认当前 shell 能找到 canonical binary：

```bash
export PATH="$HOME/.local/bin:$PATH"
command -v openagentx
```

需要时将同一 PATH 设置加入用户自己的 shell 配置；systemd unit 使用绝对路径，不依赖交互 shell。

## 默认 profile

日常命令不需要重复指定数据库、socket、Fleet manifest、Worker 目录或 credential 路径。未设置任何
override 时，OpenAgentX 使用以下路径：

| 资源 | 默认路径 |
| --- | --- |
| SQLite | `~/.openagentx/data/openagentx.db` |
| Unix socket | `~/.openagentx/run/openagentx.sock` |
| Fleet manifest | `~/.openagentx/fleet.yaml` |
| Worker 配置目录 | `~/.openagentx/workers` |
| CLI credential | `~/.openagentx/credentials.json` |

解析优先级固定为：

```text
显式 flag > 资源环境变量 > OPENAGENTX_HOME > ~/.openagentx
```

资源环境变量分别是 `OPENAGENTX_DATABASE_PATH`、`OPENAGENTX_SOCKET_PATH`、
`OPENAGENTX_FLEET_MANIFEST`、`OPENAGENTX_WORKER_CONFIG_DIR` 和
`OPENAGENTX_CREDENTIALS_PATH`。默认安装不要设置这些变量；只有多 installation 或非标准布局才使用
override。显式或环境 override 一旦为空、为相对路径或无法 canonicalize，命令会 fail closed，不会退回
默认路径。

默认目录布局为：

```text
~/.local/bin/openagentx
~/.openagentx/
  openagentx.env
  release.txt
  credentials.json
  data/openagentx.db
  run/openagentx.sock
  web/
  workers/
    <agent-id>.yaml
    <agent-id>.env
  fleet.yaml
  backups/
```

## 快速开始

完成下面的文件安装后，一个新 installation 的主流程是：

```bash
# 创建 owner；密码通过 TTY 隐藏读取。
openagentx init

# 创建控制面中的正式 Agent；identity.yaml 是必需的业务输入。
openagentx agent apply --file /absolute/path/to/identity.yaml

# 启动 daemon 并验证默认数据库和 socket。
systemctl --user daemon-reload
systemctl --user enable --now openagentx.service
openagentx schema verify

# 创建 installation-bound CLI Token；密码仍只通过 TTY 输入。
openagentx console login

# 原子导入 Worker 配置，同时生成 fleet.yaml 和 OAX workspace。
openagentx fleet init \
  --agent quote-service \
  --worker-config quote-service=/absolute/path/to/worker.yaml

# 启动用户级 Worker，并确认控制面状态。
openagentx fleet up
openagentx fleet status

# Fleet 已在 Agent window 的 pane 0 自动启动：
# openagentx console attach --agent quote-service
# 用户通常不需要重复运行该命令，直接进入 OAX 即可。
tmux attach-session -t OAX
```

这条主路径只有 `agent_id`、Agent identity 和 Worker 配置是业务输入。DB、socket、manifest、canonical
Worker 目录和 credential 均使用默认 profile，不需要显式 path flag。

## 0. 安装或测试前预检

每轮安装、迁移或集成测试开始前，都必须先记录时间、目标 commit、现有服务、监听端口、tmux 现场和
profile 状态。OpenAgentX 的代码、测试、构建、安装、服务、数据库、Fleet、网络或 tmux 验证出现非预期
非零退出、超时或结果偏离本指南时，必须立即停止；在同一文档的执行记录中写明预期、实际结果、诊断过程
和解除证据后，才恢复后续步骤。操作人员自己的辅助查询若仅因路径拼写、参数或工作目录错误而失败，且已
确认没有改变 OpenAgentX 或系统现场，则纠正命令并记录后继续，不把它记作产品失败；若影响范围不能确认，
仍按产品异常停止。记录不得包含密码、Token、Cookie、Runtime credential 或私钥。

测试工具可能在 `/tmp/openagentx-*` 留下 daemon 或 TLS `socat` 代理。测试前必须主动枚举这些可识别的
遗留测试进程：

```bash
candidate_count=0
while read -r pid; do
  [ -n "$pid" ] || continue
  exe=$(readlink "/proc/$pid/exe" 2>/dev/null) || continue
  argv=()
  if ! { mapfile -d '' -t argv < "/proc/$pid/cmdline"; } 2>/dev/null; then
    continue
  fi

  candidate=false
  case "$exe" in
    /tmp/openagentx-*/openagentx|/tmp/openagentx-*/openagentx\ \(deleted\))
      if [ "${argv[1]:-}" = serve ]; then candidate=true; fi
      ;;
    */socat|*/socat\ \(deleted\))
      for arg in "${argv[@]}"; do
        if [[ "$arg" == *openagentx-* ]]; then
          candidate=true
          break
        fi
      done
      ;;
  esac

  if [ "$candidate" = true ]; then
    ps -o pid=,ppid=,user=,lstart=,stat=,args= -p "$pid"
    candidate_count=$((candidate_count + 1))
  fi
done < <(ps -u "$(id -u)" -o pid=)
printf 'candidate_count=%d\n' "$candidate_count"
```

`candidate_count=0` 表示没有候选。扫描器按 `/proc/<pid>/exe` 和 NUL 分隔 argv 识别进程，不在完整
`ps` 文本中匹配自身携带的表达式。`ps` 快照之后退出的短生命周期进程会被安静跳过；不要把读取
`/proc` 时的消失竞态当成遗留进程或失败。

发现候选进程时，不得直接使用宽泛的 `pkill`。先逐个检查 executable、完整 argv、owner、启动时间和
监听端口，确认它属于已经结束且可以丢弃的测试目录，也没有仍在运行的测试依赖它：

```bash
ps -o pid=,ppid=,user=,lstart=,stat=,args= -p <pid>[,<pid>...]
readlink -f /proc/<pid>/exe
tr '\0' ' ' < /proc/<pid>/cmdline; printf '\n'
```

身份或用途不能确定时立即停止并保留现场。确认后只向核验过的精确 PID 发送 `SIGTERM`，等待退出并确认
对应 `/proc/<pid>` 和测试端口都已消失；不得影响正式 `openagentx.service`：

```bash
kill -TERM -- <verified-test-pid> [<verified-proxy-pid>...]
systemctl --user show openagentx.service \
  -p ActiveState -p SubState -p MainPID -p Result --no-pager
timeout --signal=KILL 5s ss -H -ltn
```

端口存在性检查默认不要加 `ss -p`。`-p` 会扫描当前用户所有进程的全部 FD；其他长寿命进程发生 FD
泄漏时，这个扫描可能长时间不返回。需要证明正式监听归属时，以
`systemctl --user show openagentx.service -p MainPID --value` 获取 PID，再将 `/proc/net/tcp*` 中的 LISTEN
socket inode 与 `/proc/<MainPID>/fd/*` 精确关联。

预检还必须只读盘点 `OAX`，不要自动 rename、move、kill 或向 pane 发送按键：

```bash
tmux list-sessions -F '#{session_name}\t#{session_attached}\t#{session_windows}'
tmux list-windows -t '=OAX' \
  -F '#{window_id}\t#{window_index}\t#{window_name}\t#{@openagentx_managed}\t#{@openagentx_agent_id}\t#{window_panes}'
```

## 1. 构建发布产物

优先使用已经完成完整验证并带有 SHA-256 的发布产物。如果需要从源码构建，应从目标 commit 的 clean
standalone checkout 构建，不要从带父仓库 VCS 歧义的 linked submodule worktree 直接发布：

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go mod verify
go build -buildvcs=true -o /tmp/openagentx-release ./cmd/openagentx

cd web
npm ci --no-audit --no-fund
npm run build
cd ..

git rev-parse HEAD
sha256sum /tmp/openagentx-release
go version -m /tmp/openagentx-release
```

`go version -m` 必须显示预期的 `vcs.revision` 和 `vcs.modified=false`。缺少 `vcs.*`、revision 不一致或
`modified=true` 的产物不得安装，应在同一 commit 的 clean standalone checkout 中重新构建。

## 2. 安装文件

创建私有 profile 目录：

```bash
install -d -m 0755 ~/.local/bin ~/.config/systemd/user
install -d -m 0700 ~/.openagentx ~/.openagentx/data ~/.openagentx/run
install -d -m 0700 ~/.openagentx/backups ~/.openagentx/workers
install -d -m 0755 ~/.openagentx/web
```

安装二进制、Web 资源和两个用户级 unit：

```bash
install -m 0755 /tmp/openagentx-release ~/.local/bin/openagentx
cp -a web/dist/. ~/.openagentx/web/
install -m 0644 deploy/systemd/openagentx-user.service \
  ~/.config/systemd/user/openagentx.service
install -m 0644 deploy/systemd/openagentx-worker-user@.service \
  ~/.config/systemd/user/openagentx-worker@.service
```

在 reload 或启动服务前，按最终安装名静态校验两个用户级 unit；此命令不会 reload 或启动 unit：

```bash
systemd-analyze --user verify \
  ~/.config/systemd/user/openagentx.service \
  ~/.config/systemd/user/openagentx-worker@.service
```

不要直接在 `deploy/systemd/` 中使用源文件名运行上述校验。该目录还包含系统级的同名
`openagentx.service`，Worker 的依赖解析可能误加载系统级 unit，并产生与实际用户级安装无关的
`User=` 校验错误。

daemon 的非秘密参数保存在 `~/.openagentx/openagentx.env`。只允许本机访问时监听回环地址：

```bash
printf '%s\n' 'OPENAGENTX_HTTP_ADDR=127.0.0.1:18100' \
  > ~/.openagentx/openagentx.env
chmod 0600 ~/.openagentx/openagentx.env
```

需要其他机器直接访问时，应改为监听所有 IPv4 接口，并通过主机防火墙只允许可信来源访问 TCP
`18100`：

```bash
printf '%s\n' 'OPENAGENTX_HTTP_ADDR=0.0.0.0:18100' \
  > ~/.openagentx/openagentx.env
chmod 0600 ~/.openagentx/openagentx.env
```

`0.0.0.0` 只是监听地址，不是客户端访问地址；客户端应使用主机的实际 IP 或受控域名。跨不可信网络
访问时，应在 OpenAgentX 前部署 HTTPS 反向代理、VPN 或等效的受控入口，不要直接暴露明文 HTTP。
更新已有 installation 时保留已经确认的监听配置，除非确实要改变访问边界。

写入本次发布证据，不在其中记录密码、Token、Cookie、Runtime credential 或私钥：

```bash
{
  git rev-parse HEAD
  sha256sum ~/.local/bin/openagentx
  go version -m ~/.local/bin/openagentx
  date --iso-8601=seconds
} > ~/.openagentx/release.txt
chmod 0600 ~/.openagentx/release.txt
```

## 3. 初始化 owner 和 Agent

daemon 启动前创建首个 owner。命令使用默认数据库路径，密码至少 12 个字符并通过 TTY 隐藏读取：

```bash
openagentx init
```

然后准备 Agent identity。`instructions_path` 必须指向非空普通文件，`workspace_root` 必须是已存在目录；
相对路径按 identity 文件所在目录解析。最小示例：

```yaml
version: 1
agent_id: quote-service
principal_id: agent-quote-service
organization_id: default
display_name: Quote Service
profile:
  instructions_path: /absolute/path/to/ROLE.md
  workspace_root: /absolute/path/to/workspace
  capabilities:
    - quote-service-development
```

通过正式管理命令应用 Agent，数据库仍使用默认路径：

```bash
openagentx agent apply --file /absolute/path/to/identity.yaml
```

重复应用相同定义是无事件幂等操作；与既有身份冲突时会 fail closed。

## 4. 启动并验证 daemon

```bash
systemctl --user daemon-reload
systemctl --user enable --now openagentx.service
systemctl --user status openagentx.service --no-pager
openagentx schema verify
curl --fail --silent --show-error \
  http://127.0.0.1:18100/api/observe/v1/health
```

健康响应应为 `{"status":"ok"}`。确认运行进程就是已安装产物：

```bash
service_pid=$(systemctl --user show -p MainPID --value openagentx.service)
sha256sum ~/.local/bin/openagentx "/proc/${service_pid}/exe"
```

两个摘要必须一致。若需要用户退出登录后 daemon 和 Worker 仍常驻，检查：

```bash
loginctl show-user "$(id -un)" -p Linger
```

`Linger` 必须为 `yes`；否则由系统管理员执行 `loginctl enable-linger <user>`。Fleet 只检查并提示，
不会自动修改 linger。

## 5. 登录并启动 Fleet

CLI Token 只能由 Console Login 创建。登录密码不会进入 argv、环境变量或 credential 文件：

```bash
openagentx console login
```

准备 Worker 配置源文件。Unix socket 和 Runtime workspace 使用绝对路径，Runtime adapter 必须能在
user-systemd Worker 环境中执行：

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
      binary: /absolute/path/to/agy-graft
      working_dir: /absolute/path/to/workspace
      models:
        - model-name
```

首次初始化使用 `--worker-config` 导入源文件。Fleet 会验证捕获的内容并以 `0600` 原子安装到
`~/.openagentx/workers/quote-service.yaml`，同时以 `0600` 创建 `fleet.yaml`，不会静默覆盖冲突文件：

```bash
openagentx fleet init \
  --agent quote-service \
  --worker-config quote-service=/absolute/path/to/worker.yaml
```

如果 canonical Worker 配置已经存在且内容有效，可以省略 `--worker-config`：

```bash
openagentx fleet init --agent quote-service
```

交互式 `fleet init` 也可以省略 `--agent`，从经认证的完整 Agent 列表中明确多选。非 TTY 环境必须
显式重复提供 `--agent`，不得从目录或 tmux 猜测 Agent。

启动和检查 Worker：

```bash
openagentx fleet up
openagentx fleet status
```

`fleet up` 会在 tmux 或 systemd 副作用前验证 canonical binary、Worker config，以及已加载用户 unit
的 `ExecStart`、`WorkingDirectory` 和 `EnvironmentFiles`。所有 Worker `systemctl` 调用使用
`--user`。Worker unit 只 `Wants` daemon；daemon stop/restart 不会通过依赖关系强停 Worker。

## 6. 使用 Console Attach

`fleet init` 会创建或补齐 `OAX` session、overview window 和每个 Agent 的受管 window。如果大小写敏感的
`OAX` 已存在，Fleet 必须复用它，不得另建、改名或替换 session；缺少的受管窗口通过 `new-window -d -b`
插入到预检时首个既有窗口之前，并保持 manifest 顺序。只有本次新建窗口会获得 managed/Agent marker；
既有窗口的 ID、名称、内容、pane、active 状态和相对顺序不变，也不会执行 `move-window`。前置插入会使
既有窗口的数字 index 顺延，因此 index 不能作为身份。如果既有 unmanaged 窗口占用了 `overview` 或
Agent ID 目标名，Fleet 仍 fail closed 并保留现场，不会接管或自动 rename。

Agent window 的 pane `0` 运行以下正式入口的等价命令：

```bash
openagentx console attach --agent quote-service
```

因此首次使用通常只需进入 workspace，再切换到目标 Agent window：

```bash
tmux attach-session -t OAX
```

Attach 无论是否显式给出 `--agent`，都必须位于大小写敏感的 `OAX` session、当前 window 的 pane
`0`，并通过 window name、managed marker、Agent marker 和经认证控制面 Agent 列表的完整校验。
错误 session、pane `1+`、冲突或 unmanaged 现场都会 fail closed，不会自动切换、覆盖或杀进程。

需要在一个有效的 `OAX` pane `0` 中手动启动时：

```bash
# 显式选择 Agent；不会显示 selector。
openagentx console attach --agent quote-service

# 当前 window 已兼容绑定到 Agent 时，可从 marker 推导 Agent。
openagentx console attach

# Diagnostic Attach 复用同一身份和控制机制，并要求 owner+diagnostic 权限。
openagentx console attach --agent quote-service --diagnostic
```

如果当前 window 未绑定，交互 TUI 会从经认证的完整 Agent 列表选择；绑定到另一个 Agent 必须明确
确认。tmux 名称不是权威身份，所有选择都要再经控制面验证。

`openagentx console` 可在 tmux 外打开主菜单，用于 Login/Replace Login、Logout 和查看已验证的 CLI
session 状态；但从菜单选择 Attach 时仍必须通过 `OAX` pane `0` preflight。在 tmux 外不能绕过这一
限制。

Attach 是全屏 TUI，包含实时状态、bounded Timeline、固定输入区、`/status` 和 `/help` overlay。
常用命令包括：

```text
/status
/dispatch <content>
/steer <task-id> <expected-version> <content>
/cancel <task-id> <expected-version>
/approve <approval-id> <expected-version>
/reject <approval-id> <expected-version>
/diagnostic
/help
/quit
/foreground
```

所有写操作只调用 authenticated official API；断线或 CLI session 过期时禁用且不本地排队。
`/foreground` 只显示“Foreground Takeover（规划中，暂不可用）”。`/quit` 仅退出 Console，不会
stop、drain 或 force-stop Worker。

Console 仅支持交互 TTY，不提供 `--once` 或连续 JSON fallback。非 TTY 自动化必须使用 Observe
API，非交互 Attach 固定 fail closed 且无副作用。

Console 退出后，受管 pane `0` 会以 dead pane 保留。可从普通 shell 显式恢复 compatible dead pane；
该命令不会碰 live pane、pane `1+`、unmanaged 或 orphaned window：

```bash
openagentx fleet workspace --respawn-dead
```

## 7. 日常使用

daemon 已启用时，日常恢复 Worker 和 Console：

```bash
openagentx fleet workspace --respawn-dead
openagentx fleet up
openagentx fleet status
tmux attach-session -t OAX
```

关闭 tmux client、window、session、SSH 或 Console 不影响 user-systemd Worker。普通停止必须使用
持久化 graceful drain-and-stop：

```bash
openagentx fleet down
```

命令先停止领取新任务，再持续展示 Agent、当前 RunAttempt、draining、elapsed 和最近状态，直到所有
目标 offline。终端中断只结束观察，不撤销已经持久化的 stop intent。不要用
`systemctl --user stop openagentx-worker@...` 冒充 graceful stop。

强制停止是独立危险路径。只有明确接受活动 RunAttempt 可能变为 uncertain 时，才使用两个确认：

```bash
openagentx fleet force-stop \
  --confirm-force-stop \
  --confirm-active-run-uncertain
```

退出本地 CLI session 使用：

```bash
openagentx console logout
```

Logout 只撤销/删除 CLI credential，不会停止 Worker、Task、Fleet 或 Web session。

## 8. 更新

下面流程适用于已经运行 ADR-008 的 installation。先 graceful drain，并在每个正在运行的 Console 中
执行 `/quit`，避免升级后继续保留旧 CLI 进程：

```bash
openagentx fleet down

sqlite3 ~/.openagentx/data/openagentx.db \
  ".backup '$HOME/.openagentx/backups/openagentx-before-update.db'"

systemctl --user stop openagentx.service
install -m 0755 /tmp/openagentx-release ~/.local/bin/openagentx
cp -a web/dist/. ~/.openagentx/web/
install -m 0644 deploy/systemd/openagentx-user.service \
  ~/.config/systemd/user/openagentx.service
install -m 0644 deploy/systemd/openagentx-worker-user@.service \
  ~/.config/systemd/user/openagentx-worker@.service
systemctl --user daemon-reload
systemctl --user start openagentx.service

openagentx schema verify
openagentx fleet workspace --respawn-dead
openagentx fleet up
openagentx fleet status
```

更新后重复 daemon health、运行二进制摘要和 `fleet status` 验证。若 CLI credential 已过期、被撤销或
installation 不匹配，先运行 `openagentx console login`；不要把 Token 放入命令参数。

从 ADR-008 之前的版本首次迁移时，旧 daemon 没有 CLI Token/session API，不能先用新版
`fleet down` 连接旧 daemon。应先按旧版本支持的正式流程完成 Worker drain。旧格式
`~/.openagentx/fleet.yaml` 不能由新版静默覆盖；确认它属于待淘汰的 pre-ADR profile 后，本次迁移按
既定决策直接删除且不备份：

```bash
rm -- "$HOME/.openagentx/fleet.yaml"
test ! -e "$HOME/.openagentx/fleet.yaml"
```

删除成功后，备份数据库和其余 profile，再安装新 binary/Web/units、restart daemon、执行 schema
验证和 `console login`，最后运行本指南的 `fleet init/up/status`，由新版创建 canonical manifest。已有的
大小写敏感 `OAX` 按第 6 节原地复用；旧 `agentx` session 不会自动迁移或合并。不要 rename、move、
kill 既有 tmux 窗口，也不要通过强停掩盖仍活动的 RunAttempt。

## 9. 回滚

安装前保留上一版二进制、unit 和 SQLite 在线备份。启动失败时先停止 daemon，再恢复二进制和 unit；
只有确认新版本改变了数据库且旧版本无法打开时，才在 daemon 停止状态下恢复数据库备份：

```bash
systemctl --user stop openagentx.service
install -m 0755 /path/to/previous/openagentx ~/.local/bin/openagentx
install -m 0644 /path/to/previous/openagentx.service \
  ~/.config/systemd/user/openagentx.service
install -m 0644 /path/to/previous/openagentx-worker@.service \
  ~/.config/systemd/user/openagentx-worker@.service
systemctl --user daemon-reload
systemctl --user start openagentx.service
systemctl --user status openagentx.service --no-pager
```

不要在 daemon 运行时覆盖数据库，也不要删除失败现场、日志或备份。

## 10. 排查入口

```bash
systemctl --user status openagentx.service --no-pager
journalctl --user -u openagentx.service -n 100 --no-pager
journalctl --user -u openagentx-worker@quote-service.service -n 100 --no-pager
openagentx schema verify
openagentx fleet status
loginctl show-user "$(id -un)" -p Linger
```

常见边界：

- 对用户级 unit 运行 `systemd-analyze --user verify` 时，校验已安装的 `~/.config/systemd/user/`
  路径，或把两个源文件复制为最终安装名后在隔离目录校验；不要让依赖解析选中
  `deploy/systemd/openagentx.service` 这个系统级 unit。
- `console login` 必须从交互 TTY 隐藏读取密码；Attach 还必须位于 `OAX` pane `0`。
- `~/.openagentx`、`workers` 等私有目录权限不宽于 `0700`；manifest、canonical Worker config、
  Worker `.env`、credential 和 release evidence 不宽于 `0600`，且必须由当前用户持有。
- Fleet 不扫描 Agent 目录，不从 tmux 猜 Agent，不生成虚构 Runtime 配置，也不静默覆盖冲突文件。
- tmux 冲突只报告位置和修复提示；不得用 `send-keys`、`capture-pane` 或自动 kill/move 掩盖现场。
- credential 失效时重新执行 `openagentx console login`；不得把密码或 Token 写入 env、manifest、日志或
  argv。

## 附录 A：本机迁移验证记录

### 2026-09-16：遗留测试进程清理与 `ss -p` 超时

迁移前只读预检发现两项从已结束浏览器测试遗留的进程，均不属于用户级 systemd installation：

- PID `3388105`：`/tmp/openagentx-browser-q4M4tu/openagentx serve`，使用临时数据库和 socket，监听
  `127.0.0.1:18238`。
- PID `3401224`：配套 `socat` TLS 代理，监听 `18239` 并转发到 `127.0.0.1:18238`。

在获得明确授权后，再次通过 `/proc/<pid>/exe`、`/proc/<pid>/cmdline`、owner、启动时间和监听端口核验
身份，仅向这两个 PID 发送了 `SIGTERM`。二者均正常退出，`/proc/3388105` 和 `/proc/3401224` 随后消失。
正式 `openagentx.service` 未被操作，仍为 `active/running`，`MainPID=1308083`，`Result=success`。

首次并行运行 `ss -ltnp` 复核测试端口和正式端口时，两条命令均在 10 秒后超时。按照异常即停止规则，
当时没有继续代码修改、文档修改或安装。获得诊断授权后进行只读串行排查，结果如下：

- `/proc/net/tcp` 和 `/proc/net/tcp6` 显示 `18100` 有 LISTEN socket，`18238/18239` 均无 LISTEN socket。
- `ss -H -ltn` 在约 62 ms 内正常完成，只显示 `*:18100`。
- `ss -H -ltnp` 在 5 秒硬超时后被终止，退出状态为 `137`。
- 受限 `strace` 显示 `ss -p` 正逐个扫描 `/proc/13222/fd/*`，超时时扫描到约 fd `45467`，不是阻塞在
  OpenAgentX socket 上。
- 无关进程 `/home/sky/.local/bin/agy`（PID `13222`）持有 `1,048,576` 个 FD，导致 `ss -p` 全局进程
  归属扫描无法在预期时间内完成。该进程不属于本次安装，未被终止或修改。

最终改用不带 `-p` 的 `ss` 和 socket inode 归属检查完成解除验证：`18238/18239` 均无监听；
`openagentx.service` 的 `MainPID=1308083` 通过 fd `12` 持有 `0.0.0.0:18100` 对应的 socket inode
`278467725`。后续端口检查固定使用受硬超时保护的 `ss -ltn`；需要进程归属时使用 systemd `MainPID` 和
`/proc` inode 关联，避免全局 `ss -p` 扫描。

### 2026-09-16：workspace 定向测试工作目录错误

完成 `workspace.go` 和 `workspace_test.go` 的首轮修改及 `gofmt` 后，自动定向测试从 `/home/sky` 而不是
OpenAgentX 仓库根目录启动。`go test ./internal/fleet ...` 因该目录没有 `go.mod` 退出 `1`，错误为
`go: cannot find main module`。这不是代码测试失败，也没有形成有效测试结论；发现后立即停止，没有重跑
测试或继续修改。

获得恢复确认后，先补写本记录。后续 Go 验证固定使用 `go -C <repository-root> test ...` 显式指定模块
根目录，不再依赖调用进程的当前目录。随后执行：

```bash
go -C /home/sky/work/touzi/OneAxe/OpenAgentX-adr008-worktree test ./internal/fleet \
  -run 'TestWorkspace(CreatesMissingOAXSessionWithMarkers|ReusesExistingOAXAndPrependsOnlyCreatedWindows|VerifiesCreatedAgentWindowBeforeStartingConsole)$' \
  -count=1
```

命令退出 `0`，结果为 `ok openagentx/internal/fleet 0.005s`。在独立 tmux socket 上的先行语义探针也确认，
连续用 `new-window -d -b -t <original-first-window-id>` 创建 `overview`、`quote` 后，窗口顺序为
`overview, quote, user-first, user-second`，原窗口 ID 和相对顺序保持不变。

新增的真实 tmux 集成测试通过独立 socket 构造两个 unmanaged `OAX` 用户窗口，其中第一个含 pane
`0/1`，第二个为 active。执行 Reconcile 后确认新建窗口顺序为
`overview, quote, user-first, user-second`；两个用户窗口的 ID、pane 和空 marker 均未改变，active
window 仍为原第二个用户窗口。命令如下，退出 `0`，结果为 `ok openagentx/internal/fleet 1.027s`：

```bash
go -C /home/sky/work/touzi/OneAxe/OpenAgentX-adr008-worktree test ./internal/fleet \
  -run '^TestIsolatedTmuxPrependsManagedWindowsWithoutTakingOverExistingOAX$' -count=1
```

同日对真实 `OAX` 只读盘点：5 个现有窗口依次为 `zsh`、`zsh`、`zsh`、`pi`、`agentx`，全部没有
`@openagentx_managed` 或 `@openagentx_agent_id`，其中 `pi` 为 active。它们与本次目标名 `overview`、
`quote-service` 不冲突，因此后续新版 `fleet init` 可以复用该 session 并只在最前面创建受管窗口；
此盘点没有对真实 tmux 现场进行任何修改。

### 2026-09-16：workspace 变更回归验证

workspace 前置逻辑和指南更新完成后，执行以下回归门禁，全部退出 `0`：

```bash
go -C /home/sky/work/touzi/OneAxe/OpenAgentX-adr008-worktree test ./internal/fleet -count=1
go -C /home/sky/work/touzi/OneAxe/OpenAgentX-adr008-worktree test ./internal/cli/fleet -count=1
go -C /home/sky/work/touzi/OneAxe/OpenAgentX-adr008-worktree test ./... -count=1
go -C /home/sky/work/touzi/OneAxe/OpenAgentX-adr008-worktree test -race ./... -count=1
go -C /home/sky/work/touzi/OneAxe/OpenAgentX-adr008-worktree vet ./...
go -C /home/sky/work/touzi/OneAxe/OpenAgentX-adr008-worktree mod verify
bash deploy/systemd/openagentx-worker_template_test.sh
bash scripts/check-legacy-control-paths.sh --release
git diff --check
```

其中 `internal/fleet` 和 `internal/cli/fleet` 的无缓存定向结果分别为 `10.236s` 和 `0.175s`；全仓普通及
race 测试均通过，`go vet` 和 shell 语法检查无输出，module 结果为 `all modules verified`，legacy
control-path release check 和 Worker template assertions 均报告 passed。全部真实 tmux 测试使用唯一
隔离 socket 并由测试 cleanup 回收，没有连接或修改默认 tmux server。

另外，将 `openagentx-user.service` 和 `openagentx-worker-user@.service` 复制到临时目录并分别改为最终
安装名 `openagentx.service`、`openagentx-worker@.service` 后，执行
`systemd-analyze --user verify`，命令以空输出退出 `0`，临时目录随后清理。未对已安装 unit 执行
reload、enable、start 或覆盖。

### 2026-09-16：遗留进程扫描器自匹配及预检恢复

在 `2026-09-16T10:26:07+08:00` 的迁移前只读预检中，为避免“无匹配”时 `grep` 返回 `1`，临时使用
`ps | awk` 枚举遗留进程。匹配表达式本身出现在执行器的完整 argv 中，结果把本次检查的
`/bin/bash -c`（PID `2132057`）和其 `awk` 子进程（PID `2132083`）误报为候选。发现结果偏离预期后
立即停止；没有删除文件、发送信号、安装产物、reload/restart systemd 或修改 tmux。

获得诊断授权后确认两个 PID 的 `/proc` 条目都已消失，且其先前输出的 executable/argv 只属于该次
检查，不是 OpenAgentX daemon 或 `socat` 代理。最初尝试保存 `checker_pid=$$` 并排除该 PID 及其
直接子进程；虽然真实扫描为 `candidate_count=0`，夹具也能排除 shell/`awk` 自身，但复核发现：若遗留
测试 daemon 本来就是当前交互 shell 的后台子进程，该方案也会把它排除，因此没有采纳。

第二版改为根据 `/proc/<pid>/exe` 和 NUL 分隔 argv 做结构化分类。首次真实扫描虽然得到
`candidate_count=0` 且分类夹具通过，但 `ps` 快照中的多个短生命周期 PID 在读取前退出，shell 为
`/proc/<pid>/cmdline` 的重定向输出了 `No such file or directory`。该非预期输出再次触发停止。根因是
shell 在启动 `mapfile` 前打开重定向文件，单独附着于命令的 stderr 处理不能覆盖这个错误。最终将整个
重定向和 `mapfile` 放进 `{ ...; } 2>/dev/null`，只跳过已经消失的 PID。

最终扫描器连续执行三次，均只输出 `candidate_count=0`，没有 stderr。固定分类夹具同时确认：临时目录中
argv `[1]` 为 `serve` 的 OpenAgentX、带 `(deleted)` executable 的同类进程，以及参数含
`openagentx-` 的正常/已删除 `socat` 会被识别；`bash`、`awk`、非 `serve` OpenAgentX 和无
OpenAgentX 临时参数的 `socat` 不会被误报。

同轮其余只读预检均正常：

- 源码 HEAD 为 `09435da2a6a3a7e113f357884dd408fa29540bad`，只有本记录和 workspace 修复对应的 4 个预期文件
  被修改，`git diff --check` 通过。
- 工具链仍为 Go `1.22.4`、Node.js `20.19.4`、npm `10.8.2`、tmux `3.4`、SQLite `3.45.1`；`/home`
  和 `/tmp` 所在文件系统分别有约 `471G`、`47G` 可用。
- `openagentx.service` 为 `active/running`、`Result=success`、`MainPID=1308083` 且 enabled；linger 为
  `yes`。canonical Worker 实例尚未安装，旧 Worker unit 仍为 disabled/inactive。
- `18100` 的 LISTEN socket inode `278467725` 仍由 daemon fd `12` 持有，loopback health 返回
  `{"status":"ok"}`；未使用全局 `ss -p`。
- 数据库 mode 为 `0600`，SHA-256 仍为
  `4b1ed0b0be5f736124ac1694e2789a33010473e295a90b77ad7095ea9d4762ce`，`quick_check=ok`，
  `user_version=0`。
- 旧 manifest 仍为 mode `0600` 且 `session: agentx`；canonical Worker template 仍不存在。真实 `OAX`
  仍有原 5 个 unmanaged 窗口，window ID、相对顺序、pane 数、active 状态和空 marker 均未漂移。

### 2026-09-16：standalone 发布检查的辅助路径错误

目标 revision `053d530b3dbb3e7bb994f361295770b2eef350f0` 提交后，使用 `--no-local` clone 创建
`/tmp/openagentx-adr008-standalone`，以 detached HEAD 检出该 revision；checkout 使用自身 `.git` 且
状态 clean。发布门禁前查询 Web 构建约定时，一条只读 `rg` 命令把仓库中不存在的 `Makefile` 与实际存在
的路径一起作为参数，因此 `rg` 报 `No such file or directory` 并退出 `2`。其余匹配输出不能改变该命令
失败的事实，也没有据此形成发布门禁结论。

这是辅助查询的路径假设错误，不是 OpenAgentX 代码、测试或构建失败。命令没有写文件、启动构建、修改
正式安装或接触真实 tmux；当时 `npm ci`、Go/Web 发布门禁和 release build 均尚未运行。后续移除不存在的
路径并继续，所有正式门禁只在 clean standalone checkout 中执行。

### 2026-09-18：Console TTY smoke 门禁失败与修复

目标 revision `9da49840b6aa80979b15315e69ac4311f7393ad8` 的 standalone checkout 保持 clean。首次并行启动
发布门禁时，执行器没有为六条命令指定 checkout：Go 在 `/home/sky` 报 module prefix 错误，shell 和
systemd 检查找不到相对路径，`npm ci` 也因 `/home/sky` 没有 lockfile 退出。这些命令没有实际运行
OpenAgentX 门禁，不构成产品失败；随后用 `go -C`、绝对路径和 `npm --prefix` 纠正。

纠正后的 `go test ./... -count=1` 出现真实失败：
`TestIsolatedTTYSmokeUsesAltScreenBindsAndPreservesExtraPanes` 在等待约 10 秒后报告 Console pane 0 未退出。
按产品异常停止，未继续 Web test/build 或 binary build。同期已经启动的其他门禁执行完毕：全仓 race、
`go vet`、`go mod verify`、tracked shell 语法、Worker template、legacy control-path release check 和隔离
user-systemd verify 均通过；`npm ci --no-audit --no-fund` 成功安装 123 个包。这些成功结果不覆盖普通测试
失败。

失败消息中的实际 pane 数据为 `0:1:`、`1:0:`、`2:0:`，格式是
`pane_index:pane_dead:pane_dead_status`。因此 pane 0 已经 dead，两个额外 pane 仍存活；测试只是因为
`pane_dead_status` 为空，没有满足硬编码的 `0:1:0`。生产 `/quit` 路径仍明确返回 `tea.Quit`，终端输出
也显示完整 `/quit` 和退出绘制。独立 tmux socket 探针确认 exit status 是与 `pane_dead` 分离的元数据；
原测试把“pane 已退出”和“helper 返回 0”错误地绑定成一个 tmux 字符串条件。

修复只修改 `internal/cli/console/smoke_test.go`，不改 Console 生产逻辑。helper 现在在 `Execute` 返回后、
`os.Exit` 前把退出码写入私有临时文件；测试先用 `pane_dead=1` 判断 pane 生命周期结束，再读取该文件并
要求内容为 `0`。`pane_dead_status` 和 `pane_dead_signal` 仅保留为诊断字段。修复后的无缓存验证均通过：

```bash
go -C /home/sky/work/touzi/OneAxe/OpenAgentX-adr008-worktree test ./internal/cli/console \
  -run '^TestIsolatedTTYSmokeUsesAltScreenBindsAndPreservesExtraPanes$' -count=1
go -C /home/sky/work/touzi/OneAxe/OpenAgentX-adr008-worktree test ./internal/cli/console -count=1
go -C /home/sky/work/touzi/OneAxe/OpenAgentX-adr008-worktree test -race ./internal/cli/console -count=1
```

结果依次为 `1.175s`、`1.233s` 和 `3.419s`，全部退出 `0`。所有 tmux 操作均使用唯一隔离 socket，
没有连接或修改默认 tmux server。
