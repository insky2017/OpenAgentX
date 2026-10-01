# 浏览器真实工作流（R＋交互，安装后另验）

源码 f3cd7a3，真实 Chrome DevTools 操作连接正式 daemon/Worker/agy-graft。该 Web 与已安装17d5cd2逐字节相同；不能因此代替安装入口测试。

- 390px：实际键盘输入并发送写文件，离线期间仍由真实AGY完成；联网后读取完整结果，接受并关联继续。独立字节为 `BROWSER-E2E\ncontinued\n`，正式API验收与只读数据库ParentTaskID一致。
- 320px：结果拒绝、正式logout撤销当前会话、写请求返回登录页；重登保留草稿/问答类型，人工再发后仅创建一次query并得到精确回复。离线草稿保留、写入口禁用、联网不自动提交（数据库0条对应Task）。
- 320px：真实70秒工具任务期间点击“补充（下轮处理）”，两个Run消费补充，原脚本仅1次，文件精确 `BROWSER-SUPPLEMENT\n`；真实工具started后点击取消，进程停止，Task canceled，超过70秒原延迟仍无late产物。
- 首次session及overview各注入一次503，页面自动恢复在线。另实际浏览器Offline时提交登录，提示连接问题而非密码错误，联网后可登录。
- DOM、截图、操作时间在01–28文件；正式Task/Run/Journal、Mailbox、服务日志与副作用互证在 `api/`。95秒续租及系统服务测试另有独立证据。

保留的工具/夹具错误：fill_form未触发React textarea状态，第一次发送未产生Task，改用真实键盘type_text后通过；某次login503导航超时未算通过，改用真实Offline复验；Observe DTO不含ParentTaskID，第一次读取报KeyError，随后用正式API上下文＋只读DB持久字段互证，未重复模型任务。完整日用UI仍存在信息密度较高的改进空间，截图不等于整体UX优秀。
