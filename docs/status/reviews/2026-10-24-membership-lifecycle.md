# 2026-10-24 成员生命周期与管理交接

对应 [ADR-0045](../../adr/0045-membership-lifecycle-and-management-handoff.md)。所有者确认 D01～D08 后，在 `dev` / `5cd7946`（schema 019）上连续实施。本轮没有提交、push、新依赖或真实实例迁移；授权范围为本机合成数据、专属 PostgreSQL、回环 HTTPS、隔离 Chrome 与故障注入。

## 实施结果

- migration 020 增加 `removed`、生命周期 / 管理代次、Workspace 会话有效起点、私密对象局部 manager 和绑定退出代次的再邀请；保留作者、正文、来源和历史身份。
- Workspace 暂停保留尚存协作授权，恢复需重新认证；正式退出清空对象授权，重新加入从普通 member 开始。两者均撤销 Deployment 授权，恢复不自动重授。
- Project 多 admin、降级 / 移除 / 交接，restricted Channel / Thread 局部成员和管理交接接入正式页面。正常退出保护剩余有效管理者；安全暂停可暂时失管，不向 owner 转交私密内容。
- 管理预览、确认摘要、原操作幂等、当前权限过滤的 Audit 与退出后本人结果查询接通。旧成员 PUT / DELETE 返回 410，原创建与读取保留；可信进程内旧删除亦检查下属管理责任。
- 所有现有 Workspace 用户业务入口统一事务准入：schema 共享锁在前，普通业务共享、权限写入排他 Workspace 协调；复核账户 / Session / membership 代次，锁等待预算 5 秒。SSE 每次输出与 15 秒 heartbeat 复权。
- 019→020、权威备份、旧 019 工件恢复后显式升级和 `.nexus` 导入互操作已覆盖。协作包 profile 不变，当前导出来源标记为 `schema-020`；历史身份不进入选任、恢复或再邀请。

## 实际验证入口

| 命令 / 入口 | 结果与范围 |
| --- | --- |
| `env GOPROXY=off GOTOOLCHAIN=local ./scripts/check-server.sh` | Go 单元、race、vet、`go mod tidy -diff` 和模块校验通过；未下载依赖 |
| `./scripts/check-server-postgres.sh` | 完整 PostgreSQL 集成回归通过；包含现有业务、migration、身份和新生命周期合同 |
| `./scripts/check-server-membership.sh` | 成员专项通过；常规运行显式跳过需人工驱动的浏览器 fixture |
| `GOFLAGS='-run=^TestMembershipStaleHandoffAndSecurityPause$\|^TestMembershipPrivateManagementAndLifecycle$\|^TestMembershipImportedIdentityBoundary$ -v -count=1' ./scripts/check-server-postgres.sh` | 最后补充的旧交接计划、唯一管理者暂停、内部撤权保护和导入边界通过；运行时 Go 正则使用普通 `|` |
| `./scripts/check-server-backup-restore.sh` | 同版本整库恢复、成员生命周期、导入来源、owner 交接、账号恢复和恢复故障通过 |
| `RADISHNEXUS_MEMBERSHIP_LEGACY_REF=5cd7946 ./scripts/check-server-backup-restore.sh` | 从本地 Git 编译真实旧 019 工件；新工件拒绝直接恢复旧备份，旧工件空目标恢复后显式升级 020 通过；独立可选旧 017 工件项未在此命令启用 |
| `cd web && npm run check` | Prettier、Oxlint、38 个测试文件 / 243 个测试、TypeScript、production build、152 个锁定 package 检查通过 |
| `./scripts/check-server-membership.sh --browser-review` | 正式生产 handler、真实 PostgreSQL、构建 Web、隔离 HTTPS / Chrome；双身份流程及最终持久状态断言通过 |
| `./scripts/check-repo.sh`、`git diff --check` | 仓库合同、文档链接与文本卫生通过（586 个文件） |

测试中的账号、密码、邮件地址、数据库与操作对象均为合成数据。没有把真实凭据、浏览器配置或未脱敏业务输出写入仓库。故障 fixture、规模 seed 与正式业务命令的证据分别记录，不把 SQL 造数当作用户页面闭环。

## G01～G22 对照

下表“通过”指所列自动化 / Agent 浏览器证据，不等于独立操作者或团队持续使用通过。

| 项 | 结果 | 实际证据 |
| --- | --- | --- |
| G01 | 通过 | `newMembershipFixture` 使用正式 bootstrap、邀请兑换、配置与管理 service；Project admin 增补 / 降级、Project 和私密 handoff、原管理员退出由真实事务完成。浏览器 owner / member 从正式登录进入页面，不手改最终权限 |
| G02 | 通过 | `ConcurrentLastAdminAndReadSnapshot` 两个 admin 同时退出仅一笔成功；`ResponsibilitiesAndArchivedHandoff` 最后 admin 不可退出；`StaleHandoffAndSecurityPause` 最后 Workspace owner 退出拒绝 |
| G03 | 通过 | 提升 Project admin 后仍看不到未加入的私密 Channel；viewer manager 不能改 Project 角色或发送 Message。管理身份不进入原业务角色 / Deployment 授权判定；既有 Decision、Deployment 负权限回归保留 |
| G04 | 通过 | 私密 Thread 添加缺失来源 Channel 的候选拒绝；Channel 和 Thread 明确分开增补、handoff；新 manager 与历史 creator 分离。导入 / 升级测试另查历史作者不变 |
| G05 | 通过 | `StaleHandoffAndSecurityPause` 覆盖目标暂停后恢复、父级移除后重加、操作者降级；旧 handoff 均拒绝。Project A→B→A 与双标签旧计划同样冲突 |
| G06 | 通过 | Project / Channel 清理下属窄授权，隐藏 Thread 只剩目标管理者时拒绝。公开管理和可信旧内部删除共用责任检查，错误不带隐藏对象明细 |
| G07 | 通过 | `PrivateManagementAndLifecycle`、`ScopedSessionsAndDeploymentRevocation` 验证暂停、恢复、重新认证；暂停期间撤销的 Channel 授权与 Deployment 授权不恢复，尚存 Project / manager 身份可恢复 |
| G08 | 通过 | 唯一 Project admin / 私密有效管理者安全暂停成功，owner 仍无法读其私密对象；恢复原负责人后管理可用。本人暂停和最后 owner 退出受保护 |
| G09 | 通过 | 正式离开后全部 Project / Channel / Thread 授权为零；原 Message 正文和稳定 user ID 保留。再次加入状态 active、role member，旧 Project 仍不可读 |
| G10 | 通过 | 通用邀请不恢复 removed，专用邀请拒绝其他用户和旧退出代次；旧 owner 生命周期邀请在 local accept 与 OIDC 完成数据库事务都拒绝；OIDC 仅数据库 fixture，未联调真实 provider |
| G11 | 通过 | 同账户其他 Workspace 仍可用；本 Workspace 旧 Session 和迟到准入代次在恢复后继续拒绝；正式再登录建立新准入。Web 不把 Workspace 重新认证要求变成全账户退出 |
| G12 | 通过 | `PauseCompetition` 用审计屏障观察真实锁等待，分别与邀请兑换、owner handoff、局部角色、Deployment grant、密码恢复及 logout 竞争；前四项不对已暂停目标成功授予，后两项可完成且旧准入不可用 |
| G13 | 通过 | 既有共享授权事务先完成，排他停权随后提交；重复读旧快照及迟到 Session 均拒绝。浏览器已有 SSE 在停权 heartbeat 后清空正文 / 草稿 / 管理页，恢复不复活旧连接 |
| G14 | 通过 | 归档 Project 不新增普通受众，已有成员可接任并安全退出；migration 不为失效 / disabled / 无父级权限的旧 creator 自动授 manager，不让 owner 自动接管 |
| G15 | 通过 | `PlanRevisionAndHiddenAudit` 证明私密成员变化不改变 Project 计划，Project audit 不混入私密操作；无私密读取者不能查询对应 audit。管理 / 责任列表过滤后分页，无隐藏总数 |
| G16 | 通过 | 本人退出后全局 Session 查询 applied；他人同 operation 为 not_observed，改 payload 复用原 operation 冲突。浏览器真实 HTTP 提交成功后丢弃响应，再到账户页查到原成功 |
| G17 | 通过 | Audit / receipt SQL 失败、context cancel、PostgreSQL backend 终止、真实测试子进程终止均不残留部分状态；TCP 代理丢弃实际 COMMIT 确认后，通过独立结果查询核对已提交事实 |
| G18 | 通过 | `MembershipUpgradeFrom019`、`BackupRestoreMembershipLifecycle`、`BackupRestoreMembershipLegacy`；检查代次、局部管理、权限、审计、历史身份与 Session / invitation 排除；没有对真实实例演练 |
| G19 | 通过 | 正式导出 / 导入 / 再导出后负责人有初始局部管理；历史作者在候选、add / resume / remove / reinvite 中均不可用，作者记录未删，源 ACL 不导入 |
| G20 | 通过 | 隔离双身份、同身份双标签、390px 手机、键盘确认、刷新 / 后退、暂停 / 恢复、重新认证、退出 / 再加入；详细浏览器边界见下文 |
| G21 | 代表性测量通过 | 254 成员、各 201 个 Project / Channel / Thread、600 个受影响管理作用域；SQL 造数只用于规模；耗时和锁等待见下文，不外推生产容量 |
| G22 | 通过 | 临时数据库 / 容器 / HTTPS 由退出 trap 清理，隔离浏览器关闭；本轮 helper / 临时匹配产物精确清理。系统信任、真实实例与远程状态未改；独立人工持续使用未执行 |

## 浏览器实际操作

最终完整轮次使用两个独立 Chrome Session；同身份竞争另开一个标签，共享 owner Session。初始四个用户、Project / 私密 Channel / Thread 由正式服务命令建立，所有页面行为由真实 handler 和数据库处理，没有用 mock HTTP 响应伪造业务成功。

1. owner 将已有成员升为 Project admin；member 刷新后仍看不到尚未加入的私密 Channel。
2. owner 显式加入该成员，再从 Channel 面板主动交接并保留 member；从真实 Message 新建私密 Thread，添加成员并交接，原作者事实不变。
3. 将接任者降为 Project viewer；成员仍可管理私密 Channel，实际发送 Message 返回 403，读取仍保留。现有部分业务表单依赖提交时拒绝，未宣称全部写按钮隐藏。
4. 两个 owner 标签：延迟第一笔 Project 角色 apply 请求，第二标签先成功更新；放行第一请求得到真实 409，旧确认复选框消失，重新读取当前状态。
5. 在 390×844 viewport 用键盘 Space 勾选、Enter 确认暂停；另一身份的既有 SSE 在 heartbeat 内清空内容、草稿与管理入口。手机文档宽度等于 viewport，没有水平溢出。
6. owner 恢复后，旧 member Session 显示本 Workspace 需要重新认证，仍可访问全局账户页；正式重新认证建立新 Session。
7. member 主动退出时，浏览器路由先等待真实 `route.fetch` 200，再中断客户端响应；账户页输入原 operation ID 查到 applied。刷新首页已无可进入的 Workspace。
8. owner 签发再邀请，原成员登录兑换；邀请码仅保留在当轮内存并及时清除。再次重新认证后 Project 列表为空，旧私密 Channel 地址不可读，后退仍为空。
9. fixture 最终数据库断言目标为 active、生命周期 revision 至少 5、Project 授权为零，并退出成功。

保留的截图为仓库忽略目录 `output/playwright/membership-20261024/` 下的 `readmission-desktop-final.png`、`suspend-confirm-mobile.png` 与 `readmission-mobile.png`。完整浏览器轮次之后补了创建者管理责任提示、重复姓名控件标签及确认复选框样式，并修正 Session 刷新竞态；这些最终改动通过 Web 单元 / 构建检查，未把早先截图描述为其逐像素复验。Session 竞态另有自动化覆盖晚到 focus 成功 / 401、撤权事件与新登录的顺序。

## 规模与隔离测量

合成固定 fixture：254 名成员，201 个 Project、201 个 Channel、201 个 Thread，正式移除涉及 600 个管理作用域。一次专项运行结果如下，均为本机单次观测：

| 操作 | 耗时 | SQL 数 |
| --- | --- | --- |
| 成员列表首屏 | 9.571209 ms | 12 |
| 移除预览 | 7.163375 ms | 13 |
| 正式移除 | 120.223041 ms | 28 |
| 暂停预览 + apply | 9.125375 ms | 未单列 |
| 恢复预览 + apply | 7.132667 ms | 未单列 |

权限清理按集合执行，管理责任检查为有限类型批量查询，未按对象逐一发 SQL。责任列表分页遍历核对全部条目；固定 SQL 上限断言防止查询数量随对象数量增长。独立 Workspace 在另一个 Workspace 的暂停事务持屏障期间读取 roster，耗时 4.864083 ms；同一 Workspace 的竞争请求观察到 PostgreSQL lock wait 后继续验证结果。

没有生产负载、海量历史、跨地域延迟或长时间并发吞吐证据；Workspace 协调锁可能使配置写入等待在途业务，5 秒超时不会自动重试高影响命令。

## 发现并修正的问题

- 统一准入暴露旧 fixture 缺少账户、使用过期 Session 时间、暂停恢复后继续复用旧 Session 等问题；补真实账户和正确时序，不放宽新权限合同。固定新增准入查询计入原有发现查询上限。
- 旧基础配置测试原先可以直接移除唯一私密 Thread 创建者；020 创建者成为局部 manager 后，应先由其正式授权接任者。补内部删除责任检查，并调整测试为真实管理命令接续，没有用直接 SQL 绕过保护。
- 实际 COMMIT 丢失、子进程终止与旧工件恢复使用真实数据库 / 工具；子进程测试修正 pgx 配置原始 DSN 与当前 database 不一致的问题，未改业务数据库行为。
- 长浏览器轮次结束时，旧 fixture setup context 先到期；最终状态查询改用新的 10 秒校验 context。另一次构建替换了运行 handler 缓存的 index 对应资源，后续 browser-review 改为冻结完整 Web 副本，避免独立构建破坏正在验收的版本。
- 一个既有 Ticket 状态 Web 用例在并行全量执行中超时；定向重跑及最终 243 测试全量通过。没有增大超时、删断言或改业务逻辑掩盖失败。
- Go HTTPS 测试与 Docker 在受限沙箱内不能监听 / 访问 daemon；按本轮授权在沙箱外运行，未下载缺失资源或修改系统配置。

## 保留边界

本轮没有执行生产部署、全局账户禁用、owner 越权接管、真实 OIDC provider、真实 Jenkins、原生中文 IME 或独立团队持续使用。普通无推送页面在下一次请求 / focus / 页面恢复时清理，不能撤回已经发送的数据。

ADR-0044 的 P05 / P06 / P10 / P13 / P20 仍按原记录补验，本轮成员进程中断与规模结果不替代包 CLI 的进程中断、文件竞态、移除关系往返和满额容量。ADR-0043 独立操作者 U27 继续暂缓。M1 文件、通知、Attention Item、完整审计运营等范围未因本阶段完成而退出。
