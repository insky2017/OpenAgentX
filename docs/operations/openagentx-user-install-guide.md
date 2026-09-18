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
~/.local/bin/agy-graft
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
install -m 0755 deploy/agy/agy-graft /tmp/openagentx-release-agy-graft

cd web
npm ci --no-audit --no-fund
npm run build
cd ..

git rev-parse HEAD
sha256sum /tmp/openagentx-release /tmp/openagentx-release-agy-graft
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

安装二进制、tracked AGY wrapper、Web 资源和两个用户级 unit。wrapper 是 AGY Worker 的唯一 Runtime
入口；必须记录其 SHA-256，并在 Worker YAML 中使用安装后的绝对路径，不得依赖交互 shell 的 `PATH`。
归档复制会保留构建目录的 mode，因此复制后显式把 Web 目录规范为 `0755`、静态文件规范为 `0644`：

```bash
install -m 0755 /tmp/openagentx-release ~/.local/bin/openagentx
install -m 0755 /tmp/openagentx-release-agy-graft ~/.local/bin/agy-graft
cp -a web/dist/. ~/.openagentx/web/
find ~/.openagentx/web -type d -exec chmod 0755 {} +
find ~/.openagentx/web -type f -exec chmod 0644 {} +
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

使用 Cloudflare Tunnel 等本机 connector 时仍保持该回环绑定，并把 tunnel origin 配置为
`http://localhost:18100`。外部客户端只访问 tunnel 的 HTTPS hostname；不要为了让本机 connector
连接 origin 而改成 `0.0.0.0`。变更后同时验证 HTTPS hostname 可用、`ss` 只显示回环监听，并使用
`curl --noproxy '*'` 确认主机的 LAN、VPN 和 bridge 地址都不能直连 `18100`。

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
  sha256sum ~/.local/bin/openagentx ~/.local/bin/agy-graft
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
      binary: /home/<user>/.local/bin/agy-graft
      working_dir: /absolute/path/to/workspace
      models:
        - model-name
```

仓库 tracked wrapper 应安装到 `/home/<user>/.local/bin/agy-graft`，并在发布清单和
`~/.openagentx/release.txt` 中记录摘要。不要把另一个交互式或 machine-local wrapper 的目录加入 unit
`PATH` 来绕过安装，也不要静默覆盖 `/home/sky/tools/bin/agy-graft` 一类不同内容的本机副本。

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
`--user`。当前命令执行 `systemctl --user start`，不启用实例；因此运行中的 Worker 可以是
`active/running` 且 `is-enabled=disabled`。Worker unit 只 `Wants` daemon；daemon stop/restart 不会通过
依赖关系强停 Worker。

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
install -m 0755 /tmp/openagentx-release-agy-graft ~/.local/bin/agy-graft
cp -a web/dist/. ~/.openagentx/web/
find ~/.openagentx/web -type d -exec chmod 0755 {} +
find ~/.openagentx/web -type f -exec chmod 0644 {} +
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

安装前保留上一版二进制、unit 和 SQLite 在线备份。如果更新 wrapper 或 canonical Worker YAML，还要在启动
Worker 前记录 wrapper 原路径是否存在，并备份旧 wrapper、Worker YAML 和 release evidence。启动失败时先
精确停止受影响 Worker，再按记录恢复旧 wrapper/YAML；原路径此前不存在时删除新 wrapper。daemon 启动失败
时先停止 daemon，再恢复二进制和 unit；只有确认新版本改变了数据库且旧版本无法打开时，才在 daemon 停止
状态下恢复数据库备份：

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
- AGY Worker 的 `options.binary` 必须是已安装 tracked wrapper 的绝对路径。看到
  `executable file not found in $PATH` 时，先精确停止失败实例、核对 wrapper provenance 和摘要，再修复
  配置；不要扩展 unit `PATH` 去依赖未纳入 release 的 shell 工具目录。
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

### 2026-09-18：最终发布门禁与可追溯产物

Console smoke 修复提交为 `315fc22a7ab431bbfa1c87f92356310172f15d13`。独立 checkout
`/tmp/openagentx-adr008-standalone` detached 到该提交，使用独立 `.git`，tracked worktree clean；Web 的
ignored `node_modules/` 不影响源码状态。所有最终门禁都在这个 checkout 中运行，普通测试与 race 套件
分开执行：

- `go test ./... -count=1`、`go test -race ./... -count=1` 和 `go vet ./...` 全部通过；普通测试中的
  Console、Fleet 包分别约为 `1.583s`、`11.341s`，race 结果分别约为 `3.686s`、`12.268s`。
- `go mod verify` 输出 `all modules verified`；tracked shell `bash -n`、Worker template assertions、
  legacy control-path release check 和隔离 user-systemd verify 全部通过。
- `npm ci --no-audit --no-fund` 安装 123 个 package；4 项 observation 测试、PWA install assertions 和
  Vite production build 全部通过，生成 8 个 Web 文件。

门禁期间有三项不改变产品现场的辅助命令错误。最初使用了不存在的
`scripts/check-no-legacy-control-paths.sh`，纠正为 `scripts/check-legacy-control-paths.sh --release` 后结果
全部 CLEAN；Web `package.json` 没有通用 `test` script，纠正为实际的 `test:observation`、`test:pwa` 和
`build`；首次改写 Web checksum 路径时误删了哈希与路径之间的分隔空格，随即从 release 目录重新生成
清单。这些错误均未修改源码或正式 installation，纠正后的正式门禁和清单检查全部退出 `0`。

最终产物位于 `/tmp/openagentx-release`。binary SHA-256 为
`3a6c14b14d6486de70582e328cff1edcb936d62059c647bdb464f2619fc3a52a`；`go version -m` 显示
Go `1.22.4`、`-trimpath=true`、`vcs.revision=315fc22a7ab431bbfa1c87f92356310172f15d13` 和
`vcs.modified=false`。binary 与 8 个 Web 文件均通过 `SHA256SUMS` 回验，`PROVENANCE` 记录同一 revision
和 `source_clean=true`。

### 2026-09-18：ADR-008 本机正式迁移

`2026-09-18T20:09:25+08:00` 的最终只读预检确认没有现场漂移：遗留测试进程
`candidate_count=0`；旧 daemon PID `1308083` 为 active/running/enabled，`18100` 的 inode
`278467725` 由其 fd `12` 持有；数据库 SHA-256 仍为
`4b1ed0b0be5f736124ac1694e2789a33010473e295a90b77ad7095ea9d4762ce`，且
`quick_check=ok`。规范健康端点在 `127.0.0.1`、LAN 地址和 Tailscale 地址均返回
`{"status":"ok"}`。真实 `OAX` 仍有 2 个 attached client 和原 5 个 unmanaged 窗口，active window 为
`@63`，所有 window/pane ID、pane 数、marker 和相对顺序未变。

预检中有两项只读辅助查询被纠正。tmux format 中的字面量 `\t` 被错误地当成真实 tab 交给 `rg`，导致
过滤器退出 `1`；改用空格分隔后取得预期快照。对 `/healthz` 的请求命中了 SPA fallback 并返回 HTML；
根据本指南改用 `/api/observe/v1/health` 后得到规范响应。两条错误命令都没有修改 OpenAgentX、systemd、
数据库或 tmux。

按迁移决策先直接删除旧 `~/.openagentx/fleet.yaml` 且不备份，并确认路径不存在。随后创建私有回滚目录：

```text
/home/sky/.openagentx/backups/adr008-migration-20260918T201312+0800
```

该目录保存旧 binary、SQLite 在线备份、daemon unit、旧 application-specific Worker unit、Web、env、
identity、canonical Worker 配置、数据库 network secret 和旧 release evidence；同时记录迁移前不存在
`openagentx-worker@.service`。SQLite 快照 `quick_check=ok`，目录 mode 为 `0700`，数据库与清单 mode 为
`0600`，19 个文件全部通过目录内 `SHA256SUMS`。安装前在错误工作目录运行一次 `sha256sum -c`，因为清单
使用相对路径而报告文件不存在；产物未改变，在 `/tmp/openagentx-release` 中纠正后 binary 和 8 个 Web
文件全部通过，才开始删除和备份。

旧 daemon graceful stop 后 PID `1308083` 和 `18100` 监听均消失。安装已核验 binary、Web、
`openagentx.service` 和 `openagentx-worker@.service`，并在备份后删除 disabled/inactive 的旧
`openagentx-quote-service-worker.service`。两个已安装 unit 按最终名称通过
`systemd-analyze --user verify`；随后 daemon-reload 并 enable/start，新 daemon PID 为 `151130`。

安装后验证结果：

- 发布物、`~/.local/bin/openagentx` 和 `/proc/151130/exe` 的 SHA-256 三者一致；安装 binary 仍显示目标
  revision、`vcs.modified=false` 和 `-trimpath=true`。
- daemon 为 active/running/enabled、`Result=success`；启动日志只有预期 stop/start。新监听 inode
  `312002431` 由 PID `151130` 的 fd `12` 持有，地址保持 `0.0.0.0:18100`。
- `openagentx schema verify` 输出 `OpenAgentX schema v1 verified`，SQLite `quick_check=ok`。daemon 正常
  写入后数据库文件 SHA-256 为 `82e6b0448191a0f4be6be97c11d37d46003bd475addd53bde58a35fedb6c621d`。
- loopback、LAN 和 Tailscale 地址上的规范 health 均返回 `{"status":"ok"}`；Web 首页及 JS、CSS、
  manifest、service worker 和三个 icon 均返回 HTTP 200，安装文件与 release 中 8 个文件逐项同摘要。
- canonical Worker template 已加载但实例仍 disabled/inactive，符合登录和 `fleet up` 前状态；旧 Worker
  unit 为 not-found。Linger 仍为 `yes`，profile 私有目录、socket、env 和 Worker 配置权限符合要求。
- 安装过程前后真实 `OAX` 的两个 client、5 个既有窗口、所有 pane ID、active window 和空 marker 完全
  一致；安装阶段没有创建、rename、move、kill 窗口或发送按键。迁移后遗留测试进程扫描仍为 `0`。
- 回滚目录的完整 `SHA256SUMS` 和 SQLite `quick_check` 再次通过，旧 manifest、CLI credential 和旧
  Worker unit 当前均不存在。

首次权限复核发现，release Web 构建目录本身为 `0775`、文件为 `0664`，`cp -a` 将这些 group-writable
mode 保留到了 installation。发现后停止后续 Fleet 操作；内容摘要、daemon 和 HTTP 当时均正常，偏差仅限
权限位。根因是归档复制会保留构建环境 mode，而原安装步骤没有在复制后规范权限。随后将所有 Web 目录
收紧为 `0755`、文件收紧为 `0644`；逐项 mode 断言、8 个文件摘要比较、首页和 7 个静态资源 HTTP 200
全部通过。第 2 节和第 8 节的复制步骤已加入相同的显式权限规范，避免不同构建 umask 影响安装结果。

操作人员随后在特权 TTY 中执行 `ufw status verbose`，实际结果是 `Status: inactive`；这说明
`ufw.service` 的 active/exited 只代表启动脚本执行成功，不代表过滤规则已启用。`ufw show added` 仅列出
尚未生效的 `allow 3389` 和 `allow 3000`。主机同时运行 SSH、RDP、Docker、ZeroTier 等入站/转发服务，
因此没有擅自执行可能改变这些服务的 `ufw enable`。操作人员明确决定先打通主要 Fleet 流程，将
`0.0.0.0:18100` 暂无已验证主机防火墙、HTTPS 反向代理或等效入站限制记录为后续安全事项。此前建议的
UFW 规则均未执行；该时间点不能声称外部入口保护已经验证。

操作人员在自己的 TTY 中使用数据库内唯一 active username `owner` 完成 `openagentx console login`；
密码和 Token 未进入 argv、环境或执行记录。CLI credential 随后只核对元数据：owner 为 `sky`、mode 为
`0600`，未读取文件内容。

### 2026-09-18：首次真实 Fleet 初始化的 Console pane 定位失败

用户登录期间真实 `OAX` 现场发生了用户侧变化，因此在 Fleet 前重新建立 baseline：1 个 attached client；
5 个 unmanaged window 仍为 `@60,@61,@62,@63,@18`，active 为 `@63`；`@63` 新增用户 pane `%224`，共
3 个 pane。其他既有 pane ID、窗口名称、空 marker 和相对顺序均记录并保留。

`openagentx fleet init --agent quote-service` 成功创建 mode `0600` 的 canonical manifest，并报告只新增
`overview` 与 `quote-service`。实际 managed windows 为 `@66 overview` 和 `@67 quote-service`，位于所有
既有窗口之前；原 5 个 window 及所有 pane ID、pane 数、名称和相对顺序未改变。Worker 尚未启动。

初始化返回后检查发现两项偏离 baseline，因此立即停止，没有执行 `fleet up` 或手动切窗掩盖现场：active
window 显示为新建 `@66`；`@67.0` 已 dead，exit status 为 `1`，输出为：

```text
Workspace binding failed: Agent "quote-service" is already bound to another compatible OAX window; switch to OAX:quote-service.0.
```

唯一隔离 tmux socket 的 attached-client 探针复现了创建顺序，并证明 `new-window -d -b` 和
`respawn-pane` 均保持原 active window；active 变化不能由这些 Fleet 命令复现，保留为 attached client
并发选择的现场观察。该探针第一次把 pseudo-TTY stdin 接到 `/dev/null`，client 立即退出而使脚本在产生
结果前退出 `1`；纠正为私有 FIFO 后得到上述结论，两次都没有连接默认 tmux server。

同一隔离探针稳定复现了 Console 失败根因：目标进程的 `TMUX_PANE` 正确指向自己的 pane，但不带 `-t` 的
`tmux display-message` 返回 session active pane。`ExecRunner` 已支持 `CurrentTarget`，而生产
`DefaultDependencies` 没有把 `TMUX_PANE` 传入，导致在非 active managed window 中启动的 Console 把
active window 当成 current，再把自己的 `quote-service` window 误报为“another compatible window”。

修复仅将 `os.Getenv("TMUX_PANE")` 接入默认 `ExecRunner.CurrentTarget`，不改变 binding 规则或 fail-closed
行为。新增真实 tmux 回归测试让 managed `quote` pane 与 session active `user-active` window 不同，断言
默认 runner 仍解析到 `quote` 且不改变 active window。修复后结果：新测试连续 10 次通过（`6.524s`）、
原 TTY smoke 通过（`1.154s`）、Console 全包通过（`1.892s`）、Console race 通过（`4.067s`），Fleet 与
CLI Fleet 包分别以 `10.195s`、`0.176s` 通过。真实 `@67` dead pane 暂时保留用于诊断；完成新的 clean
release 门禁和安装前不执行 respawn 或 Worker 启动。

### 2026-09-18：Console 修复 release 安装与恢复

最终修复 revision 为 `f49cec4ed31a0f63e82626f1de8d6332baca205d`。独立 clean checkout
`/tmp/openagentx-adr008-standalone-f49cec4` 中，全仓普通/race 测试、vet、module verify、tracked shell
语法、Worker template、legacy control path、隔离 user-systemd 以及 Web `npm ci`、observation/PWA 测试和
production build 全部通过。`/tmp/openagentx-release-f49cec4/openagentx` 的 SHA-256 为
`984bb0df415b18f49959eda474076f0c807d90e6b5392342083249e605771114`；build metadata 显示目标 revision、
`vcs.modified=false` 和 `-trimpath=true`，provenance 记录 `source_clean=true`。

覆盖前创建第二层回滚目录
`/home/sky/.openagentx/backups/adr008-console-fix-20260918T212448+0800`，保存 binary、8 个 Web 文件、两个
canonical unit、release evidence、env、Fleet/Worker 配置和 SQLite 在线备份，不复制 credential。17 个
内容文件全部通过 `SHA256SUMS`，数据库快照 `quick_check=ok`。旧 daemon PID `151130` graceful stop 后，
其进程和 `18100` 监听均消失，才原子安装新 release。

新 daemon PID `309236` 为 active/running/enabled。release、安装 binary 和 `/proc/309236/exe` 摘要一致；
schema v1、SQLite `quick_check=ok`、监听 socket inode 归属、loopback/LAN/Tailscale health、8 个 Web
资源内容与 mode 均通过。只 respawn managed `@67/%226` 后，Console PID `313504` 正常运行目标 binary，
不再出现 duplicate-binding 误报；attached client 的 active window 仍为 unmanaged `@63`。

### 2026-09-18：Worker wrapper 缺失、修复与 Fleet 重跑

首次真实 `openagentx fleet up` 启动了 `openagentx-worker@quote-service.service`，但 `fleet status` 显示
Worker `activating/offline`。journal 给出真实错误：

```text
exec: "agy-graft": executable file not found in $PATH
```

Worker YAML 当时使用相对值 `binary: agy-graft`，canonical unit 的显式 `PATH` 为
`%h/.local/bin:/usr/local/bin:/usr/bin:/bin`，而交互式副本只存在于 `/home/sky/tools/bin`。确认根因后只
停止 exact Worker instance，使其保持 `inactive/dead`，没有修改旧 tools 副本，也没有未经授权重跑门禁。
用户随后选择 user-systemd 方案并授权修复和重跑。

tracked source `deploy/agy/agy-graft` 的 SHA-256 为
`31935c961ea76532962068c0e1a8d0a258c80d2f74940dcff3532705ca38b8de`；它与
`/home/sky/tools/bin/agy-graft` 的摘要
`8ed1310cc4db2410c6b0ae94637b652449f9e7effff17a6d6a39b0c9a69bb968` 不同。tracked wrapper fixture 全部
通过；依赖 `/home/sky/.local/bin/agy`、`/home/sky/tools/bin/mgraftcp` 可执行，本地 `7897` 正在监听。

release 新增 mode `0755` 的 `agy-graft`，`PROVENANCE` 记录 source path 和摘要，目录内 10 项
`SHA256SUMS` 全部通过。变更前新增回滚层：

```text
/home/sky/.openagentx/backups/adr008-worker-runtime-20260918T220656+0800
```

该目录记录 wrapper 目标原先不存在，保存旧 Worker YAML 与 release evidence；根目录和 `workers/` 为
`0700`，记录、清单和 Worker YAML 为 `0600`，相对摘要全部通过。随后原子安装
`/home/sky/.local/bin/agy-graft`，将 Worker YAML 改为同一绝对路径并保持 `0600`，installed release
证据也加入 wrapper 摘要。source、release 与 installed wrapper 字节一致，fixture 针对 installed path
再次通过。

重跑 `fleet up` 后，`quote-service` 在第一次轮询即达到 `active/running`；`fleet status` 报告
`worker_status=online`、generation `49`。Worker PID `379492` 的 executable 为安装的 `openagentx`，摘要
与 release 一致，`NRestarts=0`；Unix transport 配置指向 mode `0600` 的
`~/.openagentx/run/openagentx.sock`。实例仍为 `disabled`，符合 `fleet up` 只调用 `systemctl --user start`
的设计。重跑前后 attached client 始终位于 `@63`；所有 `OAX` window/pane ID、名称、pane 数、marker、
active 状态和相对顺序完全一致。

本阶段还纠正了五项无现场副作用的辅助操作错误：第一次 provenance 编辑把两个目标文件误放入单文件
替换请求，工具拒绝整个请求且文件未变；一次 `systemctl is-active` 预期输出 `inactive` 但其退出码 `3`
触发 `pipefail`，备份目录尚未创建；`install -d` 首次只收紧末级 `workers/`，随即在写清单前把回滚根目录
修正为 `0700`；`env -i ... command -v` 把 shell builtin 当成外部程序，改用 `/bin/sh -c` 后 canonical
PATH 精确解析到新 wrapper；最后一次辅助断言错误假设 Worker 必须 enabled，对照实现确认 `fleet up` 只
start，随后以 active/running、online、PID/hash 和 restart count 完成正式验证。

### 2026-09-18：最终只读复核

`2026-09-18T22:25:54+08:00` 完成最终复核。复核没有执行 daemon/Worker restart、Console respawn、
`fleet up/down`、tmux 控制命令或网络策略变更。

发布与文档门禁结果：

- tracked source、release 和 installed `agy-graft` 的 SHA-256 均为
  `31935c961ea76532962068c0e1a8d0a258c80d2f74940dcff3532705ca38b8de`，三份 wrapper fixture 分别通过；
  `/home/sky/tools/bin/agy-graft` 仍为不同摘要 `8ed1310c...`，未被修改。
- release 目录内 binary、wrapper 和 8 个 Web 文件共 10 项摘要全部通过；`PROVENANCE` 与 installed
  release evidence 均记录 revision `f49cec4...`、wrapper 摘要和 `vcs.modified=false`。
- legacy release gate、全部 tracked shell 的 `bash -n`、`internal/fleet` 和 `internal/cli/fleet` 定向
  Go 测试均通过；两个包用时分别为 `10.247s` 和 `0.189s`。
- 三层回滚目录的相对 `SHA256SUMS` 全部通过，目录 mode 均为 `0700`、清单 mode 均为 `0600`；前两层
  SQLite 快照 `quick_check=ok`。

现场结果：

- daemon PID `309236` 与 Worker PID `379492` 均为 `active/running`、`Result=success`、`NRestarts=0`；
  daemon 为 enabled，Worker 为 disabled。`fleet status` 报告 `quote-service` online、generation `49`。
- Console `@67/%226` PID `313504` 仍存活且 pane 非 dead。release、installed binary 和 daemon、Worker、
  Console 三个 `/proc/<pid>/exe` 的 SHA-256 均为 `984bb0df415b18f49959eda474076f0c807d90e6b5392342083249e605771114`；
  metadata 仍为 revision `f49cec4...`、`-trimpath=true`、`vcs.modified=false`。
- schema v1 验证通过，当前数据库 `quick_check=ok`。Unix socket mode 为 `0600`，Worker YAML 使用 unix
  transport 和 `/home/sky/.local/bin/agy-graft` 绝对路径；credential 仅核对 owner、mode `0600` 和 size，
  未读取内容。
- `18100` 监听由 daemon fd `12` 持有，socket inode 为 `312474539`。loopback、两个 LAN 地址和 Tailscale
  地址的 health 都返回 `{"status":"ok"}`。8 个 Web 资源 mode 均为 `0644`，installed/release 字节一致，
  HTTP 均返回 `200` 且响应字节一致。
- 两个 installed unit 与 clean checkout source 字节一致，并通过最终安装名的 user-systemd 静态校验。
  user manager 的 degraded 状态仍只来自既知的 `freetoken-ornith-api.service` 与
  `org.freedesktop.IBus.session.GNOME.service`，OpenAgentX unit 不在 failed 列表中。
- `/proc` 精确扫描只找到上述 daemon、Console 和 Worker 三个预期 OpenAgentX 进程，
  `unexpected_candidate_count=0`。
- 最终复核前后 OAX snapshots 分别保存在 `/tmp/oax-final-review-before-f49cec4.txt` 与
  `/tmp/oax-final-review-after-f49cec4.txt`，二者逐字一致，SHA-256 均为
  `a11fa5431950827971d9fe997cd8fd389501d05e2a1885178ee725f7445c1992`；attached client 和 active window
  仍为 unmanaged `@63`，所有 window/pane ID、名称、marker、pane 数和相对顺序未变。

最终复核还纠正两项无副作用的辅助查询错误。第一批文档校验误从 `/home/sky` 执行相对路径命令，因而
报告目标不存在，并让一次 `git diff --check` 检查了 home 仓库中的无关既有变更；改用绝对路径和
`git -C` 后正式检查通过。一次并行端口归属命令中的 `systemctl --user show` 返回
`Transport endpoint is not connected`，该命令没有取得 PID，后续临时输出被判定为无效；串行重做并对
PID、inode 和 fd 分段断言后，得到上述 PID `309236`、inode `312474539`、fd `12` 的有效归属证据。

网络安全事项在该时间点仍未关闭：`OPENAGENTX_HTTP_ADDR=0.0.0.0:18100` 保持不变，
`/etc/ufw/ufw.conf` 仍为 `ENABLED=no`，与此前特权 TTY 的 `ufw status verbose` 显示
`Status: inactive` 一致。该轮没有启用 UFW、添加规则、部署 HTTPS 反向代理或更改 VPN/路由 ACL；
因此当时不能声称外部 HTTP 入口已有已验证的访问控制或传输加密。该状态随后由下一节的 loopback origin
与既有 Cloudflare Tunnel 方案取代。

### 2026-09-18：Cloudflare Tunnel-only 网络边界

操作人员随后确认不允许任何非 loopback 地址直接访问 OpenAgentX，只保留已经配置的
`agentx.oneaxe.cn -> http://localhost:18100` Cloudflare Tunnel。变更前，域名 HTTPS health 返回 `200`
且响应经过 Cloudflare；system-level `cloudflared.service` 为 `active/running/enabled`。本次没有读取或
复制 `/etc/cloudflared/token`，也没有修改 Cloudflare Tunnel 或另一个既有 user `cf-proxy.service`。

覆盖 env 前创建第四层回滚目录：

```text
/home/sky/.openagentx/backups/adr008-loopback-origin-20260918T224331+0800
```

目录 mode 为 `0700`；其中保存旧 `openagentx.env`、`release.txt`、installed daemon unit 和
`ROLLBACK.txt`，文件与 `SHA256SUMS` mode 均为 `0600`，四项相对摘要全部通过。备份明确排除了
`credentials.json` 和 Cloudflare token，并警告恢复旧 env 会重新开放所有 IPv4 接口上的 `18100`。

将 env 原子修改为 `OPENAGENTX_HTTP_ADDR=127.0.0.1:18100` 后只执行了一次 daemon restart。新 daemon
PID 为 `486159`，`active/running/enabled`、`Result=success`、`NRestarts=0`；旧 PID `309236` 已退出。
`18100` 只监听 `127.0.0.1`，由新 daemon fd `13` 持有，对应 inode `313011772`。loopback origin 与
`https://agentx.oneaxe.cn` health 均返回 `200`；Tunnel 上 8 个 Web 资源也全部返回 `200`，响应字节与
release 一致。

使用 `curl --noproxy '*'` 绕过所有代理环境后，下列地址直连 `18100` 均为 curl rc `7`、HTTP `000`：

- LAN：`192.168.1.9`、`192.168.1.10`
- Tailscale：`100.76.106.96`
- ZeroTier：`10.242.50.160`
- Docker bridge：`172.17.0.1`、`172.18.0.1`

这次 daemon restart 暴露了一次真实的 Worker 暂态。旧 Worker PID `379492` 在 Unix socket 重启窗口收到
`connection refused` 后退出；systemd 随后启动的四个进程在旧 registration 过期前收到
`409 CONFLICT: logical Agent already has a valid Active Worker`。unit 的 `Restart=on-failure` 与
`RestartSec=3s` 最终在第 5 次自动重启后恢复，当前 Worker PID 为 `486788`、`NRestarts=5`，Fleet 报告
`quote-service` online/active、generation `50`。连续三次稳定性采样中 PID 和 restart count 未再变化，
恢复后的日志没有新增 error。没有为了清零计数再次重启服务。

当前 daemon、Worker、Console 三个进程仍运行 release SHA-256 为 `984bb0df...` 的同一 binary；schema
v1 与数据库 `quick_check` 通过，非预期 OpenAgentX 进程数为 `0`。OAX 变更前后 snapshots 保存在
`/tmp/oax-before-loopback-origin-f49cec4.txt` 和 `/tmp/oax-after-loopback-origin-f49cec4.txt`，二者逐字
一致且 SHA-256 均为 `a11fa543...`；Console PID `313504`、attached client、active window 和所有
window/pane identity 均未改变。

origin 的直接网络暴露已经关闭，UFW inactive 不再使 TCP `18100` 可从其他本机接口直连。外部入口现在
仅为 Cloudflare 提供的 HTTPS hostname，Cloudflare 到 origin 的 HTTP hop 限于本机 loopback。默认
Tunnel 配置本身不等同于已验证的 Cloudflare Access 身份策略；若需要在 OpenAgentX 自身认证之外增加边缘
访问控制，应另行配置并验证 Cloudflare Access policy。
