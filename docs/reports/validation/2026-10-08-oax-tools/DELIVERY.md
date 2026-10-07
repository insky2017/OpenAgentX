# oaxops / oaxctl 落地交付（2026-10-08）

本批按用户确认一次切换为 `oaxops`（宿主运维）和 `oaxctl`（远程 HTTP 控制客户端）。两个脚本已入库并安装为 `~/.local/bin/` 下的独立可执行文件，权限 0755；未保留旧入口、软链或包装器。核心 `openagentx` 与业务调度未改动。

## 使用入口与职责

| 入口 | 源码 | 用法与边界 |
| --- | --- | --- |
| `oaxops` | `scripts/operations/oaxops` | [宿主维护指南](../../../operations/oaxops.md)，工作目录 `/home/sky/docs`，当前窗口 `OAX:7:oaxops`、window `@33`、pane `%64` |
| `oaxctl` | `clients/oaxctl` | [远程客户端指南](../../../operations/oaxctl.md)，[HTTP 契约](../../../api/http-client.md)，标准库 HTTPS 客户端 |

用户决定目标与授权，客户端提交和展示任务，宿主运维 Agent 执行具体授权维护并协助验收。宿主运维 Agent 没有新增长期运维授权，也不是已实现无人值守自愈的守护程序。

## 原型审查与修正

原型来自 `/tmp/oaxctl_staging/clients/oaxctl` 和同目录 `docs/api/http-client.md`。入库版本移除旧配置回退及配置内密码，使用私有 XDG 会话目录；密码从终端或受控环境输入。补齐 CSRF 轮换保存、会话输出脱敏、重定向拒绝、准确 readiness 展示和有界等待。写请求不自动重放，幂等键在发送前输出；消息重试保持原 `expected_version`。等待超时只退出客户端（exit 3），不取消或重新派发；失败/取消/不确定终态返回 exit 4。

状态目录为 `~/.local/state/oaxops/` 和 `~/.local/state/oaxctl/`（支持 `XDG_STATE_HOME`）；客户端配置为 `~/.config/oaxctl/config.json`（支持 `XDG_CONFIG_HOME`）。无效相对 XDG 路径按默认路径处理。宿主临时单元统一 `oaxops-event-<key>`。

## 数据迁移和安装

- 先确认旧 writer 来自 `%64` 的空闲前台 TUI，正常退出后获取独占锁；原状态目录同文件系统原子移动，锁 inode 不变。
- 原 thread `01a115e8-e528-75f3-9e30-612d617cfb84` 和 `session.json` 字节不变；39 个迁移前文件在角色说明更新前全部 SHA-256 相符。其中 30 个事件文件仍逐字保留，包含原始输出、开始记录和结果回执。
- 删除旧脚本、旧运行目录及旧命令，当前 ROLE、组织交接和阅读副本更新为新入口；有效历史与请求原文不改写。已迁移成功事件经新入口再次查询，仅返回原回执，全部状态文件哈希不变，没有调用模型。
- 同一 `%64` pane / `@33` window 恢复原 thread，窗口名为 `oaxops`。`status` 的 `writer_busy=true` 来自已打开的前台终端；第二次 `open` 被拒绝。后台事件需要先退出前台释放 writer。
- daemon 和六域 Worker 的 MainPID、启动时间与 active 状态逐项相同；OAX 全部 pane ID 和 pane shell PID 不变。宿主运维 TUI 的 Codex 进程按本次迁移正常退出并恢复，不声称该进程 PID 保持。

安装 SHA-256：

| 文件 | 源码及已安装文件 SHA-256 |
| --- | --- |
| `oaxops` | `1a7f7efbf9241d9682feddbfd1eee0ea52aee885700709d9f0493eec14b5a730` |
| `oaxctl` | `674e38a2618da69f07c0e2ebbf03377c9d7c711a88efa64ffe0f96e17fec893b` |

迁移与安装证据：[迁移](evidence/migration-result.json)、[原回执复用](evidence/migrated-receipt-check.json)、[安装和原 pane 恢复](evidence/installation.json)、[客户端帮助](evidence/oaxctl-help.txt)。原始主机快照保存在 `~/.local/state/openagentx/validation/2026-10-08-oax-tools/`。

## 验证结果

| 验证 | 结果和证据 |
| --- | --- |
| 宿主锁与回执 | 3 项隔离集成验证通过：真实跨进程 flock、成功/不确定/异正文/缺开始记录、真实 user-systemd 读取隔离 XDG 状态；[输出](evidence/oaxops-tests.txt) |
| HTTP 边界故障 | 6 项本地 HTTP 夹具验证通过：轮换/脱敏、断连只发送一次、消息版本、XDG/URL、readiness/重定向及分页截止；[输出](evidence/oaxctl-tests.txt)，这是协议故障夹具测试 |
| 真实服务端集成 | 本地 TLS → 实际 OAX daemon → 隔离 DB/Worker：登录、查询、派发、幂等重放和冲突、消息追加/重放/旧版本拒绝、等待超时通过；[最终结果](evidence/client-integration-06/result.json) |
| 独立数据核对 | SQLite 只读核对 1 Task、2 Message、0 Run，Task 为 queued、version 2；[数据核对](evidence/client-integration-06/database-crosscheck.json) |
| 独立审查 | [审查结论](evidence/INDEPENDENT-TOOLS-REVIEW.md)，最终源码与 [manifest](evidence/client-integration-06/manifest.json) 的 SHA 相符 |

隔离 Worker 仅完成健康注册，派发前暂停，以验证正式消息路由，不执行模型或业务任务。未验证手机 Termux 实机、Cloudflare 外网全链路或真实业务任务完成；不能将 API 集成通过称为这些流程已验收。未修改 Go 服务端，因此没有重复运行全仓 Go 测试或重启正式服务。

首次尝试保留：01 为夹具误读数组；02 揭示无在线 Worker 时消息路由正式拒绝；03 为夹具健康命令不可用；04 为并行修订后夹具输出期望不匹配。05 首轮正向集成通过；06 在最终客户端输出定稿后补齐冲突和版本负向，最终通过。未覆盖首次失败记录。

历史报告中的当前入口已更新；旧证据 manifest 仅将原文件定位路径更新到迁移后的目录，原始事件字节、SHA 和历史结果保持不变。历史单元以稳定事件 ID 叙述，不虚构当时已使用新单元名。冻结 ADR 未修改。

## 发布

本报告与源码、测试、迁移证据同批提交到 `main` 并推送；实际 Git commit 与远端核验值在交付回复中列出。手机侧可从该提交取 `clients/oaxctl` 安装，宿主机安装不代表手机文件已由本批远程替换。
