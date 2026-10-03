# Rhythm / Pay 原会话托管切换交付

2026-10-03：**两个原领域身份已迁入 OAX，固定原生终端可用，真实只读咨询→自动答复→自动续办通过。** 此结论不代表支付接入、回调迁移或收费业务完成。

- 原 Agent、thread、工作目录与职责保持；绑定均为 managed / active / generation 3，职责目录 revision 5。默认代理经重启后实际进程环境核对保持。
- 复验三个 query Task 均为 `succeeded/query_result_delivered`，各一个成功 Run；只有一条咨询与一条关联结果，Rhythm 自动消费后没有回复循环。
- Pay 完整答复 **7,973 字节**，正式 API 与关联消息正文一致，Rhythm 自动核对本域源码并输出接入建议。见[判定与关联ID](evidence/verdict.json)、[工具过程](evidence/runtime/rhythm-executed-tools.json)、[会话绑定与消息Journal](evidence/binding-journal-final.json)。
- [最终独立核查](evidence/independent-review.json) PASS、无实证阻断；独立实时GET与CLI再次确认7973字节答复全文一致，原thread/ROLE、自动续办及首次失败留存相符。
- 原 11 个窗口、18 个 pane 的名字/ID/PID保持；新增两窗口名字唯一、pane 0原生进程存活、自动改名关闭。两业务仓库原HEAD及干净状态保持，未执行支付、数据库、回调或部署操作。

## 从这里开始使用

```sh
tmux attach-session -t OAX
```

按 `Ctrl-b w`，选择下表名字，在 pane 0 直接输入任务。当前前缀已实核为 `C-b`；数字索引可能改变，使用名字定位。

| 领域 | 终端 | window / pane ID | 原 thread |
|---|---|---|---|
| Pay | `OAX:oneaxe-pay.0` | `@27` / `%54` | `01a0e016-d951-77d1-bc7e-d13662f4823c` |
| Rhythm | `OAX:rhythm.0` | `@28` / `%55` | `01a0b4e3-7b2e-7822-8ae1-9f0e812115a9` |

原 Desktop 保留查阅历史，后续执行从以上终端进入，避免同一thread两个宿主争用。Agent需要对方信息时按 `collaborate instructions` / `collaborate ask` 自动咨询，无需定时让模型查inbox。详细恢复步骤见[日用与切换实录](../../../operations/rhythm-pay-managed-handoff.md)。

## 安装与首次失败

当前源码 `886ba7fd255a5f6632ee550d4bdcc786274f48fa`，schema v5；干净独立checkout构建，canonical、daemon及Worker实际工件SHA均为 `15fd8699c2f07af80966dc3edc355ee90f260e109c9715f402aedd16b90d97ca`。见[安装](evidence/result-limit-installation.json)、[运行工件](evidence/final-installation.json)、[代理核对](evidence/final-proxy.json)。

本轮修复 `d0f3388`：允许已撤销external绑定在同身份、同原thread及无冲突时正式迁入managed。随后真实E2E发现普通答复被4KiB日志摘要上限截断，提交 `886ba7f` 将完整答复独立保留至32KiB，事件与错误摘要仍4KiB，实际超限仍保持uncertain。

Rhythm首次还遇到Desktop已idle但writer未释放；经正式App归档/取消归档释放并验证notLoaded后迁入，连带69个已结束子任务的归档状态均恢复。未重启共享Desktop、删锁或改数据库。原overview是unmanaged shell，Fleet预检拒绝后予以保留；新窗口使用正式 `agent open --native`，不宣称本轮fleet workspace/up成功。

首次两个失败Task原样保留；失败结果已人工查阅ACK，不冒充自动续办。修复后使用新key `rhythm-pay-managed-cutover-20261003-02`，旧失败请求仍显示needs_review，未自动重做。见[执行记录](EXECUTION-LOG.md)。

## 证据与边界

[覆盖矩阵](COVERAGE.md) · [证据SHA清单](evidence/SHA256SUMS) · [导出来源](evidence/PROVENANCE.json) · [已知凭据扫描](evidence/SECRET-SCAN.json)。原始证据在本机 `/home/sky/.local/state/openagentx/validation/2026-10-03-rhythm-pay-managed/`，私密备份、数据库、二进制、完整会话历史均不入库。

本轮origin通过正式Task API发起，真实模型自己执行发问；终端过程与结果已实际抓取。本轮没有再跑人工键盘输入矩阵或30分钟空闲实验，可参看[此前独立真实验证](../2026-10-03-managed-collaboration/DELIVERY.md)。query/ROLE不是操作系统写入沙箱；自动行动request、AGY原生前台、未知终态一键恢复和精确工具取消仍有边界。完整答复超过32KiB仍需审阅；既有脱敏会遮盖Authorization同一行的其他文字，准确接口细节同时参考源文档。

下一步：在对应终端布置各自领域的真实开发任务；本轮自动得到的[Rhythm接入建议](results/rhythm-next.md)和[Pay接口答复](results/pay-contract.md)可供审阅，执行具体支付变更前明确业务目标。
