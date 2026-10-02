# 首次验证失败

- login HTTP 200；随后 overview HTTP 401，没有 Task 派发。
- 原因：产品返回 Secure session cookie；Python urllib CookieJar 不向当前 HTTP loopback URL 发送该 cookie。
- before/after 对照 isolated daemon/Worker PID、starttime 和实际 /proc/exe SHA，均匹配候选。
- 此次不构成 Runtime、模型或三轮工作流失败/通过结论；未改产品或任务状态。

下一步：修正客户端 loopback 测试契约后使用新 evidence/run 目录复验，保留本目录。
