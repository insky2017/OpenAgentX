# 本批验收覆盖

范围：Skill 安装/发现和离线 prepare。D 为确定性契约/真实本地集成，R 为真实模型，I 为当前安装入口；不将三个等级相互替代。

| 项 | 结果 | 实际证据 | 未覆盖 |
|---|---|---|---|
| S01 项目/用户链接与可见性 | D/I PASS | [用户安装](evidence/installed-00.json)、[真实发现](evidence/installed-01.json)、[项目安装与冲突](evidence/independent-review/observations.json) | 其他机器/其他 Codex 版本，当前已运行 turn 自动刷新 |
| S02 Skill 到 prepare | R/I PASS，首次环境失败保留 | [model02 日志](evidence/model02/codex.public.jsonl)、[receipt](evidence/model02/cli-receipt.json)、[ROLE](evidence/model02/ROLE.md)、[HANDOFF](evidence/model02/HANDOFF.md)、[首次失败](evidence/model01/verdict.json) | 后台接管和 managed 消息往返 |
| S03 当前 thread 与 profile 连续 | R/D PASS | [独立比对](evidence/model02/verdict.json)、[来源](evidence/model02/source.json)、[后续 status](evidence/model02/prepared-status.json)、[profile 优先级](evidence/independent-review/additional-observations.json) | Desktop 释放 writer、不同宿主是否提供相同环境变量 |
| S04 明确失败与幂等 | D PASS | [独立观察](evidence/independent-review/observations.json)、[补充检查](evidence/independent-review/additional-observations.json)、[实际子代理线程冲突](evidence/independent-review/actual-thread-inspect.json) | 大规模并发压力；本批不追求测试数量 |
| S05 Skill 作用域与用户入口 | R/I PASS | [独立审阅](evidence/independent-review/review.json)、[模型最终答复](evidence/model02/final.txt)、[浏览器记录](evidence/browser.json) | 自然语言缺失信息补问的所有表达方式 |

模型真实线程与集成测试 UUID 分开报告。文件语法、Skill frontmatter、页面链接和源文件摘要见[最终检查](evidence/final-checks.json)。没有新建产品 Task/Run/Journal 是本批 prepare 不启动执行的预期；模型运行证据来自独立 Codex CLI JSONL，不能假报为 OAX Task E2E。
