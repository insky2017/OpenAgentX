---
doc_type: validation_report
test_id: T08
status: passed
updated_at: 2026-09-01
---

# T08 验证报告：mTLS、组织权限与 Worker Admin

## 结论

PASS（P1 阻塞已解除）。远程 HTTPS Gateway 与 UDS 共享同一 Worker API Handler，正式 `openagentx worker run` 可读取 HTTPS/mTLS 配置并连续完成 Task A/B；修复 `remotehttps.Server.Start` 的 shutdown 语义后，普通/race 重复矩阵均证明 listener 在 stop 后确定退出。systemd 动态受控 stop 演练因当前机器未安装模板 unit 未执行，浏览器生产入口负向验证留给 T09。

## 完整性与范围

- ADR-001 SHA-256：`587e9b8f27ddeb68aae8e5a507fefc10973739c8dd2c3def86e2c0bbc51db403`；本轮未修改 ADR。
- 本轮包含 HTTPS Worker 配置/客户端、远程 Handler binding 及隔离 E2E 测试变更；未修改 ADR-001。
- 报告不记录密码、Cookie、Session Token、mTLS 私钥、代理凭据或完整 fencing token。

## 执行证据

| 命令 | 结果 |
|---|---|
| `go test -count=1 ./...` | PASS |
| `go test -race -count=1 ./...` | PASS；0 race |
| `go test -count=20 ./internal/transport/remotehttps ./internal/cli/worker -run 'Test(Server|Certificate|RunWorkerProcess|LoadProcessConfig)'` | PASS |
| `go test -race -count=20 ./internal/transport/remotehttps ./internal/cli/worker -run 'Test(Server|Certificate|RunWorkerProcess|LoadProcessConfig)'` | PASS |
| `go test -race -count=20 ./internal/transport/remotehttps -run '^TestServerStopsPromptlyWithIncompleteTLSHandshake$'` | PASS |
| `go test -race -count=10 ./internal/controlplane ./internal/worker ./internal/api/workerapi ./internal/api/admin ./internal/api/panel ./internal/api ./internal/transport/unixhttp ./internal/persistence/sqlite ./internal/domain` | PASS |
| `go test -race -count=20 ./internal/controlplane -run TestRemoteHTTPSWorkerAPIEndToEnd` | PASS |
| `go test -race -count=20 ./internal/worker -run 'TestControlledStopCancelsAndReconcilesBeforeAcknowledgement\|TestLeaseLossIsFatalAndReturnsNonZeroSemantics\|TestDrainStopsNewWorkWhileMailboxPumpKeepsRunningAtZeroCapacity'` | PASS |
| `go test -race -count=20 ./internal/transport/remotehttps -run 'Test(ServerRequiresTLS13AndVerifiedClientCertificate\|NewServerRejectsWeakTLSConfiguration\|ServerRejectsMissingAndWrongCAClientCertificates\|ServerRejectsExpiredClientCertificate\|CertificatePrincipalBindingFailsClosed\|CertificateBindingGuardsWorkerAPIRegistration)'` | PASS |
| `go test -count=1 -run '^TestRunWorkerProcessCompletesConsecutiveTasksOverRemoteHTTPSAndStops$' ./internal/cli/worker` | PASS |
| `go vet ./...` | PASS |
| `git diff --check` | PASS |
| `./scripts/check-legacy-control-paths.sh --release` | PASS；仅输出预期 legacy inventory |

## 断言结果

| 断言 | 结果 | 证据等级与说明 |
|---|---|---|
| UDS Worker API 契约 | PASS | L2：`TestUnixHTTPWorkerAPIEndToEnd` 覆盖注册、心跳、claim、begin、payload、event、finish。 |
| mTLS HTTPS Worker API 契约 | PASS（API 层） | L2：新增 `TestRemoteHTTPSWorkerAPIEndToEnd` 在真实 TLS listener 上完成 register、heartbeat、claim、begin-attempt、finish。 |
| TLS 1.3 与客户端证书验证 | PASS | L1/L2：`server_test.go` 断言 TLS 1.3 与 `RequireAndVerifyClientCert`。 |
| 无证书、错误 CA、过期证书拒绝 | PASS | L2：`TestServerRejectsMissingAndWrongCAClientCertificates`、`TestServerRejectsExpiredClientCertificate`。 |
| principal→Agent binding | PASS | L1/L2：未知 principal、未绑定 Agent、非 HTTPS 注册均 fail closed；注册请求在 domain service 前被拦截。 |
| Bearer token、generation、lease、fencing | PASS | L1/L2：既有 Worker API/UDS 测试和 20 次 race 矩阵；远程请求使用相同 Handler 与 service guard。 |
| AuthorityPolicy 与业务只寻址 `agent_id` | PASS | L1：`organization_contract_test.go`、`target_contract_test.go` 及 controlplane 回归通过。 |
| Worker Admin 仅面向 WorkerInstance | PASS | L1/L2：`admin`、`controlplane` 回归通过，drain/health-check/revoke/stop 为独立 WorkerCommand/lease 边界。 |
| `Restart=on-failure` stop 语义 | PARTIAL | L1 静态：`deploy/systemd/openagentx-worker@.service` 为 `Restart=on-failure`；当前机器没有已安装 unit，未做 systemd 受控退出演练。 |
| Panel 不能调用 Worker Control API 或读取 token | PARTIAL | L1 静态：daemon Web mux 仅挂载 Auth、`/api/admin/`、Panel `/api/`；Worker API 只绑定 UDS/独立 remote HTTPS listener；Panel DTO/前端源码不返回 Worker Session Token。生产 HTTPS/浏览器负向实测留给 T09。 |
| 真实远程 Resident Worker 进程 | PASS | 正式 `RunWorkerProcess` E2E 完成 HTTPS/mTLS 注册、连续 Task A/B 和 Worker stop；普通/race `-count=20` 均通过。 |

## 缺陷与影响

远程 Gateway、mTLS binding 和正式 Worker transport 已具备。`remotehttps.Server.Start` 在停止时先给予在途 HTTP handler 250ms 优雅窗口，随后调用 `http.Server.Close()` 收尾未完成 TLS 握手和剩余连接；新增握手中断回归测试及普通/race 重复矩阵均通过，P1 listener 生命周期阻塞已解除。systemd 动态受控 stop 与生产浏览器负向证据仍分别属于部署环境演练和 T09 范围。

## 后续验证

1. 在安装 OpenAgentX 模板 unit 的环境中动态演练 systemd `Restart=on-failure` 的受控 stop 不重启语义；
2. 对生产 Nginx/浏览器补充 Worker API 404/拒绝与 token 不可见的 L4 证据，纳入 T09。
