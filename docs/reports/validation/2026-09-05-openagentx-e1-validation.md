---
doc_type: validation_preparation
scope: E1 isolated AGY wrapper and native network rules
status: historical_preparation_superseded_by_partial_execution
owner: independent-verification
prepared_at: 2026-09-05
model: gpt-5.6-terra high
---

# OpenAgentX E1 隔离网络规则验证准备

> 历史准备稿，不是当前验收结论。r3 已发生的 selected native/profile/Task 事实与未完成项见 [U2/E1 验证报告](2026-09-05-openagentx-u2-e1-validation.md)，当前状态为 `partial/incomplete`。

> 本轮最终收口矩阵仅执行 `no_blackip`（SOCKS5）与 `http_config_host_port`（HTTP）两个 native rule 案例。黑白名单优先级等其他已准备案例保留在 helper 中，但不进入本轮执行或结论。E1 还须在同一最终 Chrome 批次补充坏配置失败、关键重试不重复、真实断线恢复/离线请求计数，以及 Markdown 原文/复制/安全链接图片/手机代码表格检查。

## 当前判定

- 本文与 `verify-e1-network.mjs` 仅准备最终冻结后执行的隔离实验。本轮未启动端口、目标 HTTP 服务、代理、daemon、Worker、CLI、浏览器或模型，也未执行网络连接。
- 实验仅验证最终 `deploy/agy/agy-graft`、实际 `mgraftcp` 与受控 HTTP client 的网络规则效果，不是 AGY 模型 E2E，不得把结果外推为 Task 成功、业务副作用、Worker 常驻或连续接单通过。
- 正式执行时必须输入最终绝对路径 `OAX_E1_WRAPPER`、`OAX_E1_MGRAFTCP_BIN`、`OAX_E1_CLIENT_BIN`，每次现场重新计算 SHA-256；不复用当前源码、旧 wrapper/helper/client 或旧摘要作为证据。

## 验收矩阵

| 类别 | 待证明断言 | 证据 |
|---|---|---|
| 隔离拓扑 | HTTP marker target 仅监听 `127.0.0.2`；受控代理仅监听 `127.0.0.1`，且只允许到该 target/port 的 CONNECT 或 SOCKS5 连接 | listener 地址/端口、target marker 比对、代理 target 连接计数 |
| 无直连规则 | 使用最终 wrapper、最终 helper 和受控 curl，经 SOCKS5 配置访问 target，marker 一致且代理计数增加 | curl 退出码、marker bool、代理计数 |
| 黑名单 | `blackip` 含 `127.0.0.2` 时 marker 与无规则结果相同，代理计数为零 | 每案例独立计数和 marker bool |
| 黑白优先级 | 黑白名单均含 target 时仍直连；black target/white other 仍直连；black other/white target 经代理。IPv4 whitelist 仅在 wrapper SOCKS5 路径启用 | 五个案例、最终 wrapper argv/身份和 native 实际网络效果 |
| HTTP config | 最终 HTTP config 使用无 scheme 的 `http_proxy = 127.0.0.1:<port>`，经受控 CONNECT proxy 返回相同 marker 且代理计数增加 | HTTP 案例的 config、marker bool、代理计数 |
| 安全与清理 | fixture 目录精确 0700，配置/名单精确 0600，均拒绝 symlink；环境只传显式白名单；wrapper/helper/curl 在独立进程组；每案例与总批次有超时；任一失败立即停止而不重试；TERM/KILL、socket 与 listener 关闭均有界 | 文件元数据、受控环境、固定失败分类、清理结果 |

## 固定执行约束

- 仅在 N2/U2 独立验证、审核和最终组合冻结后，且明确设置 `OAX_E1_ALLOW_EXECUTION=1` 时运行；现阶段不能执行。
- fixture 路径必须是新建的绝对 `openagentx-e1-*` 目录，脚本不删除既有路径。配置与名单由脚本创建为 0600 常规文件，并以 `lstat` 拒绝链接。
- `OAX_E1_CLIENT_BIN` 必须是最终受控 `curl` 的绝对可执行路径。wrapper 调用固定 argv：`--disable --silent --show-error --fail --noproxy * --connect-timeout 5 --max-time 10 --proto =http --proto-redir =http --output - <target-url>`；不读取 curlrc、不继承外部代理，且脚本只核对 stdout 的 marker，不打印其内容。
- wrapper 子进程使用最小显式环境：`PATH`、`AGY_GRAFT_REAL_BIN`、`AGY_GRAFT_MGRAFTCP_BIN`、`AGY_GRAFT_CONFIG`、按案例选择的 `AGY_GRAFT_SELECT_PROXY_MODE`，以及仅 SOCKS5 白名单案例的 `AGY_GRAFT_IPV4_ONLY(_FILE)` 和黑名单案例的 `AGY_GRAFT_BLACKIP_FILE`。不继承代理环境、凭据或 shell 初始化状态。
- 代理接受受限 SOCKS5 与 CONNECT。五个规则案例走 SOCKS5，以覆盖 wrapper 可接线 IPv4 whitelist 的路径；另有 HTTP config case 覆盖无 scheme 的 `host:port` native 配置。代理只计数真实到受控 target 的连接；marker 不一致、非零退出、信号退出、超时、握手超限或非法目标均以固定分类失败。

## 本次纠偏准备

- future-run 子进程使用独立进程组，超时先发送 TERM、等待有界窗口，再发送 KILL 并再次等待；不能只终止 wrapper/mgraftcp PID 而遗留 curl 子孙。
- target HTTP 与代理的所有 socket 都被跟踪，清理先销毁连接，再有界关闭 listener。CONNECT/SOCKS 握手缓冲上限为 4 KiB、握手时限为 3 秒；完成握手后移除解析 listener、暂停 socket、转 relay 并传递已接收的剩余字节，避免重复解析 header 创建额外 upstream。
- 执行失败只输出固定 `classification`，包括 `case_timeout`、`total_timeout`、`wrapper_spawn_error`、`wrapper_signal_exit`、`wrapper_nonzero_exit`、`cleanup_process_timeout`、`cleanup_listener_timeout` 与 `cleanup_failed`；若主失败后清理失败，保留主分类并单列 `cleanup=cleanup_failed`。不输出原始 stderr、marker 或配置内容。

## 正式命令预案

1. 记录最终 commit、ADR 哈希、源码/静态资源指纹、wrapper/helper/client 的绝对路径和现场 SHA-256；确认每个输入为最终构建的非链接可执行常规文件。
2. 在新隔离路径运行：`OAX_E1_ALLOW_EXECUTION=1 OAX_E1_CASES=no_blackip,http_config_host_port OAX_E1_FIXTURE_DIR=/tmp/openagentx-e1-<id> OAX_E1_WRAPPER=/abs/final/agy-graft OAX_E1_MGRAFTCP_BIN=/abs/final/mgraftcp OAX_E1_CLIENT_BIN=/abs/final/curl node docs/reports/validation/verify-e1-network.mjs run`。
3. 保存仅含案例名、marker 布尔值、代理计数、路径摘要和 SHA-256 的脱敏结果；失败保留失败案例和退出分类，不通过重试把失败改写为成功。

## 未覆盖与后置项

- 本实验不是 AGY 模型调用、认证、代理凭据、真实 Task、RunAttempt、Event Journal、Worker 控制连接、Worker 保活或连续接单的证据。
- 真实 marker Task、隔离 workspace 文件的路径/内容/数量/摘要核对，以及随后再领取一个 Task 的常驻验证，仍作为 E1 后置批次独立执行。
- `probe_results`、`active_runs` 等正式 N2 验证契约不在本脚本中模拟或判定，待 N2 完整冻结后纳入正式独立验证。
