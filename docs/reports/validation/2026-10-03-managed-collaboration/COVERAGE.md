# 本轮覆盖：M01–M08

状态截止 2026-10-03 本轮收尾；D 为源码/事务测试，R 为隔离真实 CLI/API/Codex，I 为已安装正式工件。下表按实际边界判定，不表示完整 ADR 或所有失败组合都已验收。

| 用例 | 当前状态 | 具体边界 |
|---|---|---|
| M01 旧库升级 | D / R / I 通过 | 隔离v4→v5原42表旧列摘要保持；正式安装旧6条消息、6项绑定及职责目录原列摘要保持；原external消息协议再验通过 |
| M02 默认 Codex pane | D / R 通过 | 同安装 SHA 的 `fleet workspace --respawn-dead`，独立 tmux 中真正原生 pane、唯一输入、Task/Run/Journal、文件与终端核对；`fleet up` / systemd 首启仅源码检查，未做真实全链验证 |
| M03 自动咨询闭环 | D 通过；R 通过 | 两个真实 Codex；A 工具发问、B 真实读事实、A 自动消费；同 backend/thread；非原业务身份 |
| M04 忙时/连续工作 | D / R 通过 | B 的真实 sleep 75秒，37次采样中咨询始终 queued / 0 Run；当前任务结束后答复，A自动消费；B的持久Run区间不重叠 |
| M05 重试/重启 | D / R 通过（就绪后恢复） | 离线同key双提交仅1消息/1Task；仅重启精确B Worker，正式 `agent resume` 使Runtime就绪后原待办继续、双方thread不变。未知执行不自动重做为D层验证，未新增真实取消矩阵 |
| M06 职责/失败 | D 通过；R 协议拒绝通过 | 真实未知scope、错误owner、行动request拒绝且0Task；事务回滚、binding/role变化、failed/uncertain、0 Run隔离及普通任务继续为D层。未做真实全部职责竞争/取消组合 |
| M07 前台退出/重开 | R 通过 | 实际PTY断开后后台query成功；重开显示结果且thread一致。Ctrl-C取消当前任务是另一种操作，其首次记录保留 |
| M08 空闲30分钟 | R 通过 | 18:06:43–18:36:43，1800.107秒；Task清单不变、精确thread新增模型轮次0、工具调用0。仅指两个测试Agent，不等同全机或账户账单 |

原生输入默认 mutation，Task 可保守标记 `uncertain/business_effect_unverified`；前台、Run 与独立效果分别判定。managed query 必须 `succeeded/query_result_delivered`，不放宽为 uncertain。

可复核入口：[公共证据索引](evidence/live-e22e4a8-01/README.md)、[M04](evidence/live-e22e4a8-01/evidence-recovery-20261003T192604-3f72/M04-result.json)、[M05](evidence/live-e22e4a8-01/evidence-recovery-20261003T192604-3f72/M05-result.json)、[M06](evidence/live-e22e4a8-01/evidence-recovery-20261003T192604-3f72/M06-rejections.json)、[只读补采结论](evidence/live-e22e4a8-01/evidence-recovery-20261003T193650-574a/verdict.json)。最初漏resume及旧PID采集失败均保留，不把后续PASS覆盖原FAIL。

原 Rhythm / Pay 自动迁入、AGY 原生前台、全部审批竞争、精确仅目标工具取消、未知终态一键恢复不在已完成范围。managed→external 人工回复暂不自动续办。
