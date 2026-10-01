# 2026-09-14 基础对象与首批成员配置

基线：`d462bef`，串行在 `dev` 开发。项目所有者确认 [ADR-0026](../../adr/0026-foundation-configuration-and-membership.md)，另提出部署后首次访问创建管理员；后者形成 [ADR-0027 提议](../../adr/0027-first-visit-administrator-setup.md)，尚未实施。

## 实现结果

- owner 创建责任 Team 与 Project，明确本人为初始 Project admin；Project admin 创建普通 / restricted Channel，后者明确初始成员。
- Project 普通角色与受限 Channel 成员配置、有限字段成员选择、预期状态冲突检查、当前权限复核；管理员交接未开放。撤销 Project 角色清理从属 Channel / Thread 显式授权，重新加入和重放旧 receipt 不恢复历史授权。
- migration 009 新增不可变配置 Audit / receipt；Audit 记录初始授权、成员 before / after 和撤权清理范围。Project / Channel 创建的业务状态、事件、Outbox 与 Activity 同事务完成，成员名单不进入普通 Activity。
- 专用同源 HTTP、Session / CSRF、严格正文与 DTO；正式服务和浏览器 fixture 共用路由装配，发现 GET 和配置 POST 并存，Message 路由保持独立。
- Web 首页创建、角色 / 频道成员面板，显式授权和清理说明，重试复用 operation ID，分页与刷新清理旧内容。无新增依赖、默认对象或隐藏权限提升。
- 备份分类与恢复保留两张新表；恢复后配置重试返回原结果。旧 migration 未改写。

## 自动化证据

- `./scripts/check-server-postgres.sh`：最终版本全部集成包通过。新增测试从正式 bootstrap、登录与邀请起步，验证并发相同创建单胜者、payload 冲突、私密 Project 不可发现、非 admin 拒绝、初始 Channel 授权审计、成员发消息与私密 Thread、撤权清理、旧写请求拒绝、旧 receipt 不复权、重新授权不恢复从属私密权限、成员分页与目录边界、Audit 失败回滚、证据不可变及 Activity 重建一致。
- `./scripts/check-server-backup-restore.sh`：最终 migration 009 的非空配置 Audit / receipt 随真实备份恢复；权威表与 Activity 快照一致，恢复后相同配置 command 返回既有 Team。原有失败矩阵一并通过。
- `go test -race ./...` 与 `go vet ./...`：通过；后续路由 / DTO 收尾再次运行 `go test -race ./cmd/radishnexus ./internal/goldenpath ./internal/platform/httptransport` 与全量 vet，通过。
- Web `npm run check`：15 个测试文件、96 项测试通过；Prettier、Oxlint、TypeScript、production build 和 152 个锁定 package 来源 / integrity / 许可证 / lifecycle 检查通过。覆盖显式初始 admin、模糊失败复用 ID、只读权限不读取特权目录、刷新失权清空成员，以及安全 DTO。
- `./scripts/check-repo.sh` 与 `git diff --check`：通过，最终仓库检查覆盖 283 个文件。
- 修正既有 identity upgrade 测试的凭证创建时间：使用与登录相同的宿主时钟，避免数据库与宿主瞬时时差触发时间顺序约束；未放宽产品数据库 CHECK。

## 浏览器证据

使用 `RADISHNEXUS_BROWSER_FOUNDATION=1` 的任务专属 PostgreSQL、Go、Caddy HTTPS 与隔离 Chrome。该模式只通过正式 bootstrap 创建虚构 owner / Workspace，业务对象与成员授权全部使用正式 Web / API。临时证书仅在测试 browser context 接受，未修改系统证书或日常浏览器。

1. 空首页创建 Team、显式确认初始 admin、创建 restricted Project 与 restricted Channel。
2. 通过账户页发出一次性邀请，以新邮箱兑换并创建虚构成员账户；owner 分别授予 Project contributor 与 Channel membership。
3. 普通成员正式登录，从首页发现项目与频道，进入 canonical Channel 发送 Message；等待消息出现在历史列表后刷新，仍能读取数据库中的正文。
4. owner 撤销 Project 角色后，成员重新登录看不到 Project；直接打开旧频道路径返回不可读错误，历史与发送表单不显示。
5. 1440px 桌面与 390px 手机截图人工复核，配置页手机无横向溢出，按钮与角色选择可操作；消息页在手机正常单列换行。
6. Chrome 暴露 HTML `pattern` 对连字符的转义要求，已修复；重新启动 fixture 使用最终 production build，验证合法短标识通过、非法短标识拒绝。Go Web handler 在启动时载入 HTML，因此未把旧进程上的构建替换误记为新版本验收。

最终构建另复验 Team / Project / restricted Channel 创建与管理员本人无移除入口。两次隔离浏览器均已关闭，fixture 正常退出并清理各自容器、卷和临时证书。

自动化操作曾过早导航而取消尚未完成的请求；调整为等待已登录状态和历史列表结果后，重新执行上述步骤通过。此类被取消请求不记作成功。截图留在临时证据目录，仅含合成数据；不提交密码、邀请码或会话资料。

## 完成边界

本轮补齐“已 bootstrap 的空业务工作区 → 管理者配置 → 邀请新账户 → 普通成员发消息”。没有完成首次访问 Web 初始化、管理员交接、账号恢复、完整 Golden Path、生产升级或真实团队持续使用；没有重跑 Compose 部署或真实 Radish OIDC / Jenkins 联调。新邀请的浏览器证据不扩大为已登录账户兑换的全部组合。

ADR-0026 的完整验证矩阵是累计目标；本轮未逐一执行所有跨账户状态、归档、不同动作并发排列和成员分页的浏览器组合。底层既有权限测试继续有效，不能据此宣称生产容量或所有管理场景已验收。

没有提交、push、PR、真实实例 migration 或部署。首次访问入口待确认采用一次性初始化码和现有 Workspace owner，或单独设计实例级超级管理员范围。
