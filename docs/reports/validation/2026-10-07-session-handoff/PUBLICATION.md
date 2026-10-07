# Git 交付状态

## 2026-10-08 推送已恢复（UTC+8）

- 用户要求核实可用代理后推送。本次通过一次性 `http.proxy=http://127.0.0.1:7898` 与已有 GitHub 凭据走 HTTPS，成功将 `main` 从 `8d32583` 推至 `8de06a33b4e027794d3f7f59bba69294f7d06751`。随后同一路径 `ls-remote` 核实完整 SHA 一致（2026-10-07 20:11 UTC）。
- `7897/7898/7899` 均返回 GitHub HTTP 200；`7898/7899` 均通过认证 Git 远端读取；`1081` 的 CONNECT 返回 503，直连 GitHub 443 在 5 秒连接超时。
- 当前 SSH 配置无 `ProxyCommand`/`ProxyJump`，不会自动使用 `HTTP_PROXY`。三个 Mihomo 入口的 selector 链不同，但本次快照最终上游相同，不能仅凭换端口成功就认定此前服务端错误完全由代理造成。
- 没有修改全局 Git/代理配置、切换 selector、重启代理或改变 Git remote；本批只完成推送恢复与记录更新，没有安装产品或操作业务 thread。下方保留首次失败证据。

成功命令（凭据由现有 helper 在进程内取得，不进入命令参数或报告）：

```sh
GIT_TERMINAL_PROMPT=0 git \
  -c http.proxy=http://127.0.0.1:7898 \
  -c credential.helper= \
  -c 'credential.helper=!gh auth git-credential' \
  push https://github.com/insky2017/OpenAgentX.git main:main
```

## 首次受阻记录（2026-10-07 UTC）

- 产品提交 `e644511f2763ebe7df40dc31f6efaf7a43067b2b`、验收提交 `53a39bdad8069b8a5e7ee8dbf1d205fd23069b26` 已快进合入本地 canonical `main`。
- 2026-10-07 推送被 GitHub 服务端持续返回 `Internal Server Error` 拒绝。SSH 重试、使用现有凭据的 HTTPS，以及禁用 thin pack/delta 的传输均失败；没有强推、修改远端保护或覆盖历史。
- 远端 `main` 读取仍为 `8d32583e3100964b5869c99a537172a35ff42e1a`。GitHub API 查询本轮验收提交返回 404，不能仅通过更新 ref 完成发布。
- 本地完整提交、工件和验收证据已保留；该网络失败不改变真实验收结论，也不能宣称已经推送或部署。

| UTC 时间 | 传输 | GitHub Request ID | 结果 |
| --- | --- | --- | --- |
| 15:09:54 | SSH | `9683:30E70A:2C2B1:6D6FB:6AC660BD` | `remote rejected / Internal Server Error` |
| 15:11:13 | SSH 重试 | `9685:5E872:2C6F0:6EC08:6AC6610C` | 同上 |
| 15:12:35 | HTTPS | `CE02:2F7C24:149741:1C6904:6AC66161` | 同上 |
| 15:14:14 | SSH，`--no-thin` / `pack.window=0` | `9686:2941DD:2D125:6FFA0:6AC661C2` | 同上 |

上述阻断现已解除，无需重跑六次真实模型验收。生产部署与业务会话切换仍按 [DELIVERY](DELIVERY.md) 的范围单独记录。
