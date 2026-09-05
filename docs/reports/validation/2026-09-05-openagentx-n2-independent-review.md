---
doc_type: validation_report
scope: N2-frozen-subset-static-review
status: no-go
owner: independent-review
reviewed_at: 2026-09-05
---

# OpenAgentX N2 冻结子集独立静态审核

## 审核边界

本轮只读审核以下冻结子集，不判定 N2 总 Gate：

- `internal/network/secretstore/file_store.go`
- `internal/network/secretstore/file_store_test.go`
- `internal/runtime/network/materializer.go`
- `internal/runtime/network/materializer_test.go`
- `internal/runtime/network/prober.go`
- `internal/runtime/network/prober_test.go`

未审核仍在变化的 domain、Runner、API、identity、environment 和 main；仅在确认调用关系或外部契约时读取必要背景。未运行 Go 测试、全量、真实应用、网络、浏览器或模型，未修改源码、冻结 ADR 或既有报告。实际审核模型为 `gpt-5.6-sol` medium；用户指定的 Astra 当前不可用，本报告不冒称 Astra 证据。

上述六个文件逐文件 SHA-256 输出再做 SHA-256 的子集指纹为：

```text
68673af5c6a0bf0227062c8f98421c8209e489206cb3352247ae029b0f869164
```

逐文件指纹：

```text
cbfa5ed597f50043efa22df885317c7a343bcb8630fa0ea17c1e3f35a854cba8  internal/network/secretstore/file_store.go
bb5a9792a90ff7e0ce6344beb0ee3c3be06c4a9ff468814abf61518698184ed7  internal/network/secretstore/file_store_test.go
18b8da6a7013dcbde5534d0031f1894372de962dc0dffe01e0e0ce9da00e3276  internal/runtime/network/materializer.go
a57ca3f230281026dc3c35a8a4c6713e80147a85ab77a9c8995a0922f9deafc2  internal/runtime/network/materializer_test.go
49b0fdaaccb98dfb6bb674a379961d4af8fd82722e0e7f4d44f910d7100f0a0c  internal/runtime/network/prober.go
07a05c6cc2ceb8388dab000c2ad97dc381d22955e4a4e921f8062a5f7cc72843  internal/runtime/network/prober_test.go
```

## 验收矩阵

| 类别 | 静态断言 | 当前判定 |
|---|---|---|
| 权限与路径 | 根目录严格 `0700`，密钥、秘密和物化文件严格 `0600`，链接与替换竞态不能绕过检查 | **P1 阻塞** |
| HMAC、幂等与孤立写入 | 可猜秘密不产生裸 SHA oracle；同版本同内容幂等、异内容冲突；并发/崩溃不暴露半写 key，失败写入可安全回收 | **P1 阻塞** |
| 物化与旧 Run | HMAC key 跨重启稳定；旧 Run 固定内容可重验；缺失、篡改或 key 不一致 fail closed | 正常路径成立，故障原子性 **P1 阻塞** |
| 导入 | 来源身份为 keyed HMAC；重复键、非法端口和未知键拒绝；正式 native 配置可再次运行 | 前三项成立，HTTP 正向路径 **P1 阻塞** |
| 协议探测 | SOCKS5 认证与 HTTP 有界响应按实际层级表述；取消/超时有界；诊断不含原文和凭据 | 证明层级受限，取消 **P1 阻塞** |

## P1 发现

### P1-N2-S01：HTTP 物化配置与正式 mgraftcp 契约不兼容

`renderConfig` 对 HTTP 写入 `http_proxy = http://<host>:<port>`（`materializer.go:218-224`）。正式 wrapper 在配置来源生效时不传 `--http_proxy`，而是把该文件作为 `AGY_GRAFT_CONFIG` 交给 `mgraftcp --config`（必要背景：`deploy/agy/agy-graft:242-259,322-329`）。已核对的 native `mgraftcp` 实现把配置值直接交给 `net.ResolveTCPAddr("tcp", httpProxyAddr)`，其示例格式也是 `host:port`，不接受 `http://` 前缀（必要外部契约：`/home/sky/work/graftcp/local/local.go:65-70`、`local/example-graftcp-local.conf:25-26`）。

因此 HTTP endpoint 的本地 `OPTIONS` probe 即使通过，物化后的正式 Runtime 路径仍会在解析 endpoint 时失败。最小修复是按 native 配置格式写 `http_proxy = <host>:<port>`，并增加“物化文件经正式 wrapper/mgraftcp 配置解析”的合同测试；本地 HTTP fixture 不能替代该证据。

### P1-N2-S02：`Lstat` 后按路径读取存在 TOCTOU，链接拒绝可被同 uid Runtime 绕过

secret store 与 materializer 都先 `os.Lstat(path)` 检查非 symlink/权限，再调用 `os.ReadFile(path)`（`file_store.go:194-202`；`materializer.go:300-308`）。检查与打开之间文件可被替换。目录 `0700` 只能隔离其他 uid；受管 Runtime 与 Worker 以同一 OS 用户运行时，不能把该权限当作抵御 Runtime 文件替换的边界。

`NewMaterializer` 还对 `filepath.Abs(strings.TrimSpace(root))` 求路径，却以未 trim 的 `root == ""` 判断空值（`materializer.go:36-46`）；全空白配置在工作目录恰为 `0700` 时可能把工作目录当物化根。

最小修复是保存以 `O_DIRECTORY|O_NOFOLLOW` 打开的根目录 fd，使用相对该 fd 的 `openat/openat2` 加 `O_NOFOLLOW` 打开目标，再对同一 fd `fstat` 并有界读取；写入和 rename 也应绑定该目录 fd。空白 root 必须直接拒绝。测试需在检查与打开间替换文件，而不只测试静态 symlink。

该修复只能关闭路径替换窗口。`0700/0600` 不能完整隔离同 uid 的恶意 Runtime，HMAC 也只能在核验时发现修改，不能阻止核验后到正式读取之间再次修改；本批不据此扩张出新的 OS sandbox，但必须把同 uid 隔离明确保留为部署威胁边界。

### P1-N2-S03：持久 HMAC key 直接创建最终文件，并发与崩溃可暴露半写 key

两处 key 初始化都以 `O_CREATE|O_EXCL` 创建最终路径，再写入、fsync（`file_store.go:157-191`；`materializer.go:134-175`）。第二个并发 Open 在创建者完成写入前会看到零长度或部分文件并失败；进程在创建后、写完前崩溃会留下永久无效 key，使后续启动持续 fail closed，且没有自动恢复边界。

最小修复是先在同目录创建 `0600` 临时文件，完整写入并 fsync，再以 no-replace rename 提交最终 key，随后 fsync 目录；竞争 loser 只能在 winner 原子提交后读取完整 key。测试至少覆盖两个并发 Open 和预置半写最终 key 的确定性处理。

### P1-N2-S04：敏感文件先于数据库事实写入，失败后没有并发安全的孤立回收

secret store 提供 `DeleteOrphan`，但当前没有调用者。必要背景中 `ReplaceSecret` 在数据库命令事务前调用 `PutImmutable`，随后 CAS 或事务失败可留下无引用的密码文件（`network_workflow_service.go:367-386`）。`Materialize` 也先写含凭据的 config，之后 blackip 写入失败时直接返回（`materializer.go:82-91`），留下未返回 policy 引用的敏感配置。

不能在错误路径直接无条件删除，因为并发请求可能已经引用同一确定性版本。最小边界是让创建结果可区分“本次新建/原有幂等命中”，并为元数据失败后的文件提供不可经正常读取入口取得的隔离状态，以及引用感知的清理/重试策略；提交结果不确定时保留并标记待核对，不能猜测为 orphan。物化缓存同样需要对部分成功和旧 Run 保留期给出确定生命周期。

metadata service 尚未冻结，本轮只确认当前冻结子集及必要调用背景中没有看到上述完整策略，不宣称已经完成对 metadata 事务实现的审查。

### P1-N2-S05：连接建立后的协议 I/O 不响应父 context 的即时取消

`ProbeEndpoint` 的 `DialContext` 可响应取消，但连接建立后只把创建时的最多 5 秒 deadline 写入 socket（`prober.go:22-32`）。父 context 在 `ReadFull` 或 `ReadSlice` 阻塞期间取消，不会关闭连接或提前 deadline，调用仍可阻塞到原 deadline。现有测试只有连接拒绝，没有“accept 后不响应 + 中途 cancel”的路径（`prober_test.go:102-114`）。

最小修复是在连接建立后让 context 完成触发连接关闭或立即 deadline，并等待清理路径结束；增加 SOCKS greeting、SOCKS auth 和 HTTP status-line 三处 stall/cancel 用例，断言及时返回且 server/client goroutine 不遗留。当前固定诊断没有拼接原始网络错误或凭据，静态范围内未发现诊断原文泄露。

## 已确认边界与 P2 台账

- secret fingerprint、物化 digest、导入 secret fingerprint 和 source identity 都使用持久随机 key 的 HMAC；当前不再把可猜密码的裸 SHA 写入持久身份（`file_store.go:124-127`；`materializer.go:93-94,196-207,360-371`）。
- `ImportConfig` 已拒绝重复键，端口通过 `strconv.Atoi` 全串解析并限制为 `1..65535`，`1080junk` 不能通过（`materializer.go:319-355`；`materializer_test.go:120-139`）。但它仍允许与 mode 不匹配的额外 endpoint key 并静默忽略，建议作为 P2 收紧为每个 mode 的精确键集合。
- materializer HMAC key 在正常重启后可重用，`PrepareRun` 对缺文件、内容篡改和 digest 不匹配 fail closed；现有测试覆盖 config 篡改，未覆盖 blackip 篡改、key 被替换和旧 Run/新 profile 并存。
- SOCKS5 实现完成 method negotiation 和 RFC 1929 用户名/密码交换，但成功 fixture 没有断言 greeting/auth 字节内容，只证明两轮交互可完成；失败 fixture 无条件返回认证失败，也不能证明“错误密码”被比较。后续测试应断言完整帧，且只能声明 endpoint 接受认证，不能声明 CONNECT、外部出口或业务 Runtime 成功。
- HTTP fixture 接受 `407`，与代码注释一致：只证明 endpoint 返回一条不超过 4096 字节、语法合法的 HTTP/1.0 或 HTTP/1.1 status line。它不证明 HTTP CONNECT、认证、外部可达或正式 mgraftcp 配置可用（`prober.go:75-92`；`prober_test.go:80-100`）。由于当前产品不支持 HTTP proxy 认证，`407` 还明确表示该配置不能按现有能力使用，不能记为“endpoint 可用”。
- 分层状态至少应区分 TCP reachable、proxy protocol detected、credentials accepted 和 controlled route verified。本子集的 SOCKS5 fixture 最多证明前三层中的认证交换，HTTP fixture 只证明前两层；两者都没有 controlled route。仅凭这些结果不能支撑“代理可用”或 N2 ready/publish。若上层策略允许保存草稿可以继续，但 ready/publish 必须等待其声明所需的层级得到证据；具体状态机待尚未冻结的 workflow/domain 审核。
- 当前创建文件的正常路径要求最终根目录 `0700`、key/secret/materialized 文件 `0600`，静态 symlink 和错误 mode 会拒绝；这些正向检查不能覆盖 P1-N2-S02 的竞态窗口。

## Gate 判定

- **冻结子集：NO-GO。** 五项 P1 分别破坏 HTTP 正向路径、同 uid 路径安全、HMAC 持久化并发、敏感孤立文件生命周期和取消语义。
- **N2 总 Gate：不判定。** domain、事务、Worker/Runner/API、identity、environment、main、真实 wrapper 和浏览器仍未形成完整冻结验收证据。
- 修复后应先重算上述六文件子集指纹；指纹未变化的文件无需重复静态审核，变化文件按对应 P1 和测试证据定向复审。

## 下一步

1. 由 N2 唯一实现者集中修复五项 P1，并补最小确定性合同/故障测试，不启动真实网络、模型或浏览器。
2. 冻结后由独立验证者运行受影响包的定向、race 与故障用例；HTTP 正式配置合同和协议 probe 的证明层级分别记录。
3. 子集通过后再继续其余 N2 冻结模块审核；不得把本报告中的 HMAC 或本地 probe 结果扩大为 N2、真实出口或业务 Runtime 已通过。

## 冻结修复定向复审

本节保留以上首次审核的 `NO-GO` 历史，只记录六文件重新冻结后的定向静态复审。复审仍未运行 Go 测试、全量、真实应用、网络、浏览器或模型；必要背景仅核对 `CommitImmutable` 的调用和 `NetworkSecretReferenced` 的查询范围。实际审核模型仍为 `gpt-5.6-sol` medium，Astra 不可用。

重新冻结的六文件聚合指纹为：

```text
388ea84385f178dd3446e0561db5ba4b04f3bbe7b0a683d50422303ecc6262a5
```

逐文件指纹：

```text
4edb5108b0c4cd6ba4690ccbadd326a82497b0349dd55aff725f4ee2b88ca748  internal/network/secretstore/file_store.go
79ef49e5873079c01e11f990231326d517416b389f0ea8f79a674957cda6e601  internal/network/secretstore/file_store_test.go
bf0bf2012ffff07acc34b8d3e86fc6fb569c4da997a4f744d9a9661b8648a0df  internal/runtime/network/materializer.go
338399fb5ec950c385e5881632a59c32d400641938dfc8ca66b5828a955841c1  internal/runtime/network/materializer_test.go
20808d1a3c02cb1ac9476a016878e269945c45e983d08eeed0715ebc1537325e  internal/runtime/network/prober.go
67fcd7174a59843749a581227258a28193f024aca0acbbc61a210776ab8b0583  internal/runtime/network/prober_test.go
```

### 复审验收矩阵

| 原发现 | 修复证据 | 复审判定 |
|---|---|---|
| `P1-N2-S01` HTTP native 配置 | `renderConfig` 现在输出 `http_proxy = host:port`；新增测试拒绝 `http://` 前缀（`materializer.go:187-211`；`materializer_test.go:154-175`） | **静态清除**；未执行正式 wrapper/mgraftcp 合同测试 |
| `P1-N2-S02` 路径与 TOCTOU | secret/materialized 读取已在同一已打开 fd 上执行 `O_NOFOLLOW + fstat + bounded read`（`file_store.go:227-255`；`materializer.go:278-305`） | **仍为 P1**；空白 materializer root 与根路径替换边界未闭合 |
| `P1-N2-S03` key 原子发布 | 两处 key 均改为同目录临时文件完整写入、`fsync`、`RENAME_NOREPLACE`、目录 `fsync`；并发 Open 测试核对同一完整 key（`file_store.go:202-224,257-280`；`materializer.go:138-170,308-335`；对应测试 `68-98`、`123-152`） | **静态清除** |
| `P1-N2-S04` 敏感孤立文件 | secret 提交增加每 version `flock`、提交失败后的引用检查和无引用删除；物化改为 blackip-first、credential config-last，并有对应故障测试（`file_store.go:120-147,166-189,282-310`；`materializer.go:74-97`） | **仍为 P1**；不可读隔离和持久清理/重试未闭合 |
| `P1-N2-S05` accept 后取消 | context 完成会关闭已建立连接，返回前优先返回 `probeCtx.Err()`；测试覆盖精确 SOCKS greeting/RFC 1929 凭据帧、HTTP `407` 和 accept 后 stall/cancel（`prober.go:25-57,60-113`；`prober_test.go:17-63,95-173`） | **静态清除**；不扩大为 CONNECT、出口或 Runtime 成功 |

### 剩余 P1 阻断

#### P1-N2-S02：最终文件 fd 读取已加固，但物化根边界仍未闭合

`NewMaterializer` 对 `strings.TrimSpace(root)` 求绝对路径，却仍以未 trim 的 `root == ""` 判空（`materializer.go:38-48`）。全空白配置仍可能落到当前工作目录；当该目录恰为 `0700` 时会继续创建 integrity key 和物化文件。

读取侧已经消除“先 `Lstat` 最终文件、再按该路径 `ReadFile`”的窗口，但 `Materializer` 和 `FileStore` 仍保存根目录字符串，后续 open、临时文件创建和 rename 都重新解析绝对路径，没有绑定一个以 `O_DIRECTORY|O_NOFOLLOW` 打开的根目录 fd。同 uid 进程可替换根目录路径的威胁边界仍存在。最小修复至少要拒绝 trim 后为空的 root，并将后续目标打开和发布绑定已验证的目录 fd；补充空白 root 与根路径替换的确定性测试。

`0700/0600` 只能限制其他 uid，不能提供同 uid 恶意 Runtime 的完整 OS 隔离。本项只要求关闭当前文件 API 自身的路径竞态，不把范围扩大为新增 sandbox。

#### P1-N2-S04：并发误删已缓解，但不可读孤立状态与清理重试仍缺失

`CommitImmutable` 在同一 version 锁内执行最终文件发布、metadata commit、引用检查和删除，能够阻止两个遵循该入口的同版本请求发生“失败清理删除并发成功文件”。必要背景也确认 import ack 与 `ReplaceSecret` 已调用该入口，`NetworkSecretReferenced` 会查询 profile、test、work item 和 binding 引用（`network_workflow_service.go:117-123,386-398`；`network_workflow_repository.go:51-65`）。这部分修复成立。

但 `PutImmutable` 在 metadata commit 前仍把秘密发布到 `version.secret`，该路径立即可由正式 `Get` 读取。进程在文件发布后、commit 或清理前崩溃会留下可读孤立文件；commit 返回错误后若引用查询也失败（包括沿用已取消的 `ctx`），代码直接保留文件，且没有隔离标记、待核对记录或后续重试器（`file_store.go:132-146`）。因此“不确定提交时不误删”成立，但“不可读孤立文件与清理/重试策略”仍不成立。

metadata service 尚未冻结，本结论只说明当前六文件和上述必要背景内策略没有闭合，不宣称已经完成完整 service 或事务审查。

### 证据层级与未覆盖边界

- SOCKS fixture 静态上精确断言 method greeting 和 RFC 1929 用户名/密码帧，最多支持 `TCP reachable`、`proxy protocol detected`、`credentials accepted` 三层；没有 SOCKS CONNECT 或受控路由证据。
- HTTP `204` fixture 只支持 `TCP reachable` 和 `proxy protocol detected`；`407` 明确表示当前不支持的代理认证要求，不能证明代理可用。
- 本轮没有运行测试，以上“静态清除”表示实现和测试代码已覆盖原缺陷，不表示测试执行通过。
- 未验证正式 wrapper/mgraftcp 配置解析、外部出口、Runtime、service 总体、数据库故障注入、浏览器或 N2 端到端流程。

### 定向复审 Gate 判定

- **重新冻结六文件子集：NO-GO。** `P1-N2-S02` 和 `P1-N2-S04` 仍阻塞；`P1-N2-S01`、`P1-N2-S03`、`P1-N2-S05` 仅在本轮静态边界内清除。
- **N2 总 Gate：不判定。** 本轮没有审核尚未冻结的 service/Runner/API 总体，也没有执行真实 Runtime、网络、浏览器或模型验收。

### 定向复审下一步

1. 集中修复空白 materializer root、根目录 fd 绑定，以及秘密提交前不可读隔离和失败后的持久核对/重试策略。
2. 重新冻结六文件后重算指纹，再由独立验证者运行受影响包的定向、race 和故障用例。
3. 六文件子集通过后再进入其余 N2 模块审核；保持协议探测证据层级，不把本地 fixture 扩大解释为代理、出口或 Runtime 成功。

## 主代理采纳的 S04 边界澄清

本节只澄清 `P1-N2-S04` 的最小设计要求，保留前述审核历史。前一节把“秘密文件已位于正式目录且可由 `Store.Get` 读取”直接等同为“可经正式授权接口读取”，要求过强；本节以主代理采纳的威胁边界替代该项要求。

### 正式接口可达性

当前代码中 `s.secrets.Get` 的正式调用链只有 `PullWork`：先以 `WorkerWriteGuard` 调用 `ClaimNetworkWork`，从数据库内已存在且归属当前 worker instance/generation 的 work item 取得 `SecretVersion`，再读取对应秘密（`network_workflow_service.go:43-63`；`network_workflow_repository.go:527-579`）。worker API 也只接收经认证的 pull 请求，不提供由调用者提交任意 secret version 的读取入口（`workerapi/handler.go:94-125`）。

因此，metadata 尚未提交的孤立 version 即使已经成为 secret store 内的最终文件，也不能通过当前正式授权接口被选中和读取。文件 owner 或同 uid Runtime 直接读取 `0700/0600` 路径属于已明确保留的 OS 隔离边界；本任务不以此要求新增 sandbox，也不把它作为必须增加两阶段秘密目录的理由。

### 可接受的最小清理与重试策略

以下策略足以满足本任务锁定的“正式接口不可读、并发安全清理、失败后重试”边界，不强制引入单独的持久 orphan 标记或两阶段提交：

1. 启动时并按周期枚举 secret store 内已原子发布的最终 secret 文件；文件集合本身是跨重启可重建的待核对候选集。
2. 每个 version 的核对和删除必须持有与 `CommitImmutable` 写者相同的跨进程锁；所有能为该 version 建立 metadata 引用的写路径也必须遵守该锁不变量。
3. 锁内查询数据库中的任一历史或 active 引用；存在任一引用即保留，仅在确定无引用时删除。
4. 数据库不可用、查询超时、context 取消或结果不确定时一律保留；后续启动或周期扫描重新核对，不把不确定状态推断为 orphan。
5. 删除和目录持久化失败必须保留可观察诊断，并在后续扫描重试；扫描不能影响已有引用的读取与旧 Run 保留。

在这些条件下，额外持久标记没有独立必要性：最终 secret 文件提供候选集合，数据库提供权威引用集合，两者在同 version 锁下求差即可重建 orphan 集合。只有当最终文件不能被可靠枚举、引用集合不覆盖历史/active 使用者、部分写路径绕过同一锁，或产品需要区分尚未提交与已提交后失去引用的不同保留策略时，才需要额外持久状态。

### 修订后的 S04 判定

当前 `CommitImmutable` 已覆盖请求内的失败引用检查和无引用删除，但冻结子集与必要背景中尚未看到上述启动/周期扫描及其重试路径。因此 `P1-N2-S04` 当前仍阻塞，阻断原因收窄为“可重建 orphan 扫描与失败重核尚未实现”，不再要求秘密在 metadata commit 前必须位于另一不可读目录。该扫描按上述不变量实现并形成定向故障证据后，S04 可在本任务边界内清除，无需增加 OS sandbox、持久 orphan 标记或大两阶段提交。

## 本轮功能验收源码复审

本节按协调文档顶部“当前收口基线”对最终冻结源码做短审。此前 `NO-GO` 和 P1 标签保留为过程记录，不自动阻断本次交付；本节只以四类直接风险判定：主流程不能完成，秘密泄露或权限绕过，覆盖他人/重复执行/误删有效引用，以及把未应用或未核验事实显示为成功。本节没有运行测试、应用、网络、浏览器或模型，不宣称完整 ADR-002/003 Gate 通过。

最终六文件指纹为：

```text
eb72cd80572b2a5f23b644a5542c53741b96584bb6fcf6f15068d944856f254e
```

逐文件指纹：

```text
2773d4797af9be8ce760978750601ed9cd62adad4dc3a89cc8f8c23f083e00cf  internal/network/secretstore/file_store.go
4266bddc73ada68550ecd851facb529fe00f076e1cba66390090df34e78f3f11  internal/network/secretstore/file_store_test.go
b25a1cbf028c201a40be772fd235129cd5927a215c1f1068c9300c916cc023b2  internal/runtime/network/materializer.go
aec9562634a82e7cb08a877cd7154dd35f3667a5323b356566c8fdf5c2ba1ba3  internal/runtime/network/materializer_test.go
20808d1a3c02cb1ac9476a016878e269945c45e983d08eeed0715ebc1537325e  internal/runtime/network/prober.go
67fcd7174a59843749a581227258a28193f024aca0acbbc61a210776ab8b0583  internal/runtime/network/prober_test.go
```

为核对主要状态链，本节同时读取以下 13 文件，其逐文件 `sha256sum` 输出再做 SHA-256 的集合指纹为 `326c4cf85febb50b6d1c568a4a19a557363bf96cec394e79bb192bffcc5298fa`：

```text
internal/network/secretstore/file_store.go
internal/network/secretstore/file_store_test.go
internal/runtime/network/materializer.go
internal/runtime/network/materializer_test.go
internal/runtime/network/prober.go
internal/runtime/network/prober_test.go
internal/controlplane/network_workflow_service.go
internal/persistence/sqlite/network_workflow_repository.go
internal/persistence/sqlite/worker_execution_repository.go
internal/worker/backend_pool.go
internal/worker/runner.go
internal/cli/worker/process_test.go
internal/domain/network_workflow.go
```

### 四类直接风险结论

| 直接风险 | 最终源码证据 | 判定 |
|---|---|---|
| 主流程不能完成 | CLI 默认零值网络策略归一为 `inherit`；正式进程测试代码覆盖 mode test、七层 probe ACK、publish、apply ACK、普通及多轮 Run。profile 流程要求当前 content/secret 对应的成功 test 后才能 publish（`backend_pool.go:276-300`；`process_test.go:22-101,331-407`；`network_workflow_service.go:466-495`） | **未发现源码阻断**；运行结果待 Terra 证据 |
| 秘密泄露或权限绕过 | secret/materializer 根目录拒绝空白并绑定已验证 directory fd，内部文件使用 `openat/renameat/unlinkat`；Worker 取秘密前以数据库引用校验，未提交 version 返回无秘密；公开观察投影清除 secret/path（`file_store.go:44-77,82-130,162-227`；`materializer.go:39-66,69-112`；`network_workflow_service.go:43-66,240-248`） | **原 S02/S04 的直接风险已清除** |
| 覆盖、重复或误删 | key 与 payload 以 no-replace 原子发布；命令先查 actor/operation/idempotency key receipt；orphan 扫描与写者共用 per-version 跨进程锁，只在权威引用查询确定无引用时删除，查询失败保留并由启动和后续 pull 重核；引用集合包含 profile、test、work、binding 和 active Run（`file_store.go:133-159,187-227,352-406`；`network_workflow_service.go:43-46,137-150,637-654`；`network_workflow_repository.go:51-68`） | **未发现误删有效引用或重复提交路径** |
| 虚报已应用或已核验 | test ACK 必须包含固定七层且失败层与 work 终态一致；publish 只接受匹配版本、Worker generation 和 Runtime identity 的成功 test；apply 先由 Worker adapter 实际应用，ACK policy 去除本机路径，SQLite 再校验 guard、binding revision、manifest、secret、identity 和 policy 后才写 `applied`；Run 规划和 Begin 再要求当前 Worker/generation 的 applied 快照完全一致（`backend_pool.go:37-202`；`network_workflow_service.go:253-336,428-495`；`network_workflow_repository.go:641-767`；`worker_execution_repository.go:63-109,336-380`） | **未发现把 pending/未匹配回执写成 applied 的路径** |

`inherit` 的 secret/endpoint 为 `not_verified/INHERITED_CONFIGURATION_UNVERIFIED`，network effect/model call 为 `not_verified/NOT_VERIFIED`；这不是已核验外部出口或模型调用。源码中的 test succeeded 只表示固定七层结果满足当前规则，最终页面和报告仍必须展示每层状态，不能把 `not_verified` 改写为已核验。

### 本轮功能源码 Gate

- **N2 最终冻结源码：GO。** 在本轮四类直接风险和主要工作流范围内，原 S01-S05 对功能交付的阻断均已清除；未发现新的直接阻断。
- **最终功能验收：等待证据。** Terra 正在执行 API、真实 Worker、浏览器和观察结果核验；其结果到达前，本节只支持源码 GO，不支持宣称实际闭环已经通过。
- 未覆盖的完整 ADR 条款、极端故障矩阵、额外 OS sandbox 和增强型孤立保留策略按最新收口基线后置，不影响本节源码判定。

## 本轮功能验收证据短核

本节只核对独立执行报告 `2026-09-05-openagentx-n2-validation-executed.md` 及其归档截图，不重跑测试、不扫描新源码。执行报告的 `partial_pass` 表示完整 ADR-002 尚未验完；按协调文档顶部的最新收口基线，本节单独判定 N2 本轮功能范围。

### 已核对证据

- named profile、`inherit`、`direct` 均通过正式 Control API、真实 Worker work/ACK 和 Observe `applied` 链路；named profile 还完成 create 重放、编辑、秘密提交、target test、ready、publish 和 bind。Worker/generation 与 ACK 一致，未把 pending 或不匹配回执显示为 applied。
- named SOCKS5 使用隔离服务完成精确认证，成功计数 `1`、失败计数 `0`；materialized 配置与 integrity key 均为 `0600`。本证据证明隔离代理认证路径，不证明外部出口或真实模型调用。
- 每个 test 恰有七层结果；`inherit` 的 secret/endpoint 为 `not_verified/INHERITED_CONFIGURATION_UNVERIFIED`，network effect/model call 为 `not_verified/NOT_VERIFIED`。Chrome 页面也明确显示“网络效果 未核验”，没有把未核验事实提升为成功。
- Chrome `143.0.7499.109` 使用隔离 profile 完成真实登录和网络配置基本交互；凭据提交后用户名、密码输入均清空。未认证 Observe 返回 `401`，离线状态只证明写控件 disabled。
- 三张截图已归档到 `docs/reports/validation/evidence/n2/`。manifest 记录的 SHA-256 和尺寸分别为：桌面 `080aac91...d43ee`、`1440x900`；移动 `e49c71bd...3c426`、`390x844`；移动 `a90b6f7f...e5e83`、`412x915`。独立目视短核未见控件重叠或页面级横向溢出。
- 执行环境记录了 schema `1`、非秘密 Worker ID、generation `1`、隔离二进制 SHA-256 `552e5891...ff765`、当前应用源码树指纹 `bd8515ec...793657` 和 `web/dist` 指纹 `4d7207cc...b2311`。报告明确区分基线 HEAD 与含未提交冻结改动的当前源码树。

### 事故与证据边界

首个旧 fixture 曾因不安全脚本输入让临时凭据发生终端回显。执行报告已明确废弃该 fixture、凭据和 session，且没有把秘密值写入报告；最终功能结论只绑定重新创建的 r2 fixture。r2 使用私有 `0600` 文件和关闭终端回显的输入方式，未发现其秘密进入 argv、报告、截图或控制台。该事故不阻断 r2 证据，但旧 fixture 的任何输出不得再用于验收。

本批 active Run 仅验证空态，离线仅验证控件 disabled；没有写请求计数、SSE 断线恢复、真实模型调用或可验证网络副作用。回退、导入、双 Session CAS、旧 generation/fencing、组合错误优先级和旧 helper 矩阵属于完整 ADR 或后续范围，不冒充已通过，也不按最新收口基线阻断 N2 本轮功能交付。

### 最终判定

- **N2 本轮功能范围：GO。** 最终源码短审和 Terra 执行证据共同覆盖配置、目标测试、显式发布、真实 Worker apply ACK、Observe 状态和 Chrome 基本交互；未发现四类直接风险。
- **完整 ADR-002：未验收。** 本结论不等于完整 ADR GO，也不覆盖上述后置场景。
- **E1 最小补证：** 错误配置的失败闭环、关键重试不重复生效，以及断线后的状态恢复；离线需以实际写请求计数或等价服务端证据补证，SSE 断线恢复需单独实测。
