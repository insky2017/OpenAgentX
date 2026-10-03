# 原 Rhythm / Pay 托管切换执行记录

时间为 2026-10-03，北京时间（UTC+08:00）。原始证据保存在 `/home/sky/.local/state/openagentx/validation/2026-10-03-rhythm-pay-managed/`；脱敏副本、SHA 清单与最终判定在本目录维护。

## 授权、范围与起点

用户确认两个原任务均已停止，授权直接执行第二步切换，按 Pay → Rhythm 逐个核验，尤其核对 tmux window 名称和 pane 0。本轮保留原领域身份、原 thread 与职责目录；只读接口咨询用于验收，不执行收费接入、资金、补发、回调迁移、数据库或部署变更。两业务仓库初始干净。

## 首次迁移阻断与修复

原绑定仓储无条件拒绝 external→managed，即使 external 已撤销。提交 `d0f3388e4cc618ed1f4569ac4a2327cf1cf50e56` 仅允许 revoked external、正确 generation、同 Agent/原 thread、无相关未终结任务或咨询时迁入。真实 SessionBinding 仍由服务端核验；active external、不同 thread、反向迁回 external 拒绝。8 场景事务回归和四个相关包通过，见 `evidence/migration-tests.json`。

通过干净独立 checkout 构建并安装 d0f3388，schema v5。最初安装收证脚本遇演示 Worker 的 MainPID 暂为0而失败；安装已经完成，未重装。只读补采证实 canonical、daemon、实际 Worker 都匹配发布工件，演示 Worker 曾随 daemon 断连后自行重启，不宣称 PID 不变。首次 helper 登录Cookie/limit夹具问题修正后使用正式API，不直接改数据库。

## 固定终端与原会话

- 22:25：专用 manifest 的 `fleet workspace` 预检发现原 `overview` 为 unmanaged shell，拒绝创建且未修改。改为仅创建两个新业务窗口，在新 pane 中调用正式 `agent open --native`；没有对旧窗口打托管标记，没有全量 `fleet up`。
- 22:25–22:26：Pay 在原 thread 初始化query成功；旧external G1撤销为G2后，启用managed G3。窗口 `OAX:oneaxe-pay.0` / `@27` / `%54`。
- 22:27：Rhythm 初始化失败，Task保持 `uncertain`；专属app-server日志为 `thread-store conflict ... already has an active writer`。Desktop的idle只表示本轮结束，不等于writer已释放。服务日志还记录系统inotify watch耗尽告警；其不是本次writer错误的证据。
- 22:34–22:35：先核对Rhythm无进行中轮次，登记为open的两个后代实际均completed/notLoaded；通过正式App归档原任务，读回notLoaded，再取消归档仍notLoaded。没有重启共享Desktop、删锁或改数据库。归档连带69个结束子任务，随后逐一正式取消归档并只读比对，69/69恢复原状态。
- 22:35–22:37：只重开本轮Rhythm死pane，原thread初始化成功，再启用managed G3。窗口 `OAX:rhythm.0` / `@28` / `%55`。两窗口关闭automatic-rename和allow-rename；原11窗口18pane保持。

## 第一次真实只读咨询：失败保留

22:37发起Rhythm query `task-42fac3d0-d4de-42fb-807f-a99aee058041`。Rhythm真实调用instructions、读取正文并执行一次ask；消息 `external-message-c3f94db8-1f44-44c1-8238-94742f8290f7`，key `rhythm-pay-managed-cutover-20261003-01`。Pay自动领取 `managed-task-e66ad7dce3c60b04e5a2a631cc713718`，读取自身仓库源码/文档，给出接口、回调与职责边界。

22:42 Pay Runtime Run成功，但正常最终答复4391字节，被共用的4KiB日志摘要截断。Task因此 `uncertain/query_result_unverified`、消息needs_review，未启动Rhythm自动续办。这是产品结果保留边界缺陷，不能当作E2E成功；原Task/Run/Journal/终端输出保留，不人工改判。

## 修复与第二次真实复验

提交 `886ba7fd255a5f6632ee550d4bdcc786274f48fa`：完整答复32KiB，事件/错误4KiB。Task/Run观察与Console、ACP提前投影使用对应边界；脱敏和真正超限uncertain不变。4278字节脱敏黄金来自精确4391字节原答复；通过正式WorkerService.Finish验证Task/Run、关联结果和续办事务。9个受影响包回归通过，见`evidence/result-limit-tests.json`，不重跑无关模型矩阵。

23:00左右：确认无running/waiting_input任务后停止两空闲业务Worker，备份、原子安装干净构建并重启daemon；逐个resume两业务Worker，generation变为2。两原生前台仍持有旧进程，在核对原pane身份、无运行任务后，仅重开新建两业务pane，window/pane ID保持，使用原thread恢复，没有新初始化模型任务。

23:01:09–23:07:47：第二个唯一key `rhythm-pay-managed-cutover-20261003-02` 通过原Rhythm发起，明确带本轮origin task ID。Pay复查自身源码，7973字节完整答复成功交付；Rhythm被自动安排query，核对本域实现并给出职责清晰的接入计划。3Task/3Run均成功，仅2条关联消息，无回复循环。实际Task/Run/Journal、CLI回执、精确thread工具记录、终端抓取、原仓库HEAD/status均核对；当前范围为只读协作PASS。

首个失败result由主代理查阅后ACK，旧咨询仍needs_review，原uncertain记录保留。复验是明确的新只读任务，不伪造旧结果，不自动重做业务动作。

## 收证纠正与最终边界

首次验收脚本误要求每次resume都存在新的runtime.session.bound事件；现有会话会在dispatch时直接登记绑定。改为逐Task核对持久SessionBinding、原thread、真实rollout与终端，未重执行任务。第二项纠正是保存证据的附加Bearer脱敏使字面占位符变化1字节；实时API与实时消息全文比较为7973字节且相同。协议值比较在脱敏前进行，导出另有来源SHA，不混淆两层。两个收证纠正文件保留，见[绑定断言纠正](evidence/verdict-collector-correction.json)、[导出比较纠正](evidence/verdict-export-comparison-correction.json)。

最终[独立核查](evidence/independent-review.json) PASS、无实证阻断。独立代理核对三项Task各单次成功Run、原thread和ROLE、咨询与结果关联、自动续办Journal、实际工具及终端结果，并经实时正式GET与CLI确认7973字节全文一致。运行工件、两业务Git和原11窗18pane已独立实核；两次首次失败保持原uncertain记录，69个子任务恢复通过。本轮origin由正式API创建，不将终端结果可见扩大为本轮人工键盘输入验收，亦不将只读协作通过扩大为支付收费业务完成。

原始证据不删除；发布副本经已知凭据扫描、SHA清单核验。所有旧窗口以ID/PID/名称核对，数字索引变化不用于判断进程被替换。没有重启共享Desktop，没有修改两业务工程。系统此前有inotify watch资源告警，当前服务/真实执行正常；本轮未改主机级资源配置。
