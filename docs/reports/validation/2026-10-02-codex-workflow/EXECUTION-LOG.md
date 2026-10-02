# Codex 工作流执行记录

## 基线

- 用户已确认实施；产品分支 codex/agy-workflow，起始 e52a3ab。既有安装 9434479 属 AGY 交付。
- 计划与 E2E 矩阵先行；子代理 gpt-6-astra/high 分工：协议关卡、网络、CLI、自立验证；主代理负责 Runtime 与集成。
- 本机 Codex 0.160.0；官方在线文档返回403，依据本机help/schema与实际协议。Unix listener 为 WebSocket，最初裸JSONL失败记录由协议关卡保存。
- 当前未宣称任何 Codex 正式工作流通过或安装完成。

## 实施批次（进行中）

- CLI 已支持 Codex add、离线幂等 join、prepare/status、正式 resume 与原生桥接入口；准备态不启动服务或领取任务，resume 就绪后才持久启用 Fleet。
- 默认代理从本机持久 Mihomo 配置保存，Codex 环境支持与 AGY 分开；NO_PROXY 补回环。
- 新 Codex app-server Runtime 采用实际 Unix WebSocket；thread 已创建即受 fencing 保护保存绑定，再提交 turn。显式 native session 选择与 Task/Message/Mailbox 同事务创建。
- 原生终端写操作通过正式 Control API；独立验证发现通知跨 thread、快速完成响应排序与输入幂等问题，正在统一修复，修复前不能报整链通过。
- 主代理第一次 SQLite 定向检查因 SQL 使用 payload（真实列 payload_json）失败，已修。此诊断仅工具输出，未保存完整原始日志，不作为 E2E 证据。独立 native-session-d01 另发现旧 Task 事件非 task.created 导致重放失败；已修且 d02 通过，失败与复验保留。
- 协议探针与真实 Adapter 双轮均已通过：原生 TUI 共享会话、同活动 turn 的 start 实为 steer、错误 turn CAS 拒绝、关闭终端不中断；Adapter 真实写文件与第二 Task 原生上下文回忆通过。尚不等于正式 API/安装链通过。
- CLI 的真实 PTY 离线准备证明与模型运行区分，见 evidence/codex-cli-join-20261002T074001Z。

## 真实工作流与取消发现

- local01 隔离正式 CLI/API/Worker 已运行。real-workflow01 在登录后因测试工具未发 Secure cookie 返回401，未创建任务；只修 loopback harness，不改产品认证。real-workflow02 三轮通过，同Worker/generation：query成功、mutation Run成功且Task保留uncertain、下一query成功；产物另行精确读盘核验。
- 浏览器在实际隔离服务完成桌面/390px检查，状态、Codex标签、完整结果与刷新一致；范围为 R UI，不冒称正式安装 I。
- 延后核验发现原始 Codex turn/interrupt 的 interrupted 事件不能证明工具停止：sleep后仍写出cancel sentinel。首次协议取消记录修正为FAIL，禁止保留早期即时absence的成功结论。Runtime新增精确后台工具终止与仅自有host进程身份核验兜底，真实复验等待超过原写入时点、sentinel不存在且重建同thread下一轮成功；外部host不做越权进程终止。


## 提交、最终验证与安装

- `370af31` 固化 Task 的显式原生会话与早期受 fencing 保护的绑定；`6b68eeb` 提交 Codex adapter、原生桥接、CLI、自初始化 Skill 与 Web readiness。最终受影响 Runtime/nativebridge/Worker/CLI race、全量 go vet 和 diff 检查通过（final-checks01）；此前 full race/Web 验证保留于 deterministic01。
- 原生真实双入口完成：前台真实文件任务运行时，同 thread bus Task 保持 queued、零 Run；前一 Run 完成后约 3.6ms 后才启动下一 Run。终端退出→后台接下一单→重开历史也通过（real-native01/02）；0.160.0 的双进程 SHA 已核对，首次0.153.1横幅来源不能补造为已确认。
- 首次 clean worktree Go 1.22 构建意外带入外围仓库 revision 及 modified=true，安装门禁拒绝；首次 build-info 保留。改为有独立 .git 目录的固定 commit clone，构建嵌入 `6b68eeb810c2b65d629cd8a817bad25a21483d5c`、modified=false，binary SHA `f9b74b7df378ee05a38d3b49dc465c9168ff3ad7c70a49f81430e21b7f6b3366`。
- 安装前正式 API 及只读数据库核实无活动 Run，AGY 演示 Agent 无待执行任务；备份旧 binary/Web/数据库后替换 binary/Web，重启 daemon 与空闲 AGY Worker，核对实际 /proc/exe SHA。备份仅在私密目录，不把数据库入库。AGY 新代网络通过正式 resume 再次应用。
- 核验 PID/starttime 后仅停止 local01 自有 daemon/Worker/app-server，迁入旧 native thread `01a0fb9b-4097-79d3-902f-6603a844fa15`。正式 `join --prepare` 后仍未运行；`resume` 正式注册、网络就绪后 ready。新 Agent `codex-domain-e2e` 用新的角色与 workspace，保留旧原生历史。随后正常重启此测试 Worker，generation2 经 resume 就绪，用于代理持久验证（migration01、installed-entry01、installation01）。
- 正式服务真实模型、取消、原生终端与浏览器最终验收正在进行。C08 精确仅目标 turn 的原始要求、C09 未知终态自动恢复仍不能据此写完整 PASS；最小可用边界见操作指南和最终覆盖矩阵。


## 最终正式环境结果

- installed-api01：2026-10-02 08:38:32–08:41:30 UTC，5个Task同 Worker `worker-958389c6-d71a-4861-812e-40eafbecf1b3`/generation2。旧thread不调用工具回忆 `NATIVE-QUEUE-END01` 并回答新职责 `OAX-CODEX-DOMAIN`；精确产物、第三轮、90秒工具取消、取消后query通过。父子PID+starttime已退出，原写入点+5秒没有sentinel，执行计数1；daemon/AGYWorker/CodexWorker身份不变。
- installed-native01/02：08:44:33–08:49:03 UTC，两次正式 `open --native`，前后台实际Codex均0.160.0；原生输入落Task/Run并生成精确文件，Ctrl-D退出后同thread后台query成功，重开历史及安装后结果可见。最终自有前台退出，正式Agent保持ready。同generation2；mutation保留uncertain，未篡改账本。详细见 installed-native01/SUMMARY.md。
- browser-installed01：正式桌面/390px、query/mutation/cancel状态语义、44px主动作、无横向溢出；断网工具触发重载，离线没有写入口，恢复后session/SSE及同Task结果可读。不是原地disabled按钮测试，也不证明在途任务跨断线。窄屏结果区151px、次级原文按钮27px保留体验待办。
- AGY兼容：原Task `task-cbb673ca-d102-42f0-a198-92e55f7fbe24` 真实回复正确。首次汇总脚本错用task_id导致汇总失败；02-review仅重读同Task并核验，不重发模型调用。两次证据均保留。
- 架构七视图与阅读入口同步到实际安装6b68eeb。原来的C08仅目标turn、C09未知终态自动恢复等未全部达成，按覆盖矩阵保留部分状态，不宣称全矩阵PASS。正式测试仅使用隔离验收目录；旧local01后台已停止，正式演示Agent保留在线。

- 最终静态封装核验：613份当时证据文件已做已知真实秘密精确扫描，零匹配；496项既有SHA记录全部一致。随后保存正式Agent ready与release最终标记，并生成覆盖全部证据文件的根SHA256SUMS。冻结ADR无diff，Python脚本语法及git diff --check通过，见final-audit01。
