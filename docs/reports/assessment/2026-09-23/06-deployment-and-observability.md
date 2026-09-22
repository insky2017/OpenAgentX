# 06 部署、可观测性与维护成本评估

- 日期：2026-09-23；源码基线 `34053c0`，本批主线 `e8d7ea9`。
- 范围：当前进程和安装产物、部署入口、文档可执行性、schema 识别、测试入口与维护结构。
- 操作边界：只读正式服务状态/产物；cutover 只执行 dry-run。没有重启正式 daemon/Worker，没有执行部署或修改正式数据库。

## 结论

- 当前两个正式进程确实来自同一个可追溯安装二进制，服务 active/running，健康接口正常。这能证明产物来源与服务存活，不能证明用户任务完成。
- 代码、安装产物、Web、主线和工作树的版本分散，给评估与维护增加了显著成本。安装在 ADR-009 旁支，主线仍不是实际产品；ADR-006 另有正在实施的旁支。完成度应以部署清单为准，不能拿主线 README 或 ADR 状态替代。
- 文档里有不可执行命令和已经不对应当前部署的脚本：README 指向不存在的 `check_docs.py`；旧 cutover 演练输出系统级目录和 systemctl，而当前为用户级安装。
- 观察数据多，但面向用户的解释仍不足：online、Runtime healthy、network applied、Task outcome 是不同事实，目前没有一致地转为“为什么没开始/接下来做什么”。健康接口 `ok` 无法弥补这点。
- 不建议先做服务拆分、数据库替换或全仓重构。优先建立单一发布清单、短而可执行的安装/恢复文档和少量真实用户验收，再按反复出错的边界拆小文件。

## 现场核对

| 对象 | 本次实测 | 能证明与不能证明 |
|---|---|---|
| `openagentx.service` | PID `1686100`，active/running，NRestarts=0 | 服务存活；不是任务效果验收 |
| `openagentx-worker@quote-service.service` | PID `1687781`，active/running，NRestarts=0 | Worker进程存活；可执行性另看当前网络/身份/能力 |
| 安装程序与两进程 `/proc/<pid>/exe` | SHA-256 三者一致：`a26ebf4d84ede2fa60bd4c10aaee704056732dde9dc532cd424d85de76703e89` | 排除盘上已换而进程仍旧的歧义 |
| Go build metadata | revision `6d599aca8ce7c32fe11f23244478b495a0a78e68`，modified=false | 可追溯到提交 |
| Go 测试基线 | `34053c0` 对 `6d599ac` 的 Go 源码/go.mod/go.sum diff 为空 | 本批 Go 测试适用于同一 Go 实现；不等于重新发布 |
| 已安装 Web | `ee46038102ebd44d387e26ab912001f26b89ade5`，本批记录实际资产 hash | 与此前架构快照/安装记录衔接；前端真实行为由03报告验证 |
| 健康路由 | `GET /api/observe/v1/health` HTTP200，`status=ok` | HTTP handler 可响应；没有模型调用和任务结果承诺 |

证据：[deployment-current.json](evidence/deployment-current.json)。当前地址为 loopback `127.0.0.1:18100`；模板中默认 `0.0.0.0:18100` 不代表现场公网暴露。

## 做得好的部分

- 用户级 systemd 托管与 Console/tmux 生命周期分离；用户退出观察窗口不必停止真实 Worker。隔离 Console 流程已验证退出后同一 Worker 能领取下一项任务。
- 版本来源可通过 Go build metadata、安装 hash、进程 hash 交叉核验，历史发布记录也保留了 source/installed 的区分。
- 日志和读模型有安全投影，Event Journal、RunAttempt、SessionBinding 形成诊断基础。问题更多是如何选对当前结果、如何给用户解释，而不是缺一个新的日志平台。
- 已有 `go test`、race、前端单元检查、真实运行与浏览器脚本资产；本次新增缺陷能在隔离环境复现，无需修改生产数据库。

## 缺陷与维护风险

### OPS-01：主线、安装和工作树没有一个简短的发布事实入口

- **触发：** 新维护者照 main 文档检查，而进程实际上运行旁支；或者拿当前 ADR-006 开发进度回答线上是否可用。
- **后果：** 读到旧 UI/契约，误报已实现/未实现，验证了一个源码却操作另一个安装；本批必须额外固定 `34053c0` 并证明 Go diff 为空。
- **最小处置：** 每次发布生成一份机器可读清单：Go commit/hash、Web commit/asset hash、配置标识、schema/feature revision、服务单元、已支持入口、未通过的用户验收；安装完成后读进程/资产核对。不需要为此建立通用发布平台。
- **验收：** 从单一入口能确认现在用哪个版本、用户能做什么、怎样回滚；变更后的真实服务与清单一致。
- **裁决：** 下一次发布必做；本批只记录事实，不合并活跃工作树。

### OPS-02：文档检查命令缺失，cutover 演练仍指向旧部署

- **本次动态复现：** 固定源码执行 README 的 `python3 scripts/check_docs.py`，exit2，文件不存在。
- 用评估二进制准备临时 release 目录，执行 `deploy/scripts/rehearse-cutover.sh --dry-run`，exit0，却仍输出 `/var/lib/openagentx`、`/run/openagentx`、不带 `--user` 的 `systemctl` 和 nginx reload。没有实际执行这些写操作。
- **根因：** `README.md:143` 残留旧入口；cutover 脚本 `:14`、`:36-44` 固化系统级部署；当前已转用户级 systemd。若系统级模式仍支持，应明确它是另一种部署，而不是默认恢复流程。
- **最小处置：** 删除/更正失效命令；安装指南保留一条当前推荐路径，将系统级 runbook 明确归档或标为另一个受测模式。将一次性执行日志移到历史报告，不挤进长期操作步骤。
- **验收：** 在隔离用户环境依指南从零安装、配置、发送真实任务、重启恢复；dry-run 明确显示目标实例/目录/服务层级，并与当前安装相符。
- 证据：[deployment-document-probes.json](evidence/deployment-document-probes.json)。文档问题并不构成此次去执行真实 cutover 的授权。

### OPS-03：单一 schema version 不足以说明兼容扩展与回滚边界

- **源码事实：** `internal/persistence/sqlite/migrations/migrations.go:15` 的 CurrentVersion 仍为1，文件中通过兼容逻辑新增网络/CLI等表和字段；基础版本不能独立说明一份DB包含哪批特性。
- **影响：** 只记录 schema=1 无法证明任意旧二进制均可回滚读取当前数据。此次未发现数据损坏，也未进行生产降级实验。
- **最小处置：** 发布记录兼容特性/迁移版本与回滚支持范围；下次改变持久语义时用明确迁移序列和真实旧库升级/受支持降级验证，不立即重写数据库层。
- **验收：** 已支持旧库可升级且原数据不丢；不支持回滚时部署前明确拒绝，而非让服务在运行后异常。
- **裁决：** 下一次schema/持久语义变更触发，当前列为条件性维护风险。

### OPS-04：内部测试丰富，少量关键用户流程缺少稳定回归入口

- **本次证据：** 多组包/race测试通过，真实浏览器仍发现取消、旧结果、重连和首页状态缺陷；Console集成夹具为成功链人工补网络发布步骤，掩盖新用户实际路径。
- `git ls-files` 对 `.github`、常见统一任务runner及脚本检查没有找到已跟踪的自动CI配置；可确认的是仓库内缺统一入口，不能据此推断外部完全不存在CI。
- **最小处置：** 将“首次上手→真实任务→结果核对→取消/等待输入→下一任务→断线恢复”变成少量可复现验收脚本；明确fake、真实模型、正式发布验收三层边界。CI先跑确定性层，真实provider用专用低副作用工作区和有界预算定期/发布前跑。
- **验收：** 故意恢复本批已复现缺陷时测试能失败；不能只断言HTTP200、截图存在或Turn exit0。
- **裁决：** 与核心修复一起提交必要E2E，不另开大测试框架项目。

### OPS-05：大文件与重复状态推导正在增加变更成本

- 固定源码中 `web/src/main.jsx` 1191行、`NetworkSettings.jsx` 843行、Console `tui.go` 2052行、panel handler 1598行；本批还统计了包的生产/测试体量，见 [architecture-size.json](evidence/architecture-size.json)。行数只提示审查范围，不独立证明设计失败。
- 已证实的关联问题是 Web 首页/详情分别推导当前任务、Run数组排序约定散落、TUI状态层裁切没有可达性验收、正常finish与recovery各自解释取消终态。
- **最小重构：** 优先抽取统一任务详情/就绪读模型、有限终态决策和复用的用户错误映射；再拆任务页面、网络设置、会话连接逻辑。每次以已失败用户用例作验收。
- **不采纳：** 为减少行数机械拆包、引入通用Command Bus、重写成微服务、替换SQLite或改用事件溯源。

## 本次检查与未覆盖

| 检查 | 结果 | 边界 |
|---|---|---|
| `go vet ./...` | exit0 | 静态检查 |
| `scripts/check-legacy-control-paths.sh --release` | exit0 | 路径/源码门禁，不是发布成功 |
| 7个网络/传输/安全输出/testkit/admin/cmd package | 全通过，2.485秒 | 见 `deployment-network-tests.log`；其余模块测试按报告复用 |
| README文档检查命令 | exit2，脚本缺失 | 已复现文档缺陷 |
| cutover dry-run | exit0，但目标为系统级旧路径 | 未执行服务/数据库写操作 |
| 正式服务、安装、进程和HTTP health | 一致且在线 | 没有生产任务变更 |

- 未覆盖远程mTLS的实际跨主机部署、生产备份恢复/降级、OS级故障注入、外部HTTPS入口、连续数日稳定性、完整负载与资源上限。
- 当前“服务运行正常”不能提升为“整体功能完成”或“用户体验可验收”；反过来，局部产品缺陷也不能推断当前服务无法稳定托管Worker。
- 本批没有修复commit或部署；最终报告commit只承载评估文档与脱敏证据。

## 下一步

1. 下一批核心修复配套一条可复现真实用户验收，发布时生成最小版本清单并核对运行产物。
2. 收敛当前用户级安装/恢复指南，移出历史日志、失效命令及未受测的旧默认路径。
3. 在实际代码改动需要时，局部统一结果/就绪/错误读模型与结算规则；不先启动全仓重构。
