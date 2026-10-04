# 新 Codex 会话接入：只读核查与图页

2026-10-04，针对“新的 Codex session 怎样快速加入 OAX、接入 Skill 是否已实现”核查当前命令、源码、Skill 发现目录及官方 Skill 说明，新增[接入图页](../../../design/codex-session-join.html)。本轮仅修改文档与导航；未登记身份、启用 Worker、切换会话、安装 Skill、修改产品代码或发出协作消息。

## 结论

- `openagentx-join` 已在仓库实现为薄 Skill，调用默认离线的 `agent join --prepare`；准备资料不代表已建立运行会话或自动协作。本机已检查目录及当前会话可见列表中未发现该 Skill。
- 本机已有 `oax-collaborate`，用于 external 原宿主手动协议，不是 managed 接入流程。其 heartbeat 文字仍与当前暂停/NOT_PASSED 基线不一致，不能按其字面启用周期性模型轮询。
- 全新工作最短路径为 `agent add --runtime codex --no-open` → `agent open <id> --native` → 管理侧职责目录/双方 peer/managed enable → 真实咨询往返。add 默认会启动后台，无需重复 resume；首次 native 打开会自动执行模型初始化 query，不是只读查看。
- 已有 CLI 先 prepare，结束并退出旧入口后由管理侧 resume/open。已有 Desktop 还需实际核验 writer 释放；idle/停止 turn 不足。没有可信 thread ID 时只能交接职责与摘要，不能宣称完整原历史迁入。
- 建议补齐现有 join Skill 的发现、分段接入指引、管理侧启用与接通回执，复用当前后台。此建议未在本轮实施。

## 核查基线及证据

源码 `main@7bc36c8431f49c087d0819abffd662f22985deb7`。当前二进制 `go version -m` 返回 `886ba7fd255a5f6632ee550d4bdcc786274f48fa`，`vcs.modified=false`；SHA-256 为 `15fd8699c2f07af80966dc3edc355ee90f260e109c9715f402aedd16b90d97ca`，与既有交付一致。schema v5 来自[当前切换交付](../2026-10-03-rhythm-pay-managed/DELIVERY.md)，本轮不读写产品数据库、不重跑产品验收。

- [只读 CLI 实录](readonly-cli.json)：已安装 add/join/resume/collaborate help 与构建元数据。最初尝试的 `version` / `--version` 返回 Unknown command，完整保留；随后以实际构建信息核验版本。
- [Skill 发现范围](skill-discovery.json)：用户 `~/.agents/skills`、当前环境的 `~/.codex/skills`、管理员 `/etc/codex/skills`，以及正式工程和阅读目录的 `.agents/skills` 祖先路径。只据此判断本轮可见性，不推断其它未检查项目或插件。
- [官方文档摘录](official-skills-excerpt.txt)：实际获取 `https://developers.openai.com/codex/skills/` 后提取。官方说明用户/工程 `.agents/skills` 与符号链接发现；当前宿主另外列出既有 `.codex/skills/oax-collaborate`。目录存在、当前可见、实际读入使用分别判断。
- 源码：`internal/cli/fleet/agent.go:93`（prepare 退出）、`:110`（add 接续 resume）、`:274`（默认 runtime 与 prepare）；`agent_join.go:154`（local_prepared/ready=false）。
- 源码：`internal/nativebridge/bridge.go:145`（首次真实 query 初始化）；`internal/runtime/codex/adapter.go:550`（thread 选择）、`:607`（session.bound）。
- 源码：`internal/persistence/sqlite/managed_collaboration_repository.go:23`（enable 要求真实绑定）；`external_session_repository.go:138`（external 迁入的撤销、代次、待办及原 thread 条件）。

独立子代理使用 `gpt-6-astra / high`，只读审核命令路径、Skill 边界与新 HTML；主代理负责成稿及浏览器检查。审阅发现并修正了首次 native 自动初始化说明、已有 external 身份迁入前置条件，以及 roles 示例必须带组织参数的问题。

## 发现的已有文档/实现差异

这些是接入体验后续应处理的项，未在本次分析中修改现有产品逻辑或已安装 Skill：

1. `docs/operations/codex-agent-entry.md:40` 的“readiness 失败保持 Fleet 禁用”对 prepare→resume 表述过强。`registerPreparedAgent` 在 `prepareOnly=false` 下复用 `addAgent`，`agent.go:543` 会较早写 `Enabled:true`；新图页以真实 status、Task、SessionBinding 判定接通，不承诺该禁用不变量。
2. `docs/operations/managed-collaboration.md:14` 的“完成一次对话”可简化为等待原生入口自动初始化成功；全量 `fleet up` 不是新建身份最短路径，可能启动其它已启用身份。
3. `skills/oax-collaborate/SKILL.md:40` 保留 Desktop heartbeat 措辞；仓库和当前已安装副本相同。既有 heartbeat 验证未通过，不能当成可用事件入口。
4. join Skill 尚未覆盖职责目录、双方 peer、managed enable、固定窗口和真实往返。将其装进发现目录只改善可调用性，不会自动补齐这些能力。

## 文档 UI 验证

使用系统 Google Chrome 和独立 `agent-browser --session oax-join-docs --executable-path /usr/bin/google-chrome`，通过仅监听 `127.0.0.1:18765` 的临时 HTTP 服务打开图页。网页无外部资源依赖，无产品 API 请求；这是文档浏览器验证，不是新身份 E2E。

- [浏览器命令与断言](browser-checks.json)：三种场景切换、键盘左右切换、交接指令复制或选中文字降级、四种架构路径、桌面/390px 窄屏溢出及脚本错误。
- [桌面首屏](desktop.png) · [架构关系图](architecture.png) · [窄屏首屏](mobile.png) · [窄屏接入路线](mobile-route.png)。
- [静态核对](static-checks.json)：ID 唯一、页面引用存在、内嵌 JavaScript 语法及源码 SHA-256；[工件清单](SHA256SUMS)记录本批工件摘要。

本轮没有新接入对象，未执行注册/模型初始化/协作 E2E。既有 Rhythm/Pay 自动往返证据仍只证明其原身份与当批场景；新身份接入需单独保留其消息、Task/Run/Journal、模型过程和实际答复。
