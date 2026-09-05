---
doc_type: validation_preparation
scope: U2 and E1 final isolated closure
status: historical_preparation_superseded_by_partial_execution
owner: independent-verification
prepared_at: 2026-09-05
---

# OpenAgentX U2/E1 最终收口准备

> 历史准备稿，不是当前验收结论。实际执行事实与未完成边界见 [U2/E1 验证报告](2026-09-05-openagentx-u2-e1-validation.md)，当前状态为 `partial/incomplete`。

## 本轮收口矩阵

| 阶段 | 最小断言 | 证据边界 |
|---|---|---|
| 最终构建 | 记录最终 commit、二进制 SHA-256、Web 资源指纹、schema、Worker instance/generation；只重跑 U2 受影响 Go 包 | 不把 N2 的旧二进制或基准 HEAD 当最终实现身份 |
| 真实配置应用 | 指挥台通过正式 API 创建/测试/发布/绑定 named profile，真实 Worker ACK 为 `applied` | Control receipt、Observe test/binding、完整 ACK identity；失败保持 failed/stale/uncertain |
| 隔离 AGY Task | 使用最终 `agy-graft`、正式 Worker 配置和隔离 workspace 执行一个低副作用、可检查 Task | Task、RunAttempt、Event Journal、固定网络版本、Runtime status/body 与 workspace 结果必须相互对应 |
| Worker 常驻 | 第一项终态后无需人工注入，第二个低副作用 Task 被同一 Worker 领取并有独立终态 | Worker identity、attempt、workspace 和事件；零退出码不是业务成功 |
| E1 native 规则 | 仅 `no_blackip` SOCKS5 与 `http_config_host_port` HTTP case | `verify-e1-network.mjs` 的 marker bool、代理连接计数、wrapper/helper/client SHA；不外推为 AGY Task 成功 |
| E1 故障与重试 | 一个坏配置得到脱敏失败诊断；一个关键写请求同 key 重放不重复生效 | Control receipt、Observe 版本/工作数量不增加、脱敏代码；不扩展为完整 CAS 矩阵 |
| 单一 Chrome 批次 | 真实断线恢复和离线请求计数；Markdown 原文/复制/安全链接图片；三视口手机代码表格；Run 身份、固定网络和 Task `uncertain` | 浏览器 DOM、控制台/网络请求计数、截图；离线 disabled 不是断线恢复证据 |

## 冻结前已核对的输入

- r2 隔离服务仍在线：daemon `127.0.0.1:18180`、SOCKS5 `127.0.0.1:18081`，Worker 为 `worker-01dce665-7e0a-4632-8ed0-ff64dcdca17c`、generation `1`、agent `n2-fixture-agent`。
- 当前 r2 二进制 SHA-256 为 `552e5891074356b196a90e377fa4af2379166b464c0130b8c90132ec9a6ff765`，仅作现状记录；最终执行必须以 U2 freeze 后新构建重新记录身份。
- `deploy/agy/agy-graft` 为可执行普通文件；`agy-graft --version` 与 native `agy --version` 均为 `1.1.26`。help 已核对 `--print`、`--input-format stream-json`、`--output-format`、`--print-timeout`、`--model`、`--effort`、`--sandbox` 和权限参数。
- native `mgraftcp` 与 `/usr/bin/curl` 均为可执行普通文件；mgraftcp help 声明 HTTP/SOCKS5、black/white IP 和代理模式选项。wrapper 通过显式 `AGY_GRAFT_REAL_BIN`、`AGY_GRAFT_MGRAFTCP_BIN`、`AGY_GRAFT_CONFIG`、`AGY_GRAFT_SELECT_PROXY_MODE` 载入路径和代理配置；r2 materialized 配置及完整性 key 为 `0600`。
- r2 的私有输入、会话、服务与 Chrome profile 已永久退役；不得复用或提供可复用输入指引。

## 最终单批步骤

1. U2 作者完成最小实现和受影响单测后冻结。验证方记录最终应用/Web 指纹、ADR 未改动、schema 与最终 worker binary/config 身份，并启动或重启对应隔离服务确认新 generation。
2. 通过真实指挥台和正式 API 完成 named profile 的测试、发布和绑定；只在 Observe 证明 `applied` 与当前 Worker/generation/profile/version/binding revision 一致后创建 Task。
3. 以隔离 workspace 中可回收的低副作用指令运行真实 AGY Task。核对 Task 终态、RunAttempt、Event Journal、active run 固定网络字段、Runtime status/body 及 workspace 结果；语义缺失或不一致保持 `uncertain`。
4. 不重启 Worker，创建第二个独立低副作用 Task，核对同一在线 Worker 领取并产生新 attempt/终态。
5. 在新 `openagentx-e1-*` fixture 运行 selected E1 cases，并用同一最终 Chrome 批次执行故障/重试、断线恢复、离线请求计数、Markdown 与三视口检查。每项失败保留原始脱敏分类，不自动重试为成功。

## 执行前缺条件

- U2 最终源码 commit、受影响包清单、最终二进制和 Web 资源目录尚未产生。
- 两个真实 AGY Task 的低副作用指令、可检查 workspace 结果及可回收方式尚未冻结。
- 浏览器断线恢复的明确触发方式、计数基线和预期请求数尚未由最终实现给出。
