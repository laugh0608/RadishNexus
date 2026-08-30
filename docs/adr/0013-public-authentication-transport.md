# ADR-0013：公共认证 Transport 与可信代理边界

状态：已接受

日期：2026-08-30

## 背景

ADR-0012 已冻结本地账号、服务端 Session、Cookie、CSRF、request ID 和错误对象，但明确禁止在 public origin、代理来源、客户端 IP、登录限流与真实 HTTPS 浏览器证据完成前开放公共登录路由。仅有账号级 5 次失败锁定无法约束未知账号、分布式来源或并发 Argon2id 工作；直接相信 `X-Forwarded-*` 又会让调用方伪造安全协议和客户端 IP。

当前正式 Go server 使用标准库 `net/http`，内部可以位于 TLS reverse proxy 后。M1 需要先开放最小 login / session / logout 闭环，再评估第一个只读业务端点；这一层不能引入 insecure Cookie、可信用户 Header、临时 fixture handler 或第二套认证口径。

## 决定

### Public origin 与安全传输

- 服务启动必须显式提供唯一的 `RADISHNEXUS_PUBLIC_ORIGIN`，值必须是无 path、query、fragment、userinfo 和尾部 `/` 的精确 `https` origin。所有认证路由同时要求请求 `Host` 与该 origin 的 host 精确一致。
- 正式 server 继续使用内部 HTTP listener；浏览器请求必须先经过 TLS reverse proxy。`RADISHNEXUS_TRUSTED_PROXY_CIDRS` 必须列出直接连接 server 的精确可信代理 CIDR，缺失、非规范 CIDR或重复值均使服务启动失败。
- transport 也接受 Go request 已携带 TLS 状态的直接 HTTPS 请求，供未来原生 TLS 入口和真实测试复用；此时客户端 IP 只取直接 peer，所有转发 Header 均忽略。
- plaintext 请求只有在直接 peer 位于可信 CIDR、恰好存在一个值为 `https` 的 `X-Forwarded-Proto`，且恰好存在一条完整 `X-Forwarded-For` 链时才被接受。链中每个 IP 必须使用规范文本表示；transport 从右向左剥离可信代理，取第一个非可信地址作为客户端 IP。缺失、重复或畸形值 fail closed。
- transport 不读取 `Forwarded`、`X-Real-IP`、`X-Forwarded-Host` 或用户身份 Header。proxy 必须覆盖而非追加来自公网的安全协议信息，并保留原始 `Host`；CIDR 只应覆盖真实直接代理，不应为了方便信任整个网络。

### 登录抗滥用与请求边界

- `POST /api/v1/auth/sessions` 在进入 Argon2id verifier 前，以解析后的客户端 IP 执行每进程固定窗口限流：60 秒最多 5 次尝试。
- 进程最多跟踪 4096 个客户端窗口；容量已满且没有过期窗口时，新客户端同样得到 `429 rate_limited`，不通过无界 map 消耗内存。每进程同时最多执行 4 个密码校验，槽位已满时快速返回 `429`。
- `429` 返回整数秒 `Retry-After`。账号级 5 次失败后锁定 15 分钟继续独立生效；IP 限流成功与否不改变统一的 `invalid_credentials` 账号外部语义。
- 当前限流是单进程过载和基础暴力破解边界，不冒充跨副本或全局限流。多副本、暴露公网或高风险部署必须在受控 reverse proxy / gateway 增加全局限流与观测；没有真实需求前不为此提前引入 Redis。
- 登录 body 上限为 4096 bytes，只接受 `application/json` 和可选 `charset=utf-8`；未知字段、畸形 JSON、多个 JSON 值、其它 charset 与其它 media type 均失败。密码和原始 body 不进入响应或日志。

### 公共认证路由

只开放以下三个 `/api/v1` 路由：

- `POST /api/v1/auth/sessions`：要求安全 transport、精确 Host、精确 `Origin` 和受控 JSON；成功返回 `201`、两个 ADR-0012 Cookie，以及不包含 Session / CSRF token 的 user、Workspace 列表和绝对过期时间。
- `GET /api/v1/auth/session`：要求安全 transport、精确 Host 和有效 Session cookie；成功返回同一安全 Session context，不要求非安全方法才需要的 CSRF。
- `DELETE /api/v1/auth/session`：要求安全 transport、精确 Host、精确 `Origin`、Session cookie、CSRF cookie、`X-CSRF-Token` 和数据库 digest 一致；成功单向撤销 Session，返回 `204` 并过期两个 Cookie。

所有成功与失败响应使用 `Cache-Control: no-store`。每个请求由 server 生成新的 `req_` request ID 并覆盖调用方输入；公共错误继续使用 ADR-0012 的版本化 JSON envelope。新增稳定错误码为 `secure_transport_required`、`invalid_origin`、`invalid_proxy_chain`、`rate_limited`、`payload_too_large`、`unsupported_media_type` 与 `method_not_allowed`；认证子树的未知路径继续使用通用 `not_found`。不开放 credentialed CORS，也不在本切片开放业务 HTTP route。

## 未采用的方案

### 信任任意 `X-Forwarded-*`

Header 本身不能证明请求经过了 TLS 或可信 proxy。只有直接 peer 信任、协议值和完整转发链一起验证，客户端 IP 才能用于安全决策。

### 纯 HTTP localhost 与 insecure Cookie

开发环境特例会形成第二套浏览器行为并掩盖 `__Host-`、`Secure`、Origin 和反向代理配置错误。开发和验证同样使用 HTTPS 入口。

### 只依赖账号锁定

攻击者可以轮换登录名、集中消耗 Argon2id 或利用不存在账号的 dummy verifier。账号锁定、IP 窗口和密码工作并发上限解决不同问题，不能互相替代。

### 立即引入 Redis 全局限流

当前正式部署仍是单进程模块化单体，尚无多副本登录流量证据。先建立有界进程内保护和明确部署责任；出现多副本或真实容量需求后再冻结共享限流的一致性、失败模式与运维依赖。

## 后果

正面影响：

- 公共登录不再相信用户可控的协议、IP 或 request ID Header；
- 密码校验同时受到客户端窗口、账号锁定和进程并发上限约束；
- login / session / logout 共享唯一 Cookie、Origin、CSRF、错误和 Session application service；
- 第一个业务 HTTP route 可以直接复用已验证 Session 与 Workspace membership resolver，不需要临时身份入口。

成本与风险：

- 部署者必须正确配置 HTTPS public origin、直接代理 CIDR、Host 保留和转发 Header 覆盖；配置错误会 fail closed；
- 当前限流不跨进程共享，多副本或公网部署仍需外层全局保护；
- fixed window 在边界附近允许短时突发，后续只有真实流量证明需要时才更换算法；
- 本切片仍不包含 OIDC、邀请、密码重置、MFA、业务 API、审计查询或 Web 登录页面。

## 迁移与验证

- server 启动新增必需配置 `RADISHNEXUS_PUBLIC_ORIGIN` 与 `RADISHNEXUS_TRUSTED_PROXY_CIDRS`；数据库 schema 无变化。
- unit tests 覆盖 direct TLS、可信代理链右向左解析、转发 Header spoofing、非安全来源、畸形链、精确 Host / Origin、固定窗口、跟踪容量、密码并发、JSON 上限、Cookie 安全属性、CSRF 和 request ID 覆盖。
- 真实 PostgreSQL integration 通过正式 store 与 handler 覆盖 login → Cookie → session → logout → revoked Session 生命周期；响应不包含原始 Session 或 CSRF token。
- 测试专用 build-tag HTTPS fixture 只复用正式 handler，不形成生产路由。真实 Chrome 已验证 `Secure` / `HttpOnly` / `SameSite=Strict` Cookie、有效 Session 读取、错误 CSRF 不撤销、正确 CSRF 登出并清除 Cookie，以及第二 HTTPS Origin 登录得到 `403 csrf_failed`。
