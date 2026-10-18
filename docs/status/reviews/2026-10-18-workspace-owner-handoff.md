# Workspace owner 主动交接实施与验收

日期：2026-10-18

基线：`dev` / `333c4ce`，schema 017。所有者确认 [ADR-0042](../../adr/0042-workspace-owner-handoff.md) 推荐范围，授权实现、隔离 PostgreSQL / Compose HTTPS / 浏览器验收及清理，不操作真实账户或业务实例。实现完成后，所有者追加授权一致性核对并本地提交，仍不 push；本记录随实现一并提交。

## 实现范围

- migration 018 增加 membership `role_revision`、邀请创建代次、配置审计的双主体前后角色 / 版本和交接 receipt 部分唯一索引；角色变化必须精确递增，审计 / receipt 继续不可变。有效旧邀请绑定代次 1，无效旧邀请使用不可匹配的 0，不修复零 owner。
- 独立 owner transfer service / Store / HTTP 路径按账户 → Session → membership → Workspace 排序锁定。双角色、代次、Audit 和 receipt 同事务；全部等待后重新检查近期认证、有效期与预期版本，不隐藏重试。
- 原 owner 降为 member 后可读取自己的最小历史成功，精确重试不再写入；同 ID 换目标 / 版本冲突。当前账户、Session、active membership 仍需有效，其他配置命令没有该历史读取例外。
- 首页提供候选、明确确认、原请求重试、可复制操作 ID 与历史查询。Session / ownership 重新读取决定当前能力，历史成功不推导当前角色；换账户清理原操作，不使用 Web Storage。
- 邀请创建 / 本地兑换 / OIDC Store 准入复核创建者任期。Project / Channel / Thread、显式 Deployment 权限、业务对象、密码和外部绑定不随交接转移。无新依赖、Activity 或 Outbox 事件。

## 可复验入口与结果

| 实际执行 | 结果与范围 |
| --- | --- |
| `cd server && go test -race ./...` | 通过；Go 单元与 race 检查，不能单独作为数据库并发证据 |
| `cd server && go vet ./...` | 通过 |
| `./scripts/check-server-postgres.sh` | 最终通过；真实 PostgreSQL 全相关回归、017→018 升级、锁屏障、故障注入与 HTTP 当前权限链 |
| `./scripts/check-server-backup-restore.sh` | 通过；当前版本双实例备份 / 空目标恢复；旧工件项未提供 ref 时明确跳过 |
| `RADISHNEXUS_OWNER_TRANSFER_LEGACY_REF=333c4ce ./scripts/check-server-backup-restore.sh` | 通过；本地旧工件构建、017 备份 / 空目标恢复、显式升级与管理责任重新核验 |
| `./scripts/check-web.sh` | 通过；228 项测试（含提交前补充的 2 项恢复查询保护回归）、格式 / Lint、类型、production build、依赖基线 |
| `RADISHNEXUS_OWNER_TRANSFER_BROWSER_REVIEW=true ./scripts/check-self-hosted-compose.sh` | 通过；全新隔离 Compose、当前 018 工件、恢复 CLI 原有回归、正式邀请 / 独立登录、HTTPS 交接与敏感日志检查；暂停阶段完成浏览器操作后继续自动链 |
| `./scripts/check-repo.sh`、`git diff --check` | 通过；正式文档、链接与文本卫生 |

数据库主要证据集中在 [authn/postgres/owner_transfer_integration_test.go](../../../server/internal/platform/authn/postgres/owner_transfer_integration_test.go)；下表简称省略共同的 `TestOwnerTransfer` 前缀。普通账户由正式 bootstrap / 邀请建立，成员独立登录；多 owner、禁用、暂停、时间边界、锁与故障仅在一次性测试库明确注入。全表快照比较排除预期改变的 membership / 配置 Audit / receipt 后，对其他表逐项比较，并单独验证其他 Workspace membership 保持。

## 36 项矩阵结果

| 编号 | 结果与具体证据 |
| --- | --- |
| P01 | 通过：`AtomicTenureAndHistoricalRetry`、`HTTPWithCurrentSessionAuthority`、Compose / 浏览器 A→B；双角色与版本、旧新 Session、setup 关闭均核对 |
| P02 | 通过：`ConcurrentOperations` 多 owner 同 / 不同目标；其他 owner 和数量保持 |
| P03 | 通过：`RejectedWithoutWrites`、实际 Session HTTP；member / 原 Project admin 无新 owner 写权，非成员与跨 Workspace 不可发现；候选另有当前权限复核 |
| P04 | 通过：拒绝用例全表快照；自己、已有 owner、未知 / 跨域、disabled / suspended 目标均无写入，HTTP 错误分类测试通过 |
| P05 | 通过：拒绝用例、`LockTimeAndCancellation`；未来 / 过期 / 撤销 Session、超界和等待跨界；Store 单元精确覆盖 10 分钟包含边界及后一纳秒 |
| P06 | 通过：`AtomicTenureAndHistoricalRetry`，多 Workspace、受限 Project / Channel、显式环境记录权与全表快照；原 Project admin 和显式记录权仍可使用 |
| P07 | 通过：`ConfigurationLockOrder` 与原子交接测试；旧 owner 创建 / 授权及旧配置 receipt 重试拒绝，原对象级能力保留 |
| V01 | 通过：[service 单元](../../../server/internal/goldenpath/owner_transfer_test.go)、[HTTP 单元](../../../server/internal/platform/httptransport/owner_transfer_test.go)、Web adapter；严格 JSON、确认、UUID / ID、bigint 字符串及溢出 |
| V02 | 通过：`CandidatePaginationAndDBGuards` 正式邀请 27 个同名用户、25＋2；HTTP cursor actor / scope 绑定；降级后旧 cursor 拒绝；Web 稳定 ID 消歧 |
| V03 | 通过：原子交接 A→B→A 和单方过期版本拒绝；旧成功 ID 仅历史读取，Web 冲突重新选择 |
| C01 | 通过：`ConcurrentOperations` 单 owner 两目标屏障竞争，仅一笔新写入成功 |
| C02 | 通过：同测试两 owner 同目标 / 不同目标竞争，保持 owner 数量 |
| C03 | 通过：同测试同请求、同 ID 换目标 / 版本并发；一条事实、Audit / receipt，变体冲突；实际 HTTP 精确重试 |
| C04 | 通过：`TargetStateSerializes` 在账户 / membership 锁前后禁用 / 暂停；角色竞争由 C01 / C02 覆盖，不扩张到未来禁用接口的最后 owner 保证 |
| C05 | 通过：`ConfigurationLockOrder` 对 Team / Project / Component / Environment / Repository 创建、环境授权和组件关联分别验证先后顺序，无稳定锁环 |
| C06 | 通过：`RevocationInterleavings` 恢复 / 外部解绑 / logout 两种顺序；先获得有效授权锁者完成，后续旧 Session 失效 |
| C07 | 通过：锁等待 / 取消、`RealDeadlockAndCancelAfterWrites` 真实 SQLSTATE 40P01、写后取消及 revision 溢出；整笔回滚，不自动重跑 |
| I01 | 通过：原子交接、`InvitationAndOIDCTenure`、Compose HTTPS；旧任期无效、新 owner 邀请有效、其他 owner / Workspace 保持，已加入成员不移除 |
| I02 | 通过：A 重新接任后旧邀请仍拒绝，新代次邀请正常兑换 |
| I03 | 通过：`InvitationInterleavings` 创建 / 兑换先后提交，真实锁屏障；先兑换者保留唯一成员和审计 |
| I04 | 通过：同测试已登录 B 接受 A 邀请与 A→B 竞争，验证接收账户 → 创建者账户 → membership 顺序及角色不被覆盖 |
| I05 | 通过（Store 合同）：OIDC start 后交接，callback 旧邀请拒绝；已有 binding 正常登录，不宣称真实 provider 联调 |
| R01 | 通过：原子交接 / HTTP / 浏览器已提交响应丢失后查询、精确重试；降级后只读本人事实，无新写入 |
| R02 | 通过：接收人后续再交接 / 禁用后，原操作者历史读取仍返回原最小事实，不返回目标当前身份状态 |
| R03 | 通过：原子交接与实际 Session HTTP；失效 / suspended 拒绝，其他 actor / Workspace `not_observed`，member 新操作拒绝 |
| R04 | 通过：`LostCommitAndInFlightStatus` 经 PostgreSQL wire proxy 实际 COMMIT 后丢确认，及锁等待期间先 `not_observed` 后 `applied`；浏览器另验证 HTTP 响应丢失 |
| R05 | 通过：`RollbackAtEveryWrite` 在升权、降权、Audit、receipt 后及延迟提交约束处失败；全快照恢复，同 ID 后续可正常提交 |
| A01 | 通过：审计完整形状与非法 actor / subject / role / revision / 授权混用被 DB 拒绝；既有配置回归与升级旧审计兼容 |
| A02 | 通过：DB 不可变 Audit / receipt、role_revision trigger；同 ID 变体并发拒绝；绕过 service 直接插入不同 subject 的同操作 receipt，验证部分唯一索引返回 23505 |
| A03 | 通过：严格 API / Web DTO、全表快照无 Activity / Outbox 改动；Compose 检查应用 / 代理日志没有测试邮箱、密码、Cookie / CSRF token；不外推到自定义生产日志采集 |
| B01 | 通过：[TestOwnerTransferUpgradeFrom017](../../../server/db/owner_transfer_upgrade_integration_test.go)，旧有效 / 失效邀请、多 owner、旧审计 / receipt 和零 owner 基线 |
| B02 | 通过：升级测试新旧 readiness mismatch；[TestBackupRestoreOwnerTransfer](../../../server/internal/backuprestore/owner_transfer_integration_test.go) 保留角色 / 版本 / 历史，临时身份数据空、重登查询及 setup 关闭 |
| B03 | 通过：`TestBackupRestoreOwnerTransferLegacy` 使用 `333c4ce` 真实旧工具；旧备份被当前 restore 拒绝，旧 restore 空目标后升级，原 owner / revision 1 回来且没有后来 receipt；源库交接保持 |
| W01 | 通过：HTTPS 桌面 A→B、B 独立登录创建 Team；390px 手机 B→A，均通过正式 UI；近期认证提示由 Web 状态测试与数据库时间边界覆盖，未在浏览器等待 10 分钟老化 |
| W02 | 通过：Web 状态测试、App 最新 Session 请求胜出；双标签恢复、失权清理、已提交响应丢失与换账户重挂载。浏览器明确派发 `pageshow` 复核，未把 CLI 切标签宣称为真实系统焦点事件 |
| W03 | 通过：手机键盘 Tab / Enter 明确确认、同名稳定 ID、复制操作 ID、退出重登后粘贴查询；localStorage / sessionStorage 为空；错误 / 焦点恢复另有 Web 状态测试 |

## 浏览器、修正与清理

浏览器使用本机既有 Chrome，在一次性 Compose 的精确 localhost HTTPS origin 复核，测试上下文临时允许该自签证书，未改系统证书信任。A 正常交给正式受邀 B 后，owner 控件清理，可读 Project 保留；B 独立登录并通过 UI 创建 Team。手机宽 390px，页面与 viewport 宽相同，无横向溢出。

B→A 时，浏览器拦截器先取得真实 POST 200，再中断该响应交付；数据库已提交而 UI 显示结果尚未确认。原 ID 保留，GET 查询得到历史成功并刷新为 member；退出重登后粘贴 ID 仍可查本人历史。另一标签在 `pageshow` 重新读取 Session，换账户后原查询输入不继承。完成 B→A 后继续 Compose 自动 A→B→C 链，最终脚本通过。

实施中修正了审计 INSERT 参数算术的 PostgreSQL 类型推断、017→018 后旧审计快照新增 nullable 字段的比较方式，以及测试 fixture 缺少正式 request correlation ID；未放宽生产约束。公共 Workspace resolver 原有无 active membership 内部 403 在 transport 转为 404，ADR 已按真实既有规则纠正。Compose 登录限流触发后按 `Retry-After` 等待，未放宽限流。

临时 PostgreSQL、双实例恢复、Compose 容器 / 网络 / volumes / Secret 和浏览器会话已退出清理；最后只读容器清单为空。未安装依赖、修改系统信任或写远程状态。截图留在忽略的 `output/playwright/` 供本机复核，仓库不收录浏览器私有运行材料。

本记录不证明真实 OIDC / Jenkins、原生中文 IME、生产容量、带流量升级、通用升级中断恢复或外部团队持续使用。owner 应急接管、Project admin / 私密对象管理权交接、暂停 / 删除的全局最后 owner 治理和完整离职流程仍独立设计。

## 提交前一致性复核

逐项对照 ADR、正式 service / Store / HTTP / Web、migration、升级 / 恢复测试和 36 项证据记录。后端合同与已有验证记录一致；此次未更改数据库或 Go 生产实现，沿用本轮已通过的 PostgreSQL、旧工件恢复与 Compose / 浏览器证据，不重复声明新的数据库或浏览器验收。

复核修正了一处 Web 遗漏：重登后只有操作 ID、原请求已丢失，查询 `not_observed` 或网络失败时，原界面仍可发起新交接。现在保留所有未确认查询 ID 并禁用新的交接提交；清空输入、恢复焦点或查询另一笔成功都不能解除原操作的保护，只有对应 `applied` 清除该 ID。仍不持久化原请求、不重构精确重试。两项新增状态测试覆盖未观察到结果与网络失败，验证无新写请求、不会出现原请求重试按钮，并在对应成功后解除保护。已重跑完整 Web 检查、仓库检查与 diff 卫生检查。

同时将 ADR 的重登行为明确为重新选人、刷新代次并再次确认，与不使用浏览器持久存储的现行实现一致；更新当前状态及提交授权说明。真实 OIDC、生产升级和其他既有范围限制保持。
