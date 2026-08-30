# RadishNexus Web

`web/` 是 RadishNexus 第一正式产品形态的 React + TypeScript 入口。当前根路径已经建立最小 authenticated Web Shell，消费正式 login / session / logout transport，允许从当前 Session context 选择 Workspace，并用已知稳定 Deployment ID 进入 canonical `/workspaces/{workspace_id}/deployments/{deployment_id}` 页面。canonical 页面通过类型化 adapter 消费第一个真实业务 HTTP DTO；原 Decision、CI Run 与 Deployment 代表原型移动到显式 `/prototype/nexus-view`，不参与真实失败 fallback。

## 本地运行

使用 Node 24 LTS 与 npm 11：

```bash
cd web
npm ci
npm run dev
```

从仓库根执行可信检查：

```bash
./scripts/check-web.sh
```

该检查覆盖 Prettier、Oxlint、Vitest + jsdom 状态测试、严格 TypeScript、Vite production build，以及 lockfile 来源、integrity、许可证和 lifecycle script 基线。`npm ci` 默认受 `.npmrc` 约束，不执行依赖 lifecycle scripts。

需要人工复核完整 browser → HTTPS → Session → PostgreSQL 链路时，从仓库根显式运行：

```bash
./scripts/run-authenticated-web-browser-fixture.sh
```

该入口会先构建 Web，再启动一次性 PostgreSQL 容器和带临时证书的 HTTPS fixture，并输出 origin、测试账号、canonical Deployment path 与 stop 文件。它不会拉取缺失镜像，也不是产品默认账号或常驻开发服务；浏览器复核结束后创建输出的 stop 文件，fixture 会退出并清理容器与临时状态。

## 当前边界

- Web 只消费已经按当前主体过滤的 `NexusViewData`，不接收角色或权限集合，也不在浏览器中重新判断对象可读性。
- `restricted` 条目在类型上不携带 EntityRef、对象类型、关系类型、标题、来源或时间；`hidden` 条目不进入客户端数据。
- 根路径先通过 `GET /api/v1/auth/session` bootstrap；只有稳定 `unauthenticated` 错误进入登录表单，网络、服务和响应契约错误必须显式失败并允许重试。
- 登录密码只保留在受控表单和当前同源请求体，不写入 URL、浏览器 storage 或自建 Cookie。Session token 只由服务端 `HttpOnly` Cookie 承载；登出从可读 CSRF Cookie 构造正式请求。
- Workspace 选择来自 Session context，但不写入 Session 或授予权限；canonical 业务请求仍按路径 Workspace 重新验证 current membership。
- 当前未开放 Deployment list API。根路径只校验用户输入的正式 `dpl_` ID 并导航到 canonical 路径，不从 fixture 猜测对象。
- canonical Deployment 页面只调用同源 `/api/v1/workspaces/{workspace_id}/deployments/{deployment_id}/nexus-view`，使用 `credentials: same-origin`、`cache: no-store`、显式公开 DTO 和运行时校验；未知形状不会被当成成功页面。
- `/prototype/nexus-view` fixture 是明确标注的静态代表数据，不作为 canonical 页面请求失败时的 fallback。
- 公共结构化 ref 只在展示 adapter 中转换为 `entity://type/id`；静态 fixtures 与组件断言也使用同一 canonical 引用格式和正式类型前缀。
- CI Run fixture 与后端安全投影同形，只包含状态、四个受控时间、当前 Component 与唯一 `ci-run.recorded`；不包含 source ID、external run key、delivery receipt、digest、Secret、原始 payload 或外部 URL。
- Deployment fixture 只包含终态、三个受控时间、Environment、来源 CI Run、`deploys` Relation 与唯一 `deployment.recorded`；不包含 authorization、调用 source、Jenkins 来源字段或执行日志，并明确区分“来源构建成功”和“部署失败”。
- 状态检视器只用于人工复核 Deployment 的 succeeded、failed、loading 与 error；Decision 的 empty / restricted 和 CI Run 的安全状态继续由组件测试覆盖。检视器不是未来产品导航。
- 当前三个页面由最小 pathname adapter 识别，不引入 router、状态库、组件库、图标包或远程字体。production build 由 Go server 从显式绝对 `RADISHNEXUS_WEB_ROOT` 同源交付；缓存、安全 Header 与页面 allowlist 见 [ADR-0015](../docs/adr/0015-same-origin-authenticated-web-shell.md)。

## 依赖与许可证

生产依赖只有 React 与 React DOM。构建、测试和格式工具使用 Vite、TypeScript、Oxlint、Vitest、Testing Library、jsdom 与 Prettier；直接依赖采用 MIT，TypeScript 采用 Apache-2.0。完整锁定依赖只允许来自官方 npm registry，必须携带 SHA-512 integrity，并限定在 `scripts/check-dependencies.mjs` 已审阅的 SPDX 许可证集合内；许可证或 lifecycle script 漂移会让检查失败。
