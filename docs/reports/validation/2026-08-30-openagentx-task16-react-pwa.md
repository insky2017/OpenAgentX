---
doc_type: validation_report
task: 16
status: passed
updated_at: 2026-08-30
---

# Task 16 验证报告：React Mobile-first PWA

## 覆盖内容

- Vite + React 生产构建。
- 手机 390x844 与桌面 1440x900 视口真实浏览器渲染。
- 指挥、任务、组织导航，审批、回复和指令输入交互。
- Manifest、Service Worker 注册及 `/api/` 不缓存策略。
- Chromium `beforeinstallprompt` 安装入口、`appinstalled`/standalone 状态收敛，及 iOS 手动添加提示边界。

## 验证命令

```text
npm install --no-audit --no-fund
npm run build
agent-browser open http://rtx4090:4173/
agent-browser set viewport 390 844
agent-browser snapshot -i
agent-browser set viewport 1440 900
agent-browser reload
agent-browser snapshot -i
agent-browser errors
agent-browser console
python3 scripts/check_docs.py
```

构建、DOM/交互、双视口检查和文档检查均通过；冻结 ADR 文件未修改。

## 安装入口补充

`web/src/main.jsx` 现在保存浏览器提供的 deferred install prompt，仅在事件可用时显示安装按钮；用户关闭或接受对话框后不宣称已安装，收到 `appinstalled` 或检测到 standalone 后隐藏入口。iOS Safari 未提供 `beforeinstallprompt` 时显示简短的“可从浏览器菜单添加到主屏幕”提示；不支持安装提示的桌面环境保持隐藏。

本次代码级验证执行 `npm run build`（Vite 生产构建通过）。真实 OS 级安装仍需在目标浏览器中验证，不将构建结果作为“已安装”证据。
