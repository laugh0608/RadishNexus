# ADR-0015：同源 Authenticated Web Shell 与显式静态资源装配

状态：已接受

日期：2026-08-30

部分替代：[ADR-0014](0014-session-scoped-deployment-nexus-view-transport.md) 中“Web 根路径继续保留静态状态检视器”的阶段性决定

## 背景

ADR-0012、ADR-0013 与 ADR-0014 已分别建立服务端 Session、公共认证 transport 和第一个 Deployment Nexus View 业务读取。此前 Web 根路径仍是静态代表入口，canonical 页面只能从已知 URL 单独消费业务 DTO；真实 PostgreSQL 认证验证与真实浏览器 adapter 验证也是两段证据，尚不能证明浏览器登录、Workspace 选择、Session 权限解析和业务页面在同一部署边界内能够闭环。

下一步需要建立最小 authenticated Web Shell，但不能因此引入第二套身份、客户端 token storage、credentialed CORS、隐藏 fixture fallback，或提前开放 Deployment 列表和写入。Web build、Go transport 与 TLS reverse proxy 的职责也需要明确，否则部署容易依赖工作目录、任意 SPA fallback 或漂移的静态缓存规则。

## 决定

### Same-origin 交付与装配

- 浏览器只访问 ADR-0013 冻结的精确 HTTPS public origin；TLS reverse proxy 仍是唯一公共入口，并把 Web 页面、静态资源和 `/api/v1` 转发给同一个 Go server。当前不开放 credentialed CORS，也不增加第二个静态站点 origin。
- Go server 通过必需的 `RADISHNEXUS_WEB_ROOT` 装配 Vite production build。该值必须是已存在的绝对目录，且包含非空 `index.html`；缺失、相对路径或无效 build 必须让进程启动失败，不能依赖当前工作目录或内置开发 fixture。
- API 路由优先于 Web catch-all 装配。HTML 只在 `/`、`/prototype/nexus-view` 和 canonical `/workspaces/{workspace_id}/deployments/{deployment_id}` 上返回；未知页面路径返回 `404`，不使用任意 SPA fallback 掩盖未注册路由。
- `/assets/` 只读取 build root 下的合法常规文件。带内容哈希的资源使用 `public, max-age=31536000, immutable`；HTML 使用 `no-cache`；ADR-0013/0014 的认证与业务 API 继续保持各自的 `no-store` 边界。
- HTML 与资源响应设置同源 CSP、`nosniff`、拒绝 framing、`no-referrer`、最小 Permissions Policy 和 same-origin opener policy。当前不加载远程字体、脚本或样式，也不为开发便利放宽 production CSP。

### Session bootstrap 与最小导航

- Web 根路径改为 authenticated shell。首次加载先调用现有 `GET /api/v1/auth/session`；`401 unauthenticated` 显示登录表单，网络、服务或契约错误显示可重试错误，不能把系统失败冒充为“未登录”。
- 登录只调用现有 `POST /api/v1/auth/sessions`。密码只存在于受控表单状态和当前请求体，不写入 URL、日志、`localStorage`、`sessionStorage` 或自建 Cookie；登录成功直接使用服务端返回的 Session context。
- Shell 使用 Session context 中的当前 active Workspace memberships 供用户选择，但不把选择写入 Session。业务请求仍以路径 Workspace 重新验证 current membership，浏览器中的 Workspace 选项不授予权限。
- 在尚无 Deployment list API 时，根路径只提供“选择 Workspace + 输入已知稳定 Deployment ID”的受控入口。客户端只接受正式 `dpl_` ID 并导航到 canonical 路径；不从 fixture 猜测对象，也不新增发现或写入端点。
- canonical Deployment 路径必须先完成 Session bootstrap，再调用 ADR-0014 的业务读取；业务读取返回 `401` 时回到登录态。用户可在 canonical URL 直接登录并继续读取同一对象。
- 登出只调用现有 `DELETE /api/v1/auth/session`，从可读的 `__Host-radishnexus-csrf` Cookie 取得 CSRF token；成功后清除客户端 Session context，正式 transport 负责撤销 Session 和清除 Cookie。
- 原静态代表检视器移动到显式 `/prototype/nexus-view`。它继续用于受控状态复核，但不参与认证、导航或真实请求失败 fallback。
- 当前 pathname adapter 足以覆盖三个已注册页面，不为该切片引入 router、状态库、组件库或第二套 API client。

### 端到端验证边界

- 建立只在显式 `integration && browserfixture` build tags 下启用的真实 fixture：运行正式 migration，通过 application service 记录 CI Run 与 Deployment，写入正式本地账号 verifier，并用真实 PostgreSQL store 装配认证、业务 handler 和 production Web build。
- fixture 使用本机临时 HTTPS listener、一次性数据库容器、受限测试账号和显式 stop 文件；结束后清理容器和临时状态。测试凭据只属于可丢弃 fixture，不能复用为产品默认账号。
- 浏览器验证必须覆盖匿名 Session、登录、Secure/Strict Cookie、无浏览器 storage、Workspace 入口、canonical Deployment 读取、窄屏、登出、Cookie 清理，以及在 canonical URL 上重新登录。业务页控制台与网络响应不得依赖 fixture fallback。

## 替代方案

### 把 Web build 嵌入 Go 二进制

嵌入可以减少部署文件，但会把前端 build 时机、二进制体积和缓存变更绑定在一起。当前构建与容器交付尚未冻结，显式绝对 build root 更容易检查、替换和在失败时停止；未来若采用 embed，应由新的构建与发布决策替代。

### 由 reverse proxy 单独托管 Web

这会增加两套路由和缓存配置，并需要保证 canonical SPA 路径、API 与安全 Header 长期同步。当前最小模块化单体由 Go server 统一交付更容易形成唯一合同，reverse proxy 只负责 TLS、Header 清洗和全局保护。

### 对所有未知路径返回 `index.html`

任意 SPA fallback 会把拼写错误、尚未实现的产品路径和静态资源缺失伪装成成功 HTML。当前页面集合很小，显式 allowlist 更符合 fail closed；新增正式页面时必须同时更新 handler 和测试。

### 先开放 Deployment 列表

列表会同时冻结发现权限、分页、排序、摘要 DTO 和空状态。已知稳定 ID 入口足以关闭 browser → Session → PostgreSQL 的证据缺口，因此列表保留为后续独立纵向切片。

## 后果

正面影响：

- 浏览器、Web、认证 transport、current membership、正式 Deployment query 与 PostgreSQL 第一次在同一 HTTPS origin 内形成完整可复验链路；
- Web 不保存身份 token 或权限快照，Session 和 Workspace 权限继续以服务端当前状态为准；
- production Web build 的来源、启动失败语义、页面 allowlist、安全 Header 与缓存边界形成明确部署合同；
- 静态原型与真实产品入口分离，fixture 不再占用根路径或掩盖真实失败。

成本与风险：

- 每次运行 server 前必须先产出 Web build 并提供绝对路径，后续自部署工件必须把两者正确装配；
- 新增正式客户端路由需要同步更新 Go 页面 allowlist，不能只改 React；
- 当前入口仍依赖用户持有 Deployment 稳定 ID，没有对象发现、完整导航或管理入口；
- 匿名 Session 探测会产生预期的 `401` 网络响应；应用必须把它作为受控登录状态处理，但不能吞掉其它认证失败。

## 迁移与验证

本决策不新增数据库 migration，也不改变 ADR-0013 的 Cookie / CSRF / public origin 合同或 ADR-0014 的业务 DTO。部署配置新增必需的 `RADISHNEXUS_WEB_ROOT`，旧的“只启动 Go API、不装配 Web build”方式不再是正式 server 入口。

验证必须覆盖：

1. 无效、相对、缺少 `index.html` 的 Web root 启动失败；显式 HTML 路径、资源、缓存、安全 Header、未知路径和方法语义；
2. Session bootstrap、登录错误、Workspace 选择、稳定 Deployment ID、canonical route、业务 `401`、登出和重试状态；
3. auth client 的同源 credentials、`no-store`、公共错误映射、运行时响应校验和 CSRF Cookie 读取；
4. 真实 PostgreSQL + migration + application 写入 + HTTPS + production Web build 的浏览器登录到 Deployment 读取，并验证退出后的 Session 撤销；
5. 桌面与 390px 窄屏布局、Cookie flags、浏览器 storage、业务页 console 和公共响应缓存边界；
6. server、Web 与整仓门禁继续通过，且不新增依赖或业务端点。
