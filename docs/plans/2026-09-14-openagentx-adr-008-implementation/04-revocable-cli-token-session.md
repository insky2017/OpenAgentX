---
doc_type: implementation_task
status: pending
owner: openagentx
updated_at: 2026-09-14
---

# 任务 04：可撤销 CLI Token 会话

## 目标

以服务端可撤销的 opaque CLI Token 替代 Console 每次携带 username/password；本地不保存密码，Web cookie+CSRF 安全边界保持不变。

## 依赖

- 任务 03 已通过；Attach API cursor 契约稳定。

## 实施步骤

1. 增加独立 CLI Token 领域记录和 repository：随机高熵 token 只在签发时返回，数据库只保存抗枚举摘要及必要绑定元数据。
2. Token 至少绑定 canonical socket/installation ID、user identity、scope、created/last-used/absolute-expiry/revoked 时间；默认绝对期限不超过 30 天，不以无限 sliding expiry 延长。
3. 按现行 schema v1 兼容升级机制，同时更新空库 target schema 和既有 v1 reopen 路径；不得只修改 `001_target_schema.sql`。升级必须事务化、幂等，required schema 校验和 reopen 测试覆盖 `cli_tokens`。
4. 添加 versioned、默认仅本地 UDS 暴露的 CLI login/session/logout API。Web login cookie、Secure/SameSite 和 CSRF 验证不得被弱化；CLI bearer 不得意外开放到不预期的远程/Web mux。
5. Console read/write/diagnostic/control API 按 scope 和 role 检查 CLI principal；未授权、已撤销、过期、installation/socket 不匹配全部 fail closed 并有稳定错误。
6. owner 密码发生变更时提供撤销其既有 CLI Token 的 repository/service 钩子；若当前产品尚无密码修改入口，测试该服务能力并记录集成边界，不伪造未存在的 UI。
7. 实现本地 credential store：JSON 只含 opaque token 和非秘密元数据，目录最小权限，文件精确 `0600`，临时文件+fsync/rename 原子替换，拒绝不安全权限、owner 或软链接场景。
8. `console login` 只从 TTY 的隐藏输入读取密码，不允许密码 flag、argv、环境持久化或日志回显；非交互环境无安全输入时 fail closed。
9. `console logout` 在 daemon 可达时先请求撤销，并在所有结果下删除本地 token；若远端撤销失败，明确警告“本地已删除、服务端状态未知”。
10. 日志、错误、测试快照和 execution log 禁止出现原始 token/password；测试只比较摘要或占位符。

## 必测场景

- 登录成功、错误密码、无 owner/operator 权限、scope 不足；
- token 到期、撤销、重复 logout、不同 installation/socket 重放；
- 数据库只含摘要，原始 token 搜索不到；
- 既有 v1 DB reopen 自动获得完整表，失败升级不留下半表；
- credential `0600`、错误权限拒绝、原子替换、崩溃前旧文件仍可用；
- daemon 不可达 logout 仍删除本地文件；
- Web cookie+CSRF 回归测试保持通过。

## 验证

```bash
go test ./internal/auth/... ./internal/api/auth ./internal/client/console ./internal/cli/console
go test ./internal/persistence/sqlite/... ./internal/persistence/sqlite/migrations/...
go test -race ./internal/auth/... ./internal/api/auth ./internal/client/console
git diff --check
```

## 退出条件

- 密码仅出现在受控登录请求内，本地和数据库均不保存明文；
- token 生命周期、scope、撤销、过期和绑定有正反测试；
- 旧 DB 与空库路径均通过；
- Web Auth 安全属性无回归；
- 阶段提交和 execution log 完成后暂停。
