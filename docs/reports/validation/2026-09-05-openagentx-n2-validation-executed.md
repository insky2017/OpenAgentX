---
doc_type: validation_report
scope: N2 network configuration workflow
status: partial_pass
owner: independent-verification
executed_at: 2026-09-05
---

# OpenAgentX N2 功能验收报告

## 判定

- 本批通过 named profile、`inherit`、`direct` 的正式 Control API -> Worker work/ACK -> Observe `applied` 路径，以及当前网络配置页的真实 Chrome 基本交互。
- 本批不是 ADR-002 的完整验收。回退、导入、双 Session CAS、九类未实现的幂等/冲突 helper、旧 generation/fencing、安全优先级和真实模型/网络效果仍未覆盖，不能标记为全部 GO。
- 冻结源码基线：`083934199775defabd66c5b747e4f53597c98926`；隔离二进制：`/tmp/openagentx-n2-final-20260905.bin`；SHA-256：`552e5891074356b196a90e377fa4af2379166b464c0130b8c90132ec9a6ff765`。
- 当前应用源码树指纹不是上述基线 HEAD：对当前工作树的 `cmd`、`internal`、`deploy`、`web/src` 共 158 个文件逐一 SHA-256 后排序再哈希，结果为 `bd8515ec3583f387a29e52f26d5fb0d1c0dc42f19f2c35bafa906c3b7a793657`。`web/dist` 8 个构建资源按同一方法的指纹为 `4d7207cccff7c289640a2eaa6dbceca79ea982eb55d3fffe9048a7b829db2311`。

## 验收矩阵

| 场景 | 结果 | 证据 |
|---|---|---|
| 未认证读取 | Observe 网络概览被拒绝 | `unauthenticated-read-check` 返回 `401` |
| named profile | create 重放、编辑、私有 secret、target test、ready、publish、bind 全部完成 | `named_binding_applied=true`、`named_test_succeeded=true`；ACK identity 与 ready test 一致 |
| named SOCKS5 | 真实隔离 SOCKS5 完成精确认证 | success `1`、failure `0`；materialized 配置和 integrity key 均为 `0600` |
| inherit | mode test 不改 binding/health；显式 publish 后真实 Worker ACK | `fixture_mode=inherit`、binding/test 均成功 |
| direct | candidate test、pending publish、Worker ACK 完成 | `fixture_mode=direct`、binding/test 均成功 |
| 七层诊断 | 每 test 恰七层；inherit 的 secret/endpoint 为 `not_verified/INHERITED_CONFIGURATION_UNVERIFIED`；后两层为 `not_verified/NOT_VERIFIED` | authenticated Observe 与 Chrome Runtime DOM |
| 运行时身份 | named 和 fixture 均为 `applied`，Worker 和 generation 与 ACK 一致 | `applied-state-check`；Worker/daemon/SOCKS5 持续在线 |
| 认证与秘密清空 | Chrome 真实登录；凭据提交后两个输入框均为空 | `browser_login_succeeded=true`、`browser_secret_submission_succeeded=true`、`secret_inputs_cleared=true` |
| 空态和离线 | 显示无 active Run；离线禁用 Refresh/New/Test/Edit/Secret/Publish/Bind | `agent-browser --cdp 18240` DOM 快照 |
| 三视口 | 1440x900、390x844、412x915 无控件重叠 | fixture 内三张 `n2-runtime-*.png` 截图 |

## 构建与环境

- 通过：`go test -count=1 ./internal/controlplane ./internal/persistence/sqlite ./internal/worker ./internal/api/panel`、`web/npm run build`、`git diff --check`。
- schema 版本为 `1`；fixture：`/tmp/openagentx-n2-final-20260905-r2`，权限 `0700`；daemon `127.0.0.1:18180`；SOCKS5 `127.0.0.1:18081`；Unix socket、DB、secret store 均在 fixture 内。
- Worker 非秘密身份为 `worker-01dce665-7e0a-4632-8ed0-ff64dcdca17c`、generation `1`、agent `n2-fixture-agent`。后续 E1 可复用的私有输入路径为 `/tmp/openagentx-n2-final-20260905-r2/worker.yaml` 与 `/tmp/openagentx-n2-final-20260905-r2/owner-password`，两者均为 `0600`；不得读取或输出其内容。
- 浏览器：`/usr/bin/google-chrome`，`Chrome/143.0.7499.109`，独立 profile 和本机 DevTools；页面交互使用 `agent-browser --cdp 18240`。
- 三张已审核 Chrome 截图归档于 `docs/reports/validation/evidence/n2/`，SHA-256、尺寸和格式见同目录 `manifest.sha256`。
- 秘密仅从 r2 fixture 的 `0600` 文件经本机 DevTools 输入真实表单，未出现在 r2 的 argv、报告、截图或控制台。秘密清空测试创建了隔离草稿 `browser-secret-clear-n2`，未改已发布 named profile。

## 旧 Fixture 事故与处置

- 首个旧 fixture `/tmp/openagentx-n2-final-20260905` 在初始化时使用了不安全的脚本输入方式，导致其临时凭据发生终端回显。
- 该 fixture、相关临时凭据和任何 session 已全部废弃，绝不用于服务、验收、截图或证据；本报告不记录该值。
- 当前结论只适用于 r2 fixture。r2 使用私有 `0600` 文件与关闭终端回显的延迟管道供给口令，且本批检查未发现 r2 secret 进入控制台输出。

## 未覆盖与风险

- 准备稿中的十类 helper 要求已由最新收口基线后置；本 N2 功能范围不把它们视为已执行或阻塞当前 partial pass。除 create 外，edit、secret、test、publish、bind、rollback、mode、import 的同 key 重放和异请求 `409` 仍需后续 helper。
- 未验证双 Session 冲突、旧 generation/lease/fencing、组合安全错误优先级、失败/stale、回退、导入、Journal/RunAttempt 完整关联。
- `network_effect`、`model_call` 仍为诚实的未核验；本批没有真实模型调用或可验证网络副作用，留给 E1/U2。
- `active_runs` 是明确空态；固定网络版本的真实 Task/RunAttempt 证据复用 N1 的确定性交错测试范围，本批不额外启动慢 Runtime 或真实模型 Task。
- 离线只验证页面控件 `disabled`，未执行写请求计数、SSE 断线恢复或 PWA 安装。
