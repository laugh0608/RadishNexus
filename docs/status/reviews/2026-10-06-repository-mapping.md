# 2026-10-06 Repository 映射与 Component 关联

## 范围与实现

所有者确认 [ADR-0033](../../adr/0033-repository-mapping-and-component-relations.md) 后，在本地 `dev` 实施。起始 HEAD 为 `5abc6a9`，相对本地 `origin/dev` 领先一笔既有联系邮箱提交；未 fetch、提交、push 或应用业务实例迁移。

- migration 013 注册 `repository / rep_`，新增不可变映射、同 Workspace 外部身份唯一性及 `source-repository` 多对多 EntityLink。关系来源和 removed 历史不可重写；重新关联生成新 ID。
- 三类窄命令复用配置 Audit / receipt、严格 Session / CSRF 与当前权限。active Workspace owner 写入；active 成员共享读取。新关联要求 Component active，解除允许清理非 active Component。
- URL 统一规范化 HTTPS origin 与同 origin 浏览地址，拒绝凭据、query、fragment、控制字符、歧义路径和浏览器特殊 IPv4 表达。默认分支为保守应用子集，不调用 provider 或 Git。
- 新建、关联、解除与事件、Outbox、Activity v4 同事务提交。事件不复制外部 URL、身份或分支；关联事件只投影状态与受控 Repository 引用，精确 link ID 留在源事件。普通读不返回管理明细。
- Web 在既有“组件、环境与代码库”配置区提供创建、双向发现、显式关联和解除。外链仅在用户点击时打开并禁用 opener / referrer；未打开外部测试域名。
- 网络结果未知时冻结原输入，保留 operation 精确重试。关联表单与待解除关系独立于列表刷新，避免 focus 刷新丢掉原请求或把旧解除指向新关系；失权与作用域切换清理数据，迟到响应丢弃。

## 自动化证据

已执行并通过：

- `./scripts/check-server.sh`：Go race 测试、vet、`go mod tidy -diff`、模块校验。
- `./scripts/check-server-postgres.sh`：全套真实 PostgreSQL 集成；从正式 bootstrap 与邀请开始创建业务映射和关系。覆盖跨 Workspace 不可发现性、Project admin 不越权、精确重试、重复冲突、多对多和分页、双 owner 竞争、非 active 清理、账户 / 成员撤权与写入的两种串行顺序、五个提交位置故障回滚、正常投影与重建一致。
- `./scripts/check-server-backup-restore.sh`：临时源 / 全新目标恢复 Repository、active / removed 关系、事件、Audit 与 receipt；恢复后重放原请求，当前关系仍指向最后一次重新关联。
- migration 012→013 真实升级：旧配置 Audit / receipt、Activity 内容保持不变，只推进投影版本；不补造 Repository。旧二进制 schema readiness 拒绝新版本。旧 11 类配置命令摘要形状保持兼容。
- `./scripts/check-web.sh`：25 个文件、148 项测试，格式、Lint、TypeScript、构建及锁定依赖基线。覆盖未知响应精确重试、focus 刷新、旧解除代次、成员只读、安全外链与迟到响应。

初次数据库回归发现旧升级测试硬编码 Activity v3，已更新为匹配当前版本并补 012→013 原证据快照。Web 检查发现 README 格式和控制字符正则 Lint 问题，已修复后全量通过；没有放宽检查或增加依赖。

- `./scripts/check-repo.sh` 与 `git diff --check`：仓库基线、文档链接与文本卫生通过。

## 隔离 HTTPS 浏览器

当前任务授权后使用既有 `RADISHNEXUS_BROWSER_FOUNDATION=1` fixture、已缓存 PostgreSQL / Caddy 镜像和已安装 Chrome。只绑定本机随机端口；一次性证书仅用于测试浏览器，不修改系统信任。Repository / EntityLink 全部经正式页面创建，不用 SQL 预置业务映射或关系。

第一轮已完成正式 bootstrap owner 登录、Team / Component / Repository 创建、Component 关联、Repository 反向发现、手机端确认解除与重新关联。只读数据库核对为一条 removed 与一条 active，事件分别为创建 1、关联 2、解除 1；旧来源保留。关联通过聚焦按钮后 Enter 提交；1440px 桌面与 390px 手机截图已目视复核，手机 `scrollWidth=innerWidth=390`。本地截图在被忽略的 `output/playwright/repository-desktop.png` 和 `repository-mobile.png`。

Web 全量检查重建静态资源后，旧 fixture 的缓存入口仍引用原 hash，刷新出现测试资源 404；已按固定工件方式停止旧实例并以最终构建重启，继续成员 / 撤权验收。此项是验收环境内原地重建导致的版本不匹配，不是业务写入失败。

最终构建重启后，重新通过页面创建 Team / Component / Repository 并关联。随后仅在一次性库用 SQL 将测试 owner 降为 member、再暂停 membership，作为权限故障注入条件：member 能读取元数据 / 外链与反向组件，但创建和解除入口均为 0；暂停后刷新详情，详情、元数据和外链均为 0。此处不宣称通过了新的成员治理 UI。普通成员读取后的刷新无静态资源错误；初始登录检查的 401 与撤权后的拒绝响应属于预期。

两轮 fixture 均正常退出并完成清理，隔离 Chrome 会话已关闭。没有保留测试服务、数据库容器或临时证书卷；只保留被忽略目录中的本地截图和浏览器诊断材料。

## 未覆盖与接续

无 provider 连接验证、代码读取、仓库编辑 / 删除、commit 或 CI Run 来源推导、Jenkins 绑定或生产部署。真实 Jenkins → 新配置对象 → 浏览器记录 staging 的整链、持续终态采集、原生中文 IME 和真实团队持续使用仍未验收。

后续沿当前状态推进 Ticket / Component / 交付关系与持续采集。业务实例升级、依赖变更、提交及远程操作保持独立授权边界。
