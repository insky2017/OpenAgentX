# 安装 I 首次执行：入口成功，角色文本严格断言失败

使用已安装17d5cd22b442fb5fd02bd661b36c5db4299fea9d，二进制SHA-256为666cbd876c540d63b1214f81e0e65b7922b9a41f987ba21364d03718c7e22ccd。正式daemon PID306551；只新增测试Agent agy-onboarding-e2e，真实user-systemd及未加capture的agy-graft。

已完成真实三问PTY add、重复add保持同Worker、open及watch。角色+input文件查询成功。A的工具started后正式创建B，B queued且0 Run；修改测试ROLE后pause等待A完成并退出Worker。

首次FAIL原因：A Task/Run均succeeded，角色SHA256仍旧值，最终行是正确旧角色，但AGY在最终响应前多了一句进度说明。夹具原先比较整段字符串，因此抛AssertionError。本批verdict保留FAIL，未改写模型原输出。角色冻结验收改为旧摘要+最后一行旧标记+没有新标记，语义边界见下一批。

不重复A或B，后续仅在 ../installed-17d5cd2-onboarding01-resume01 读取原A并恢复原B。正式服务原始资料位于 /home/sky/.local/state/openagentx/evidence/installed-17d5cd2-onboarding01；本目录保存首次API/PTY/日志/脚本版本，SHA256SUMS可复核。
