# 架构图册验证记录 · 2026-10-02

本轮交付[离线 HTML 图册](../../../design/current-architecture.html)与[Markdown 架构说明](../../../design/CURRENT_ARCHITECTURE.md)。仅更新文档和文档渲染工具，未修改产品实现或操作业务服务。本记录证明图册的内容核对、显示与交互；不构成新的产品 AGY E2E 验收。

## 要点

- **事实基线**：源码 `b0b7e09`，最近已安装 Go / Web 与日用验收 `9434479`。两位 `gpt-6-astra / high` 子代理并行只读核对运行主链与入口/依赖，随后一位进行一次有界成品审阅。架构状态与 D/R/I 验收边界分开，来源见图册的 29 个引用。
- **成品范围**：6 个视图、72 个节点；JSON 为同一维护源，标准库 Python 同时生成 HTML、Markdown 与阅读目录副本。HTML 无 CDN、外部字体或联网图表依赖。
- **真实浏览器**：Chrome `143.0.0.0`，Linux；桌面 `1440×1100`，移动模拟 `390×844`。六张图、72 个节点详情、跨图跳转、键盘选择、待补突出、缩放及 SVG 下载动作已检查。SVG Blob 有 12 个节点、XML 解析无错误；未另行核验浏览器下载目录。
- **离线与布局**：离线重新打开阅读目录副本，六个视图均可切换；页面宽度保持 390，图在自身容器内横向滚动；展开待补表格也不撑宽页面。浏览器未加载外部资源，无 console error/warn。
- **首次失败已保留**：首次桌面几何检查发现运行恢复和依赖图有 4 处标签与节点相交；之后视觉检查又发现交叉线压到标签。初次原始返回与复验分别保存在 `browser-records.json`。缩短标题、调整标签与连线路径后，受影响视图再次检查通过，并逐图保留最终截图。

## 内容核对与修正

独立复核后修正：mutation 的系统完成条件是 `SideEffectsKnown`，不能等同独立业务验收；已有补充在明确失败等满足条件的分支也能进入下一 Run；终态后直接继续无需先 review。图中补齐了同 Task 新 Run 与关联新 Task 两条回路。取消子图不再把运行补充接到停止信号路径。

最近安装版本不等于实时服务状态；本机访问 URL 也不代表源码默认仅监听 loopback。远程 mTLS 标为已实现但当前未启用；现有 CLI 一键自初始化、外部原生会话导入标为待实现。裸 Worker 强杀后的账本恢复没有被画成孤儿工具进程自动清理。

## 检查与原始记录

- [浏览器工具原始返回](browser-records.json)：初次布局、最终全视图检查、受影响三图复验、控件/键盘/跳转、SVG、移动离线、console。
- [实际执行的浏览器布局检查函数](browser-layout-check.js)：从页面实际 SVG 文本几何检测溢出和节点碰撞，逐一点击节点读取详情。该脚本仅操作离线文档页面。
- `python3 docs/design/render_architecture.py --reading-dir /home/sky/Documents/ChatGPT/OpenAgentX`：生成成功，29 处本地引用的文件/行号、节点/边与跳转目标存在性检查通过。
- 提取 HTML 内联 JavaScript 后执行 `node --check`：通过。检查 HTML 无外部资源标签；Markdown 本地链接可解析。
- 再次生成的文件哈希不变；`git diff --check` 通过；冻结 ADR、AGENTS 和产品代码均无修改。

浏览器截图：

| 图 | 截图 |
|---|---|
| 完整桌面页面 | [desktop-full.png](desktop-full.png) |
| 01 总览 | [overview.png](overview.png) |
| 02 工作流 | [work.png](work.png) |
| 03 领域初始化 | [init.png](init.png) |
| 04 运行与恢复 | [runtime.png](runtime.png) |
| 05 观察与重连 | [observe.png](observe.png) |
| 06 外部依赖 | [dependencies.png](dependencies.png) |
| 390px 移动模拟、离线 | [mobile-390.png](mobile-390.png) |

文件完整性见 [SHA256SUMS](SHA256SUMS)。证据复用边界：布局微调后只复验受影响视图；未宣称真实手机、所有浏览器、打印版或所有系统字体均已验证。图册中的产品结论继续引用原 AGY 覆盖矩阵和真实执行证据。

## 下一步

1. 从总览选择关心的模块，再展开子图和节点来源；先处理已标明的日用缺口。
2. 产品逻辑或证据变化时修改 `architecture-map.json` 并重新生成两种格式，避免图文与实现脱节。
