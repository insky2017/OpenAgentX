# 最终安装浏览器日用链：正式只读取证

来源：已安装9434479候选（精确Go build revision/modified、安装binary hash见`*-process-provenance.json`）。取证期间daemon PID406989、测试Worker PID419459、generation4，前后PID/starttime/exe hash一致。HTTP地址 `http://127.0.0.1:18100`。浏览器用户动作由主代理执行，本目录只调用正式登录/Observe读取接口和SQLite `mode=ro`，没有派发Task、改业务状态或重启服务。

## 三项正式结果

- A `task-1a805ec2-0939-4dd5-9c94-eda6fbc36602`：mutation Run succeeded，Task uncertain/business_effect_unverified，top-level review accepted绑定当前Run；55条Journal事件。
- B `task-63dae86b-d09b-4649-8b74-866d8eeef6f2`：继续mutation Run succeeded，Task uncertain/business_effect_unverified，review accepted绑定当前Run；69条Journal事件。SQLite只读确认parent=A。
- C `task-f2633591-6978-431a-9314-a07754cab043`：独立query succeeded/query_result_delivered，回复两行为`DAILY-WORKFLOW-20261002`与`OAX-ROLE-NEW-ff09d70390`；14条Journal事件。SQLite只读确认parent=NULL。

三项均只有一个Run，Worker instance/generation一致；最终API均无older/live事件分页余量，已保存完整当前Task事件列表。人工接受不会改写A/B原执行不确定状态。query成功和人工接受都不被扩展为通用业务效果自动验证。

## 证据定位

- `tasks/<task-id>/latest-task-run-journal.json`：最终正式Task、messages、Run、review、完整Journal；同目录Run独立Observe响应。
- `http/`：带时间/方法/路径/状态的脱敏请求与正式响应；登录密码、cookie、CSRF未入库。
- `*-process-provenance.json`：systemctl实际argv、daemon/Worker PID/starttime、`/proc/<PID>/exe` SHA-256、cmdline、go buildinfo；只读取所需进程字段，不导出环境凭据。
- `*-agent-worker-overview.json`、`*-network.json`：正式Worker generation4和当前网络应用；`final-api-and-network-proof.json`核验全部Run同代、网络applied到同Worker/代、review绑定、Journal无遗漏页。
- `*-sqlite-independent.json`：只读URI、SQL及参数、每项父任务与Run数量；`final-three-task-independent-proof.json`独立断言A/C parent NULL、B parent A、共3Task/3Run。
- `*-verdict.json`：取证执行结果，PASS仅指读取及独立核验成功，不把A/B Task误标succeeded。

首次取证脚本把Observe DTO中未提供的`parent_task_id`当作null比较，导致`parent mismatch` FAIL；该批verdict和HTTP原始证据保留。修正为正式API核验其实际提供的状态/Run数量、SQLite独立核验parent后，A/B复验和A/B/C最终取证分别PASS。属于取证假设错误，没有改产品或数据。原始版本与修正版collector均保存。

私有原始目录：`/home/sky/.local/state/openagentx/validation/2026-10-02-agy-workflow/installed-browser-9434479-daily01/formal-api`，含原始HTTP认证headers及collector，仅限本机，不入库。公开副本使用既有redact并验证未含owner密码；SHA256SUMS覆盖本目录。

本目录不独立证明浏览器点击、离线/重连、文件新增内容和截图；这些与主目录浏览器证据及独立产物记录共同关联。取证完成已通知主代理，随后服务生命周期动作不在本快照范围。
