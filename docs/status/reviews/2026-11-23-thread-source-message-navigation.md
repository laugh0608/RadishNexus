# 从讨论返回原消息的技术验证

日期：2026-11-23

两个隔离的合成账号经本机 HTTPS 打开生产构建的 Web。一个账号在私密频道发起讨论，再从该讨论里的一条回复发起子讨论。另一个账号在 430 像素宽的 Chrome 中通过工作区、项目和频道讨论列表进入。授权与撤权使用成员管理的预览和确认接口。[ADR-0062](../../adr/0062-thread-source-message-navigation.md)

| 场景 | 实测结果 |
| --- | --- |
| 顶层来源 | 键盘激活「打开原消息」后，频道定位卡片显示发起讨论的那条消息，地址中的消息 ID 与来源一致。 |
| 回复来源 | 从子讨论打开来源后，浏览器进入父讨论，定位卡片显示原回复正文。后退直接回到子讨论。 |
| 撤销父讨论读权 | 正在显示的原回复在重新校验后清除。后退、刷新子讨论后标题仍在，来源行只有受限提示。完整详情和 summary 的来源均严格等于 `{"visibility":"restricted"}`，无来源消息或父讨论标识。 |
| 重访旧链接、恢复读权 | 旧频道定位地址的单条消息 GET 返回 `404`，卡片没有正文；频道讨论列表也不再展示子讨论。重新授权父讨论后，来源链接恢复。 |
| 单独撤销子讨论读权 | 重新打开子讨论被拒绝。仍有父讨论读权的成员可以打开父讨论中的原回复。 |

真实 PostgreSQL 用例同时核对完整、summary、单条消息和成员撤权后的读权。Go 与 Web 回归检查来源缺失、重复、受限占位带字段等错误形状。项目历史 Thread 沿用无频道、无来源的读取形态。[PostgreSQL 用例](../../../server/internal/goldenpath/postgres/message_started_thread_integration_test.go) · [HTTP 用例](../../../server/internal/platform/httptransport/collaboration_test.go) · [Web 合同](../../../web/src/collaboration/api.test.ts)

```bash
./scripts/check-server.sh
GOFLAGS='-run=TestChannelThreadsCurrentAccessAndPagination|TestMessageStartedThreadCurrentAccess -count=1 -v' ./scripts/check-server-postgres.sh
./scripts/check-web.sh
./scripts/check-repo.sh
```

Go race、vet 和模块检查通过。两项定向 PostgreSQL 测试、318 项 Web 测试、格式、Lint、类型、生产构建及依赖检查通过。Vite 报告主 JS chunk 超过 500 kB。
