# 正式指挥台只读验收

- 正式入口 `http://127.0.0.1:18100`；candidate `6b68eeb810c2b65d629cd8a817bad25a21483d5c`。真实 Chrome DevTools 独立上下文 `codex-installed-observe01`，page 3。没有从浏览器派单、取消或验收任务。
- 桌面 1440×1000：新 Agent 可开始工作；运行中 DOM 显示“正在工作，新任务将排队”；结束后绿灯 `status-dot ready`。02-running-dom 捕获忙状态，02-running.png 截图时任务已经取消恢复 ready，不能将该截图声称为忙灯色证据。首次 01-ready-dom 的 AGY 未就绪为 root 随后 resume 前的真实过渡，02 已恢复 ready。
- 首 query 显示 `NATIVE-QUEUE-END01|OAX-CODEX-DOMAIN` 与 `codex-app-server · gpt-6-astra`。mutation 保持 Task uncertain、Runtime succeeded、business_effect_unverified，cancel 显示 canceled/Runtime canceled，后续 query 正常显示完整结果。浏览器所取正式 API 见 10，实际 API/文件/进程验收见相邻 installed-api01。
- 390×844 窄屏：document scrollWidth=390，无横向溢出；继续/接受/结果有问题三个主动作高44px。结果区 overflow auto，可滚动看到完整回复，11截图验证。结果区约151px高，查看原文小按钮约27px高，属于已存在的非阻断体验项，没有声称所有按钮均44px或实体手机验收。
- 将独立浏览器网络切为 Offline 后工具同时引起页面重载，navigator.onLine=false，认证请求实际 ERR_INTERNET_DISCONNECTED，页面仅“连接中…”、没有写控件。恢复网络后无需重新输入密码，所选 Task 与结果恢复；session 200，SSE `after_sequence=272931` / `272960` 200。没有声称原地任务页按钮 disabled、草稿保留、在途任务跨断线或所有SSE事件无遗漏。原地离线写保护沿用此前AGY基线证据，本次仅证明离线认证路径没有开放写入口及恢复。
- 浏览器实际加载 app.js SHA256 `eefd58729fbd087a955f39131442779d20171d8d1aa0980993f56c5082285d86`，app.css `0160b18a815f3d2ea8dd32c4b9063514202072e9003ffc27d1411a4f435bef8a`；与隔离已验版本相同。恢复后控制台无 error/warn。
- 18截图中列表最上方出现 native 代理随后创建的两项任务；本观察者没有操作它们。原始 evidence 持久位于本目录，公开副本为仓库 evidence/browser-installed01，SHA256SUMS 覆盖每个交付文件。

工具/夹具偏差：第一次状态几何选择 `.agent-card` 无匹配，随后用真实 `.status-dot` 读取。06截图滚动过远，只显示回复末尾，11修正滚动位置后显示完整回复。首次尝试 `networkConditions="No emulation"` 被工具参数校验拒绝；按照 schema 省略 networkConditions 恢复成功。这些均非产品失败。视口变更的即时“连接中…”为重载过渡，正式证据采用稳定后的状态。
