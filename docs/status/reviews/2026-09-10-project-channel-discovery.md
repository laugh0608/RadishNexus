# 2026-09-10 Project / Channel 发现入口

基线：`d0e8c94`（schema readiness 已按项目所有者要求提交）。本切片继续推进首页最小使用入口，范围见 [ADR-0025](../../adr/0025-project-and-channel-discovery.md)。

## 已完成

- 两个正式只读 GET 列表：Workspace 内可读 Project、指定 Project 内可读 Channel。返回安全引用、标题与归档状态，不返回权限、成员、隐藏对象或总数。
- 当前权限过滤先于稳定 ID keyset 分页；默认 25、上限 50，严格 query、作用域绑定 cursor、active Workspace membership、restricted Project / Channel 显式成员边界。
- 首页提供 Workspace → Project → Channel 导航，不要求输入 ID。原已知 ID 工具保留为次级入口；账户与邀请继续使用既有页面。
- 列表分页、空态、失败、重试、归档说明，以及作用域切换 / 翻页 / 焦点复核的旧数据清理；取消请求和迟到响应不覆盖当前作用域。Project 失去访问权时重新加载父级列表，Session 失效回到登录。
- 首页从 `App.tsx` 移入 `workspace/`，按页面、列表状态和 API 合同分工；正式服务和浏览器 fixture 同步装配发现入口，fixture 也补齐既有本地账户与邀请 handler。
- 浏览器复核后将首页欢迎区收紧为顶部区域，统一列表按钮、焦点与禁用样式；桌面双列、手机单列，保留次级 ID 入口。

## 已执行验证

- `./scripts/check-server.sh`：Go race、vet、模块图一致性与模块校验通过。
- `./scripts/check-server-postgres.sh`：真实 PostgreSQL 全部集成包通过。覆盖未授权 Workspace、restricted Project / Channel、owner / admin 不穿透、归档读取、空列表、分页过滤、旧 cursor、成员删除 / 暂停和 Session 撤销；列表可读项与既有 Channel 读取权限核对。最初 fixture 缺少账户创建时间，修正后全套通过。
- `GOFLAGS=-v ./scripts/check-server-postgres.sh`：记录代表性分页开销。各 100 个候选对象、半数不可见、每页返回 25 项：Project 为 4 次 SQL、约 3.04 ms；Channel 为 6 次 SQL、约 3.58 ms，包含事务与权限检查。该结果只针对本地临时 PostgreSQL，不构成生产容量或延迟承诺。
- `./scripts/check-web.sh`：91 个测试通过，格式、Lint、类型检查、production build 与 152 个锁定 package 检查通过。新增测试覆盖无需 ID 导航、分页替换、Workspace / Project 切换、迟到响应、焦点复核、失败清空、过期 Session、归档和无 Workspace 状态，以及严格响应合同。
- `go test -tags='integration browserfixture' -run '^$' ./internal/goldenpath/postgres`：浏览器 fixture 编译通过，只编译、不代表浏览器行为验收。

## 浏览器与剩余边界

项目所有者确认后，使用现有浏览器 fixture、临时 PostgreSQL / Caddy / Go 服务与隔离 Playwright Chrome 会话完成真实同源 HTTPS 验收。只在该测试 context 接受临时自签证书，没有修改系统证书或日常浏览器配置，也没有安装依赖。布局调整后重启 fixture，使用最新 production build 重新验收。

- 虚构账户通过邮箱登录，首页选择 Project 后展示普通、受限及归档 Channel；从列表进入普通 Channel 后显示既有消息和实时连接状态。
- 仅向临时数据库增加各 30 个 Project / Channel 分页样本：Project 首页 25 项、第二页 6 项；Channel 首页 25 项、第二页 7 项。浏览器断言第二页替换旧页，返回项目第一页后仍可继续导航。
- 删除临时用户的受限 Channel membership 后刷新列表并检查两页，原受限 Channel 不再出现；普通 Channel 仍可进入。切换 Workspace 后旧 Project 清空，展示另一个 Workspace 的 Project / Channel；无 Channel 的测试 Project 显示空态。
- 检查 1440px 桌面与 390px 手机完整截图；320px、390px 的页面 `scrollWidth` 均等于 viewport 宽度。桌面双列、手机单列，按钮和分页可见。截图与快照保存在 Git 忽略的 `output/playwright/discovery/`，不是正式产品资产。
- 退出登录后关闭隔离浏览器，停止 fixture；两次临时环境均正常退出并自动清理数据库、Caddy、卷和临时证书。布局调整后的 `./scripts/check-web.sh` 全部通过。

本次只补充邮箱登录和首页发现入口的浏览器证据；不将其扩展为邀请全流程、账户恢复或真实 Radish OIDC 联调证据。分页样本通过临时 fixture 数据库准备，不能作为产品创建功能的证据。

没有新增依赖、migration 或对真实实例操作；没有 push。Project / Channel 创建、owner Team、Project 角色与受限 Channel 成员配置、其他协作对象列表仍未完成，因此不能宣称全新实例无需运维配置即可开始团队协作。Radish OIDC 继续属于未来规划。
