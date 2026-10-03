# 公共验收证据导出

- 安装工件：e22e4a8；主隔离 profile：live-e22e4a8-01。来源、源 SHA-256、脱敏 SHA-256 与转换记录见 export-manifest.json。原始材料未改动。
- Fleet：evidence-fleet-observe-20261003T184336-73b0 含 Task、Run、Journal、pane 回显和独立 marker；唯一输入的 Run succeeded，原生 mutation Task 仍是 uncertain/business_effect_unverified。范围仅含 fleet workspace 默认原生 pane，不含 fleet up/systemd 首启。首次超时保留在 evidence-fleet-20261003T183859-0ffd。
- 前台关闭：fleet-owned-tmux-close.json 记录隔离 tmux 关闭；fleet-independent-export-verification.json 再次只读确认 no server running。后者进程快照与独立 recovery 验收重叠，部分旧 Worker PID 已退出，不能混作 Fleet 关闭时的进程状态。
- 咨询与结果续办：evidence-chain-20261003T180128-1865；原生关闭后继续与 1800 秒空闲：evidence-native-continuation-20261003T180548-58be。Task/Run/Journal、PTY、过程日志和失败记录均保留。
- EVIDENCE-GUIDE.md 是历史索引，仍含 idle RUNNING/Fleet 待执行的旧文字。最终核验入口：idle-result.json、verdict-native-continuation.json、verdict-fleet-observe.json。
- 不导出 profile、凭据、数据库、二进制、password、http-raw、原始 rollout。6 个 rollout 只保留逐行 source_line/timestamp/type/payload.type/turn_id/model/effort/thread/session ID 元数据投影，可重算事件计数；源 SHA 与投影 SHA 对应记录。
- 源 SHA256SUMS 原样保留为 SOURCE-SHA256SUMS.txt，仅作历史来源记录，可能引用未导出的原始 rollout 或旧文件 SHA；当前导出文件校验使用 SHA256SUMS。
- 恢复验收：evidence-recovery-20261003T192604-3f72 保留 M04 忙时排队、M05 离线恢复、M06 拒绝的完整 Task/Run/Journal、独立 fact 及最终旧进程引用采集失败；evidence-recovery-resume-20261003T193523-a743 保留正式 resume 恢复 runtime 就绪；evidence-recovery-20261003T193650-574a 只读收取既有任务证据，M04/M05/M06_rejections 为 PASS，未新增 Task，M06_cancellation 为 NOT_RUN。
- installation01 的两个业务只读状态快照以及 html-status01 历史页面核验均保留；最终隔离清理见 evidence-recovery-cleanup-20261003T193815-8229/cleanup-result.json，保护正式三个 PID 不变；最终 HTML/Markdown 的静态核验与 SHA 见 ../html-status02/verification.json，未实施浏览器视觉复验。

- 秘密扫描扩充：从本验收 profile 通信凭据、Worker 环境、password 和 http-raw 认证字段抽取 26 个去重实际秘密值；全公共证据逐字节扫描零命中，Bearer/API-key/private-key 模式零残留。仅记录字段计数、来源路径与结果，不导出秘密值。html-status01 为历史快照，html-status02 为最终静态核验，已再次核对文件 SHA。
