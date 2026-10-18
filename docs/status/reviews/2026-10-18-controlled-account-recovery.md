# 2026-10-18 自部署本地账户受控恢复

依据：[ADR-0041](../../adr/0041-controlled-local-account-recovery.md)。基线 `5ddcc1c`，开发分支 `dev`。所有者在本轮明确授权实现和隔离 PostgreSQL / Compose / HTTPS 浏览器验收及清理，随后授权核对范围与验收记录后本地提交；没有 push、生产实例或真实账户操作。

## 交付

- 独立 `nexus-account-recover inspect / apply / status --request-stdin`，严格 8 KiB JSON、现有邮箱 / 密码规则、UUID v4、六位微秒 UTC 预期版本和人工确认。无恢复 HTTP / Web 路由，无新依赖。
- migration 017 扩展 `identity_audit`：保留旧 user actor，新增 operator / target / operation / 前后密码版本形状，唯一 operation_id 与不可变恢复记录。
- 事务外 KDF，账户 → 凭证加锁复核；数据库时钟必须前进；密码、临时锁清零、全账户旧 Session 撤销和恢复审计原子提交。迟到失败登录同时匹配校验时的 hash / 版本。
- 已记录 ID 拒绝再次应用；status 只返回历史事实；COMMIT 确认丢失 / 连接级终止及提交后 stdout 失败提示 `result_unconfirmed`。明确拒绝与确定回滚不写成功审计。
- operations 镜像与显式 Compose profile 装配，常驻 app 无恢复二进制。Compose 验收发现自定义 `X-Csrf-Token` 未被默认日志过滤，已对访问和默认运行日志加入字段过滤；语法依据 [Caddy 官方文档](https://caddyserver.com/docs/caddyfile/directives/log#filter)。不更改真实系统证书、数据库权限或线上日志设置。

## 验证入口与结果

| 验证 | 结果与范围 |
| --- | --- |
| `./scripts/check-server.sh` | 通过：Go race、vet、模块一致性与校验；无 lockfile / 依赖变化 |
| `./scripts/check-server-postgres.sh` | 通过：真实 PostgreSQL、017 升级、CLI、事务 / 并发 / 错误路径以及原业务回归 |
| `./scripts/check-server-backup-restore.sh` | 通过：原 Golden Path 恢复回归及新增正式 bootstrap / 登录 / 邀请 / 恢复后的双实例备份恢复 |
| `./scripts/check-web.sh` | 通过：33 个测试文件、216 个测试及格式 / Lint / 类型 / production build |
| `./scripts/check-self-hosted-compose.sh` | 通过：固定镜像构建、migration、Secret 实际挂载、CLI、HTTPS、Cookie / CSRF、两类 SSE 关闭、日志敏感标记、退出和内部端口；新增辅助脚本 `check-account-recovery-compose.py` |
| HTTPS 浏览器 | 真实 Chromium：恢复前 owner 登录；恢复后刷新项目触发 401、清空工作台；新密码重登保留同一账户、Workspace owner 和原项目 |
| 旧工件 / 旧备份链 | 从 `5ddcc1c` 的精确 server 源码构建旧 migrate / bootstrap / backup / restore；两个隔离 PostgreSQL 容器，016 备份 → 旧工件空目标恢复 → 新 migration 017 → 重新 inspect / apply → 正式 Service 登录通过 |
| 仓库与差异 | `./scripts/check-repo.sh`（513 文件）、`git diff --check` 通过 |

旧工件链额外证明：旧 backup 拒绝新 017 schema；新 recover 拒绝未升级的 016 目标；原实例恢复后，旧备份副本仍有相同 user ID / 旧版本，原操作 status 为 `not_observed`，旧密码可登录而原实例的新密码不可登录。隔离目标经新的 operation_id 恢复后，新目标密码成功、旧密码失败。未开放任何恢复目标到用户网络。

## ADR 验收矩阵证据

以下编号逐一对应 ADR 原矩阵；“通过”限定于所列证据层，不扩展为真实 OIDC、生产升级编排或全体成员持续使用结论。

| 编号 | 结果 | 证据 |
| --- | --- | --- |
| P01 | 通过 | `TestRecoveryAtomicSuccessAndHistoricalStatus`、CLI 数据库测试、正式 bootstrap 的备份测试与 HTTPS owner 浏览器；`TestRecoveryOrdinaryMemberKeepsRole` 明确验证邀请建立的普通成员恢复后仍为 member，并覆盖跨账户并发恢复 |
| P02 | 通过 | 多 Workspace、suspended membership、受限 Project / Channel 正式配置的全表快照；所有旧 Session / CSRF 拒绝，HTTP / HTTPS 401；重登仅当前有效 Workspace |
| P03 | 通过 | 没有恢复路由装配；Compose 已登录 owner 猜测恢复路径为 404；CLI / Store 只读连接拒绝且无半完成；无产品角色旁路 |
| P04 | 通过 | disabled account / credential 拒绝、完整快照不变；临时锁定账户成功后 count=0 / locked_until=null |
| P05 | 通过 | 不存在、OIDC-only、无本地凭证、legacy 未映射、邮箱 / user 不符，Service / PostgreSQL / CLI 拒绝 |
| P06 | 通过 | 旧备份副本保留相同 user ID，旧版本可恢复；部署说明明确实例核对和 inspect 不认证实例 |
| I01 | 通过 | CLI 空 / 超限 / 非 UTF-8 / 重复 / 未知 / null / 错类型 / 尾随 / 参数拒绝；环境值不能补足缺失密码 |
| I02 | 通过 | 15 / 128 code point 边界、中文与空白原文、旧密码复用、损坏 verifier、KDF 随机失败；新随机 salt；无写入 |
| I03 | 通过 | false confirmed、ID / UUID / operator / reason / 时间精度检查；缺字段不默认成功 |
| T01 | 通过 | 事务快照、全账户 Session 撤销、原已 revoked 行保留、审计 actor / target / 版本与单条操作；不签发会话 |
| T02 | 通过 | credential update、Session revoke、audit insert、deferred COMMIT 约束错误和写入后取消均回滚；全表快照一致 |
| T03 | 通过 | verifier barrier 两个恢复同版本，不同 / 相同 ID 最多一个成功 |
| T04 | 通过 | 同 ID 更改输入 / 换 user 拒绝；不同账户并发同 operation_id 由唯一约束兜底；status 不暴露其他账户记录 |
| T05 | 通过 | 旧密码验证后暂停，恢复先提交则不能创建 Session；登录先完成则 Session 被恢复撤销；HTTPS 旧密码拒绝 |
| T06 | 通过 | 迟到的错误密码验证不修改新计数；新凭证失败仍按既有次数锁定 |
| T07 | 通过 | 账户禁用及绑定 / 解绑交错、锁超时与真实 PostgreSQL deadlock；受害恢复事务完整回滚，不恢复已移除绑定 |
| T08 | 通过 | 未来密码版本触发 `clock_not_advanced`；未来 created_at 的 Session 使用合法 revoked_at 并不能解析 |
| S01 | 通过 | 多个正式 Session、旧 CSRF、HTTPS 浏览器清空和新密码重登 |
| S02 | 通过 | Compose 真实开放 SSE：无新消息由 heartbeat 关闭，有新消息在发送复核时关闭；新正文未泄露给旧订阅 |
| S03 | 通过 | 外部绑定与邀请保留；旧 Session 发起的 OIDC callback / unlink 拒绝；新合法外部身份仍可按 Store 合同登录，不代表 provider 联调 |
| A01 | 通过 | 016 → 017 保持旧审计五字段，默认 user actor；旧 INSERT 兼容，新恢复 shape 受 CHECK 约束 |
| A02 | 通过 | UPDATE / DELETE 恢复记录、将旧 action 改成恢复均拒绝；备份恢复 INSERT 成功且不可变 trigger 保留 |
| A03 | 通过 | wire proxy 在 PostgreSQL 实际 COMMIT 后丢弃确认，命令不伪报回滚；真实在途 audit INSERT 屏障下 status 未观察，提交后可查；stdout 故障单测保留提交并指引 status |
| A04 | 通过 | 第二次恢复后旧 operation_id 返回原历史结果，不改当前凭证 / Session |
| A05 | 通过 | CLI 捕获成功 / 拒绝 / 数据库和 Secret 文件错误分类；不输出输入、verifier、DSN 或 SQL Detail；Compose 采集 app / Caddy 日志检查密码 / Cookie / CSRF 标记 |
| B01 | 通过 | `TestRecoveryUpgradeFrom016` 与 readiness 既有缺失 / 漂移 / 新旧不匹配；旧工件实际拒绝 017，新 recover 实际拒绝 016 |
| B02 | 通过 | `TestBackupRestoreAccountRecovery`：同版本空目标、新密码与原身份 / 权限 / 审计保留；Session / 邀请 / OIDC 数据为空，setup 关闭 |
| B03 | 通过 | 上述 5ddcc1c 旧工件、016 备份、隔离目标显式升级与重新恢复链 |
| B04 | 通过 | 原 `TestBackupRestoreGoldenPath` 的非空目标、manifest / dump 损坏、未知 relation 和 schema mismatch 回归 |
| R01 | 通过 | 全权威 / 业务表快照，以及非目标账户登录、权限和会话不变；受限 Project / Channel 配置经正式 service 建立 |
| R02 | 通过 | 当前 operations 镜像与文件 Secret，Compose inspect → apply → status → HTTPS 登录；server target 仅包含原应用二进制 |
| R03 | 通过 | 失败轮与成功轮均清理本轮 project / volumes / Secret；旧备份测试容器与工件清理，浏览器关闭；不触及真实账户 |

数据库专项中的禁用、损坏、未来时钟、故障 trigger / 只读连接和锁屏障是显式异常夹具，不属于产品入口。普通身份均从正式 bootstrap / 邀请建立，Session 走正式登录。跨 Workspace 基础 membership 是权限夹具；私密 Project / Channel 通过正式配置 service 创建。已有身份生命周期测试原先与其他 package 共用数据库，新增测试延长时序后暴露污染，现改用独立数据库，未改变断言。

Compose 扩展验收的三处夹具修正分别是：SSE `ready` 帧可以先有 `id:`；首个 401 会清理 cookie jar，因此旧 CSRF 验证保留旧 Cookie 快照；Python 的 `localhost.local` cookie jar 表示导出给 curl 时使用实际 host。都未放宽服务端行为、会话或权限断言。完整门禁在修正后重跑通过。

## 保留边界

只接受已有 active 本地凭证；不提供 SMTP、自助恢复、禁用解锁、管理员交接、强制改密、MFA 或 OIDC 接入。恢复不改变邀请、外部 IdP 登录能力，也不瞬间取消所有已授权在途业务事务。operator_ref 依赖受控运维人员映射，不是产品 Session 认证或不可抵赖证明。Compose 数据库角色仍未拆分。

本次证明当前工件的全新 Compose 装配、017 升级与两类备份恢复链；没有验收生产容量、生产证书、带流量升级、任意升级中断恢复、网页 setup overlay、真实 Jenkins / OIDC、原生中文 IME 或外部团队持续使用。数据库审计 / statement / 参数调试与宿主采集策略是部署运维边界，禁止额外开启会记录输入或 verifier 的日志。
