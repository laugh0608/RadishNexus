# RadishNexus 产品路线图

状态：长期阶段范围；按纵向场景交付，里程碑日期未承诺

日期：2026-09-05

## 推进原则

- 先验证真实研发协作闭环，再扩充功能宽度；
- 先用 Golden Path 验证独特价值，再横向补全聊天、工单和文档；
- 先完成 Web App，再启动 Flutter；
- 先用 Jenkins 验证插件边界，再发布通用 SDK；
- 每一阶段都必须能独立演示、测试和退出；
- 自部署、升级和备份与业务功能同步建设；
- 不用微服务数量、SDK 数量或插件数量衡量进展。

## 阶段与交付方式

M0～M7 表达能力范围和退出条件，不要求先补齐一个模块的全部功能才能开始下一个模块的最小切片。M0.5 可以使用 M1 的必要身份与导航能力，并提前验证 M3 / M4 / M5 中的最小交付、工单和 Markdown 场景；这不表示对应完整阶段已经完成。

阶段推进分别记录：内部契约已验证、公共入口已开放、普通成员可独立操作、真实团队持续使用。测试数量和静态演示不能替代后两类证据。近期顺序只维护在[当前状态](status/current.md)，完整场景和使用观察见 [Golden Path](golden-path.md)。

技术预研应声明要解决的问题、会影响的决策、结束条件和投入上限；不能影响当前切片的实验可以延后。最小 Markdown Document 与后续结构化编辑、CRDT、离线协同分阶段验收，不以协同实验作为基础文档交付的必经步骤。

## M0：产品和技术预研

目标：把容易导致长期返工的边界通过原型验证，而不是直接铺开页面。

主要产物：

- 产品对象和权限模型；
- Workspace、Project、Initiative、Component、Repository、Environment、Decision、Ticket、Document、CI Run 和 Deployment 的稳定 ID 草案；
- EntityLink、Activity 和 Attention Item 投影草案；
- HTTP、最小 Message SSE 与后续双向实时需求的契约草案；
- PostgreSQL Outbox 原型；
- 消息实时收发技术实验；
- Jenkins Webhook 幂等与统一 CI Run 技术实验；
- 文档编辑器和协同技术评估；
- Docker Compose 最小开发环境；
- 免费书面授权模板草案。

退出条件：

- 可以解释一次 Thread 如何产生 Decision、Ticket、CI Run 和 Deployment；
- 权限检查不会因跨对象引用泄漏私密内容；
- Jenkins 实验不需要插件直接访问核心数据库；
- 已选定首期 Go 服务端基础栈和 Web 工程基线。

## M0.5：Golden Path 纵向原型

目标：在补全任何单点模块前，用最薄可运行产品验证“上下文不断链”是否创造真实价值。

范围：

- 单 Workspace、Project、Component、Repository 和 staging Environment；
- 单频道、Message 和 Thread；
- Thread 创建 Decision，Decision 创建基础 Ticket；
- 简单 Markdown Document；
- Jenkins Webhook 映射 CI Run；
- 由获环境授权的成员显式记录并关联一条 staging Deployment；
- EntityLink、Activity 和最小 Nexus View；
- 权限过滤、审计、Docker Compose 与最小备份恢复。

原型明确不包含完整聊天、工单看板、CRDT、插件市场、多语言 SDK、生产发布和 Flutter。精确故事与验收标准见 [Golden Path](golden-path.md)。

退出条件：

- 15 分钟内可以演示从讨论到 staging Deployment 的完整链路；
- 全程不需要复制同一段背景到多个对象；
- Jenkins 故障和重复 Webhook 不破坏核心业务；
- 私密来源不会通过关系、时间线、通知或搜索泄漏；
- 备份恢复后实体、关系、来源和审计一致；
- 正常写入后的 Timeline 与来源 / 结果双向发现通过正式入口验收，不在演示中手工重建投影或预置交付结果；
- 根据原型结果决定保留或丢弃哪些实现，不默认将验证代码生产化。

## M1：Web 平台基础

目标：建立可登录、可管理、可自部署的平台基础。

范围：

- 本地账号和基础认证；
- Workspace、Project、成员和角色；
- Team、Initiative 和 Component 最小元数据；
- 文件、通知、审计基础；
- EntityLink、Activity 和 Attention Item 基础；
- HTTP API、错误模型与最小 Message SSE；双向 WebSocket 按 M2 / M5 的实际需求另行冻结；
- 数据库迁移和系统健康；
- Docker Compose；
- 备份恢复的最小闭环；
- 版本化结构化导出，并验证全新实例导入；
- React Web Shell 和管理入口。

退出条件：一个新实例可以完成安装、建团队、邀请成员、账号恢复、撤销成员权限、备份和恢复演练；普通成员可发现可访问对象，无需维护者提供稳定 ID 或直接修改数据库。版本化结构化导出与全新实例导入须独立往返验证，不以整库备份代替。

### 试用与生产边界

邀请外部团队前，准备[评估授权路径](licensing-strategy.md)、受支持的部署范围、备份操作、账号恢复及故障报告方式。优先使用范围受控的真实需求试用；数据范围、参与者和观察方式事先明确，私密工作区内容不进入仓库证据。

从开发拓扑走向生产前，至少还需冻结并验证：数据库 schema 兼容性与 readiness、运行 / migration / backup 身份的最小权限、支持版本与升级窗口、失败中断与 forward repair / 恢复、安全 Audit 的查询和保留、数据生命周期、日志与容量观测。验收覆盖“持续使用 → 备份 → 升级 → 失败诊断 → 恢复”，不能只重跑全新安装。

团队人数是目标用户假设，不是已验证容量。并发连接（含多标签页）、频道热点、消息 / 关系规模、数据库查询与恢复耗时必须用代表性数据测量后再形成支持说明；不因设置了资源上限就宣称达到目标容量。

## M2：Web 沟通闭环

目标：让真实团队能够用 Web App 进行日常沟通。

范围：

- 私聊和群聊；
- 公开与私密频道；
- 消息、Thread、表情、附件和 @提醒；
- 未读和提及；
- 需要用户行动的 Attention Item，与未读状态分离；
- 消息搜索；
- 置顶、收藏和基础管理；
- WebSocket 断线重连和消息幂等。

退出条件：小团队能够连续使用，不依赖数据库人工修复处理常见断线、重发和权限变化。

## M3：Jenkins 纵向插件

目标：用一个真实外部集成检验插件系统的收益和成本。

范围：

- Jenkins 实例配置和连接检查；
- Secrets；
- Webhook；
- Component、Repository、CI Run、构建状态和消息卡片；
- staging Environment 与 Deployment 关联；
- 项目级构建列表；
- 权限、审计和失败隔离；
- 插件启用、禁用和升级最小闭环。

首期可以将插件与主仓库一起发布，不要求立即提供完整市场、通用 WASM 运行时或三种语言 SDK。

退出条件：Jenkins 插件不修改聊天和工单核心表，也不会因外部系统不可用阻塞主业务。

## M4：工单与上下文关联

目标：完成从讨论到执行的主闭环。

范围：

- 需求、任务和缺陷类型；
- 状态、负责人、优先级、标签和截止时间；
- 列表和看板；
- 消息转工单；
- Message 或 Thread 转 Decision，Decision 生成 Ticket；
- 工单讨论和双向引用；
- Decision 拒绝、替代与复核交互，保留原结论、理由和来源；
- 工单关联 Jenkins CI Run；
- 基础筛选、搜索和历史。

退出条件：团队无需复制聊天内容，即可从需求讨论和确认结论追踪到执行、构建和部署结果。

## M5：在线协作文档

目标：完成从执行到知识的主闭环。

范围：

- 结构化编辑器；
- 多人在线协作；
- 评论、提及和版本历史；
- 文档关联消息、Decision、工单、Component 和 CI Run；
- 导入、导出和备份；
- 协同故障恢复和权限变化处理。

退出条件：设计文档可以在多人协作、版本恢复和跨对象引用场景中稳定使用。

## M6：插件 SDK 和更多 CI/CD 能力

目标：在真实接口已经稳定后开放生态。

范围候选：

- 开放源码 Plugin SDK；
- 示例插件和兼容测试；
- 插件签名和私有仓库；
- GitLab/GitHub/Gitea 插件；
- Jenkins 重新构建、取消、审批和部署；
- Deployment 和环境时间线；
- 自动化 Trigger/Action；
- Initiative 周期状态更新、健康度和过期提醒；
- 发布、故障和迁移的 Playbook/Run 最小模型。

退出条件：外部开发者能够仅依赖公开契约构建、测试和安装插件。

## M7：离线文档与 Flutter 客户端

目标：在 Web 协同和服务端协议稳定后扩展离线和安装客户端。

推进顺序：

1. 浏览器离线缓存和文档同步；
2. Flutter 移动端基础壳层；
3. 消息、通知和工单快速操作；
4. 文档阅读和受控编辑；
5. Flutter PC 端适配；
6. 根据真实使用决定完整离线文档能力。

Flutter 启动前至少应满足：

- Web 沟通、工单和文档的主协议稳定；
- API 可以生成或稳定维护 Dart 客户端；
- 权限、同步和错误语义不再频繁破坏性变化；
- 有明确移动端或桌面端真实使用需求；
- 团队有能力同时维护 Web 和 Flutter 的发布质量。

## 后续候选

- OIDC、LDAP、SAML/SCIM；
- 高可用和 Kubernetes 部署；
- 事故处置 Playbook；
- 发布说明和复盘辅助；
- 可选本地 AI 插件，只生成带证据引用且需要人工确认的草案；
- 可移植 `.nexus` 上下文包；
- 音视频会议集成；
- 外部访客和跨组织协作；
- 插件市场。

以上候选不进入当前承诺，必须由真实用户需求和维护能力决定。
