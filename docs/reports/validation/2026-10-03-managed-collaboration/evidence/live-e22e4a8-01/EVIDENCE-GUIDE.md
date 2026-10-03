# 本轮真实验收证据索引（idle运行中）

- 当前执行工件：`openagentx-e22e4a8`，SHA-256 `14f4221694eedce95a7ce81eefae52b14f58393d8d7368b3057407d10437f2d9`；daemon与两Worker替换前后PID/starttime/exe SHA见 `evidence-restart-chain-20261003T175744-ea1b/`。
- 初始线程由旧候选SHA `1884bcf100e04a12729414ed38a59000703b25748d5d3599b7ba90fa98fdf4eb` 建立。该工件Go内嵌VCS来源不合安装要求，保留历史，不把整个生命周期说成同一工件执行。
- 新工件真实咨询闭环：A任务 `task-06253e4d-3f6a-43b5-8478-00c3d49a4109`；B查询 `managed-task-8a0d11820a1040b67d6fc77425ac3ffa`；A消费 `managed-task-546763f190d95ae40467b74035d134f9`。三项均为query成功，结果与B工作区随机fact文件独立匹配，保持各自既有thread。同key复用原消息/Task。见 `evidence-chain-20261003T180128-1865/collaboration-result.json` 和 `independent-fact-verification.json`。
- 原生输入：`task-a472591c-fb6b-4d9f-9be6-061b0ed374e0` 的Run成功，独立文件字节匹配。原生接口固定mutation，Task仍是 `uncertain/business_effect_unverified`，没有改状态或冒充query成功。
- 关闭前台后后台继续：`task-7d6b7e0f-d7be-472e-8f09-a1ce6a572e25` 在只对验收PTY进程组发送SIGHUP后成功；重新打开显示结果且thread不变。见 `evidence-native-continuation-20261003T180548-58be/native-result.json`。
- 首次失败均保留：PTY ANSI前缀解析；复用profile选到旧历史reply（后改为key→reply_to精确关联）；把Ctrl-C误作终端关闭导致真实取消（后改PTY挂断）。后两次恢复没有重复初始化或重发已有咨询origin。
- 30分钟idle：北京时间18:06:43开始，预计18:36:43结束。核对OAX Task集合及两个精确Codex rollout的turn/model/tool事件计数；期间不派模型工作。当前只标RUNNING，不能提前标PASS。
- Fleet正式入口：canonical SHA已安装匹配，等待idle结束后在独立tmux server下执行。

每次运行单独保存 `harness.py`、`attempt-provenance.json`、HTTP/CLI/Task/Run/Journal。原始资料保留本目录私有文件，提交时仅复制 `evidence*` 的脱敏副本；密码、profile凭据和http-raw不得入库。
