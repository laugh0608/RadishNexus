# staging Deployment 正式记录实施与验收

日期：2026-09-26

## 范围与结果

项目所有者确认 [ADR-0031](../../adr/0031-session-scoped-staging-deployment-recording.md) 后，已将既有 `RecordStagingDeployment` 接到正式 Session API 和 React 页面。从成功 CI Run 打开表单，仅选择当前持有独立授权的 active staging 环境，明确填写外部已完成的部署结果与时间，确认后进入既有 Deployment 页面。没有实际部署调用，也不会把成功构建自动记为部署成功。

新增目标分页查询与写入端点，沿用 Session / CSRF / Origin、当前读取权限和独立环境授权。migration 011 扩展既有 collaboration receipt 的 CHECK 约束；不新增业务表或依赖，不修改历史 Deployment。业务事实、来源关系、事件、Outbox、Activity 和 receipt 在同一事务内提交。相同操作与内容重试返回原结果；更改内容、不同操作或不同用户重复同一 Environment / CI Run 仍冲突。

前端不预选结果或时间；修改事实会取消确认，发送期间防止重复点击。请求失败后保留原操作 ID 与冻结的 payload，只允许显式重试，不自动生成新操作或乐观展示成功。重试材料仅保存在当前页面内存中；关闭或刷新丢失材料不代表撤销已提交事实。

关键入口：[服务端说明](../../../server/README.md)、[Web 说明](../../../web/README.md)、[HTTP transport](../../../server/internal/platform/httptransport/staging_deployment.go)、[事务实现](../../../server/internal/goldenpath/postgres/deployment.go)、[表单](../../../web/src/nexus-view/StagingDeploymentForm.tsx)。

## 自动化证据

以下检查实际执行通过：

| 检查 | 覆盖范围 |
| --- | --- |
| `./scripts/check-server.sh` | Go race 测试、vet、模块验证及依赖文件一致性 |
| `go test ./cmd/radishnexus`（server 目录） | 后续补充的正式路由装配回归 |
| `./scripts/check-server-postgres.sh` | migration 011、真实 Session HTTP 三种部署终态、顺序 / 并发精确重试、内容 / 操作 / 用户冲突、当前授权、分页与唯一事实 |
| `./scripts/check-server-backup-restore.sh` | 备份恢复后继续读取既有 Deployment，精确 receipt 重试仍返回相同结果 |
| `./scripts/check-web.sh` | 格式、Lint、类型、21 个测试文件共 131 个测试及生产构建 |
| `./scripts/check-repo.sh`、`git diff --check` | 仓库约束、文档链接和文本卫生 |

新增真实数据库测试位于 [staging_recording_integration_test.go](../../../server/internal/goldenpath/postgres/staging_recording_integration_test.go)。并发重试只有一次新写入；撤销环境授权、归档环境、暂停 Workspace membership 先持有修改锁时，写入等待后拒绝，未留下 Deployment 或 receipt。翻页重新检查授权，owner / Project 角色不会隐式授予环境权限，production 与失败 / 取消的来源构建不能通过记录检查。

响应丢失恢复采用“丢弃首次 HTTP recorder 结果，再发送相同请求”验证提交后重放；没有在浏览器或真实网络中注入 TCP 中断。HTTP 单元测试覆盖严格字段、重复键、类型、时间精度、体积、Session / CSRF / Origin、游标作用域与投影验证。Web 测试覆盖确认重置、重复点击、冻结重试、读取失败恢复、空状态、分页和 Session 失效。

## 隔离浏览器证据

项目所有者单独授权临时本机服务、隔离 PostgreSQL 和浏览器后，运行 `RADISHNEXUS_BROWSER_STAGING=1 ./scripts/run-authenticated-web-browser-fixture.sh`。使用现有 Chrome 与缓存 Playwright CLI，无新依赖安装。Go 和 HTTPS 入口只监听本机，数据库为一次性虚构 fixture；该模式不预置 Deployment，由正式页面首次创建。

1. 使用虚构成员通过实际登录页面建立 Session，从正式 CI Run 页面加载已授权环境。
2. 检查 1280 × 1000 桌面与 390 × 844 手机截图；表单标签、确认摘要和按钮可读，手机没有横向溢出。
3. 表单默认不选择环境、结果或时间；确认前无法提交，改变结果后勾选重置。键盘 Tab 可到确认按钮，Enter 完成提交。
4. 实际记录 failed Deployment，跳转正式页面并刷新后仍为同一失败事实，包含一项来源关系与一项 Timeline；来源 CI Run 仍显示构建成功。
5. 在本轮一次性数据库撤销虚构成员的环境授权；重新读取后环境列表为空、提交禁用。已有 Deployment 仍按读取权限展示。

本轮隔离浏览器允许测试证书错误，没有修改系统证书信任；fixture 就绪检查使用临时 CA 验证 HTTPS。此证据不替代生产 TLS 配置或真实用户持续使用。

验收后关闭本轮浏览器，停止临时 Go 服务并清理本轮 PostgreSQL、Caddy、代理卷、状态文件和截图 / DOM 临时工件。其他历史测试卷不在本轮清理范围。未启动 Jenkins，未修改业务数据库或 Jenkins 持久化目录。

## 剩余边界与下一步

本轮证明已有配置和授权下的正式记录流程。Environment / Component 配置、环境授权管理、Repository 映射、交付反向关系与 Jenkins 持续采集仍未闭环，不能据此宣称普通成员可从空 Workspace 独立完成整个 Golden Path。桌面原生中文 IME 的人工验收仍按所有者要求暂缓。

下一步优先明确最小 Environment / Component 配置与显式环境授权管理的权限、审计和 API 范围，再单独确认实施。本轮没有应用 migration 到业务实例；使用本版本的实例需通过既有显式迁移流程应用 011，schema readiness 不允许旧 schema 冒充可用。
