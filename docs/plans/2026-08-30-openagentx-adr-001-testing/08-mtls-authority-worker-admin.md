---
doc_type: test_task
status: passed
owner: openagentx
test_id: T08
updated_at: 2026-09-01
---

# T08：mTLS、组织权限与 Worker Admin

## 目标

验证远程 Worker 与本机 UDS 使用相同 Worker Control API 契约，确认 mTLS 身份隔离、组织授权、WorkerInstance 运维边界和浏览器隔离。

## 本轮结论

PASS（P1 阻塞已解除）。远程 HTTPS transport、mTLS 身份边界和正式 `openagentx worker run` 已通过重复普通/race 矩阵；`remotehttps.Server.Start` 现在对在途请求提供短暂优雅窗口，随后强制关闭握手/活动连接，确保 stop 后 listener 确定退出。systemd 动态受控 stop 演练因当前机器未安装该模板 unit 未执行，浏览器生产入口负向验证留给 T09。

## 已通过

- TLS 1.3、`RequireAndVerifyClientCert`、无证书、错误 CA、过期证书拒绝；
- principal 解析及 principal→Agent 显式 binding，未绑定 Agent fail closed；
- 真实 HTTPS listener 上 register、heartbeat、claim、begin-attempt、finish；
- 正式 `RunWorkerProcess` 读取 HTTPS/mTLS 配置，连续完成 Task A/B，并可响应 Worker stop；
- UDS Worker API、Bearer token/generation/lease/fencing、AuthorityPolicy 和 Worker Admin 领域测试；
- Panel/daemon 路由未注册 Worker Control API，代码级 Session Token 隔离；
- systemd Worker 模板为 `Restart=on-failure`，静态语义与 ADR 一致。

## 遗留范围（不阻塞本测试项）

- systemd 受控 stop/不重启未执行动态演练，浏览器 L4 负向留给 T09 生产入口测试。

## 执行证据

```text
go test -count=1 ./...                             PASS
go test -race -count=1 ./...                       PASS
go test -count=20 ./internal/transport/remotehttps ./internal/cli/worker -run 'Test(Server|Certificate|RunWorkerProcess|LoadProcessConfig)' PASS
go test -race -count=20 ./internal/transport/remotehttps ./internal/cli/worker -run 'Test(Server|Certificate|RunWorkerProcess|LoadProcessConfig)' PASS
go test -race -count=20 ./internal/transport/remotehttps -run '^TestServerStopsPromptlyWithIncompleteTLSHandshake$' PASS
go test -race -count=10 ./internal/controlplane ./internal/worker ./internal/api/workerapi ./internal/api/admin ./internal/api/panel ./internal/api ./internal/transport/unixhttp ./internal/persistence/sqlite ./internal/domain PASS
go test -race -count=20 ./internal/controlplane -run TestRemoteHTTPSWorkerAPIEndToEnd PASS
go test -race -count=20 ./internal/worker -run 'TestControlledStopCancelsAndReconcilesBeforeAcknowledgement|TestLeaseLossIsFatalAndReturnsNonZeroSemantics|TestDrainStopsNewWorkWhileMailboxPumpKeepsRunningAtZeroCapacity' PASS
go test -race -count=20 ./internal/transport/remotehttps -run 'Test(ServerRequiresTLS13AndVerifiedClientCertificate|NewServerRejectsWeakTLSConfiguration|ServerRejectsMissingAndWrongCAClientCertificates|ServerRejectsExpiredClientCertificate|CertificatePrincipalBindingFailsClosed|CertificateBindingGuardsWorkerAPIRegistration)' PASS
go test -count=1 -run '^TestRunWorkerProcessCompletesConsecutiveTasksOverRemoteHTTPSAndStops$' ./internal/cli/worker PASS
go vet ./...                                      PASS
git diff --check                                 PASS
./scripts/check-legacy-control-paths.sh --release PASS（仅输出预期 legacy inventory）
```

远程 E2E 证据：真实 TLS listener 完成 Worker register、heartbeat、Mailbox claim、begin-attempt 和 finish；正式 `RunWorkerProcess` 读取 HTTPS/mTLS 文件配置，在同一逻辑 Agent 上连续完成 Task A/B，并响应 Worker stop。修复后普通和 race `-count=20` 均通过；新增 `TestServerStopsPromptlyWithIncompleteTLSHandshake` 覆盖未发送 ClientHello 时的确定退出。测试代码位于 `internal/controlplane/worker_remotehttps_test.go`、`internal/cli/worker/process_remotehttps_test.go` 与 `internal/transport/remotehttps/server_test.go`。

## 后续验证

1. 在安装 OpenAgentX 模板 unit 的环境中动态演练 systemd `Restart=on-failure` 的受控 stop 不重启语义；
2. 对生产 Nginx/浏览器补充 Worker API 404/拒绝与 token 不可见的 L4 证据，纳入 T09。

任何凭据、Session Token、mTLS 私钥、代理凭据或完整 fencing token 均不得进入报告。
