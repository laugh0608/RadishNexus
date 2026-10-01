# 2026-09-14 首次访问创建管理员

基线：`f9086f3`（基础对象与首批成员配置）。项目所有者确认 [ADR-0027](../../adr/0027-first-visit-administrator-setup.md)，采用一次性初始化码与现有 Workspace owner。

## 交付

- 新实例首页在 Session 确认未登录后读取初始化状态。已配置码展示创建表单，未配置提示部署者处理，已有账户展示正式登录；数据库或 schema 检查失败不被视作空实例。
- `GET /api/v1/setup` 与 `POST /api/v1/setup` 使用精确 Host / Origin、可信代理、无缓存、严格字段和受限正文。POST 与登录共用 IP 次数和密码运算并发限制，不输出账户资料、Session 或初始化码。
- `RADISHNEXUS_SETUP_CODE_FILE` 输入部署专用随机码；服务只保留 digest，常数时间比较。文件或格式错误明确拒绝启动，码不进入数据库、备份、普通响应或日志。
- 复用正式 bootstrap 创建唯一账户、Workspace 和 owner；网页与 CLI 共享数据库事务锁。成功后清理表单中的码和密码，再由用户登录。模糊失败停止重放并要求重新读取状态。
- 任何账户已存在后关闭初始化；禁用账户、退出、刷新、服务重启或恢复备份均不提供重新初始化 / 重置管理员的后门。
- 共用产品页头与既有表单样式；补可选 Compose setup overlay、随机码生成 / 权限 / 移除步骤。没有新增依赖、migration、全局超级权限、默认 Team / Project / Channel 或自动 Session。

## 自动化验证

- Go 定向单元、HTTP 和路由测试通过，覆盖码校验与未配置、持久关闭、读取失败、文件边界及错误脱敏、Host / Origin、字段缺失 / 重复 / null / 正文超限、限流、readiness 拒绝、成功无 Cookie 和重复创建冲突。
- `go test -race ./...` 与 `go vet ./...` 通过。首次普通 HTTP 测试因沙箱禁止临时监听失败，获准本机测试监听后通过；未把环境阻止记为功能失败或成功。
- `./scripts/check-server-postgres.sh` 全部集成包通过。新建隔离空数据库：错误码拒绝；在凭证写入处注入约束失败后，账户 / 用户 / Workspace 整体回滚；两个 SetupService 与一个 CLI bootstrap 同时竞争，恰一成功、二冲突；首位用户正式登录并持有 owner；禁用账户与新服务仍为 complete。
- `./scripts/check-server-backup-restore.sh` 通过，真实备份恢复后的账户使 setup 状态保持 complete，再次 Complete 返回 AlreadyBootstrapped。
- Web `npm run check`：16 个文件、102 项测试通过，包含创建防双击、成功清理、模糊失败禁止重放、状态失败 / 未配置关闭表单、迟到响应保护、严格状态 DTO 和同源 POST。格式、Lint、TypeScript、production build 与 152 个锁定 package 检查通过。
- `docker compose -f deploy/compose.yaml -f deploy/compose.setup.yaml config --quiet` 在显式测试 origin / 测试文件参数下通过；只校验合并配置，没有运行完整 Compose 镜像构建或部署。

- `./scripts/check-repo.sh` 与 `git diff --check` 通过，仓库基线覆盖 297 个文件。

## HTTPS 浏览器验证

使用 `RADISHNEXUS_BROWSER_SETUP=1` 模式启动任务专属 PostgreSQL / Go / Caddy。只执行 migration，没有 bootstrap 或业务 seed；使用受保护的合成码文件和隔离 Chrome，只有测试 context 接受临时证书，未修改系统信任或日常浏览器。

1. 首次访问展示初始化表单；390px 手机无横向溢出，1440px 桌面和手机截图人工复核。
2. 错误码返回拒绝，码与密码输入清空；正确码创建首位管理员，初始化表单消失并转到正式登录。
3. 新邮箱 / 密码通过正式 Session 登录，首页显示新 Workspace 的 owner 与“创建团队与项目”；工作区没有预置项目。
4. 退出再刷新仍显示登录；只读状态 complete，重复 POST 返回 `409 setup_complete`，不生成第二账户或工作区。
5. 复用产品页头和按钮间距后，重启临时环境检查最终页面布局。后续纯 DTO 拒绝用例由 Web 自动化覆盖。

测试只使用虚构账户、密码与合成码，截图不含敏感输入。两次浏览器和 fixture 在验收后关闭，任务容器、卷、证书与临时测试文件清理；未对真实实例执行 migration 或部署。

## 剩余边界

完整 Compose 首访演练、Linux Secret 文件所有权 / ACL 的实际部署验证、生产证书和容量尚未执行。部署者需保证 app 的 `10001:10001` 能只读访问码文件；配置合并通过不代替真实挂载验证。管理员交接、账户恢复和独立实例级超级管理员不属于本次范围。

首次访问初始化与基础对象配置分别建立证据，不能据此宣称完整 Golden Path 或真实团队持续使用完成。下一顺位为最小 Markdown Document 合同审查。
