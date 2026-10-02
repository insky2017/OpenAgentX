# 外部会话协作覆盖

| 用例 | 状态 | 证据与结论 |
|---|---|---|
| E01 原宿主定位 | 自动投递受阻 | [host-probe01](evidence/host-probe01/README.md)：共享 daemon 两目标均 notLoaded；Desktop 队列入口没有受支持的外部消费证明 |
| E02 身份与绑定 | 通信绑定已实现、真实身份已登记 | [pilot-bind01](evidence/pilot-bind01/verdict.json)：用户给定 Agent/thread 精确匹配，无 Worker、无业务消息；原会话尚未读入说明 |
| E03 持久通信 | 隔离及正式安装 CLI/API 通过 | [smoke02](evidence/smoke02)及[installed01](evidence/installed01/result.json)：真实 send/inbox/ack/reply/status、幂等及权限；不含原业务会话 |
| E04 原会话自主请求→答复→续办 | 未执行 | 首条 rhythm-pay-handoff-01 尚未投递，主代理未冒用身份替写 |
| E05 忙时排队、闲时唤醒、回执恢复 | 未接通 | 没有连接器；不会将直接 steer 或共享 daemon resume 当作原宿主排队 |
| E06 持久与恢复 | 通信服务范围通过 | smoke02 服务重启后绑定、消息、ack及幂等保留；未覆盖宿主投递未知恢复 |

逻辑验证：完整 Go race 通过，外部身份轮换、事务故障回滚、并发幂等、双向 managed 互斥及 schema 守卫都有对应测试。真实 API 验证与数据库只读核对见 [evidence/README](evidence/README.md)。测试身份产生的消息不计作 Rhythm/Pay 原会话协作。

安装后的正式 API 和来源核验逐项记录在 EXECUTION-LOG；不得将通信绑定 `active` 解读成原宿主在线或已经自我初始化。非阻断事项：既有 panel CreateTask 将领域冲突投影为 HTTP400，本轮保留原契约；external API 自己的冲突使用409，错误正文明确说明应使用消息通道。
