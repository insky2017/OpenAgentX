# 安装 I 第一次受控续验：角色与排队通过，崩溃新代等待窗口过严

承接 ../installed-17d5cd2-onboarding01，未重派A/B。A摘要仍旧值、最终行正确旧标记且无新标记；额外进度句保留。暂停后原B仍queued且0 Run，正式agent resume后generation2网络应用，原B仅一次Run使用新ROLE摘要并返回新标记。角色、E11排队暂停恢复均已通过。

随后真实AGY运行systemd-crash.py（工具延迟120秒），started后对测试Agent精确MainPID336964发送SIGKILL。daemon不动。旧主进程、脚本及sleep约2秒后已退出；由于旧Worker租约约30秒，新注册暂时409 CONFLICT，systemd重试后约29秒注册generation3。

本批FAIL是夹具把“进程停止”和“新Worker在线”同时限为25秒，未将25秒物理停止与75秒状态收口分开。本批HTTP0048在故障后2.013秒记录overview；执行该请求之前recovered()已核验三个旧PID全部停止，因此该停止上界来自保存的实际代码控制流和HTTP时间，非独立保存的即时liveness快照。

没有再次发送SIGKILL或重派崩溃任务。后续 ../installed-17d5cd2-onboarding01-resume02 读取原故障的自动收口Journal/updated_at，恢复当前generation网络并查询。本批verdict保持FAIL，原始失败及原模型输出未覆盖。
