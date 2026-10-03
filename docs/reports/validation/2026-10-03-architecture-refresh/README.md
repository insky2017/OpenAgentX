# 架构图与终端协作图更新

本轮仅更新说明文档、图页及生成源，没有修改产品运行逻辑、服务配置或业务工程。当前能力基线仍为已安装 `886ba7f` / schema v5，原身份真实协作结论来自[切换交付](../2026-10-03-rhythm-pay-managed/DELIVERY.md)。文档提交不是新版本安装。

## 修正内容

- 总体架构从 `6576c559` / schema v3 更新为当前实现：七个视图补齐 managed 咨询、Task 与结果续办、Codex Native Bridge 及两条事件连接。
- 原 Rhythm / Pay 已保留原 thread 迁入；固定终端与职责边界、完整答复 32 KiB / 事件摘要 4 KiB、三项成功 query 及独立核查均有对应依据。
- 清除事件图中仍写“原会话未迁入”的 SVG 注释，将概念示意与真实验收区分。原 Desktop 事件入站仍未验，heartbeat 暂停；历史独立身份键盘、重连、空闲矩阵保留原范围。
- 新增仓库内[事件协作 HTML](../../../design/event-collaboration.html)，与[总体架构 HTML](../../../design/current-architecture.html)互链；发布版本均使用仓库相对路径，下载整个仓库后可离线阅读。GitHub 文件页本身不执行 HTML。

## 检查方式与证据

检查结果：**PASS**。检查跨入 2026-10-04；目录沿用本批开始日期，能力快照仍对应 2026-10-03 的已安装版本。七视图、83节点均已检查，节点文字无越界；两条事件连接统一为 A＝Bridge/TUI、B＝Worker/Adapter。

页面使用真实 Chrome，在仅监听 `127.0.0.1` 的临时 HTTP 预览中检查；无业务消息、模型轮次或产品 API 调用。浏览器记录保存具体命令与返回，截图用于核对实际布局；这不是新的产品 E2E。

- [事件图浏览器记录](event-browser.json)：关系路径切换、全图展开/Escape、前台关闭示意、空闲计数、忙时入队与后续执行、本地演示状态及窄屏溢出检查。
- [总体图浏览器记录](architecture-browser.json)：七个视图、节点详情、缺口筛选、放大、更新后的工作流和原生连接，以及窄屏布局。
- [静态核对](static-checks.json)：生成源与输出一致、链接/引用、HTML ID、内嵌 JavaScript 语法与差异检查。
- [SHA-256 清单](SHA256SUMS)保存本报告工件的校验值；页面源文件的 SHA 记录在静态核对中。

检查环境的首次默认 Playwright 启动缺少下载的 Chromium，改用系统已有 Google Chrome；修改 executable-path 时需关闭本轮独立浏览器 session 才生效。该版 agent-browser 把 file URL 误转为 HTTPS，因此改用本机临时 HTTP 预览。保留这些检查环境限制，不把它们写成产品运行失败。

截图：[事件图桌面](event-desktop.png) · [事件图窄屏](event-mobile.png) · [完整工作流](architecture-work.png) · [原生连接全图](architecture-native.png) · [总体图窄屏](architecture-mobile.png)。

## 维护

总体架构以 `docs/design/architecture-map.json` 和 `render_architecture.py` 为源，运行生成器同时更新 HTML/Markdown；`--reading-dir` 可生成本机阅读副本。事件图的仓库入口是 `docs/design/event-collaboration.html`，本机阅读副本保持相同正文，仅链接适配本地路径。修改时同步对应副本，不将历史报告的旧状态替换为新结论。
