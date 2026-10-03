# 本轮实际覆盖

D=源码/集成回归，R=正式运行链路，I=独立效果核对。仅列本轮必要场景，不按测试数或覆盖率判定。

| 场景 | 结果 / 证据 |
|---|---|
| 已撤销external→同原thread的managed | D/R PASS；`d0f3388`回归、双方G3绑定及原SessionBinding，[迁移测试](evidence/migration-tests.json)、[Journal](evidence/binding-journal-final.json) |
| Pay、Rhythm原thread与workspace/ROLE | R/I PASS；两初始化query真实确认，具体[Pay](evidence/tasks/task-b7622c6b-3501-4fde-9834-0e541bdfea6a.json)、[Rhythm复验](evidence/tasks/task-20c0ff59-07a8-4b2b-89ae-87adb5b43289.json) |
| Desktop停止轮次但仍持有writer | 首次FAILED，正式生命周期释放后PASS；[失败](evidence/rhythm-initialization-failure.json)、[释放](evidence/rhythm-desktop-release.json)、[69子任务还原](evidence/rhythm-descendants-restored.json) |
| 原生pane及无关窗口保留 | R/I PASS；[坐标/进程/原窗比对](evidence/final-panes.json)、[Pay终端](evidence/panes/oneaxe-pay-final.json)、[Rhythm终端](evidence/panes/rhythm-final.json) |
| 原有overview与fleet workspace | 预检拒绝，未冒认托管；[失败回执](evidence/cli/pay-workspace.json)。本轮两新窗走正式agent open --native |
| 正常长答复完整传播 | 首次4391bytes被4KiB截断FAILED；修复后黄金与超限事务回归PASS，[测试](evidence/result-limit-tests.json)、[黄金来源](evidence/result-limit-golden-source.json)；真实7973bytes完整往返R PASS |
| 原领域自动咨询/答复/续办 | R/I PASS，三Task三个Run、两消息、关联源任务与原thread一致，[完整判定](evidence/verdict.json)、[Pay工具](evidence/runtime/oneaxe-pay-executed-tools.json)、[Rhythm工具](evidence/runtime/rhythm-executed-tools.json) |
| 默认代理与安装来源 | R PASS；[最终工件](evidence/final-installation.json)、[重启后代理](evidence/final-proxy.json)、旧通信凭据[401拒绝](evidence/old-credentials-rejected.json) |
| 业务仓库与执行边界 | I PASS；[前后状态](evidence/final.json)、实际工具仅只读源码/文档及本次通信；无业务API、数据库、资金、回调或部署动作 |
| 最终独立核查 | I PASS、无实证阻断；[独立记录](evidence/independent-review.json)核对三Task/单Run、原thread/ROLE、自动续办Journal、实际工具与终端、原窗/业务Git保持，并实时确认7973bytes协议全文一致；两次历史失败保留 |
| 本轮业务支付/收费E2E | 未执行；本轮验收的是协作渠道，Pay与Rhythm也明确当前支付链仍有业务待办 |
| 30分钟空闲、忙时/离线矩阵、键盘输入 | 本轮不重复；历史独立测试在[此前交付](../2026-10-03-managed-collaboration/DELIVERY.md)，不冒充本轮两业务身份再次完整验证 |

本轮证据收集有两项断言纠正：resume复用既有SessionBinding，并不每次发新的runtime.session.bound；导出脱敏文本不能直接与未二次脱敏的协议值比较。均只读补核，无新增模型任务，无篡改失败结果，见执行记录。
