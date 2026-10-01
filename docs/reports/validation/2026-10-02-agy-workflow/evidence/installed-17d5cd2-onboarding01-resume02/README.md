# 安装 I 最终受控续验及交接

三批证据必须联合读取：../installed-17d5cd2-onboarding01 为真实向导入口和首次严格文本断言失败；../installed-17d5cd2-onboarding01-resume01 完成原A/B角色与队列验证并保留崩溃等待窗口错误；本批未再派A/B、未重打故障，仅读取原崩溃恢复事实并完成正式网络恢复与查询。

## 已验证

- 最终已安装候选17d5cd22b442fb5fd02bd661b36c5db4299fea9d；正式binary SHA-256为666cbd876c540d63b1214f81e0e65b7922b9a41f987ba21364d03718c7e22ccd。真实user-systemd、未加capture的正式agy-graft。
- E01/E03：真实三问添加、首次网络、重复添加；本批canonical identity+worker-config正式导入成功，文件hash、Worker ID/generation、5个Task和5个Run集合均未改变。详见canonical-import-{before,after,verdict}.json及15-canonical-import.pty.log。
- E02/E11：A真实工具运行时创建B；当前Run旧ROLE摘要冻结；修改后pause，B仍0 Run；resume后B仅一次Run，使用新ROLE摘要并输出新标记。角色有一行额外模型进度文本，因此角色语义通过不等于严格“只返回标记”的格式完全服从。
- E17已安装systemd Worker：旧主进程/脚本/sleep退出上界2.013秒（由原控制流+HTTP0048时间推导，非独立即时快照）；原故障Task/Run在32.660秒自动uncertain；daemon PID306551/starttime9994153前后相同。未重启daemon、未清理子进程、未手改DB。原进程生命周期由systemd control-group管理。
- 旧Worker租约未过期时新注册被409拒绝，6次systemd restart后generation3上线；正式agent resume完成该代network test/publish/applied，随后真实query succeeded。旧未知任务仍单Run、脚本invocations=1且无120秒late产物。

## 边界及交接

本批证明最终安装部署下systemd Worker故障恢复；之前14d7350裸Popen SIGKILL的E17总项PARTIAL保留，不能被这里覆写，也不证明脱离systemd时自动清理后代。daemon崩溃故障只在此前隔离R执行，未杀正式daemon。

测试Agent agy-onboarding-e2e保留在线并已交Console代理独占，未清理。handoff.json记录workspace、角色、input及当时服务事实；之后Console会新增自己的Task，因此本批5 Task/5 Run是canonical导入瞬间的验收快照。

私有原始资料继续位于 /home/sky/.local/state/openagentx/evidence/installed-17d5cd2-onboarding01，原HTTP编号连续；三批PTY/API/日志副本及原始harness日志分别封存。harness-source保留实际执行夹具，不包含__pycache__。SHA256SUMS覆盖本批所有归档文件。
