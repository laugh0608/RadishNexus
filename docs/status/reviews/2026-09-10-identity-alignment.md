# 2026-09-10 账户与联合登录调整

基线：`73cc04b`；范围按项目所有者确认的 [ADR-0023](../../adr/0023-local-account-and-radish-oidc-login.md)。项目所有者随后明确将 Radish OIDC 延至未来规划，并授权提交本地账户改动、继续 schema readiness。本记录区分已实现路径和未来接入。

## 已实现

- migration 008 从原本地账户提取 `user_accounts`，保留稳定用户、历史作者与业务关联；密码凭证使用 `local_credentials`。邮箱由管理员显式映射，不推导假邮箱或重建账户；升级吊销旧 Session。
- 新 bootstrap 接收标准输入 JSON 邮箱 / 密码；新 `nexus-identity-migrate --mapping-stdin` 支持原子映射，重复、无效、未知或已经映射的输入使整批回滚；输出不包含邮箱或密码。
- 正式本地登录使用 `email / password`，展示名独立。会话和 Workspace resolver 校验独立账户状态；密码锁定不会变成账户全局禁用。
- 一次性邀请、邀请者当前 owner 校验、原子创建账户 / 普通成员 / Session、已登录用户加入 Workspace；邀请码不写入 URL 或 Web Storage。公开注册保持关闭。
- 账户页和邀请入口、严格 Web consumer；本地密码存在 / Radish 绑定状态由服务端提供。敏感操作继续使用同源 Origin、Cookie / Header / digest CSRF，并共享有界认证请求与并发门禁。
- OIDC 授权事务、浏览器绑定、一次性消费、显式 link、近期认证与发起 Session 重查、解绑与最后登录方式保护、绑定冲突、账户禁用和 Session 撤销的领域 / PostgreSQL / transport 实现。
- 备份包含账户、密码凭证与外部身份映射；Session、未兑换邀请和 OIDC 授权事务仅保留 schema。

## 延后规划与验收边界

- Radish OIDC 按项目所有者要求延后，候选依赖方案留在 ADR-0023，恢复时重新审查；未改 `server/go.mod / go.sum`，未下载或安装这些依赖。
- `radishoidc` 当前只有配置校验；真实 discovery / token exchange / JWKS 验签适配器尚未实现和装配。正式 server 的 `radish` capability 为 `false`，OIDC start 返回不可用；测试 provider 仅证明事务边界，不宣称密码学验证通过。
- 新页面尚未执行真实浏览器验收；旧 Activity / Relations 的浏览器证据不用于本次身份变更。
- 真实 Radish client registration、exact issuer / redirect、secret 与签名策略、测试账户和真实浏览器联调尚未提供或执行。
- 未对真实数据库执行 migration 或账户映射；未构建新的 Compose 镜像、启动用户实例或变更 Radish / RadishMind 工作区。
- 本轮只进行本地提交，不包含推送或远程操作。

## 验证

已运行：

- `./scripts/check-server-postgres.sh`：真实 PostgreSQL 通过本地身份、邀请并发单胜者、权限撤销、外部身份准入、state 重放 / 浏览器绑定、显式绑定、最后登录方式与 Session 撤销测试；额外隔离数据库从 migration 007 升级到 008，验证旧账户 / verifier / owner 关联、重复映射回滚和禁用保持。
- `./scripts/check-server-backup-restore.sh`：两个临时 PostgreSQL 实例往返通过，身份权威表保持，Session、邀请和 OIDC 事务不恢复。
- Web 定向交互测试覆盖登录方式配置、邀请码兑换、显示名与邮箱分离、解绑确认、错误不变成登录成功、严格响应与不安全 URL 拒绝。
- `git diff --check` 与仓库文档 / 文本检查通过。

- `./scripts/check-server.sh`：Go race、vet、`go mod tidy -diff` 与模块校验通过，没有模块图变化。
- `./scripts/check-web.sh`：79 个测试、格式、Lint、类型检查、production build 与 152 个现有锁定 package 检查通过。

上述事务测试不等价于真实 OIDC 验签、浏览器认证或生产可用性结论。
