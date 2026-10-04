# 执行记录

1. 核对 main@33ab29c、已安装 OAX 886ba7f/Codex 0.160.0，读取官方 [Skills](https://developers.openai.com/codex/skills/) 与 [app-server](https://developers.openai.com/codex/app-server/)；保留[技能发现规范摘录](evidence/official-skills.txt)和[实际协议摘录](evidence/official-app-server.txt)。
2. 在既有实施/E2E计划追加本批目标与 S01–S05。分配 astra/high 实现安装器和 prepare helper，另由独立 astra/high 做真实本地功能核验；未修改 Go 或扩展后台协议。
3. 项目软链安装/重放/冲突和真实 skills/list 先在隔离工程验证，再实际安装本机用户级软链。[安装实录](evidence/installed-00.json)与[发现实录](evidence/installed-01.json)分别记录，未将链接存在冒充可见。
4. 真实模型 model01 受 bwrap 环境限制，两次读取均失败，未执行 helper；停止原验证路径，保留[日志](evidence/model01/codex.public.jsonl)。改用已授权宿主模式另建 model02，独立新 thread 实际读取 Skill、生成 brief、prepare 与产物检查通过。[模型调用输入](evidence/model02/invocation.json)和[执行记录](evidence/model02/codex.public.jsonl)保存实际模型与 high 强度。
5. 独立检查覆盖真实 CLI receipt、重放摘要、缺失/冲突 thread、summary、输出路径、profile 优先级及既有身份冲突；不以一致的测试 UUID 冒充模型自身 thread。见[独立报告](evidence/independent-review/review.json)。
6. 更新操作说明、接入 HTML 和总体架构中的接入节点。浏览器 HTTP 预览因 ERR_ACCESS_DENIED 失败，转为真实 Chrome 离线载入本地 HTML，验证文字/交互/窄布局。使用前一轮已经通过的其它图布局，不重复整套架构验收。
7. 筛选并删隐公开证据，核对源码语法、Skill 格式、链接、冻结 ADR、默认 Fleet 和独立审阅文件摘要；按主分支约定提交。当前服务工件来源没有随脚本/文档提交更改。
