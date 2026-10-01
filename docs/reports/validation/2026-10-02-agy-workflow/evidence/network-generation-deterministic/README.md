# Agent 网络跨代恢复修复：D 证据

触发条件：已发布的 inherit/direct 网络绑定属于旧 Worker generation，随后运行 agent resume。原入口只等待自动 reapply，真实 E17 的 generation 2 online 后等待 45 秒仍为旧代 applied、readiness=false。

最小修复：保留既有模式，用当前 binding.Version 作为正式 mode test 和 publish 的 ExpectedVersion；仅在当前 Worker/generation/绑定版本均 applied 后返回。named_profile 不修改，立即提示工作台为当前 Worker 重新测试发布。

验证命令与实际输出见 test-results.json。新增测试通过真实 TCP httptest 服务核对 HTTP 请求模式、CAS、Worker/generation、幂等键；分别返回测试/发布 409 验证不得误成功。这是 D 层，服务处理器仍是 fixture，不能替代正式 API 或安装 I。

真实触发和后续正式 API 恢复分别见 ../recovery-14d7350-02 和 ../recovery-14d7350-02-resume01；正式已安装 agent resume 尚未由本目录证明。
