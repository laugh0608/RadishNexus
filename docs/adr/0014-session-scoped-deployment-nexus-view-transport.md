# ADR-0014：Session 作用域下的 Deployment Nexus View Transport

状态：已接受

日期：2026-08-30

部分已替代：[ADR-0015](0015-same-origin-authenticated-web-shell.md) 已替代“Web 根路径继续保留静态状态检视器”的阶段性决定；本 ADR 的业务路由、权限、公共 DTO 与缓存合同继续有效。

## 背景

ADR-0011 已冻结 Deployment 的 application 读取权限和安全投影，但当时本地身份、Session、可信代理与公共错误对象尚未建立，因此明确禁止为了原型增加临时业务路由。ADR-0012 与 ADR-0013 随后建立了服务端 opaque Session、当前 Workspace membership resolver、精确 HTTPS public origin、可信代理、Secure Cookie、request ID 和版本化错误对象。

现在需要用第一个只读业务端点验证同一安全边界能否承载真实业务数据，并让 Web 从静态代表原型进入类型化消费。若把 Session 固定到 Workspace、信任客户端用户 Header、直接序列化内部 application struct，或为 Web 另建 fixture 身份和错误语义，都会破坏已经冻结的身份、权限与兼容边界。

## 决定

### 路由与 Workspace 选择

- 第一个业务路由固定为 `GET /api/v1/workspaces/{workspace_id}/deployments/{deployment_id}/nexus-view`；Workspace 由稳定路径参数显式选择，不写入 Session，也不从客户端 Header 推断。
- 路由复用 ADR-0013 的精确 Host、TLS / trusted proxy、客户端地址和 Session Cookie 校验；当前不开放 credentialed CORS 或 insecure HTTP fallback。
- Session resolver 必须用路径中的 Workspace ID 重新验证当前 active membership，再把 `VerifiedUser` 转换为 application `Principal`，最后调用 ADR-0011 的 `GetNexusView` query。transport 不读取角色 Header、用户 Header 或 OIDC claim。
- `GET` 是唯一允许的方法；包括 `HEAD` 在内的其它方法返回版本化 `method_not_allowed`。读取不要求 CSRF token，因为它不改变服务端状态。

### 不可发现性与错误

- Session 缺失或失效继续返回 `unauthenticated`；合法 Session 但当前 membership 不可用时收敛为 `not_found`，与未知、跨 Workspace 或 application 不可读的 Deployment 保持不可区分。
- 语法无效的稳定 ID 返回版本化 `invalid_request`；未注册路径返回版本化 `not_found`。未知内部失败只返回通用错误和 server-generated `request_id`。
- handler 在输出前验证 application projection 是否仍符合 ADR-0011：Current、`deploys` Relation、`deployment.recorded` Timeline、引用、状态、actor 和 subject 必须互相一致。投影漂移 fail closed，不能发送部分结果或原始错误。

### 公共响应 DTO

- 成功响应使用 `{"data": ...}` envelope。引用在 JSON 中保持 `{ "type", "id" }` 结构；Web 展示时才转换为 `entity://type/id`，不把内部 Go struct 的字段布局冻结为公共协议。
- Current 只包含 Deployment ref、`succeeded / failed / canceled`、nullable `started_at`、`completed_at`、`recorded_at`，以及可读 Environment 和来源 CI Run 的 ref / title。
- Relation 使用 `visibility` discriminant；`readable` 只包含 `deploys` 和来源 CI Run，`restricted` 不携带目标身份或关系线索。
- Timeline 只包含稳定事件 ID、`deployment.recorded`、安全 user actor、发生时间、status 和权限过滤后的 subjects；restricted subject 不携带引用或标题。
- 响应不得包含 deployment authorization、调用 source、membership / role、Jenkins source、external run key、delivery receipt、digest、Secret、原始 payload、执行日志或外部 URL。

### 缓存与 Web 消费

- 成功与错误响应统一设置 `Cache-Control: private, no-store` 和 `Vary: Cookie`，避免权限撤销、Session 切换或共享缓存复用旧结果。当前不增加 ETag、离线缓存或 stale fallback。
- Web 的 canonical 页面路径为 `/workspaces/{workspace_id}/deployments/{deployment_id}`。该路径通过同源 `fetch`、`credentials: same-origin` 和 `cache: no-store` 读取公共端点，并在适配展示模型前运行时校验响应。
- Web 根路径继续保留明确标注的静态状态检视器；它不是隐藏 fallback。真实页面加载失败必须显示错误并允许重试，不能回退到 fixture 冒充成功。
- 当前不为一个页面引入 router、状态库、生成式 API client 或第二套实体引用格式。

## 替代方案

### Session 在登录时固定 Workspace

这会让 Workspace 切换、membership 撤销和多 Workspace 用户产生过期授权，并与 ADR-0012 已接受的 Session 边界冲突。业务请求必须按路径重新解析 membership。

### 直接序列化 application `NexusView`

内部模型同时服务 Decision、Ticket、CI Run 和 Deployment，包含公共接口不应承诺的字段与投影细节。直接序列化会扩大敏感面，并让内部重构变成公共破坏性变更。

### Web 在 404 时加载静态 fixture

这会把权限失败或数据缺失展示为成功，掩盖真实契约问题。fixture 只属于独立代表入口，不能成为真实路径 fallback。

### 首先开放 Deployment 列表或写入端点

列表需要额外冻结排序、分页、可发现性和摘要字段；写入还需要 CSRF、确认、幂等与审计协议。先完成一个只读对象页能以更小范围验证认证、权限、DTO 和 Web 适配链。

## 后果

正面影响：

- ADR-0011 的读取权限第一次通过正式公共 transport 和 Web 类型化 adapter 复用，没有建立第二套身份或权限判断；
- Workspace 选择、membership 撤销、not-found 不可发现性和无缓存响应形成后续业务读取的可复用基线；
- 公共 DTO 与内部 application model 解耦，客户端对错误和契约漂移 fail closed；
- 静态代表原型和真实数据路径职责明确，真实路径不会用 fixture 隐藏失败。

成本与风险：

- 当前 Web 仍没有 login / session bootstrap、Workspace 导航或 Deployment 发现入口；只能从 canonical URL 消费已知 Deployment ID；
- 当前 actor 只提供稳定 user ID，成员展示名和历史身份快照尚未冻结；
- 当前验证分别覆盖真实 PostgreSQL → HTTP 与真实浏览器 → Web adapter，尚未形成一次完整的 HTTPS browser → login → PostgreSQL → page 自动化；
- 每个新增业务 DTO 都需要显式映射和运行时校验，不能依赖内部类型自动传播。

## 迁移与验证

本决策不新增数据库 migration，复用 migration 004 的 Deployment 数据与 migration 005 的本地身份 / Session。

验证必须覆盖：

1. Session → current Workspace membership → `VerifiedUser` → `Principal` → `GetNexusView` 的精确调用链；
2. 未登录、无 membership、跨 Workspace、未知 Deployment、错误方法、错误 Host / transport 和 projection 漂移的失败语义；
3. 成功 DTO、nullable started time、restricted 投影、敏感字段排除、`private, no-store` 与 `Vary: Cookie`；
4. 真实 PostgreSQL 上的可读、跨 Workspace 与未知对象 HTTP 请求；
5. Web URL、请求选项、运行时解析、canonical EntityRef、loading / error / retry 与真实浏览器渲染；
6. 服务端、Web 与整仓门禁继续通过，且不新增依赖。
