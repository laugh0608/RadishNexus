# 从频道原消息找回讨论的技术验证

日期：2026-11-23

本批按 [ADR-0060](../../adr/0060-message-started-thread-lookup.md) 接通 PostgreSQL、Session GET 和频道 Web。复验使用本机 HTTPS、生产构建的 Web、真实 Go 与一次性 PostgreSQL。Playwright 驱动两个隔离的 Chrome 会话，第二个会话使用 430 像素宽视口。

| 场景 | 实测结果 |
| --- | --- |
| 创建、刷新与重进 | 从工作区选择 Project 和频道，发送一条合成消息并发起私密 Thread。刷新后，历史加载没有逐条请求讨论。在原消息点击「查看讨论」，GET 返回创建时的引用和标题。离开频道，再从工作区重新进入，链接打开同一个 Thread。原消息定位卡片也能打开该 Thread。 |
| 已读源消息、私密目标 | 第二个账号能读频道。加入私密 Thread 前，查询原消息与查询尚未发起讨论的消息得到完全相同的 `200` 和 `{"data":{"thread":null}}`。管理者通过 Thread 成员面板将它加入后，原页面重新查询即可打开讨论。 |
| 撤权后重新读取 | 管理者通过成员面板移除第二个账号。该账号对顶层源消息的 GET 返回空结果；原消息卡片移除讨论标题、ID 和链接。刷新后结果一致，直接打开之前的详情地址也无法读取。创建者仍能打开。 |
| 来源是私密回复 | 第二个账号先在频道里看到已授权的回复，撤权后在该回复上点击「查看讨论」得到 `404 not_found`。频道重新读取可读历史并移除该回复正文和所属 Thread 引用。 |

数据库用例同时覆盖 viewer、跨 Workspace / Channel、归档历史，以及一条回复作为另一个 Thread 的来源。HTTP 测试检查只读请求的 Session、缓存头、精确输出、无效投影和数据库故障。Web 回归覆盖空态、权限变化后晚到的响应，以及来源失权后的频道重读。[数据库测试](../../../server/internal/goldenpath/postgres/message_started_thread_integration_test.go) · [HTTP 测试](../../../server/internal/platform/httptransport/channel_messages_test.go) · [频道交互测试](../../../web/src/channel/ChannelPage.test.tsx)

本次执行了以下检查：

```bash
./scripts/check-server.sh
GOFLAGS='-run=TestMessageStartedThread -v -count=1' ./scripts/check-server-postgres.sh
./scripts/check-web.sh
./scripts/check-repo.sh
```

Go race、vet、模块检查与定向 PostgreSQL 检查通过。Web 的 309 项测试、类型检查、Lint、生产构建和锁定依赖检查通过。Vite 仍提示主 JS chunk 大于 500 kB。浏览器夹具补挂了正式服务已有的成员管理路由，最终授权和撤权均通过同源 Web 执行。[浏览器夹具](../../../server/internal/goldenpath/postgres/authenticated_web_browser_fixture_test.go)

## 从频道回复返回所属讨论

同日的后续 Web 切片复用频道消息已有的 `thread` 引用。两个隔离的 Chrome 会话通过正式页面创建私密回复，再从该回复发起另一个私密讨论。310 项 Web 测试、生产构建、锁定依赖检查、真实 PostgreSQL 的 `TestMessageStartedThreadCurrentAccess` 和仓库检查通过。

| 场景 | 实测结果 |
| --- | --- |
| 两个讨论分别可达 | 430 像素宽的频道页通过 SSE 收到回复。键盘激活「打开所属讨论」直接进入原 Thread，全程没有调用查找新讨论的 GET。刷新频道后，「查看此回复发起的讨论」按需读取另一个 Thread，打开后标题与路径均匹配。 |
| 分别撤权 | 只加入所属讨论的成员能返回原 Thread；在回复上查询另行发起的讨论时得到空结果。通过成员面板加入后两个讨论均可打开。撤销新讨论权限后，其旧地址拒绝访问，所属讨论仍可打开。再撤销所属讨论权限后，原链接的详情请求返回 `404`，浏览器后退和刷新都不再展示这条回复或其链接。 |
| 恢复历史页 | 在 Chrome 中显式触发 `pageshow.persisted`，观察到频道重建实时连接并按当前权限读取历史。Vitest 还复现了恢复前的分页请求晚到，修复后该响应无法把失权回复重新插回页面。 |
