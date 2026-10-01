# 初始基线真实 AGY 与浏览器验证

## 要点

- 来源为提交 `7400806` 的独立 git archive，Go 工件 SHA-256 `457d229074ebb4d3c01f0ddb86d36a9f0cad53ca212bd433f22e67a54ba96336`。真实 AGY 与正式 agy-graft 版本均 `1.2.14`，模型 `gemini-3.7-flash-low`。这不是最终修复候选。
- 正式 CLI 初始化隔离账号及 Agent，正式 HTTP API 从无 binding 状态 test/publish inherit 后，两项 query 自动连续完成。同一 Worker `worker-b6940f65-3981-4d21-bbe0-b66d74dd9563`，generation `1`。Task 分别为 `task-a86bbc5b-b072-4b60-90c1-5c7d986bba77` 和 `task-6e7c7661-0eaa-41f5-9811-936cf0e01550`，均为 `succeeded/query_result_delivered`，回复是各自唯一标记。
- CUA 真实表单登录后，通过页面进入工作台、选择“查询任务”、填写指令并点击发送，创建 `task-0f048f54-82ca-4162-b72a-b78c1318f52f`。Run `run-18aafc21-c28a-4ff0-b577-38df790b5d77` 成功，实际回复精确为 `OAX-BROWSER-7400806-1002F`。随后正式 Observe API 交叉核验通过。界面运行过程及结果原样保存于 `cua-desktop-01.png`～`cua-desktop-03.png`；`cua-dom.txt` 保留七次原始页面状态。
- 这次网络是明确记录的底层 API 配置，**没有证明向导首次使用**；没有证明安装链、文件效果、review、continue、queued补充、取消、超时或最终候选。query 回复交付也不证明答案真实性、全程只读或任意业务副作用正确。
- `live-7400806-1002a`～`e` 保存 setup 失败：HTTP 客户端用法、Secure Cookie 的 HTTP 发送策略、tee 管道保活、Unix socket 长度和自定义环境变量被 allowlist 移除。均为夹具准备错误，未冒充 AGY 产品失败或通过。
- password、原始 Runtime stdout/stderr、原始 HTTP、配置及数据库在 `/home/sky/.local/state/openagentx/validation/2026-10-02-agy-workflow/` 私有目录持久保留。重复版本探针移入该目录的 `public-version-probes/`，公开索引记录逐文件摘要。该实例专属 daemon/Worker 已按 PID＋starttime 清理，见 `cleanup.json`；正式 daemon 仍为 PID `1877`、active、NRestarts `0`。

## 下一步

1. 在最终候选提交上重跑真实 AGY 文件/验收/继续/补充/取消/超时与浏览器链。
2. 首用向导和 user-systemd 安装链分别执行；正式安装 smoke 另用没有 capture 监督器的正式 agy-graft 路径。
