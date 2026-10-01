# RadishNexus Web

`web/` 是 RadishNexus 第一正式产品形态的 React + TypeScript 入口。当前根路径已经建立最小 authenticated Web Shell，消费正式 login / session / logout transport，允许从当前 Session context 选择 Workspace、分页浏览可读 Project 并打开可读 Channel；Deployment、CI Run、Thread、Decision 与 Ticket 的已知 ID 工具保留在次级入口。Deployment / CI Run 页面安全读取 Nexus View，成功 CI Run 可显式记录外部 staging 部署结果；Ticket 可创建 Markdown Document，Project 提供文档列表，Document 页面提供编辑与历史恢复；Channel 页面通过类型化 adapter 分页读取 Message、幂等发送并从 Message 发起 Thread；Thread → Proposed Decision → 人工 Accepted Decision → Ticket 继续使用权限过滤后的 canonical Nexus View 和幂等短请求。原 Decision、CI Run 与 Deployment 代表原型移动到显式 `/prototype/nexus-view`，不参与真实失败 fallback。

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

该入口会先构建 Web，再启动一次性 PostgreSQL 容器、fixture upstream 与固定 Caddy HTTPS reverse proxy，并输出 origin、当前 Caddy CA、contributor / decider 两个测试账号、canonical Deployment / Channel / Thread path 与 stop 文件。它不会拉取缺失镜像，也不是产品默认账号或常驻开发服务；浏览器必须在连接前精确核对并临时信任本次 CA，不应绕过证书告警。复核结束后创建输出的 stop 文件，fixture 会退出并清理容器、volume 与临时状态；临时导入钥匙串的 CA 仍须由操作者按本次完整指纹删除。

## 当前边界

- Web 只消费已经按当前主体过滤的 `NexusViewData`，不接收角色或权限集合，也不在浏览器中重新判断对象可读性。
- `restricted` 条目在类型上不携带 EntityRef、对象类型、关系类型、标题、来源或时间；`hidden` 条目不进入客户端数据。
- 根路径先通过 `GET /api/v1/auth/session` bootstrap；只有稳定 `unauthenticated` 错误进入登录表单，网络、服务和响应契约错误必须显式失败并允许重试。
- 登录使用私有邮箱与密码，展示名独立；凭据只保留在受控表单和当前同源请求体，不写入 URL、浏览器 storage 或自建 Cookie。Session token 只由服务端 `HttpOnly` Cookie 承载；登出从可读 CSRF Cookie 构造正式请求。
- `/account` 展示服务端返回的登录方式，提供 owner 创建邀请与当前用户接受邀请；未登录用户可在首页兑换邀请并创建本地账户。邀请码不写入 URL 或 Web Storage，不自动授予 Project 角色或受限 Channel 成员资格。Radish 登录按 capability 隐藏，真实 provider 当前未装配；精确身份合同见 [ADR-0023](../docs/adr/0023-local-account-and-radish-oidc-login.md)。
- Workspace 选择来自 Session context，但不写入 Session 或授予权限；canonical 业务请求仍按路径 Workspace 重新验证 current membership。
- 首页已消费 Project / Channel 只读 list API，行为见下文；Deployment、Thread、Decision 与 Ticket 独立列表仍未开放。次级 ID 工具校验正式 `dpl_` / `chn_` / `thr_` / `dec_` / `tkt_` ID 并导航到 canonical 路径，不从 fixture 猜测对象。
- canonical Deployment 页面只调用同源 `/api/v1/workspaces/{workspace_id}/deployments/{deployment_id}/nexus-view`，使用 `credentials: same-origin`、`cache: no-store`、显式公开 DTO 和运行时校验；未知形状不会被当成成功页面。
- canonical Channel 页面用原生 EventSource 调用 ADR-0020 的同源 SSE，并继续用 ADR-0018 的三个短请求读写：先等待 `ready`，再读取 canonical history；history 期间的 `message.created` 先缓冲，完成后按 Message ID 去重合并。发送失败时为未修改正文保留同一 `client_operation_id`，`200` 精确重试不会追加重复 Message。
- 浏览器自动重连沿用原生 `Last-Event-ID`；`resync-required` 关闭旧流、建立新边界并全量重读，断线错误链只允许一次 Session + canonical history 诊断，不做持续轮询。`access-revoked` 或诊断所得 `404` 会清空正文、草稿和 Thread 结果，Session `401` 回到登录态；事件顺序、cursor、空控制数据或 DTO 漂移均 fail closed。
- Channel 的 `401` 回到登录态；任何后续 `404` 都立即移除已经渲染的 Message 正文和本地草稿。Thread 创建只发送 Source Message ref、标题和可见性，不复制 Message 正文。
- canonical Thread、Decision 与 Ticket 页面只调用 ADR-0019 的六个同源短请求。Thread 创建 Proposed Decision；Decision 必须由有权主体勾选明确确认后人工 acceptance，接受后才能创建 Ticket；写入发生网络歧义且表单未变化时保留同一 `client_operation_id`。
- 协作页区分来源与后续结果，支持 Thread → Decision → Ticket 的双向 canonical 跳转；readable relation 必须携带 `direction`，Go 与 Web 成套升级和回退，见 [ADR-0022](../docs/adr/0022-transactional-activity-and-incoming-relations.md)。
- 协作 adapter 对 Current、Relations、Timeline、结构化来源、状态与受控时间执行严格运行时校验；restricted evidence 不携带类型、ID、关系名、标题或时间。协作请求的 `401` 回到登录态，后续 `404` 会清除已渲染内容、草稿和成功结果。
- `/prototype/nexus-view` fixture 是明确标注的静态代表数据，不作为 canonical 页面请求失败时的 fallback。
- 公共结构化 ref 只在展示 adapter 中转换为 `entity://type/id`；静态 fixtures 与组件断言也使用同一 canonical 引用格式和正式类型前缀。
- CI Run fixture 与后端安全投影同形，只包含状态、四个受控时间、当前 Component 与唯一 `ci-run.recorded`；不包含 source ID、external run key、delivery receipt、digest、Secret、原始 payload 或外部 URL。
- Deployment fixture 只包含终态、三个受控时间、Environment、来源 CI Run、`deploys` Relation 与唯一 `deployment.recorded`；不包含 authorization、调用 source、Jenkins 来源字段或执行日志，并明确区分“来源构建成功”和“部署失败”。
- 状态检视器只用于人工复核 Deployment 的 succeeded、failed、loading 与 error；Decision 的 empty / restricted 和 CI Run 的安全状态继续由组件测试覆盖。检视器不是未来产品导航。
- 当前 authenticated shell、账户页、Deployment、CI Run、Document / Project 文档列表、Channel、Thread、Decision、Ticket 与代表检视器由最小 pathname adapter 识别，不引入 router、状态库、组件库、图标包或远程字体。production build 由 Go server 从显式绝对 `RADISHNEXUS_WEB_ROOT` 同源交付；缓存、安全 Header 与页面 allowlist 基线见 [ADR-0015](../docs/adr/0015-same-origin-authenticated-web-shell.md)，账户页扩展见 ADR-0023。

## 依赖与许可证

生产依赖只有 React 与 React DOM。构建、测试和格式工具使用 Vite、TypeScript、Oxlint、Vitest、Testing Library、jsdom 与 Prettier；直接依赖采用 MIT，TypeScript 采用 Apache-2.0。完整锁定依赖只允许来自官方 npm registry，必须携带 SHA-512 integrity，并限定在 `scripts/check-dependencies.mjs` 已审阅的 SPDX 许可证集合内；许可证或 lifecycle script 漂移会让检查失败。

## Project / Channel 浏览

首页消费 [ADR-0025](../docs/adr/0025-project-and-channel-discovery.md) 的两个只读 GET 合同。列表每次显示一个查询页，翻页与返回均重新读取，不拼接权限可能过期的旧页。切换 Workspace / Project、主动刷新和窗口重新获得焦点会清理旧内容；请求取消与迟到结果保护避免旧作用域覆盖新页面。归档对象标明可浏览，访问失效清空内容，Session 失效回到登录。没有权限变化实时推送，最终打开频道仍由服务端复权。

`workspace/WorkspaceHome.tsx` 承载首页和次级 ID 工具，`ProjectBrowser.tsx` 承载列表状态，`api.ts` 集中校验安全响应。没有新增浏览器存储、前端权限推断或依赖。基础配置入口按 [ADR-0026](../docs/adr/0026-foundation-configuration-and-membership.md) 开放，所有命令仍由服务端独立重新授权。

## 基础对象与成员配置

`workspace/ConfigurationPanel.tsx` 提供 owner 的 Team / Project 创建、显式初始 admin 确认、Project 普通成员角色配置、Channel 创建与受限成员管理；`configuration-api.ts` 校验专用响应。成员选择使用稳定 ID 与展示名区分重名，不返回邮箱。界面说明撤权会清除从属私密授权；管理权交接尚未开放。

写请求复用现有同源 Session / CSRF；连续点击被阻止，网络或 5xx 模糊失败保留原 operation ID，成功后重新读取当前列表。配置面板和分页在刷新、焦点复核与作用域切换时清理旧数据，取消或忽略迟到结果。普通配置能力由服务端返回，客户端显示不构成授权。首次访问通过 `auth/SetupGate.tsx` 与 `setup-api.ts` 实现 [ADR-0027](../docs/adr/0027-first-visit-administrator-setup.md)：Session 确认未登录后读取初始化状态，required 展示首位管理员表单，complete 进入正式登录，unavailable 提示部署者配置。状态读取失败可重试；提交后清理码与密码，模糊结果必须重新检查状态，不自动重放创建。

## 共享工作台

已登录页面由 `WorkbenchShell.tsx` 与 `workbench.css` 提供共享布局，采用 Radish 家族暖纸底、灰玉品牌色和墨蓝主操作。桌面侧栏宽 224px；900px 及以下改为顶部导航与原生模态 `dialog` 抽屉，支持 Escape、焦点约束与关闭后焦点返回。导航只使用 Session 中的 Workspace 和现有入口，不推断 Project 名称或权限。

Document 默认集中阅读，来源 Ticket 保留标题下入口；信息展开在宽屏显示 288px 侧栏，1180px 及以下进入单栏信息视图，关闭后返回触发按钮和阅读位置。编辑、冲突、历史恢复与错误状态沿用原有 API 与显式操作。样式局限于工作台与 Document，不改变登录及静态原型主题。范围和验证见[视觉落地记录](../docs/status/reviews/2026-09-26-workbench-ui.md)。

## 最小 Markdown Document

`document/api.ts` 消费 [ADR-0028](../docs/adr/0028-minimal-markdown-document.md) 的八个操作并严格校验响应。Ticket 提供创建入口，明确提示新文档对 Project 可读成员可见；首页 Project 与文档页面提供 Project 文档列表。`DocumentPage.tsx` 使用普通 textarea 编辑服务端权威 Markdown，预览复用服务端 parser；`MarkdownView.tsx` 只将封闭节点映射为固定 React 元素，不注入 HTML、不使用第二套 parser，也不渲染图片或嵌入。

草稿只在当前页面内存；显式保存携带基线 revision，冲突保留草稿并展示最新正文，用户确认重新应用后才可生成新操作。网络 / 5xx 不确定结果冻结原输入并保留 operation ID 以精确重试；不会自动以最新基线重放。历史恢复同时展示旧版与当前版，确认后追加版本。脏草稿离开、刷新及退出均有提示，没有 Enter 保存快捷键。读取失败显式提示，权限失效立即清除正文、草稿、预览、历史和待重试输入；焦点恢复和主动刷新会重新验证，未接入权限推送或轮询。

安全展示失败时仅提供显式源码查看；源码以 React 文本输出。链接只接受绝对 HTTP(S)，拒绝用户凭据、控制符与反斜杠，外链使用 `noopener noreferrer` 和 `no-referrer`。新功能没有新增 Web 依赖或浏览器持久化。自动化、隔离 Chrome、手机布局和原生 IME 剩余复核范围见[本轮记录](../docs/status/reviews/2026-09-26-markdown-document.md)。

## CI Run 正式读取

`nexus-view/ci-run-api.ts` 校验 [ADR-0029](../docs/adr/0029-session-scoped-ci-run-nexus-view.md) 的字段白名单、终态、受控时间和一致的 Component / Timeline，并消费同源无缓存 GET。`CIRunPage.tsx` 复用已登录工作台与 Nexus View 组件，提供主动重读、失败重试、焦点复权、401 退出与 404 清空；取消或迟到响应不能覆盖更新后的视图。空开始时间显示“未提供”。

正式路径为 `/workspaces/{workspace_id}/ci-runs/{ci_run_id}`。首页次级 ID 工具支持 `cir_`，Deployment 页可跳转来源 CI Run；Component 本身尚无正式页面，不制造无效跳转。页面说明构建成功不代表已经部署，不公开 Jenkins 外部地址；成功构建的显式记录入口见下节，失败 / 取消构建不提供该动作。静态代表数据仍只服务原型和测试。

## staging 部署结果记录

成功 CI Run 的正式页面提供“记录 staging 部署结果”。表单读取当前用户已获授权的 staging 环境，要求选择实际结果、填写外部完成时间并确认摘要；不预选成功，也不复用构建时间作为部署时间。记录成功后进入既有 Deployment 页面。

网络或响应异常时保留原操作 ID / payload，用户可显式“重试原请求”。刷新或放弃本地重试不会撤销可能已经写入的事实。UI 不授予权限，提交时服务端重新校验；此功能不执行部署。合同与精确范围见 [ADR-0031](../docs/adr/0031-session-scoped-staging-deployment-recording.md)。

隔离浏览器 fixture 可在仓库根设置 `RADISHNEXUS_BROWSER_STAGING=1` 后运行 `scripts/run-authenticated-web-browser-fixture.sh`：通过正式 service 准备成功 CI Run，跳过预置 Deployment，返回 `ci_run_path` 供页面真实记录验收。需要事先授权启动测试服务；fixture 的 Go 后端与 HTTPS 入口仅绑定 loopback，结束后按 stop 文件清理。


## 组件与环境

首页“组件与环境”由 `workspace/DeliveryConfigurationPanel.tsx` 承载，使用当前 Workspace 发现既有 Component / Environment，并展示配置详情。owner 使用现有 Team 选择器创建 active Component 或 staging Environment；普通成员不读取 owner 专用目录。对象创建不自动授权任何人。

`EnvironmentAuthorizationEditor.tsx` 使用同 Workspace 成员选择器和最新授权 ID / 状态进行显式授予、撤销与重新授予；给本人授权也必须确认。归档 staging 仅允许撤销，非 staging 只展示已有授权。失权或切换作用域清空管理视图，迟到请求被丢弃。网络结果不明时冻结原 payload / operation ID，用户可精确重试或放弃本地待确认请求后刷新；成功不乐观推断当前权限。合同见 [ADR-0032](../docs/adr/0032-component-environment-configuration-and-authorization.md)。

分页、反馈与基础创建请求复用 `configuration-hooks.ts` / `configuration-ui.tsx`，API 运行时校验集中在 `delivery-configuration-api.ts`。配置样式复用工作台，在手机保持可读并提供键盘确认。未增加依赖、浏览器持久化、完整 Component / Environment Nexus View 或构建来源配置入口；staging 记录仍从成功 CI Run 页面进入。
