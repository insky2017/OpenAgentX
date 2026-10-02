# 三轮真实 Codex 工作流复验

- 已通过：读取真实文件并返回角色验证码；工具创建精确字节文件；同一 Worker/generation 接受下一独立 query。
- 首末 query Task succeeded，completion_basis=query_result_delivered；文件 mutation 的 Run succeeded、Task uncertain，未改写其诚实未知状态。独立第二次读盘确认输出字节与输入完全相同。
- 三项 Task 均保留正式 API 的 Task/Run/Journal。daemon/Worker 的 PID、starttime、实际 exe SHA 前后相同，匹配 candidate01 的记录。
- 初次 real-workflow01 在登录后的 Secure-cookie 客户端契约失败，没有派 Task；修复测试客户端后在本独立目录复验，首败保留。
- 这是隔离进程与真实模型/API/文件链证据，尚不证明正式安装、原生 PTY、浏览器操作或取消回收。文件效果独立通过不等于 mutation Task 自动成功。

下一步：由主代理继续受管原生终端及最终安装验收。
