# 频道内可读讨论列表的技术验证

日期：2026-11-23

[ADR-0061](../../adr/0061-channel-thread-discovery.md) 已接入现有 PostgreSQL 来源关系、Go 发现接口和频道页。使用本机 HTTPS、生产构建的 Web、真实 Go 服务与一次性 PostgreSQL，通过两个隔离的合成账号驱动 Chrome。读者会话视口宽 430 像素。

| 场景 | 观察结果 |
| --- | --- |
| 来源是私密回复 | 只给读者目标 Thread 的成员资格时列表为空。再给父 Thread 读权后两条均出现，单独撤掉目标读权后只列出父讨论。 |
| 权限变化横跨真实页边界 | 在私密频道创建 28 条讨论并给读者授权，首页返回 25 条。撤掉首页末行和下一页中间一行后，原游标取得剩余 2 条。回退时重新填满 25 条，已撤权标题消失。每行响应只含引用与标题。 |
| 重新进入 | 从列表打开对应的 Thread，使用移动导航回到工作区，经过 Project 重新进入频道。展开列表与整页刷新后均从当前可读的首页开始。 |
| 第二页请求期间退出频道 | 自动化暂缓发出真实分页 GET，页面立即移除旧链接。读者通过成员管理预览并确认退出后放行请求，服务返回 `404 not_found`；页面进入频道不可读状态。 |

真实 PostgreSQL 测试另覆盖项目 viewer、归档频道、来源和目标权限独立、隐藏行位于可读行前后及中间、含大小写 ID 的分页、仅剩隐藏行时游标为空，以及所属 Project 撤权。HTTP 合同测试覆盖只读方法、缓存头、精确字段、游标作用域和错误响应。Web 回归还覆盖关闭面板后迟到的响应、会话失效及服务临时失败。[数据库用例](../../../server/internal/goldenpath/postgres/channel_threads_integration_test.go) · [HTTP 用例](../../../server/internal/platform/httptransport/discovery_test.go) · [Web 用例](../../../web/src/channel/ChannelThreads.test.tsx)

执行的仓库检查如下。

```bash
./scripts/check-server.sh
GOFLAGS='-run=TestChannelThreadsCurrentAccessAndPagination|TestMessageStartedThreadCurrentAccess -count=1 -v' ./scripts/check-server-postgres.sh
./scripts/check-web.sh
./scripts/check-repo.sh
```

Go race、vet、模块检查和两项定向 PostgreSQL 集成用例通过。Web 的 316 项测试、格式、Lint、类型、生产构建和依赖检查通过。Vite 仍报告主 JS chunk 超过 500 kB。浏览器流程使用仓库的[认证 Web 夹具](../../../scripts/run-authenticated-web-browser-fixture.sh)。
