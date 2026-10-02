# 本批证据索引

完整原始资料位于本机 `~/.local/state/openagentx/validation/2026-10-03-desktop-collaboration/`。各批保留时间、实际命令/输出、来源及SHA256SUMS；私密凭据、数据库备份和完整业务正文不入库。

| 批次 | 结果与证据边界 |
|---|---|
| [manual01](manual01/README.md) | 两原会话真实咨询、关联回复、双方ACK与独立文件比对；人工初始化的工作轮 |
| [heartbeat01](heartbeat01/README.md) | 原生触发8轮，0工具调用；自动续办NOT_PASSED，已暂停；包含独立turn用量 |
| [checks01](checks01/README.md) | accepted重试目录校验漏洞首次失败与修复；最终全量race通过 |
| [domain-e2e01](domain-e2e01/result.json) | 首轮隔离验收FAIL：夹具把legacy未ACK收件误判为空 |
| [domain-e2e02](domain-e2e02/result.json) | 13组真实隔离daemon/CLI PASS，包含v3→v4与重启恢复 |
| [release01](release01/README.md) | 干净提交构建与工件SHA，不代表执行结果 |
| [installation01](installation01/README.md) | 正式schema4/工件、旧消息保留、原宿主未变、Skill安装 |
| [installed01](installed01/export-manifest.json) | 验证脚本权限预检过严，服务写入前退出 |
| [installed02](installed02/result.json) | 指定历史密码不匹配当前owner，创建测试身份前失败 |
| [installed03](installed03/result.json) | 6组正式消息协议PASS；临时scope清理、绑定撤销、原服务保持 |

scope声明、处理回执与恢复API的通过，不证明模型已读新版Skill、正文语义强制约束、原Desktop自动续办或支付业务成功。具体结论见[覆盖矩阵](../COVERAGE.md)。
