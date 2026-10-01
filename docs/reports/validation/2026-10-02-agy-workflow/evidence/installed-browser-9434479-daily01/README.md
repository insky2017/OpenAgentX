# 最终安装 AGY 日用链：真实浏览器与实际产物

判定：**本批约定的日用子链 PASS（I＋R）**。运行版本为 `9434479e4fd3b6802aa2e4d57610bb710200cd01`，实际地址 `http://127.0.0.1:18100`，使用已有正式历史库和受管 Agent `agy-onboarding-e2e`。本批没有 mock Runtime、额外 AGY capture 包装器或手写数据库状态。三问新建/导入及 Console 的先前安装证据按改动影响复用，不宣称全部22组或所有入口都在943重新执行。

## 实际执行与证据

1. 通过正式 `agent resume ... --no-open --wait 5m` 启动 generation4；[实际命令](resume-command.json)、[输出](resume.log)、[退出码](resume-exit.json)。[首次在线 DOM](01-online-dom.json)与[事件记录](08-before-offline-events.json)证明首次流从安全下界 `267881` 建立；旧17版本的[from0首连失败](../installed-browser-17d5cd2-attempt01/failure.json)仍保留。
2. 桌面浏览器真实 click/type_text/发送，读取两份输入并生成报告。[输入和按钮状态](02-first-prompt.json)、[发送](03-first-send.json)、[完整结果](04-first-result.json)、[接受前独立文件核验](05-file-verified-before-review.json)。原始报告1,249字节，两源标记、三个问题及行动均实际存在；[用户接受记录](06-accepted.json)。
3. 从“继续此工作”发送修改，[请求](07-continue-started.json)明确先执行一次 `sleep 40`，再只追加清单。真实浏览器设为 Offline，[DOM](09-offline-dom.json)显示离线禁写。浏览器仍离线时，独立磁盘读取和SQLite只读核对确认后台已完成：[记录](11-completed-while-offline.json)，报告2,306字节，原1,249字节逐字保留。
4. 恢复网络后，没有点击刷新或再次发送。新观察流从最后已确认的 `267966` 开始，补回 `267967` 至 `268049` 范围内可投影事件；没有用新overview的 `268049` 跳过期间事件。[恢复记录](13-reconnected-events.json)、[在线结果](14-online-continued-result.json)、[实际桌面截图](15-desktop-result.png)。恢复后的瞬时“连接中”快照也保留在[12](12-reconnected-result.json)，后续状态通过14核验。
5. 独立核验后接受追加结果，[记录](16-continued-accepted.json)。刷新页面仍选中正确Task、显示完整结果及原验收，[返回证据](17-reopened-result.json)。再通过真实下拉框选择问答、键盘输入和发送，[下一项输入](18-next-query-input.json)、[发送](19-next-query-started.json)、[完整回复](20-next-query-result.json)。回复精确为材料标记与职责中的角色码；用户prompt没有提供角色码。
6. [正式只读API与独立账本证据](formal-api/README.md)记录3个Task各1个Run，B parent=A、C独立；同一Worker/PID/generation4，网络已应用。Task/Run/Journal分别55/69/14条事件，无剩余分页。[最终命令和service状态](23-final-command-0.json)、[Agent可用状态](23-final-command-1.json)、[实际systemd journal](23-final-command-2.json)、[文件未被query修改](24-final-workspace-check.json)齐全。

## 独立复核与状态语义

- 可运行 `python3 verify_daily.py` 重验已保存的真实证据；[结果](22-independent-browser-file-verdict.json)只对应游标/浏览器/文件断言，不冒充又一次模型运行。
- 初次与继续产物分别为 [initial-workflow-review.md](initial-workflow-review.md)、[continued-workflow-review.md](continued-workflow-review.md)。输入原件一并保存，SHA256SUMS覆盖本批文件。
- A/B 的Runtime succeeded，但Task仍 `uncertain/business_effect_unverified`；用户接受作为独立review绑定Run，原执行记录未改写。文件效果由本次验收者独立确认，不把系统状态改成虚假业务成功。C为 `succeeded/query_result_delivered`。
- API取证首次误把未提供的parent字段当null导致夹具FAIL，原记录保留；改为正式DTO核验状态、SQLite `mode=ro`独立核验父子关系后PASS。没有重跑Task来覆盖失败。

## 范围与环境留置

本批是桌面安装Web日用，窄屏真实操作引用此前隔离浏览器批次。本次Offline影响整个浏览器网络，不宣称仅SSE断流、跨retention 409或原生Last-Event-ID独立故障场景已测。未重新执行全套Go测试。生产AGY原始stdout本批未额外capture；保留正式Runtime事件/结果、Task/Run/Journal、浏览器动作、文件字节及服务进程证据，不声称逐字节CLI流已保存。

演示Worker有意保留在线、无活动Run，方便用户立即打开；daemon继续服务，原本停止的业务Worker不启动。用户可运行 `openagentx agent open agy-onboarding-e2e` 查看本批工作，或 `agent pause agy-onboarding-e2e` 暂停演示服务。不要修改/删除证据目录中的原件来当日常材料；实际工作另建目录和Agent。当前Web没有通用本地文件预览/下载入口，模型回复的 `file://` 链接不代表能在浏览器打开，产物以记录的工作目录为准。

私有原始持久目录：`/home/sky/.local/state/openagentx/validation/2026-10-02-agy-workflow/installed-browser-9434479-daily01/`。原始HTTP认证材料仅留本机；Git为脱敏副本。本批浏览器通过已有Chrome DevTools工具执行，EventSource记录器仅记录真实创建URL、open/error与message.lastEventId，原构造器/事件传输未替换为夹具。
