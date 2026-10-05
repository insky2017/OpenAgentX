# 本批验收边界

| 项目 | 状态 | 证据 |
|---|---|---|
| 安装工件/源码与两套 marker 来源核对 | 已完成来源核查，非 E2E | evidence/provenance.json |
| 六域 Worker、原生终端、thread 基线 | 已采集，以完整进程身份快照为准 | evidence/baseline-verified.json；此前快照保留 |
| native/Console 窗口固化与正确定位 | 真实隔离 Codex / tmux / TTY PASS | evidence/runtime-e2e/run-02/native-verdict.json；console-verdict.json；terminal-review/REVIEW.md |
| 同身份补齐、冲突保护、部分失败恢复 | PASS，真实 tmux 与关键状态故障注入分别记录 | evidence/terminal-impl/implementation-evidence.json |
| 两个 Skill 真实发现、短入口准备 | 真实准备及 canonical 安装发现 PASS；不等于自动接管 | evidence/join-review/review.json；evidence/join-model-short/verdict.json；evidence/skill-installed/check.json |
| Overview 列表、详情、跳转、断线恢复 | 真实 160×45 / 80×24 TTY PASS；含超屏首/尾/回首 | evidence/runtime-e2e/run-02/overview-verdict.json；overview-scroll-verdict.json |
| 原子安装、临时 marker 迁移、overview 替换 | PASS；只替换授权 overview shell | evidence/cli-installed.json；evidence/migration-applied/；evidence/overview-installed/ |
| 六域前后持续运行、thread/generation 不变 | PASS；部分进程直接 exe 哈希读取受内核限制，依据明确记录 | evidence/final-review/review.json；evidence/final-review/final-independent.json |
| 新总览读取现有旧 daemon | 实际读取及安装后在线 PASS；现场未实际切换用户 client | evidence/overview-live-read/read-compatibility.json；evidence/overview-installed/verdict.json |
| 文档 HTML 浏览器 | 实际原文渲染、交互、宽窄布局 PASS；file/HTTP 导航未验 | evidence/browser/README.md |
| 跨域咨询/答复/续办全局摘要 | 第二批，不在本批范围 | — |
| join 自动接管 | 不在本批范围 | — |
