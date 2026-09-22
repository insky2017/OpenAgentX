# 05 网络配置与应用闭环评估

- 基线：固定源码 `34053c0`，与已安装 Go `6d599ac` 的 Go 源码一致；以下源码路径及行号以固定副本为准。
- 范围：Web 配置、NetworkWorkflow、版本/测试/发布/绑定、Worker 应用回执、Runtime 网络物化、SecretStore。
- 证据：本批现有测试、Web/Console 动态首次使用流程、真实 AGY 的隔离网络配置；没有修改正式代理设置或重启正式 Worker。

## 结论

- 配置版本、运行快照、应用回执、旧代拒绝和秘密投影已有较完整的内部实现，值得保留。它解决的是“哪项配置由谁真正应用”，不是单纯保存一份代理地址。
- 用户路径没有跟上这套控制机制。新 Agent 看似在线、允许提交任务，却可能因为未建立当前代网络绑定一直排队；Web/Fleet/Console 之间缺少清楚的首次引导和阻塞原因。
- “测试连接成功”只证明配置处理、代理协议和 CLI 健康检查等已执行层。`network_effect`、`model_call` 可以仍为 `not_verified`；页面却用“确认通畅”“测试成功，可保存并应用”等宽泛文字，容易让用户理解成模型已可调用。
- 普通重启后需重新测试和应用，是现有代际隔离的真实成本。应先提供明确解释及一次完成的恢复入口；ADR-007 尚处于 proposed，不能为方便直接取消 fencing。
- SecretStore 有严格文件权限、版本与引用保护，但磁盘 payload 未加密，与 ADR-004 的文字承诺不一致。需要裁决实际威胁模型和承诺，不能仅为补一句文字再造一套密钥平台。

## 承诺与实现

| 用户/系统要求 | 当前实现与证据 | 判断 |
|---|---|---|
| 设置简单，一次保存后可用 | 前端折叠草稿/发布步骤，但仍要选择 Runtime 代次、测试、保存、等待应用；首次任务无前置引导 | 部分实现，主流程衔接不足 |
| 测试配置不影响正在执行的任务 | Probe clone 与运行网络快照分离；Repository 测试检查旧 Run 快照不变 | 内部实现较好 |
| 发布不等于应用成功 | desired/applied、revision、instance/generation 分开；pending 阻止新运行，旧 applied 事实保留 | 实现与测试较好 |
| 重放与并发变更可控 | expected revision、幂等、测试目标匹配、事务内发布/绑定 | 现有隔离测试通过 |
| “连通”证明真实模型可用 | Endpoint 只测代理协议，Health 只调 `--version`；模型调用未验证 | 不满足宽泛用户预期，需收紧文案或补有界诊断 |
| 重启后可理解地恢复 | 新 generation 必须重测/应用；用户看到 unavailable/queued | 安全边界合理，恢复体验不足 |
| 秘密不向读模型泄漏且加密存储 | 文件权限/引用/脱敏存在；payload 原样写入文件 | 脱敏有实现，加密承诺未兑现 |

## 实现较好的部分

- `internal/persistence/sqlite/network_mode_workflow_test.go:153` 验证测试不改 binding；`:173` 验证 pending 切换保留旧应用事实；`:177` 验证 pending 不可调度；`:244` 验证失败不覆盖已知应用事实。这些并非无意义的状态机复杂度。
- `internal/worker/backend_pool.go:141` 通过独立 clone 执行 probe；运行读取固定 policy/revision/RuntimeIdentity。配置变化不静默漂移进活动 Run。
- SecretStore 使用不跟随软链接的目录/文件操作、`0700/0600`、immutable version、HMAC 指纹、同步落盘、引用核对和孤儿清理。HMAC 在这里保护指纹比较，不等于 payload 加密。
- Web 模块本次确实走过“测试→保存应用→当前代 applied→任务开始执行”，Runtime 模块也通过正式 NetworkWorkflow 为真实 AGY 准备 inherit；不是只靠仓库中测试名称判断可用。

## 主要问题与最小处置

### NET-01：网络就绪缺口在提交任务后才显现

- **触发与后果：** 新 Worker 注册或代际变化，当前代尚未有有效 binding。Console/Fleet 主流程仍允许 dispatch，Task 留在 queued；Web Agent 表示 online 并不代表可执行。用户需要跨界面排查。
- **证据强度：** Console 真实 TUI 和 Web 真实 Chrome 均复现；分别见 [04](04-console-fleet-and-onboarding.md)、[03](03-web-and-pwa.md)，证据 `console-onboarding-no-network.txt`、`console-no-network-ui-r2.txt`、`web-02-first-send.txt`。这是同一跨模块问题，不重复计算多个后端缺陷。
- **最小处置：** 在权威读模型提供可执行性与明确阻塞原因，入口显示“网络尚未应用—去设置”；如允许排队，提交前说明并在任务中保留可行动提示。首次引导完成一次“选择网络方式→测试/应用→就绪”，不要求用户理解 generation。
- **验收：** 新临时用户/Agent 从零启动，不依赖测试脚本偷偷 publish；未就绪能看懂原因，按页面操作后原 queued 任务得到执行，下一任务也能执行。
- **主代理裁决：** 下一批核心流程优先项；本批只完成有界核实，不改变网络权限。

### NET-02：探测结果的用户语义大于实际验证范围

- `internal/runtime/network/prober.go:21` 明确只检验代理协议：SOCKS 协商/认证不打开外部目标；HTTP OPTIONS 合法响应不证明 CONNECT/目标可达。
- `internal/runtime/agy/adapter.go:185`、`internal/runtime/codebuddy/adapter.go:160` 的 Health 运行 `--version`。`backend_pool.go:208` 初始化七层 `not_verified`，`:175-178` 在 RuntimeHealth 后就返回 succeeded，未执行 model_call/network_effect。
- 固定 Web `web/src/NetworkSettings.jsx:451` 写“确认通畅后再保存应用”，`:698` 写“测试成功，可保存并应用”。这并不是后端伪造 model_call=passed，而是 UI 把有限探测压成过宽的结论。
- **最小处置：** 显示“配置检查通过，模型调用未验证”，保留具体分层事实。若真实模型可用性是当前必须回答的问题，提供一次有预算、低副作用、用户可见的诊断任务；不要让每次 heartbeat 触发模型计费。
- **验收：** CLI `--version` 正常但模型不可达时，页面不得显示完整可用；真实任务失败能指出阶段并保留诊断。仅给按钮改名不能替代任务失败反馈。
- **裁决：** 与 NET-01 同批处理读模型/文案；完整代理矩阵另按实际支持后端评估。

### NET-03：正常维护的恢复成本过高，缺少产品化恢复入口

- **源码与决策确认：** ADR-007 明确旧 binding 不能直接跨 generation，当前尚未接受；用户面对新代 Worker 时须重新验证。此次没有为了复现重启正式服务。
- **不做的后果：** Worker“在线”却不能执行，用户容易重发任务、重建 Worker或怀疑模型；隐性队列随后可能一起运行。
- **最小处置：** 保留 generation fencing；将“当前配置需在新 Worker 重新应用”变成一个明确恢复动作，后台继续使用既有 test/publish/ack 协议。是否允许低风险继承必须在 ADR-007 中另行裁决，而非此次评估默认同意。
- **验收：** 在隔离 Worker 重启后，读模型展示失效原因，用户一次流程恢复，旧代回执不能使新代变 ready，旧 Run 快照保持不变。
- **裁决：** 恢复入口属于近期体验收敛；自动继承在实际重启频率和可信身份明确后再评估。

### NET-04：SecretStore 的实现与“加密存储”承诺不一致

- `docs/decisions/ADR-004-streamlined-command-center-network-ux.md:77` 要求加密；`internal/network/secretstore/file_store.go:108` 直接 `tmp.Write(payload)`，`:162` 原样读取。此结论来自源码，不读取任何生产 secret。
- **影响：** 当前保护依赖 OS 用户权限和存储介质；能读到这些文件的主体可以获得明文。没有证据表明现场发生泄露，也不把它夸成公开 API 泄漏。
- **最小处置：** 明确本机同用户/备份/磁盘威胁边界。若现阶段选择权限保护，需要正式修正承诺与备份规则；若确需静态加密，采用现成 OS keyring/受控密钥边界并说明恢复方式，不把密钥与密文同目录后声称已解决。
- **验收：** 选定边界后证明磁盘/备份行为与说明一致；继续测试普通读模型和日志不含秘密、失败清理不删在用版本。
- **裁决：** 契约差异必须记录；是否实施加密取决于支持环境，不能挤占已证实的取消/结果/首次使用缺陷修复。

## 复杂度取舍

| 保留 | 简化或隐藏 | 暂缓 |
|---|---|---|
| 版本化配置、原子发布、应用回执、Run 快照、代际隔离、secret 投影 | 普通用户不必逐个理解 profile head/content version/test/binding/application；高级诊断再展开 | 多机自动继承、策略 DSL、跨 Runtime 全网络矩阵、独立密钥管理平台 |
| 同一份后台就绪判断 | Web/Console 共用原因码及行动提示，避免各算“在线/可用” | 未贯通前扩展更多 Runtime 网络模式 |

复杂度主要过度暴露在用户路径和文档中。直接删数据库版本或 fencing 会损伤已实现的保护；更小的改法是把普通路径折叠成“选网络方式—确认可执行”，让协议细节留在诊断层。

## 测试边界与证据

- 主代理执行 `go test -count=1 ./internal/network/... ./internal/transport/... ./internal/safeoutput/... ./internal/testkit/... ./internal/cli/admin/... ./cmd/openagentx`：7 个 package，通过，2.485 秒；日志 [deployment-network-tests.log](evidence/deployment-network-tests.log)。其中本模块直接对应 SecretStore，其余用于部署/传输审查。
- NetworkWorkflow/持久化 race 测试由控制面模块统一执行；BackendPool/Runtime network 测试由 Runtime 模块执行，避免重复跑同一长测试。对应日志 `core-package-tests.log`、`runtime-package-tests.txt`、`runtime-race-tests.txt`。
- Web 的 fake Runtime 网络链与 AGY 隔离链分别提供交互和实际运行证据；不能由此推断所有代理协议、远程 Worker、凭据轮换及跨代恢复已获 E2E 通过。
- 未覆盖生产代理变更、真实 HTTP/SOCKS 全组合、生产重启、Secret 轮换时崩溃、远程跨宿主恢复。没有修复 commit 或部署。

## 下一步

1. 先把当前代网络就绪与阻塞原因接入 Web/Console 的任务入口和任务详情，完成从零上手的真实 E2E。
2. 收紧“测试成功”文案并补最小真实诊断；继续保留未验证层，避免把配置检查等同模型可用。
3. 再依据实际维护频率评估一次恢复入口、ADR-007 连续性和 Secret 存储承诺，避免展开新平台工程。
