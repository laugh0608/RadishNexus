# 项目决策状态筛选验证

2026-11-23，两个隔离的合成账号经本机 HTTPS 访问生产构建的 Web。项目管理员在受限项目的私密 Thread 中创建 32 个提案，经正式命令接受、拒绝和替代其中部分提案。另一个账号只有项目 viewer 权限，在 430 像素宽的 Chrome 中从工作台进入项目决策页。[ADR-0063](../../adr/0063-project-decision-status-filter.md)

| 浏览器操作 | 实测结果 |
| --- | --- |
| 从全部切到待确认，再选其他三种状态 | 全部列表和待确认列表的第一页各显示对应顺序的 25 条。四种状态的行与按 ID 排序的预期集合一致。 |
| 翻页前接受待确认的第 25、27 条 | 下一页仅显示原第 26、28 条，已到末页。回到上一页后，页面重新读取当前仍待确认的前 25 条。 |
| 复用游标 | 旧的无筛选游标仍可续页。把筛选游标用于另一状态、用于全部，或把无筛选游标用于待确认，均返回 400。 |
| 刷新筛选列表、打开详情并重新进入 | 页内刷新保留待确认。列表行仅含 `ref / title / status`，详情显示受限来源占位。由详情重新进入项目列表时恢复全部状态。 |
| 翻页期间撤销 viewer 的项目成员资格 | 浏览器在请求发出前暂停翻页，旧行已清除。管理员完成正式移除命令后放行原请求，服务端返回 404。页面显示不可访问提示，旧项目名与决策标题均已清除。 |

## 分层回归

```bash
./scripts/check-server.sh
GOFLAGS='-run=TestProjectDecision -count=1 -v' ./scripts/check-server-postgres.sh
./scripts/check-web.sh
./scripts/check-repo.sh
```

Go 检查覆盖 race、vet 与模块一致性。HTTP 合同回归逐字节核对无筛选游标，检查非法参数、游标作用域及筛选结果中的错状态。真实 PostgreSQL 覆盖私密来源、状态变化后的空尾页及翻页撤权。在 3 个项目、1,227 个决策的合成数据中，8 组首页、尾页及空页读取各执行 11 次 SQL 调用。此测量仅覆盖本机夹具。[HTTP 测试](../../../server/internal/platform/httptransport/project_decisions_test.go) · [数据库测试](../../../server/internal/goldenpath/postgres/project_decisions_integration_test.go)

Web 的 320 项测试、格式、Lint、类型、生产构建和依赖检查通过。切换筛选时，延迟到达的上一页响应无法恢复旧行。Vite 仍提示主 JS chunk 超过 500 kB。[页面测试](../../../web/src/workspace/ProjectDecisionsPage.test.tsx)
