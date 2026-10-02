# 本批覆盖与证据边界

| 项目 | 结论 | 证据与限制 |
|---|---|---|
| 原会话咨询/关联答复/双方ACK | PASS | [manual01](evidence/manual01/README.md)：正式API、原thread工具、正文与产物逐字节一致；由用户初始化的工作轮执行 |
| H01 原thread原生自动触发 | 仅触发已证 | [heartbeat01](evidence/heartbeat01/README.md)：两thread各4轮；有原生输入和终态 |
| H02 忙时避让 | 未验 | 实际开始试点时两原thread已自然结束；无权据源码宣称竞争安全 |
| H03 自动收信与后续闭环 | NOT_PASSED | 两版提示词、8轮、0工具调用，无新试点咨询；已暂停，不以聊天回复算通过 |
| heartbeat常驻成本 | 不采用 | 实测输入905,850（缓存902,656）、输出845；普通程序监听+有工作才启动模型为后续方向 |
| H04 已读未完成/重复/重启 | PASS：消息API | [domain-e2e02](evidence/domain-e2e02/result.json)：真实隔离CLI/API，ACK+accepted重启仍可recover；重复receipt不覆盖澄清，reply后移出recover；不代表业务副作用或Desktop恢复 |
| H05 目录、错owner、转交 | PASS：声明与协议 | 目录CAS/owner-only/同组织；scope拒绝；单跳关联、不扩大peer；正文语义仅Skill约束，原Agent新版Skill行为未做真实自动验收 |
| H06 v3→v4迁移 | PASS：隔离及安装 | 隔离旧列rowset保留，Journal事务故障回滚；[installation01](evidence/installation01/post-install-independent-verification.json)正式4条既有消息旧列哈希相同 |
| H06 正式新协议 | PASS：6组消息协议验收 | [installed03](evidence/installed03/result.json)：随机测试身份，scope/幂等/ACK/accepted/recover/reply/completed；临时scope已清理、凭据已撤销；无业务Worker/模型/资金操作 |
| H07 Desktop重启/人工输入竞争/审批等待 | 未验 | 未重启用户共享宿主、未人为打断业务会话 |
| 源码检查 | PASS | [checks01](evidence/checks01/README.md)：accepted重试漏洞首次失败保留；修复后全量go test -race ./...通过 |

首次隔离CLI失败为夹具将legacy未ACK消息误判为不应在收件箱，修正断言后13组真实验收通过。第一次记录与复验分开保留。不得把本表的消息层PASS扩大为原Desktop自动执行或支付接入完成。

正式安装前两次验收未完成亦保留：[installed01](evidence/installed01/export-manifest.json)是脚本权限前置过严；[installed02](evidence/installed02/result.json)是历史密码文件不匹配当前owner。均未创建测试身份/消息；纠正输入后installed03通过。
