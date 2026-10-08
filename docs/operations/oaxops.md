# oaxops：宿主运维 Agent

`oaxops` 在 `/home/sky/docs` 的独立 Codex 会话中执行授权维护和跨 OAX 重启核验；不自动接收 OAX Mailbox。没有事件时不调用模型。

## 安装与使用

当前宿主已安装到 `~/.local/bin/oaxops`。从仓库根更新：

```sh
install -Dm755 scripts/operations/oaxops "$HOME/.local/bin/oaxops"
export PATH="$HOME/.local/bin:$PATH"
```

状态保存在 `${XDG_STATE_HOME:-$HOME/.local/state}/oaxops/`；`session.json` 保存原 thread，`events/<key>/` 保存事件及回执，`writer.lock` 保证单 writer。

```sh
oaxops status
oaxops open
oaxops send --key <稳定事件ID> --prompt <绝对路径文件>
oaxops send --maintenance --key <稳定事件ID> --prompt <已授权维护交接文件>
```

`status` 查看 thread、最近结果及忙锁；`open` 进入原会话。`send` 成功仅表示临时 systemd 单元已提交，完成情况要查回执。普通事件默认只读；`--maintenance` 只承接正文的具体授权。当前宿主使用 `danger-full-access`，只读提示词不是 OS 隔离。

## 结果与排障

将 `YOUR_EVENT_ID` 换成提交时的 key：

```sh
oaxops_key=YOUR_EVENT_ID
oaxops_state="${XDG_STATE_HOME:-$HOME/.local/state}/oaxops"
cat "$oaxops_state/events/$oaxops_key/result.json"
journalctl --user -u "oaxops-event-$oaxops_key.service" -n 50 --no-pager
```

- `writer_busy=true`：前台 `open` 也占锁；空闲时退出前台，或等当前事件结束，再提交。不要删除锁文件。
- 缺少结果回执或 `status=uncertain`：先核对该事件的 `started.json`、`events.jsonl` 和日志，不换 key 重跑。临时单元结束后可能已被回收，结果以持久回执为准。
- 同 key、同正文返回原回执；改正文会被拒绝，失败或不确定回执返回非零。该轮 `completed` 不代替业务效果核验。

已有会话无需 `init`。只有明确授权新建且没有活动会话时才使用 `init --key ... --prompt ...`；保留原 thread 和历史回执。角色与交接见状态目录中的 `ROLE.md`、`organization-handoff.md`。
