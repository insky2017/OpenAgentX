# Codex 工作流端到端验收

每行以实际命令、版本、时间、API Task/Run/Journal、Runtime 日志和独立效果核验支撑。D=确定性契约，R=真实 Codex，I=安装及用户入口；三者不能替代。原始资料放 ~/.local/state/openagentx/evidence，脱敏副本入当批 evidence。首次失败保留，复验另存。没有证据只能标记未验或受阻。

| ID | 用户场景及不变量 | 要求证据 |
|---|---|---|
| C01 | 固定版本/schema；真实 query；两个客户端可观察同 thread | help/schema/RPC/版本，真实 final |
| C02 | add/join 幂等；职责/cwd/交接生效；prepare 不启动接单 | CLI、配置摘要、服务状态、真实职责结果 |
| C03 | 默认代理作用于 Worker/app-server；重启保留；NO_PROXY 保持本机可用；坏代理明确失败可修复 | 脱敏环境清单、进程来源、网络探测、真实运行 |
| C04 | query 完整结果；mutation 独立文件内容/hash正确 | Task/Run/Journal、RPC、文件独立读/hash |
| C05 | 第一轮结束空闲，再自动接第二/第三工作，无终端输入注入 | 三轮账本、worker心跳/日志、不同任务和turn对应 |
| C06 | 同Task补充/显式原生续接；并发native与bus输入有界仲裁 | 输入来源、Task/Run/thread/turn映射、竞态证据 |
| C07 | 终端实时展示、关闭不中断、重连历史与进度 | PTY操作与transcript、API状态、原生产物 |
| C08 | cancel/timeout/approval；只影响目标turn；子进程停止 | 控制事件、真实长任务、独立PID/文件采样、其它会话存活 |
| C09 | Worker/app-server故障、早期绑定、迟到事件、未知效果不盲重放 | 进程故障记录、恢复API/Journal与独立效果 |
| C10 | 已工作Agent准备交接→完成当前轮→重连同thread→ready接单 | 旧/新thread证据、CLI receipt、服务状态和新任务 |
| C11 | Console结果/观察/错误可理解；桌面/窄屏及断线恢复 | 真实浏览器、DOM/截图、SSE游标与API |
| C12 | 准确安装来源；AGY相关回归；新Codex Agent日用 | commit/build SHA、systemd来源、正式路径R/I、go race/vet/Web必要检查 |

禁止用mock、单独exit0、截图或测试结论JSON替代真实端到端。协议探索失败不自动阻止其它独立实现，但不能以未经验证的协议语义发布。

## 2026-10-05 接入 Skill 批次

仅覆盖接入准备和安装发现，不重开全部 C01–C12，也不将本批成功扩大成新领域已能自动协作。

| ID | 本批行为 | 实际证据与判定 |
|---|---|---|
| S01 | 项目/用户软链安装，重复安装不变，冲突不覆盖 | 真实文件类型/目标/内容哈希；真实 Codex `skills/list` 的路径、enabled；缺失或冲突应非零 |
| S02 | 模型从可见 Skill 收集四项资料，helper 调用已安装 OAX 准备 | 新建隔离 Codex 会话的 JSONL、实际工具命令、生成 ROLE/HANDOFF、原 CLI receipt、disabled Fleet；无数据库、socket或Worker启动 |
| S03 | 自动识别当前 thread 与后续 profile 连续性 | CLI thread.started 与 helper 来源及 receipt 一致；生成 status 命令在同隔离 profile 返回 prepared；没有绑定到测试发起者的旧 thread |
| S04 | 明确失败与幂等 | 缺 ID、来源冲突、非法 brief、目标配置冲突保留失败日志；相同 brief 重放产物哈希不变；明确 summary 模式不传 thread |
| S05 | Skill 可调用与范围可理解 | 独立 astra/high 审阅和真实模型执行；区分 linked/discovered/enabled/prepared，不启动当前会话的 resume，不发送协作消息 |

使用实际安装的 OAX/Codex 与隔离目录；不使用 fake/mock 结果替代行为。原始证据持久保存在 `~/.local/state/openagentx/validation/2026-10-05-join-skill/`，筛选无秘密的副本和 SHA-256 清单写入本批报告。若真实模型不可用，保留失败并明确 S02/S03 未通过，不以脚本集成检查顶替。
