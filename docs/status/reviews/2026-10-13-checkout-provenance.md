# CI Run checkout 来源切片 A 实施记录

日期：2026-10-13

范围：[ADR-0037](../../adr/0037-ci-run-repository-checkout-provenance.md) 的 A。所有者确认后先提交提案为 `3c6fe24`，随后在本地 `dev` 实施；未 push，未迁移业务实例，未安装依赖或运行真实 Jenkins。B 继续暂缓。

## 实现结果

- sources / payload v2 采用受控单 Repository binding 与完整 SHA-1 Git OID；N 分界保持旧 v1 原始摘要，HMAC 签名域和原始字节语义不变。
- migration 015 新增不可变 source binding 与来源状态；observed 恰好一条 built-from，OID 只在 EntityLink。绑定、CI Run、receipt、关系、schema 2 CI 事件、Outbox 与 Activity 6 同事务；提交时延迟约束核对来源事实，禁止覆盖、补写和解除。
- CI Run Current、Relations、Timeline 共用当前授权；Repository 不可读时只有无内容占位。Repository 反向列表先复核当前 Component 权限再分页，cursor 限定 Workspace / Repository。
- 正式 Web 显示完整 OID、复制、未采集 / 不可用文案，以及 Repository 稳定页面和双向发现。刷新清除旧来源证据。浏览器发现的 Repository shell 白名单遗漏已修正并加入回归。
- 一次性 sender 可原样发送 v2；持久 worker / collector 仍只支持 v1，v2 保留并 blocked，文件格式升级属于 B。

## 自动化与数据库证据

| 检查 | 结果与覆盖 |
| --- | --- |
| `./scripts/check-server.sh` | race、vet、模块 tidy diff / verify 通过；严格 DTO、cursor 作用域、来源隐私、真实 TLS v2 sender 原字节、v1 digest 固定值与 worker v2 阻断 |
| `./scripts/check-server-postgres.sh` | 真实 PostgreSQL 集成通过；混合 v1 / v2、observed / unavailable、同 commit 不同 build、并发幂等、冲突、启动绑定核对、逐事务写入点注入失败、来源不可变、当前权限与反向分页、Activity 重建 |
| 014→015 | 在旧 schema 建立历史事实；旧 CI 权威字段和既有配置证据保持不变，仅新增 not_collected；旧程序 readiness 拒绝新 schema |
| `./scripts/check-server-backup-restore.sh` | v1 / v2 混合数据备份到全新目标，binding / EntityLink / receipt 保持一致；来源精确重试和 Current 可读，Activity 重建一致，旧 Session 不恢复 |
| `./scripts/check-repo.sh` | 469 个文件的仓库基线检查通过；`git diff --check` 通过 |
| Web `npm run check` | 28 个文件、158 项测试，Prettier、Lint、类型检查、生产构建、152 个锁定 package 的依赖检查通过 |

首次 SQL 验证发现延迟约束函数的 CASE 表达式需要括号，修正后完整集成通过。受 sandbox 限制的 Docker / TLS 监听检查在获得所需执行权限后重跑；不将受阻的首轮计作通过。

## 隔离 HTTPS 浏览器

使用已有 Chrome 与 Playwright CLI、本机临时 PostgreSQL / Caddy；证书忽略仅限测试浏览器上下文，不改系统信任。正式 Web 表单创建新的 Component 和 Repository，Repository 通过 Tab / Enter 提交。随后通过仅 browserfixture 编译的本地配置文件交接设置测试 source，实际 HTTPS / HMAC receiver 接收 29 条合成输入：1 条 v1、27 条 observed（含 succeeded / failed / canceled）、1 条 unavailable。

已验证 CI Run → Repository → CI Run、完整 OID 复制、稳定地址、25+2 条分页及末页禁用下一页；unavailable 保留真实取消终态、明确原因且无 built-from。390×844 下页面宽度与 scrollWidth 都为 390，完整 OID 保持可读。暂停测试用户的 Workspace membership 后，Repository 显式刷新清除列表，浏览器后退到 CI Run 返回不可读状态，未保留 OID、来源原因或仓库信息。

首轮发现 Repository shell 未注册导致 404，已修正 Go 页面白名单与测试；还修正 canonical Repository / Component 路由未带入当前 Workspace 的问题。截图保存在本地忽略目录 `output/playwright/2026-10-13-checkout-*.png`，不纳入提交。fixture 中对象、来源、仓库地址和 OID 全部为合成数据，没有真实外部 staging 或 SCM 采集；接收成功不构成 Jenkins SCM 证据。

最终生产构建再次确认复制成功、Repository 显示当前 Workspace、桌面与移动端来源内容可读。本次三轮浏览器 fixture 均已停止并通过清理钩子退出，专用浏览器已关闭，未残留本次 `radishnexus-authenticated-web` 容器。

## 保留边界

真实 Jenkins checkout profile、Git / git-client 固定依赖、controller 观察与持久交接、原生 Linux 文件系统 / 重启验收尚未实施。真实 observed 来源不得在 B 验收前启用。未新增 Ticket 与具体构建的交付关系、自动 Deployment、外部 provider 调用或完整构建输入证明；错误来源当前只能停用并保留事实，正式更正另立合同。
