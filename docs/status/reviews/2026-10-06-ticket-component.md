# 2026-10-06 Ticket 与 Component 人工关联

## 范围与实现

所有者确认 [ADR-0034](../../adr/0034-ticket-component-relations.md) 后，在本地 `dev` 基于 `f878090` 实施；本地相对 `origin/dev` 领先两笔既有提交，未 fetch、提交本切片、push 或应用业务实例迁移。

- migration 014 注册人工 `ticket / affects / component` 多对多关系、来源约束、active 唯一与反向索引。精确解除保留 removed 历史，重新关联创建新 link ID。
- 独立 Ticket Component service / store / Session handler 复用现有当前账户、Workspace、Project 权限与协作 receipt。contributor / decider / admin 写；Workspace owner 不绕过 Project，归档只读。新关联允许 planned / active / deprecated，retired 仅可解除。
- 关系、receipt、事件、Outbox、Activity v5 同事务。两类关系事件只进入 Ticket Timeline，以 `relation_state` 表达关系变化，不修改 Ticket 的 Current、内容更新时间或唯一来源 Decision。
- Ticket 组件列表和组件反向 Ticket 列表按当前权限分页；反向先过滤再占页，cursor 只来自已返回对象。公共 DTO 不返回上游私密对象或配置管理明细。
- React 提供显式关联、精确解除、组件稳定详情和双向跳转。未知结果保留原 operation / payload；焦点刷新不会替换待解除代次，失权与作用域切换丢弃相关数据及迟到响应。
- 没有新增依赖、通用 EntityLink 编辑器、Ticket 状态流转或 Ticket 已交付推导。

## 自动化证据

已执行并通过：

- `./scripts/check-server.sh`：Go race 测试、vet、模块 tidy 差异与校验；补组件稳定路径后另跑 HTTP Web App 与 Ticket Component 定向测试。
- `./scripts/check-server-postgres.sh`：全部真实 PostgreSQL 集成。新测试经正式 Message / Thread / Decision / Ticket 与配置命令建立对象，再以真实 Session / CSRF 调用 HTTP；覆盖关联 / 解除 / 重连、旧 receipt、changed payload、restricted 上游不穿透、原 Ticket 来源 / Current 保持、多对多与跨 Project 过滤分页、跨 Workspace、viewer / 无 Project 角色 owner、admin / decider、retired、双 actor 竞争。
- Project 归档、Project role 收回、Workspace membership 停用和账户禁用分别验证写入先行 / 权限变化先行的串行结果；旧 receipt 重试也必须重新授权。receipt、关系、事件、Outbox、投影五处故障全部整单回滚，正常投影与重建一致。
- 013→014 真实升级保留旧配置 Audit / receipt 和 Activity 内容，仅推进投影版本；不补造新关联。旧 013 readiness 拒绝新 schema；旧命令摘要保持兼容。
- `./scripts/check-server-backup-restore.sh`：全新目标恢复 active / removed 关系、事件与 receipt；恢复后重放关联、解除及重连原请求，当前仍保持最后一代关系，权威表快照与 Activity 重建一致。
- `./scripts/check-web.sh`：27 个测试文件、156 项测试，以及格式、Lint、TypeScript、生产构建和 152 个锁定 package 的依赖基线。新增用例覆盖严格 DTO、CSRF / 精确路径、独立关系状态、未知响应原请求重试、focus 刷新、旧解除代次、失写权保留只读、反向目标失读清理待确认表单、失读通知父页与卸载后迟到响应。
- `./scripts/check-repo.sh` 与 `git diff --check`：仓库基线、文档链接和文本卫生通过。

实现中修正迁移事件 Project 列名、测试初始化角色约束、SPA 组件稳定路径白名单和 React hook 依赖；没有放宽门禁或增加 fallback。截图发现名称与状态相邻，已补分隔符并重新通过完整 Web 检查。

## 隔离 HTTPS 浏览器

使用当前任务已授权的一次性 PostgreSQL / Caddy、已缓存镜像与已安装 Chrome；只绑定本机随机端口，证书仅用于隔离测试浏览器，不修改系统信任。业务对象全部经正式页面创建：bootstrap owner 登录 → Team → restricted Project 与初始 admin → Channel → Message → Thread → proposed Decision → 人工接受 → Ticket；随后通过正式配置创建 Component。

实际通过的操作：

- Ticket 选择组件、勾选明确确认，以按钮聚焦后 Enter 提交；列表、核心 Relations 与 Ticket Timeline 同步出现人工关联，原 `implements` 来源保留。
- 首页与 Ticket 进入稳定组件地址，组件反向列表可打开 Ticket；组件地址重新加载仍可读取。
- 390px 手机视口从组件端显式解除，再回 Ticket 重新关联；Timeline 展示 created、linked、unlinked、linked，Ticket 仍为 open，原内容时间不变。
- 1440px 桌面与 390px 手机截图已目视复核，主要内容和操作入口可用。本地材料位于被忽略的 `output/playwright/ticket-component-desktop.png`、`component-ticket-mobile.png`、`ticket-component-mobile-relinked.png`。截图早于名称 / 状态分隔符的小修正。

fixture 在完成上述流程后达到 30 分钟上限，以超时失败退出并自动清理。随后计划执行的 SQL 只读来源核对与 Project viewer 故障注入因容器已不存在而没有执行；刷新产生连接失败，不能据此声称 viewer 或撤权浏览器通过。该轮没有取得浏览器后退专项、原生中文 IME 或最终分隔符修正后的截图。权限 / 来源 / 关系代次 / 迟到响应有上述真实数据库和 Web 自动化证据，保持证据类型区分。

隔离 Chrome 已关闭，本轮数据库 / 代理容器和证书卷已清理；没有删除其他会话既有资源。fixture 超时属于验收环境时限，不将其退出状态记为通过。

## 接续与未覆盖

实现和自动化已完成；浏览器权限撤回与后退专项仍可在下一轮补验，真实团队持续使用尚无结论。下一段先冻结持久终态采集与具体交付来源，不能凭 Component 共属推导 Ticket 已进入某次 CI Run 或 Deployment。

未操作真实 Jenkins、业务实例升级或生产部署。原生中文 IME 继续按所有者要求暂缓；没有新的依赖、提交或远程状态操作授权。
