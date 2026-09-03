# RadishNexus 总体架构

状态：方向基线，M0.5 / M1 首批纵向边界已冻结

日期：2026-09-03

## 架构目标

RadishNexus 首期架构必须同时服务于四个目标：

1. 小团队能够持续开发，不被基础设施和多语言边界拖慢；
2. 消息、工单、文档和 CI/CD 使用统一身份、权限、引用和事件；
3. 自部署实例能够安装、升级、备份、恢复和诊断；
4. 插件可以扩展外部能力，但不能破坏平台稳定性和数据边界。

## 初始形态

采用模块化单体，而不是从微服务起步。

```text
React + TypeScript Web App
            │
      HTTP API + SSE / future WebSocket
            │
┌────────── Go Server ──────────┐
│ Identity / Workspace / RBAC   │
│ Chat / Decision / Ticket      │
│ Document / Component / Deploy │
│ EntityLink / Activity / Event │
│ Plugin Manager / Job Runner   │
└──────────────┬────────────────┘
               │
        Plugin Boundary
       ┌───────┴────────┐
 Declarative/WASM   Sidecar Process
                    Jenkins / GitLab
```

模块化单体允许共享事务和快速重构；统一事件 Outbox 和稳定模块接口为未来拆分提供边界，但在实际容量或组织需求出现前不拆服务。

## 技术职责

### Go 主服务

Go 承担首期主要业务能力：

- HTTP API、M0.5 单进程 SSE 和后续版本化 WebSocket；
- 身份、Workspace、Project 和 RBAC；
- 私聊、频道、消息和 Thread；
- Decision、工单及其状态流；
- Initiative、Component、Repository、Environment 和 Deployment；
- 通知、审计和后台任务；
- EntityLink、Activity 投影和事件 Outbox；
- 插件安装、配置、权限和调用编排；
- 自部署健康检查、迁移和运维接口。

### Rust 组件

Rust 不作为第二套平行业务后端。只有出现明确收益时才引入，例如：

- 文档 CRDT 或离线同步内核，并且确实需要跨 Web/Flutter 复用；
- WASM 插件运行时或受限执行组件；
- 本地索引、附件处理、加密或高性能协议；
- Flutter FFI 共享核心；
- 经测量确认的性能瓶颈。

每个 Rust 组件都必须有清楚的进程、FFI、WASM 或协议边界，不能直接共享 Go 进程内状态。

### React Web

Web App 是前期唯一正式用户界面，负责：

- 团队空间和项目导航；
- 私聊、频道和 Thread；
- Decision 和 Attention Item；
- 工单列表、详情和看板；
- 文档编辑和协作状态；
- Component、Environment、CI/CD 卡片和 Nexus View；
- 管理、授权、插件和自部署诊断页面。

Web App 应从一开始适配常见 PC 分辨率和移动浏览器，但移动 Web 适配不等于启动 Flutter 客户端开发。

### Flutter 客户端

Flutter 在 Web 产品闭环稳定后统一承载移动端和 PC 安装包。首期客户端应优先覆盖消息、通知、工单快速操作和文档阅读，再按真实需求增加完整编辑能力。

Flutter 与 Web 共享：

- HTTP/WebSocket 协议；
- OpenAPI 或其他可生成的客户端契约；
- 权限和错误语义；
- 设计 token 和品牌规范；
- 离线同步协议。

Flutter 与 React 不强求共享 UI 代码。

## 平台内核与业务模块

### 平台内核

- Identity 与认证；
- Workspace、Team、Project 和 Membership；
- RBAC 与对象权限检查；
- 稳定实体 ID 和引用；
- EntityLink 与 Activity 投影；
- 文件与对象存储抽象；
- 通知、搜索和审计框架；
- Event Outbox；
- Plugin Manager、Secrets 和配置；
- 数据迁移、健康检查和系统诊断。

M1 Identity 首段使用一次性显式 bootstrap 建立本地账号、首个 Workspace owner 和服务端 opaque Session；OIDC 延后并复用同一 user、membership 与 Session 边界。Session 不携带固定 Workspace 授权，路由选择稳定 Workspace ID 后必须重新验证 active membership。浏览器 Cookie、CSRF、request ID、版本化错误对象和恢复时 Session 失效语义见 [ADR-0012](../adr/0012-local-identity-and-session-foundation.md)；精确 public origin、可信代理、客户端 IP、登录限流和三个公共认证路由见 [ADR-0013](../adr/0013-public-authentication-transport.md)；首个 Workspace 路径业务读取、公共 DTO 和 no-store Web 消费见 [ADR-0014](../adr/0014-session-scoped-deployment-nexus-view-transport.md)；同源 authenticated Web Shell、显式 production build root、页面 allowlist 与静态缓存边界见 [ADR-0015](../adr/0015-same-origin-authenticated-web-shell.md)。

### 内建业务模块

- Chat；
- Decision；
- Ticket；
- Document；
- Initiative 与 Component Context；
- Environment 与 Deployment；
- 基础自动化；
- 管理控制台。

这些模块可以遵循统一内部接口，但不要求通过第三方插件运行时加载。

### 外部插件

- Jenkins、GitLab、GitHub、Gitea 等开发工具连接器；
- 日历、视频会议和通知渠道；
- AI、知识检索和自动总结；
- 行业或组织专属扩展；
- 可独立安装和升级的高级能力。

## 统一实体引用

跨模块关联不直接泄漏数据库表结构。每个对象拥有稳定引用，例如：

```text
entity://thread/thr_123
entity://decision/dec_234
entity://ticket/tkt_456
entity://component/cmp_321
entity://environment/env_002
```

引用必须经过原对象权限检查。能够看到工单不表示自动获得关联私密频道或文档的读取权。

类型注册、结构化表示、Workspace 解析和受限占位的 M0 基线见[核心实体、授权与事件契约](core-contracts.md)。Thread、Decision、Ticket 的首段 Project 作用域与物理 schema 已由 ADR-0004、ADR-0005 和正式 migration 落地；Component、CI Run 的来源与读取边界已由 ADR-0006、ADR-0007 和 migration 003 落地；Environment、显式 staging Deployment、环境级写授权与安全读取已由 ADR-0009、ADR-0011 和 migration 004 落地；Channel、Message 与 messaging-origin Thread 的最小身份、来源、权限和幂等边界已由 ADR-0017 与 migration 006 落地；Thread → Decision → Ticket 的 Session transport、人工确认和命令 receipt 已由 ADR-0019 与 migration 007 落地。具体 ID 生成算法及 Document、Repository 等其余对象 schema 仍未冻结。

## EntityLink 与 Nexus View

跨模块关系使用一等 `EntityLink` 记录，而不是让每个模块分别维护不可追溯的关联字段。EntityLink 至少保存关系类型、两端实体、来源、创建者、建立时间和状态。

关系的事实强度分为：

- asserted：用户或受权系统明确确认的事实；
- derived：插件、导入器或规则推导的关系。

关系来源另行记录为 user、system、plugin 或 import，并指向具体来源主体。事实强度与来源是两个维度：用户可以明确确认 `derived-from` 关系，插件也只能在获权范围内写入推导关系。两者必须在 API 和 UI 中可区分，自动推导的关系不能冒充人工确认的 Decision 或 Deployment 事实。

每个主要对象通过 Nexus View 统一提供：

- Current：当前状态、负责人和关键结论；
- Relations：经过权限过滤的相关实体；
- Timeline：由领域事件投影的 Activity；
- Actions：当前用户有权执行的操作。

首期使用 PostgreSQL 表和索引实现关系查询。除非真实数据规模和查询模式证明有必要，不引入图数据库。

## 事件模型

首期使用 PostgreSQL Transactional Outbox 保证业务写入和事件记录的一致性。事件至少包含：

- event ID 和 type；
- workspace 和可选 project context；
- actor 和 source；
- primary entity；
- correlation ID 和 causation ID；
- occurred time；
- schema version；
- 最小且经过权限评估的 payload。

Activity 不是 Outbox 的副本。Outbox 用于可靠投递，Activity 是可重建、可权限过滤的产品时间线投影；审计日志则保存安全与合规所需的操作证据。三者可以来自同一领域事件，但保留不同职责和生命周期。

M0 契约把不可变领域事件事实与可变投递状态作逻辑分离，避免已投递 Outbox 清理后无法重建 Activity 或验证备份；具体一表或分表由 PostgreSQL 原型决定。详见[核心实体、授权与事件契约](core-contracts.md)和 [ADR-0002](../adr/0002-stable-entity-reference-and-event-projection.md)。

早期不强制引入独立消息中间件。只有插件吞吐、跨进程可靠消费或服务拆分形成真实需求后，再评估 NATS JetStream 等方案。

## 数据和基础设施

首期建议：

- PostgreSQL：核心业务、插件命名空间、Outbox 和初始全文搜索；
- Redis：在线状态、短期缓存、限流和可丢失的实时协调状态；
- S3 兼容对象存储：附件、图片、文档资源和可选构建制品；
- SSE：M0.5 单进程、Session 作用域的 Message 单向增量；
- WebSocket：M2 出现双向 presence、typing 或协作控制需求后的版本化目标；
- Docker Compose：首个正式自部署方式；
- OpenTelemetry 兼容日志、指标和追踪边界。

独立搜索集群、Kubernetes 和高可用拓扑都在真实规模需求出现后引入。

canonical Channel Web 对 SSE 使用“先建立连接并收到 `ready`、再读取 canonical history、随后合并缓冲增量”的状态机。原生 EventSource 负责在同一进程 generation 内携带 `Last-Event-ID` 重连；`resync-required` 必须建立新边界并全量重读，`access-revoked` 必须清空已渲染正文和草稿。写 command 继续使用具备 CSRF 与幂等语义的短请求，不通过 SSE 发送，也不建立隐藏 polling fallback。Document 协同在自身协议、权限和恢复合同冻结前不得复用或扩展这条 Message transport。

## API 原则

- 对 Web、Flutter 和外部开发者提供稳定的 HTTP API；
- M0.5 Message 单向实时增量使用可回到 canonical history 的版本化 SSE 事件；M2 双向实时能力另行冻结 WebSocket 协议；
- 公共 API 生成机器可读契约；
- 插件不能直接访问核心数据库表；
- 外部写操作必须支持幂等键、权限检查和审计；
- 列表和搜索 API 从第一版考虑游标分页和权限过滤。

## 自部署边界

首个正式部署必须同时包含：

- Docker Compose 和显式版本镜像；
- 配置校验；
- 数据库迁移；
- 健康检查；
- 备份和恢复说明；
- 插件和密钥数据的备份边界；
- 升级前检查和失败停止线；
- 不依赖 RadishNexus 官方云才能完成的核心运行路径。

M0.5 已建立第一条可验证恢复路径：显式命令生成版本化 manifest 与 PostgreSQL custom archive，只在本地或受控私有连接的全新空 PostgreSQL 17 目标上以单事务恢复，随后执行正式 forward-only migration 校验并从不可变领域事件重建 Activity。当前工件是同 major 整库运维备份，不是 `.nexus` 开放导出，也不包含自动覆盖、TLS 工具桥接、跨大版本承诺、远程存储、加密或 Secret 备份。精确边界见 [ADR-0010](../adr/0010-verified-postgresql-backup-and-restore.md)。

M0.5 / M1 已建立首个正式 Docker Compose 开发拓扑：固定 digest 的 Caddy 是唯一宿主 HTTPS 入口，Go server 同源交付 production Web build 与 API，PostgreSQL 只位于内部数据网络；migration、一次性 bootstrap、backup 和 restore 继续使用现有显式 CLI，数据库密码通过按 service 挂载的文件 Secret 输入。该拓扑已经从全新命名 volume 验证 PostgreSQL readiness、migration、一次 bootstrap、重复 bootstrap 拒绝、HTTPS login / Session / logout、转发 Header 清洗和非公开应用/数据库端口；它仍不是公网证书、高可用或跨 major 升级方案。精确边界见 [ADR-0016](../adr/0016-minimal-docker-compose-self-hosting.md) 和 [`deploy/README.md`](../../deploy/README.md)。

### 可移植上下文包

除整库备份外，规划可移植 `.nexus` 导出包，用于项目迁移、归档和离线检查。建议包含：

- 版本化 manifest；
- JSONL 或等价开放格式的结构化对象和 EntityLink；
- Markdown 等可读文档表示；
- 附件、校验和和导入报告；
- 被省略或脱敏内容的明确清单。

导出包默认不包含 Secrets、Token 和不可迁移的授权材料。正式交付前必须通过“导出 → 全新实例导入 → 关系与权限复核”的往返测试。

## 安全原则

- 默认拒绝未声明的插件权限；
- Secrets 不通过普通配置 API 返回明文；
- 插件网络访问受声明和管理员批准控制；
- 所有高风险 CI/CD 操作进入审计；
- 私密对象的引用、搜索摘要和通知不得泄漏正文；
- Attention Item、Activity、导出包和 AI 插件必须复用同一对象权限语义；
- 第一阶段优先 TLS、静态加密、RBAC、审计和备份安全，不默认承诺全局端到端加密。

## 当前仓库布局

当前已经建立根级 `web/` React App、唯一正式 `server/` Go module、受控 `deploy/` Compose 工件、可丢弃的 `experiments/` 和共享 `scripts/`；SDK、插件与公共契约目录只在对应产物真正进入实现时创建：

```text
RadishNexus/
├── web/
├── server/
│   ├── cmd/
│   └── internal/
├── deploy/
├── experiments/
├── scripts/
├── docs/
└── LICENSE
```

未来 SDK 和插件目录必须拥有独立许可证，不能因位于同一仓库而模糊授权边界；`deploy/` 只承载已经冻结且可复验的部署工件，不提前放置高可用、Kubernetes 或公网生产占位结构。
