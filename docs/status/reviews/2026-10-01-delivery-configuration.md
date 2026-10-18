# Component、Environment 与环境授权管理实施记录

日期：2026-10-01

实现基线：`efa002b`；提案提交：`a206be5`。所有者在本轮批准 [ADR-0032](../../adr/0032-component-environment-configuration-and-authorization.md) 并要求继续实施。本文区分实现、自动化、隔离浏览器和未验收范围，不作为业务实例迁移或远程操作授权。

## 交付范围

- 复用既有配置 service、事务、成功 Audit / receipt，新增 Component / staging Environment 创建、成员发现与配置读取、owner 环境授权管理。无新依赖、通用命令 API 或独立权限框架。
- 对象创建要求当前 active Workspace owner，责任 Team 必须属于同 Workspace。创建不自动授权；本人也须独立确认。普通成员可发现历史生命周期 / 分类对象，但不能读取 owner 管理目录。
- migration 012 用 active 部分唯一索引及不可变 generation 支持撤销后重新授予；旧授权、授予来源与历史 Deployment 外键不改写。expected ID / 状态阻止旧页面覆盖，旧 receipt 只确认原请求已处理。
- 授予、撤销、Audit 和 receipt 原子提交；创建同时写入事件、Outbox 与 Activity。Activity v3 加入安全创建事件，授权成员明细不进入普通活动。Deployment 在事务内新增账户状态检查，和新授权命令统一账户 → membership → Environment → 授权锁序。
- 正式 Web 首页加入“组件与环境”，复用 Team / 成员选择、分页与配置样式。结果不明的授权操作冻结原请求，精确重试后重新读取；撤销不隐藏历史 Deployment。

对应实现入口见 [服务端说明](../../../server/README.md#component--environment-配置与环境授权) 与 [Web 说明](../../../web/README.md#组件与环境)。本轮不增加改名、归档、删除、完整 Nexus View、Repository 映射、持久 Jenkins source、production 或执行器。

## 自动化与真实数据库证据

| 检查 | 实际结果与范围 |
| --- | --- |
| `./scripts/check-server.sh` | 通过：Go race、vet、tidy diff、module verify。沙箱首次因临时 TLS 监听受限失败，按授权在沙箱外重跑通过 |
| `./scripts/check-server-postgres.sh` | 最终通过：一次性 PostgreSQL 全集成检查，含下面的新增合同与竞争用例 |
| `./scripts/check-server-backup-restore.sh` | 通过：两容器空目标恢复，保留两代授权和 Deployment 原引用，恢复后可追加第三代；既有失败矩阵仍通过 |
| `./scripts/check-web.sh` | 通过：格式、Lint、23 个测试文件 / 140 项测试、TypeScript、生产构建和 152 个锁定 package 的既有依赖基线 |
| 仓库检查 | `./scripts/check-repo.sh` 通过（393 files checked）；`git diff --check` 与 Go 格式检查通过 |

主要新增证据：

1. 由正式 bootstrap 与邀请建立 owner / member，再通过正式命令创建 Component、Environment 和授权。证明创建重试、key 冲突、普通成员发现、无隐式授权与 owner 也不能绕过显式记录权。
2. 并发首次精确授予、旧 expected 冲突、撤销再授予、旧授予不复权、旧撤销不影响新代次；两次 Deployment 分别保留原授权 ID。禁止修改 generation、复活或删除 revoked 历史。
3. 双 owner 同时互授、本人重复授予的 `changed=false` Audit，以及操作者降权后不能读取旧成功 receipt。第二 owner 的晋升仅是隔离 fixture，不是新增管理权交接入口。
4. 正式撤销与 Deployment 的两种确定性锁顺序：先取得记录锁则允许该笔事实提交，撤销在后；撤销先持锁则后续记录被拒绝。旧 Deployment receipt 在撤权后仍被拒绝。另测事务内账户禁用、membership 暂停和 Environment 归档的等待 / 重查。
5. Audit 故障注入使授权状态和 receipt 一起回滚；归档 staging / 禁用成员可清理授权。恢复账户后，历史 Deployment 仍按共享读取合同可读。归档与禁用使用受控 fixture SQL，不冒充产品入口。
6. 旧 001～011 数据升级到 012，既有授权变为第一代并保持来源；旧配置 digest / receipt 可合法重试；Activity v2 元数据升级后正常读，v3 正常写入与全量重建一致。新旧授权各自关联历史事实。
7. HTTP 严格嵌套 expected、重复 / 未知字段、CSRF、游标作用域、路由注册及 DTO 白名单；Web 显式自授权、重复点击、模糊结果原请求重试、403 清空、归档只撤销、普通成员不读管理目录和迟到响应丢弃。

并发补测曾因复用 fixture 的 `usr_admin` 只是普通 Workspace member 而失败；显式补齐该测试的 owner 前提后通过，未放宽产品权限。测试屏障和故障约束仅存在于一次性数据库。

## 隔离 HTTPS 浏览器

使用现有 Chrome 与已缓存 Playwright CLI，无依赖安装。`RADISHNEXUS_BROWSER_FOUNDATION=1` fixture 通过正式 bootstrap 提供空业务工作区；浏览器经真实 HTTPS、Session / CSRF 和 PostgreSQL 操作，未 mock 配置 API。

- 登录 → 创建责任 Team → 明确选择类型并创建 Component → 回读列表和详情。
- 创建 staging Environment，页面明确显示尚未授权任何成员。
- 选择本人、独立勾选影响确认后授予；撤销后回读 revoked，再次授予后回读 active。
- 最终工件在 1440×1000 与 390×844 视口复核；手机确认内容与稳定 ID 能换行，没有水平溢出。使用 Space、Tab、Enter 完成确认与撤销。
- 发现并修复 HTML pattern 在浏览器 Unicode sets 模式下的连字符转义问题；修正授权确认框继承普通输入框尺寸及误用列表样式名。修复后重新启动 fixture、创建环境并授予，控制台只保留登录前预期的 Session 401。

截图保留在本地忽略目录 `output/playwright/delivery-desktop-final.png` 与 `output/playwright/delivery-mobile-final.png`，不包含真实用户或业务数据。首次 fixture 在 30 分钟时限后自动清理；后续 fixture 通过 stop 文件正常退出。专用浏览器会话已关闭，临时 PostgreSQL、Caddy、卷和测试状态由脚本清理。本轮使用隔离浏览器的 `ignoreHTTPSErrors` 连接一次性本机证书，没有修改系统信任，因此不将其当作证书部署验收。

## 证据边界与后续

本轮新配置对象接续成功 CI Run 和 staging 记录由真实 PostgreSQL 自动化证明，其中 CI Run 来自受控 `VerifiedJenkinsDelivery` 测试输入。未重新运行真实 Jenkins 构建，未复验“新建对象 → 真实 Jenkins → 浏览器记录”的整链；既有 Jenkins HMAC 与三态联调证据仍引用 [9 月 26 日记录](2026-09-26-real-jenkins-lab.md)。

新入口尚无真实团队持续使用证据，也没有新增普通成员构建列表、Repository 映射或持久来源管理。原生中文 IME 人工验收按所有者要求继续暂缓；程序填充中文名称不代表输入法验收。当前完成不构成 M0.5 / M1 退出。

后续从 Repository 映射、交付关系与持续采集中选择下一窄切片并先冻结合同。业务实例应用 migration 012 需独立授权，Go / Web / schema 须配套更新；不能删除授权历史恢复旧唯一约束。未 push、创建 PR、发布或部署。
