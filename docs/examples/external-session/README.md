# Rhythm / Pay 原会话接入文字

这两段文字用于管理员完成身份绑定之后，由**对应原会话自己执行**。主代理不会冒用它们的凭据生成业务答复。当前宿主自动投递仍受阻，以下文字尚不等于已发送或已执行。

## 给 Rhythm 原会话

> 你继续负责 `/home/sky/work/touzi/OneAxe/rhythm`，保留当前会话和开发任务。现在增加 OAX 通信身份 `rhythm`，不运行 `agent resume`，不另起 Worker。先运行 `openagentx external --help` 和 `openagentx external status --agent rhythm`，核对登记 thread 为 `01a0b4e3-7b2e-7822-8ae1-9f0e812115a9`。若不是，停止发信并说明。
>
> 在当前任务可安全衔接处，准备只读咨询 `rhythm-pay-handoff-01`，通过 `openagentx external send --agent rhythm --to oneaxe-pay --key rhythm-pay-handoff-01 --content-file <正文绝对路径>` 发出。正文给出你方 app/product/customer/order/operation、独立 runtime 和 callback 的当前事实及证据；请 Pay 确认是否延续 `steadyflow-334`、商品 `steadyflow-334-30d-v1`，及 `18458/internal/payment-events` 当前归属。正文/答复产物保存在自己的 OAX 本地状态目录，不覆盖当前业务代码。
>
> 用 `external inbox --agent rhythm` 收信，读入后用 `external ack --agent rhythm --message <ID>` 确认。对关联结果整理“一致项、差异项、迁移前置、最小下一步”，在原会话呈现。不要对 result 自动再 reply，不制造回复循环。新问题用新咨询和新 key。
>
> 本轮只读确认：不创建订单/退款，不改业务凭据，不迁移数据，不启动/重启/替换服务，不改支付仓库。区分当前源码、既有运行快照和本轮实测。现有未提交工作保持原样。当前 OAX 无 Desktop 自动唤醒能力；完成本轮后如无新输入，不能承诺持续监控或自动续办。

## 给 Pay 原会话

> 你继续负责 `/home/sky/work/touzi/OneAxe/oneaxe-pay-service`，保留当前会话和开发任务。现在增加 OAX 通信身份 `oneaxe-pay`，不运行 `agent resume`，不另起 Worker。先运行 `openagentx external --help` 和 `openagentx external status --agent oneaxe-pay`，核对登记 thread 为 `01a0e016-d951-77d1-bc7e-d13662f4823c`。若不是，停止发信并说明。
>
> 在当前任务可安全衔接处，运行 `external inbox --agent oneaxe-pay`。对 Rhythm 的 `rhythm-pay-handoff-01`，读入后 `external ack --agent oneaxe-pay --message <ID>`，依据当前源码及交接文档确认 `steadyflow-334` 的注册作用域、商品、通知URL、18458接收者归属及凭据交接边界。标明源码事实与带时间的运行快照；解释沿用应用身份或新登记分别需要什么。只给凭据保管位置和交接方式，不输出秘密。
>
> 将正文保存在自己的 OAX 本地状态目录，通过 `openagentx external reply --agent oneaxe-pay --message <原请求ID> --content-file <答复绝对路径>` 直接回复，附证据定位；结果默认不要求对方再回结果。保留命令回执和 message ID，在原会话展示结论。
>
> 本轮不执行支付、注册、部署、业务凭据修改或跨仓库修改。保护原开发工作。当前没有原 Desktop 会话自动唤醒接入；无新输入时不能承诺自行醒来处理后续消息。
