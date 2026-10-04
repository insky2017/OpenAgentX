# 接入 Skill 标准化交付

本批完成 `openagentx-join` 的标准安装、实际发现自检和结构化离线准备。**真实模型读取 Skill → 自动捕获自身 thread → 生成交接 → 已安装 OAX CLI 准备回执**已通过；本批不包含后台接管、writer 释放或新身份的 managed 自动协作。

## 已交付

- [安装与自检脚本](../../../../scripts/oax-skill.py)：用户 `~/.agents/skills` 或工程 `.agents/skills` 软链；重复同源安装不变，冲突不覆盖。`check` 用独立 Codex app-server 的 `skills/list` 核对实际路径与 enabled，不创建 thread/turn、不调用模型。
- [接入 Skill](../../../../skills/openagentx-join/SKILL.md)和[prepare helper](../../../../skills/openagentx-join/scripts/prepare.py)：收集稳定名字、目录、职责、协作对象；模型写一份 brief，helper 生成标准 ROLE/HANDOFF、来源、CLI receipt 及保持同 profile 的后续命令。
- 当前 thread 来自实际 `CODEX_THREAD_ID` / `CODEX_SESSION_ID` 或明确传入 ID，必须有效且一致。缺失/冲突失败；只有明确 `history=summary` 才省略原历史。不会扫描最近会话或猜测绑定。
- 私密产物存入 OAX `workers/joins/<id>.intake/`，不改业务工作树。协作对象只是意向；准备结果明确为 `local_prepared / ready=false`，没有自动 resume、发信或改变权限。
- 已更新[使用指南](../../../operations/codex-agent-entry.md)与[接入 HTML](../../../design/codex-session-join.html)。本批不新增单元测试；功能检查使用真实 CLI/文件和真实 Codex 模型。

## 本机安装与最短使用

已建立用户级软链 `/home/sky/.agents/skills/openagentx-join`，指向本仓库唯一 Skill 源；实际 Codex 0.160.0 返回目标路径 `visible=true / enabled=true`。见[安装实录](evidence/installed-00.json)及[实际发现](evidence/installed-01.json)。这证明新进程可见，不能替代已运行 turn 的上下文加载证明。

在目标 Codex 中使用 `$openagentx-join`，说明名字、目录、职责和协作对象即可；已有上下文中的资料不必再重复填写。Skill 会生成 brief 并调用 helper。部署到另一工程或机器时：

```sh
python3 scripts/oax-skill.py install --scope user
python3 scripts/oax-skill.py check --project /absolute/project
```

仅工程内安装可选 `install --scope project --project /absolute/project`。通常选择一种范围。软链依赖来源仓库位置；切换来源或发现失败时先自检，不覆盖已有不同来源。

## 验收结论与证据

- **真实准备链路 PASS**：独立 `gpt-6-astra / high` Codex 会话 `01a10843-7322-70a1-81a8-de6237f8949c` 读取 Skill、生成 brief、执行 helper。brief 未手填 thread，helper 的实际环境来源与 CLI `thread.started` 完全一致；对端意向 `pay-domain` 被保存，未发送消息。
- 原 CLI receipt 为 `local_prepared / ready=false`；生成的同 profile status 也确认 prepared；Fleet disabled，隔离 profile 没有数据库或 socket，workspace 文件摘要不变。见[真实执行日志](evidence/model02/codex.public.jsonl)、[独立断言](evidence/model02/verdict.json)、[原回执](evidence/model02/cli-receipt.json)、[同 profile 状态](evidence/model02/prepared-status.json)、[标准交接](evidence/model02/HANDOFF.md)。这些是本次真实准备过程，不是模拟结果。
- **独立审阅与集成检查 PASS**：安装、发现、重放、冲突、明确 summary、profile 优先级及已有身份冲突均使用真实已安装 CLI/文件检查；无实证阻断。其一致测试 UUID 只证明集成分支，真实当前 thread 由上述模型批次另行证明。见[独立报告](evidence/independent-review/review.json)及[覆盖边界](COVERAGE.md)。
- **首次失败保留**：model01 的 workspace-write 执行被系统 `bwrap: No permissions to create new namespace` 阻断，helper 未运行，判定 FAILED。按本机已授权的执行模式改用 danger-full-access，保持相同隔离目录约束和实际产物检查，model02 复验通过；未改变系统沙箱或全局配置。见[首次日志](evidence/model01/codex.public.jsonl)、[首次判定](evidence/model01/verdict.json)。
- **页面验证 PASS**：本批 HTTP 预览被 Chrome 返回 `ERR_ACCESS_DENIED`，保留首次失败；改为在实际 Chrome 中加载同一独立 HTML 内容，核验更新文字、场景切换、复制/选中降级、窄屏无横向页面溢出。不是产品 E2E。见[离线载入记录](evidence/browser-offline-load.json)、[浏览器检查](evidence/browser.json)、[桌面](evidence/skill-page-desktop.png)及[窄屏局部](evidence/skill-page-mobile.png)。

## 来源与限制

本批源码基线 `33ab29c`，最终脚本与文档由本批 Git 提交和[源文件/检查记录](evidence/final-checks.json)的 SHA-256 定位。核心已安装 OAX 工件仍为 `886ba7f`、SHA-256 `15fd8699c2f07af80966dc3edc355ee90f260e109c9715f402aedd16b90d97ca`；本批没有编译、替换或重启守护进程/Worker。冻结 ADR 未改，默认 Fleet 未改变，当前 openagentx 身份及 Rhythm/Pay 没有重新接管。

原始证据保存在 `/home/sky/.local/state/openagentx/validation/2026-10-05-join-skill/`。入库副本排除私密 profile 的 `.env`；模型日志中的无关记忆输出与推理文本做明确删隐，工具命令、实际结果、回执和产物保留。见[删隐说明](evidence/redactions.json)和[SHA-256 清单](SHA256SUMS)。

后续仍需单独实施/验收管理侧激活流水线：原宿主释放、定向启动、职责目录、双方 peer、固定终端、真实咨询往返。已接入会话应先 status，不再次 join；当前会话缺失资料时的自然语言补问质量没有在本批穷举。已知 prepare→resume 的 Fleet 提前 enabled 和 Desktop writer 生命周期问题未在本批改动，触发完整自动激活工作时一起核对。
