# Codex 验收覆盖

当前已安装源码 `6b68eeb`。D=确定性测试；R=真实 Codex；I=已安装正式服务与用户入口。以下按原 [C01–C12](../../../plans/2026-10-02-codex-workflow-e2e.md) 判定，不把某一层替代另一层。“部分”表示该组有已通过子项，也有未完成或不支持项；不是全部通过。

| 组 | 已证实 | 未覆盖/限制 | 主要证据 |
|---|---|---|---|
| C01 协议与版本 | D/R：0.160.0 help/schema、Unix WebSocket、真实 final、双客户端广播；I：实际前后端来源 | 首次 TUI 显示0.153.1的来源无法事后确证，原样保留 | [协议](evidence/protocol-150457/SUMMARY.md)、[安装来源](evidence/installed-provenance01/README.md)、[原生版本](evidence/real-native02/README.md) |
| C02 登记与职责 | D：幂等/冲突；R：prepare真实PTY零connect/未启动服务；I：登记→resume、角色冻结、目录写入 | 薄Skill已提供但未自动安装全局；非Codex已有会话迁入不在本轮 | [join](evidence/codex-cli-join-20261002T074001Z/README.md)、[正式入口](evidence/installed-entry01/)、[正式效果](evidence/installed-api01/verdict.json) |
| C03 默认代理 | D：inherit/direct和回环合并；R/I：私密环境→Worker→app-server一致、重启持久、网络当前代已应用 | **部分**：没有实际外网代理抓包/bypass证明；坏代理→修复的真实组合未测，named_profile不支持 | [R网络](evidence/network-live01/README.md)、[I重启来源](evidence/installed-provenance01/README.md) |
| C04 回复与产物 | D/R/I：query完整、精确文件独立字节/hash、Task/Run/Journal齐全 | mutation的Task仍可为uncertain；独立测试验证文件不改写业务账本 | [三轮R](evidence/real-workflow02/README.md)、[正式I](evidence/installed-api01/verdict.json) |
| C05 连续接单 | R/I：同Worker/generation三轮；取消后下一query同Worker成功；无按键注入 | 更长连续运行/资源趋势未做长期试跑 | [正式I五Task](evidence/installed-api01/verdict.json)、[R终端退出后接单](evidence/real-native02/README.md) |
| C06 输入仲裁 | D：CAS/幂等/顺序与早期绑定；R：同thread原生运行时bus零Run排队，前一Run完成后下一Run才启动 | **部分**：原生steer协议有实测，正式API同Task多次steer完整组合未逐个实测 | [独立D](evidence/deterministic-native/README.md)、[R并发](evidence/real-native02/README.md) |
| C07 原生终端 | R/I：正式Task/Run输入、文件、关闭后后台继续、重开历史 | 正常路径通过；取消重建引擎时前台需重开 | [R完整证据](evidence/real-native02/README.md)、[I原生终端](evidence/installed-native01/SUMMARY.md) |
| C08 取消/超时/审批 | D/R/I：真实长shell启动后API取消，父子PID+starttime退出；等原90秒写入点+5秒仍无sentinel；下一query成功，其它服务不受影响 | **部分**：实证依赖专属host全树兜底，可能停同Agent旧后台工具；不能声称“仅目标turn”。非command/外部host不确定时保留uncertain；正式人工审批及timeout组合未实测 | [首败与R复验](evidence/protocol-150457/SUMMARY.md)、[I五Task](evidence/installed-api01/verdict.json) |
| C09 故障与未知效果 | D：早期绑定/迟到事件/丢ACK不误报成功；R：明确取消后重建同thread；I：正常服务重启不丢配置 | **部分，待补**：未知终态/活动Worker崩溃持续隔离，普通resume不能解除；没有本轮Codex崩溃后自动恢复I，不借用AGY证据 | [独立D](evidence/deterministic-native/README.md)、[最终Runtime检查](evidence/final-checks01/)、[正常重启I](evidence/installed-provenance01/README.md) |
| C10 已有Agent迁入 | R→I：原生CLI旧thread完成后退出，旧自有后台停止，join/resume新长期身份；不调用工具准确回忆旧文件内容并回答新角色码 | **部分**：已知thread的空闲交接通过；不承诺任意运行中PID无感接管，未单独让模型在活动turn里自行调用join | [旧thread交接](evidence/real-native02/migration-handoff.json)、[停止旧环境](evidence/migration01/)、[正式入口](evidence/installed-entry01/)、[新角色与回忆](evidence/installed-api01/verdict.json) |
| C11 网页观察 | R/I：真实桌面/390px、结果/状态/模型与刷新；I：离线无写入口、恢复session/SSE后结果回来 | **部分**：断网工具触发重载，没有原地disabled按钮或在途任务跨断线证明；窄屏结果区较矮、次级按钮仍待优化 | [R浏览器](evidence/browser-observe01/README.md)、[I浏览器](evidence/browser-installed01/README.md)、[I工件](evidence/installed-provenance01/README.md) |
| C12 安装与回归 | clean clone源码6b68eeb、vcs.modified=false；实际daemon/两Worker SHA一致；8份Web工件；原AGY真实query回归 | 全量race在较早快照；之后改动按受影响包重新race及全量vet，不声称最终源码再次全仓重跑 | [安装](evidence/installation01/)、[独立来源](evidence/installed-provenance01/README.md)、[AGY回归](evidence/installed-agy-regression02-review/verdict.json)、[全量D](evidence/deterministic01/README.md)、[最终受影响D](evidence/final-checks01/) |

## 首次失败与复验

- 协议裸JSONL连接不成立；改用实际Unix WebSocket。原生interrupt的即时“文件不存在”被延后写入反证，修复后必须延后核验，不保留伪PASS。
- native session旧事件幂等回放、bridge跨thread通知/响应排序与测试夹具问题分别保留，最终受影响验证通过。
- real-workflow01因测试工具漏发loopback Secure Cookie而401，未创建Task；修harness后real-workflow02通过。
- 首次Go worktree构建嵌入错误外围仓库revision，拒绝安装；独立clone构建后来源通过，首次build-info保留。
- installed-agy-regression01已真实完成正确query，但汇总脚本使用错误字段task_id而失败；随后只读重新核对原Task，未重复调用模型，02-review通过。

所有证据目录均保留SHA-256清单。原始日志在本机 `~/.local/state/openagentx/validation/2026-10-02-codex-workflow/` 或 `~/.local/state/openagentx/evidence/`，具体路径见各manifest/README；入库仅脱敏副本，不含数据库、凭据或私密env全文。
