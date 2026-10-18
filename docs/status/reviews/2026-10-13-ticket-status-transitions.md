# Ticket 最小状态流转实施记录

日期：2026-10-13

范围：[ADR-0039](../../adr/0039-ticket-status-transitions.md)，基线为 `dev` 上 ADR-0038 实现 `7ace9b4` 与已提交提案 `3a2e008`。所有者确认后实施；无新增依赖，不扩负责人、优先级或看板，真实 Jenkins B 继续暂缓。

## 实现与合同

- 新增独立 TicketStatusService / PostgreSQL store / HTTP handler；唯一 POST 状态命令携带 operation ID、base revision、目标状态与明确确认。当前账户、Workspace、Project 权限在新命令与重试前重新检查，owner / 创建者不穿透，私密来源不额外限制 Ticket 执行权限。
- open / in-progress 可转到其余状态；done / canceled 只能重新打开。有效变更递增独立状态版本；同状态、非法边、旧版本和上限冲突不消耗操作。排他 Ticket 锁后确定严格递增的数据库微秒时间。
- Ticket、receipt、不可变状态事件、Outbox 与 Activity 同事务。精确重试返回原 applied 状态与版本，当前事实通过 Nexus View 重新读取，避免后续流转被旧重试覆盖。
- migration 016 为正常旧 open Ticket 初始化版本 1，拒绝非 open 旧基线；扩展 receipt 的状态结果 / 来源约束，Activity 升级至 7。备份包含现有权威表的新增字段，无新增备份类别。
- Nexus View 的 Ticket Current 增加版本，与 change_status 能力同事务读取；详情、Project / Component 列表共享四状态。创建首次 201 必须 open / 1，200 重试返回同一 Ticket 当前值。旧创建与组件事件保留原事实。
- Web 在现有 Ticket 详情增加状态区。完成、取消、重新打开二次确认；提交中防重复，不乐观更新。409 清理旧确认并重读，网络未知保留原请求显式重试；刷新 / focus / 可见性与历史恢复复权，过期结果不能恢复旧页面，无权限时清除动作。

## 自动化证据

| 验证 | 结果与覆盖 |
| --- | --- |
| `./scripts/check-server.sh` | Go race 单元测试、vet、tidy diff、module verify 通过 |
| `./scripts/check-server-postgres.sh` | 真实 PostgreSQL 全套通过；新增状态权限、并发、ABA、原 receipt、读入口、投影重建与失败注入测试 |
| `./scripts/check-server-backup-restore.sh` | 实际 pg_dump / 空目标恢复通过；四次状态转换及原 receipt 与原创建 / 关系 / Document / checkout 事实共存，恢复后重试不再应用，20 行 Activity 重建一致 |
| `./scripts/check-web.sh` | 格式、Lint、183 项测试、TypeScript / production build、152 个锁定 package 依赖检查通过 |
| `./scripts/check-repo.sh` / `git diff --check` | 仓库文本、链接与合同检查通过 |

新增 Go / PostgreSQL 验证明确覆盖：完整转换矩阵；未知 / 越界输入与严格 JSON / query / Host / Origin / CSRF；不同 ID 同版本竞争仅一成功，相同 ID 并发仅一次应用；回到 open 后旧 base 冲突；终态非法直转与同状态拒绝；上限不允许新转换但允许旧 receipt 确认；后续多次变更后的原结果；Project / Component 列表和创建精确重试回读真实状态。

权限测试覆盖 viewer、公开只读基线、owner / 创建者不越权、restricted 失权、暂停 Workspace 成员、禁用账户、归档以及私密 Thread 独立性。账户、Workspace membership、Project 角色与归档变更分别验证先撤权 / 先写入两种真实锁等待顺序，重试不能绕过最终权限。注入 receipt、事件、Outbox、Activity 写失败验证 Ticket 状态 / 版本 / 时间与所有伴随事实一并回滚；损坏 result revision 的 receipt 在提交时被约束拒绝。

升级测试从精确 015 建立历史 Ticket：正常 open 升为版本 1 且原字段不变、不虚构事件；in-progress / done / canceled 分别拒绝，原 schema history 与对象保持不变。新二进制拒绝旧 schema，旧 readiness 拒绝 016。

Web 自动化还覆盖确认流程、成功重读、未知结果跨刷新保持原请求、状态相同不能推断自身成功、冲突关闭旧确认、写权撤回、401 / 404 清空、取消中的迟到成功、pageshow 历史恢复及严格响应漂移拒绝。

## 隔离 HTTPS 浏览器

使用现有 foundation fixture、真实 Go / PostgreSQL / Caddy 和 Chrome，经 Playwright 操作正式页面。仅 bootstrap 身份由 fixture 建立；Team、restricted Project、私密 Channel、Message、restricted Thread、Proposed / Accepted Decision、长标题 Ticket、普通成员邀请和 contributor 角色均经页面创建。没有 SQL 修改 Ticket 状态替代产品操作。

实际结果：

1. 普通 contributor 从 Project 列表进入 Ticket，依次开始、完成、重新打开、取消；Current / Timeline 正常更新。Decision 显示 restricted evidence，成员没有来源 Thread 打开入口，但仍可流转 Ticket。
2. 两标签同为版本 6；先提交完成到版本 7，再释放旧版本开始请求，旧请求返回 409，详情显示版本 7 且提示重新选择，没有覆盖。
3. 将开始操作真实发送并取得服务端 200 后丢弃浏览器响应。刷新后仍保留原 base / ID；另一标签再完成至版本 10。精确重试请求与原请求逐字段相同，返回“原操作处理中 / 版本 9”，详情仍为“已完成 / 版本 10”。
4. 已完成 Ticket 可关联正式 Component 并创建 Document，Component 反向列表与 Project 列表显示已完成；来源、文档及后退跳转通过，关系动作未增加状态版本。
5. 390×844 手机视口无横向溢出，通过键盘明确重新打开至版本 11；桌面 1440×1000 与长标题、状态区截图已视觉复核。开始操作也通过 Enter 提交，状态不依赖颜色表达。
6. 保留未提交取消确认后，通过另一管理员页面降为 viewer；focus 复权后只读且旧确认消失。再从详情进入列表，撤销 restricted Project 角色后后退，页面仅显示不可读错误，旧标题和状态区均不存在。恢复 contributor 并重新读取后为 open / 11，没有自动提交。
7. 收尾只读数据库核对：open / 11，10 条状态事件、10 条状态 receipt、10 条对应 Activity，CI Run 与 Deployment 均为 0。未生成重复状态事实或交付事实。

截图保存在被忽略的 `output/playwright/ticket-status-desktop-actions.png` 与 `ticket-status-mobile.png`，不作为需要提交的测试资产。预期 401、409、404 与人为丢弃响应产生的浏览器错误不计为成功请求。

## 首次失败、修正与清理

初次测试发现复用了不含 `tkt_` 的配置 ID 校验，已改为既有 EntityRef 注册表；旧 open-only 测试、缺少状态版本的 fixture 和早期未建立账户的权限 fixture 同步更新。Web 修正生命周期 state 重置的 Lint 问题与测试类型错误，没有放宽公共 parser。旧 DTO 非法状态探针改为真正非法值或损坏标题，保留整页拒绝。

验收中曾在 Web 服务启动后重建 dist，旧进程缓存的入口引用已被替换的旧资源而返回 404。已清理该临时实例，在最终构建上全新启动并重走正式创建链和上述全部状态验收；没有用错误资源映射掩盖问题。部署说明补充完整构建配套重启要求。

两个浏览器会话均已关闭；两轮 foundation fixture 的 Go、PostgreSQL、Caddy、专属 volume 和临时目录由停止协议清理。PostgreSQL 集成与备份恢复脚本均完成自身清理。未操作业务数据库、真实 Jenkins、远程 Git 或系统证书。

## 交付边界

完成结论限定于当前权限下显式四状态流转、版本冲突与原操作重试、现有读取和恢复合同一致。没有完整工单系统、自动工作流、CI / Deployment 联动或真实团队持续使用证据。浏览器操作为自动化验收，不替代此前暂缓的 macOS 原生 IME 人工复核。

Go / schema / Web 必须配套升级，旧二进制不能直接写入 016；正常升级停写并保留升级前备份，失败时恢复备份到全新目标或独立 forward repair，明确备份点后的数据取舍。当前未对业务实例迁移或部署，完整 Compose / 生产升级演练仍独立验收。实现与文档保留工作区待审阅，没有额外提交或 push；下一顺位为独立梳理 Decision 只读发现。
