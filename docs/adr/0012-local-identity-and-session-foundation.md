# ADR-0012：本地身份与服务端 Session 基线

状态：已接受

日期：2026-08-30

## 背景

M0.5 已证明 Golden Path 的写入、权限过滤读取和 PostgreSQL 备份恢复，但正式 HTTP transport 仍没有可验证的用户身份来源。现有 `authn.VerifiedUser` 只是认证成功后的内部适配边界；它不读取 Header、Cookie 或 OIDC claims，也不能把测试 fixture 或可信 Header 冒充成认证。

M1 需要先让自部署实例能够建立首个管理员、登录并选择 Workspace，再开放任何业务 API。首期同时引入 OIDC 会额外冻结 issuer、redirect URI、claim mapping、账号关联和 provider 失效语义，无法减少本地账号、恢复与应急访问的基础工作，因此本切片先建立本地账号和服务端 Session，OIDC 后续复用同一用户与 Session 边界。

## 决定

### 首次管理员与 Workspace 角色

- 新实例通过显式 `nexus-bootstrap` 运维命令创建唯一的首个本地账号、用户、Workspace 与 membership；命令不随服务启动执行，不生成默认密码，也不接受命令行参数中的密码。
- bootstrap 密码只从标准输入读取；成功输出只报告稳定 user / Workspace ID，不回显登录凭据、密码 hash、Session 或 DSN。
- bootstrap 在 PostgreSQL 事务和固定 advisory lock 下检查 `local_accounts` 为空；已经存在任何本地账号时失败，不提供覆盖、重置或第二次初始化模式。
- `workspace_memberships.role` 首期只允许 `owner / member`。bootstrap membership 是 `owner`，迁移前已有 membership 统一得到 `member`；Workspace owner 不隐式获得 restricted Project 或 Environment Deployment 权限。
- 登录名是不可变的 3–64 字符小写 ASCII 标识，只允许字母、数字、点、下划线和连字符；显示名继续独立保存在 `users`。这避免依赖 email 投递、Unicode 大小写和隐式账号合并语义。

### 密码与登录失败

- 密码只保存为自描述、版本化的 Argon2id verifier；每个密码使用独立的 16-byte CSPRNG salt，当前参数为 19 MiB memory、2 iterations、1 parallelism、32-byte output。
- 首期密码长度为 15–128 个 Unicode code point，UTF-8 编码不得超过 1024 bytes；不强制大小写、数字或特殊字符组合，不截断密码。
- 当前 verifier 参数随 hash 保存，以便未来登录后升级；解析器对算法、版本、参数和编码长度 fail closed。未知登录名仍执行固定 dummy verifier，外部只得到统一的 `invalid_credentials`。
- `local_accounts` 可以被显式设为 `disabled`。连续 5 次失败会锁定该账号 15 分钟；锁定、禁用和不存在均不向调用方泄漏差异。IP / reverse-proxy 级限流仍是正式公网暴露前必须补齐的 transport / deployment 边界，不能用账号锁定冒充完整抗滥用能力。

密码实现固定直接依赖 Go 团队维护、[BSD-3-Clause](https://github.com/golang/crypto/blob/v0.50.0/LICENSE) 的 [`golang.org/x/crypto v0.50.0`](https://pkg.go.dev/golang.org/x/crypto/argon2)，只使用 `argon2` package；模块图同时新增 `x/sys v0.43.0`，并把既有间接 `x/sync` 从 `v0.17.0` 升到 `v0.20.0`、`x/text` 从 `v0.29.0` 升到 `v0.36.0`。所有版本与 checksum 固定在 `go.mod / go.sum`。Go 1.25 标准库虽有 PBKDF2，但当前没有 FIPS 强制条件，选择次优 KDF 只为避免依赖不符合长期安全收益；自行实现 Argon2id 的审计和维护风险更高。

### Session 与 Workspace 选择

- 登录成功创建 32-byte CSPRNG opaque Session token 和独立 32-byte CSRF token；数据库只保存各自的 SHA-256 digest，不保存原 token。
- Session 使用固定 24 小时绝对有效期，不做静默滑动续期。登出只设置一次 `revoked_at`；过期或 revoked Session 都不可再认证。
- active 且未过期的 Session 不允许物理删除；revoked 或 expired 行可以由后续受控清理任务删除，避免非权威登录态无界增长。认证与安全审计不得依赖 Session 行永久保留。
- Session 只绑定 user，不固定 Workspace。会话上下文列出该用户当前 active membership；业务路由必须携带稳定 Workspace ID，再由 resolver 以当前 membership 状态构造 `VerifiedUser`。引用、Header、Cookie 或上次选择都不能授予 Workspace 权限。
- `local_accounts` 是恢复登录能力所需的权威数据，纳入受控 PostgreSQL 运维备份；`user_sessions` 只备份 schema、不备份数据，因此恢复实例保留账号 verifier，但所有旧登录态和 CSRF token 自动失效。这不是未来 `.nexus` 可移植导出携带凭据的授权。

### Cookie、CSRF 与公共 transport

- 浏览器 Session cookie 固定为 `__Host-radishnexus-session`，属性为 `Secure`、`HttpOnly`、`SameSite=Strict`、`Path=/` 且无 `Domain`；CSRF cookie 为 `__Host-radishnexus-csrf`，同样 `Secure`、`SameSite=Strict`、`Path=/` 且无 `Domain`，但允许 Web 客户端读取并放入 `X-CSRF-Token`。
- 登录请求必须使用 `application/json` 并通过配置的单一 public origin 精确校验；不开放 credentialed CORS。所有基于 Session cookie 的非安全方法同时要求精确 `Origin`、CSRF cookie、`X-CSRF-Token` 和数据库 digest 一致。
- 服务端 HTTP 可以位于受控 TLS reverse proxy 后，但正式浏览器认证不提供 insecure-cookie fallback。开发环境需要同样使用 HTTPS 入口；不能因为 localhost 方便形成第二套 Cookie 契约。
- 公共 API 使用 `/api/v1`。每个请求由服务端生成 `req_` 稳定格式的 request ID，并通过 `X-Request-ID` 返回；不信任客户端提供的 request ID。
- 错误响应固定为 JSON `{"error":{"code","message","request_id"}}`，不包含内部原因、SQL、账号存在性、权限细节或 credential。不可读业务对象仍映射为 `not_found`，transport 不把它改写成 `forbidden`。

本 ADR 冻结 transport 契约，但本切片先完成 bootstrap、credential verifier、Session store 与 Workspace resolver；在 IP / proxy 限流和 public-origin 部署配置完成前，不把登录或业务 application service 暴露为公共 HTTP 路由。

## 未采用的方案

### 本地账号与 OIDC 同时首发

OIDC 不能替代首个本地管理入口，反而会立即引入 provider、claim mapping、账号关联和 callback 安全语义。后续 OIDC verifier 应解析为同一内部 user，再签发相同服务端 Session，而不是建立第二套权限主体。

### 可信用户 Header 或 fixture handler

Header 只说明请求携带了字符串，不能证明谁完成了认证、谁能设置该 Header 或 membership 是否仍 active。它会让业务 API 在没有身份边界时提前形成公共兼容债务。

### JWT 作为浏览器 Session

当前需要即时响应账号禁用、membership 暂停、登出和恢复后全量失效。数据库 digest Session 能直接表达这些状态；JWT 会额外引入签名密钥、撤销列表和 claim 漂移，而没有减少数据库授权读取。

### 在数据库中保存原始 Session token

数据库泄漏会直接变成可重放登录态。digest 足以查找和吊销 Session，原 token 只存在于浏览器 Secure Cookie。

### 自动 bootstrap 或默认管理员密码

服务启动时隐式初始化会让部署竞态、日志和默认凭据决定实例控制权。bootstrap 必须是一次性、显式、失败关闭的运维动作。

## 后果

正面影响：

- 自部署实例拥有不依赖官方云或外部 IdP 的恢复与应急登录根路径；
- OIDC 可以复用 user、membership、Session 与 CSRF 边界，不需要平行权限模型；
- Workspace 选择始终重新验证当前 membership，不把 Session 或引用当作授权；
- 数据库泄漏不会直接暴露可重放 Session token，恢复也不会延续旧登录态；
- Cookie、CSRF、request ID 和错误对象在首个业务 HTTP 端点前已有唯一合同。

成本与风险：

- 首期只有本地账号，不包含邀请、密码重置、MFA、OIDC、SCIM 或账号合并；
- 账号锁定不能替代 reverse-proxy / IP 级限流，公共登录路由仍需后续部署证据；
- Session 数据不备份意味着恢复后所有用户必须重新登录；
- Secure `__Host-` Cookie 要求浏览器入口使用 HTTPS，纯 HTTP localhost 不能冒充正式认证路径；
- 受控数据库备份包含密码 verifier，备份工件必须继续按敏感运维资产保护。

## 迁移与验证

- migration 005 为既有 Workspace membership 增加默认 `member` 角色，建立 `local_accounts` 与 `user_sessions`，并用约束和 trigger 保护身份、digest、有效期与单向撤销。
- unit tests 覆盖输入规范化、密码长度、Argon2id 编码与拒绝路径、统一 credential 错误、Session digest、过期 / revoked、CSRF 和 Workspace membership 解析。
- PostgreSQL integration tests 覆盖一次性并发 bootstrap、owner membership、失败锁定、登录成功重置、Session 创建 / 解析 / 吊销，以及暂停和跨 Workspace 的 fail-closed 行为。
- 备份恢复往返必须证明 `local_accounts` 保留、`user_sessions` 数据为空，并继续拒绝任何未分类 relation。

在开放 `POST /api/v1/auth/sessions` 前，必须补齐 public origin、代理来源与 IP 限流的部署合同，并以真实 HTTPS 浏览器验证 Cookie 和 CSRF；在开放第一个业务端点前，必须复用本 ADR 的 request ID、错误对象和 Workspace resolver。
