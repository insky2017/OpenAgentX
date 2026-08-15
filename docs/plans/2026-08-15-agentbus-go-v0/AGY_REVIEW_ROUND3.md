# AGY Review Round 3：单行格式收尾

只修改 `AgentBus/internal/store/store.go` 第 156 行附近，删除 `checkQuery` raw SQL 第一行末尾的多余空格；不得改变 SQL 内容或任何逻辑，不修改其他代码文件，不暂存、不提交，不操作真实 panes。

完成后运行：

```bash
cd AgentBus
gofmt -l .
go test -count=1 ./internal/store
```

只简要汇报两条命令结果。
