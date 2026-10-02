# 外部原会话协作执行记录

基线：产品 `9454a81`，已安装 `6b68eeb`，本轮开始工作树 clean。计划见 [本轮计划](../../../plans/2026-10-03-external-session-collaboration.md)。

## 初始事实

- 用户授权保留两个原 thread 及宿主，实施 OAX 消息通道并做只读双向交接试点。
- 当前产品 managed Worker 有真实 Codex 证据，但没有 external-session 通信模式；历史验收不覆盖本轮。
- 本机 CLI 0.160.0；Desktop 内嵌版本与共享 daemon 不同，不能混用其会话所有权。
- 首次 `app-server proxy` 按 JSONL initialize 超时。后续只读 Unix WebSocket initialize 成功，两个 thread 在共享 daemon 返回 `notLoaded`。这只证明可读历史，没有证明原宿主定位或投递。
- 未向两个原会话发送新指令，未启动它们的 Worker，未改业务仓库。

## 实施与裁决

1. 规则/计划提交 `2d46756` 后开始实现。原生宿主能力由两个独立代理按本机实际版本核实，原始材料持久保存；[host-probe01](evidence/host-probe01/README.md)保留精选 wire、版本及来源上下文。共享 daemon 的 raw proxy 需要 WebSocket Upgrade，修正探针后能读历史，但两目标均未加载；Desktop 自己的排队与直接发消息机制不能提供本轮需要的外部 queue-only 保证。
2. 最小实现提交 `6576c55`：两张表、Agent 专用身份、真实作者、peer范围、幂等消息、关联结果和事务 Journal。直接读取不自动 ack；结果不能回复结果。使用 SQLite v3 守卫防止 managed/external 双写，已有未终态 Task 不能切换；不引入 Exchange 状态机或假 Run。
3. 独立审查指出命令取消误报 exit0、JSON 转义容量不足和错误状态投影遗漏，已集中修复。既有 panel 创建 Task 的领域冲突仍投影 HTTP400，本轮只要求精确拒绝且没有入账，不另做全局 API 重构。

## 逻辑与真实通信验收

- [checks01](evidence/checks01/result.json)：首次 `go test -race ./...` 只有旧 testkit 硬编码 schema2 失败，原始日志保留。改为 CurrentVersion 后，[checks02](evidence/checks02/result.json)全量命令通过；已通过包使用 Go 缓存，发生变更的 cmd/testkit 重新执行。
- [smoke01](evidence/smoke01)：首次真实隔离 daemon/CLI/UDS 验收通过；不是 mock。请求、收信、确认、关联回复及错误身份/peer/回复环均有实际命令和响应。
- [smoke02](evidence/smoke02)：最终候选 14 组验证通过，增加64KiB最坏 JSON 转义完整往返、65537字节拒绝；服务重启后身份、消息、ack及幂等保留。独立SQLite只读核对无Task/Run/Worker。
- smoke02 对正式 v2 数据库以 `mode=ro` 备份，只升级私密副本；原38表279,560行的行数和逐表数据指纹全部保留。数据库/凭据不导出到Git。

## 安装与真实领域登记

- [release01](evidence/release01/manifest.json)：发现 Go1.22 在嵌套工作树构建时误采集上级 OneAxe Git metadata，最终使用独立干净 clone 构建。`go version -m` 为源码6576c55、modified=false；最终SHA `432cbd044902351670179e7eaaf463871f59bec0d274ed8810f49eab56d7fd39`。功能候选与最终源码相同，工件路径/版本戳不同，安装后再次核验实际API。
- [installation01](evidence/installation01/manifest.json)：先从正式 API 确认两个演示 Worker 无 active/pending工作，再停止它们和OAX daemon；备份旧binary/v2 DB、原子安装、启动三服务。正式schema3与三服务进程二进制哈希一致。未触碰业务Codex宿主，PID1632229的start/hash未变。
- [pilot-bind01](evidence/pilot-bind01/verdict.json)：实际 `agent apply` 加 `external bind/status/inbox` 完成 `rhythm`、`oneaxe-pay` 登记。只有通信身份，两个Agent的Worker及消息计数均0；原会话尚未读入说明。用户给定业务目录没有被修改。
- [installed01](evidence/installed01/result.json)：独立代理通过安装工件和正式服务完成7组真实通信断言；测试身份的Task/原Message/Mailbox/Run/Worker均0，仅有2条external消息及相应Journal。误派managed Task返回精确guard HTTP400；重复result409。服务PID/starttime/工件SHA在验证前后保持一致。收尾正式撤销两个测试绑定，旧token401；保留身份和消息账本，不操作真实业务绑定。
- 两份私密接入资料已准备：`~/.openagentx/external/rhythm/START.md`、`~/.openagentx/external/oneaxe-pay/START.md`，正文源文见[双角色接入](../../../examples/external-session/README.md)。不需要再次生成凭据或初次bind。

## 最终边界

持久通信API与身份已落地；原Desktop会话原生队列入口受阻，首条 `rhythm-pay-handoff-01` 未发送，E04/E05未执行。没有主代理代写回复、没有共享daemon上的第二执行者、没有修改会话存储或自动化轮询来冒充自动协作。安装后的独立正式API证据见[证据目录说明](evidence/README.md)；完整覆盖见[COVERAGE](COVERAGE.md)。

原始证据分别保存于本机 `~/.local/state/openagentx/validation/2026-10-02-external-session/` 和 `2026-10-03-external-session/`；跨日目录保留原样。精选脱敏副本、来源映射及SHA入库；首轮失败与复验独立保留。
