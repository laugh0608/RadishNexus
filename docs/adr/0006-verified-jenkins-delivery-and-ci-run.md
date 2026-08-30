# ADR-0006：已验证 Jenkins delivery 与 CI Run 原子记录

状态：已接受

日期：2026-08-29

## 背景

Golden Path 要求 Jenkins Webhook 形成统一 CI Run，并证明重复 delivery 不会产生重复业务对象、领域事件或 Activity。此前可丢弃实验已经验证了 receipt、CI Run、事件和 Outbox 的单事务方向，但正式 `server/` 尚未建立 Component 表、CI Run 稳定标识、来源映射和可保留的幂等凭据。

Webhook 来源认证、签名格式和 HTTP 协议仍未冻结。如果把原始请求直接送入领域服务，核心事务会同时承担网络协议、Secret 读取、来源校验和业务映射，既扩大可信边界，也会诱使普通业务表保存 Secret 或原始 payload。另一方面，仅按 delivery ID 静默忽略冲突会掩盖同一幂等身份承载不同内容的重放或配置错误。

CI Run 还必须与 Deployment 保持不同事实。Jenkins 构建成功只说明一次流水线运行完成，不能据此自动声明制品已进入某个 Environment。

## 决定

### Component 与 CI Run 身份

- 正式 PostgreSQL schema 建立 `components`，落实已冻结的 `component` / `cmp_`、Workspace 内唯一 key、类型、责任 Team 和 lifecycle 字段；`active / deprecated / retired` Component 必须有责任 Team。
- 冻结 `ci-run` EntityType 与 `cir_` ID 前缀。CI Run 是 Workspace 内稳定、不可复用的业务对象，归属一个 Component，但不是 Component 的 Repository 映射或 Deployment 记录。
- CI Run 首批字段为 `component_id`、`source_kind`、`source_id`、`external_run_key`、`status`、`started_at`、`completed_at`、`created_at` 和 `updated_at`。
- 状态集合为 `queued / running / succeeded / failed / canceled`；终态必须有 `completed_at`，开始时间不能晚于完成时间。本切片只接收 `succeeded / failed / canceled` 的完成事实，不提前定义运行中状态更新协议。
- `source_kind` 当前只允许 `jenkins`。同一 Workspace、source 和 `external_run_key` 只能对应一个 CI Run；来源映射创建后不可修改。

### 已验证 delivery 边界

- application service 只接收 `VerifiedJenkinsDelivery`：Workspace、受控 source ID、delivery ID 和规范化 payload SHA-256，以及已经映射出的 Component 与 CI Run 最小事实。
- `Verified` 表示调用方已经完成来源认证、Secret 使用、重放头校验、payload 解析和字段映射。当前核心不读取 Header、Cookie、Token、签名、Secret 或原始 webhook body，也不对外暴露 Jenkins HTTP route。
- SHA-256 只用于确认同一 delivery 的内容一致性，不能代替 HMAC、网络来源认证或授权。
- source ID 是受控 adapter 的不透明稳定标识。本 ADR 不建立通用 Integration、Plugin installation、Repository 或 Secret 数据模型。

### 幂等、冲突与事务

- `inbound_deliveries` 以 `workspace_id + source_kind + source_id + delivery_id` 为幂等键，只保存 payload digest、最终 CI Run、最终领域事件和记录时间，不保存 Secret 或原始 body。
- 首次 delivery 在同一 PostgreSQL 事务内写入不可变 receipt、CI Run、`ci-run.recorded` 领域事件和 Activity projector Outbox。receipt 对 CI Run 和事件使用可延迟外键，因此可以先抢占 delivery 唯一键；任一步失败时全部回滚，不留下半处理状态。
- 完全相同的重复 delivery 返回既有 CI Run，且不创建第二个 CI Run、事件、Outbox 或 Activity。
- 同一幂等键携带不同 payload digest 时 fail closed，返回 conflict，不覆盖既有 receipt。不同 delivery 若映射到同一 source external run，也返回 conflict，失败事务不保留新 receipt。
- receipt 不允许普通更新或删除。其最低保留期必须覆盖产品承诺的幂等窗口；具体归档和长期保留策略留给后续运维设计。

### 事件与投影

- 首次成功记录产生 schema version 1 的 `ci-run.recorded`。actor 和 source 都是受控 plugin source，primary entity 是 CI Run，根操作使用独立 correlation ID，不伪造 Project 上下文。
- 事件 payload 只保留 CI Run status 和 Component EntityRef。外部 run key 留在权威 CI Run 记录中；digest、原始 payload、Secret、标题快照和外部响应不复制进事件。
- Activity projection version 1 将 `ci-run.recorded` 加入显式白名单，只投影 status 与 Component EntityRef。重复重建由既有唯一键保持幂等。
- 本切片不建立 CI Run 的用户读取授权或 HTTP/API 表示。Activity 已具备可重建事实，但在读取能力冻结前不新增临时公共入口。

### 明确停止线

- 不实现 Jenkins HTTP route、公开签名协议、Secret 存储或网络重试 worker；
- 不建立通用插件 runtime、插件市场或多 CI provider 抽象；
- 不冻结 Repository 模型，不把 Jenkins external run key 伪装成 Repository 或 commit；
- 不创建 Deployment、Environment 关系或“CI 成功即部署”的自动转换；
- 不为本切片制造临时公共 API 或放宽既有权限边界。

## 未采用的方案

### 直接把 webhook body 交给领域服务

这会把签名算法、Secret、网络协议和不受信输入扩散到事务核心，并提高原始 payload 进入日志、事件或普通业务表的风险。来源 adapter 应先验证和映射，再进入窄 application boundary。

### 只按 delivery ID 忽略所有重复请求

相同 delivery ID 携带不同内容可能来自重放、错误代理或 source 配置漂移。静默成功会隐藏冲突，因此必须比较 digest 并 fail closed。

### 使用 external run key 作为 CI Run ID

外部 key 的格式、生命周期和 provider 作用域由 Jenkins 决定，不能承担 RadishNexus 的稳定 EntityID。它只在明确的 Workspace 与 source 作用域内建立唯一映射。

### 首轮同时实现 queued/running 更新与 Deployment

状态更新需要定义乱序、回退、终态重开和多 webhook 语义；Deployment 还需要独立权限、Environment、确认和审计。当前完成事实已经足以验证幂等与事件边界，提前扩展会混淆两种生命周期。

## 后果

正面影响：

- 外部来源验证与核心事务边界清楚，普通业务记录不接触 Secret 和原始 body；
- 并发重复、内容冲突和中途失败都有数据库约束与集成测试支撑；
- CI Run 成为可引用、可投影且独立于 Deployment 的正式事实；
- Component 从文档字段进入正式 schema，为后续 CI Run 读取与关联提供稳定目标。

成本与风险：

- source 配置、签名协议、失败审计和重试仍需后续 adapter 切片完成；本轮 conflict 只能由调用方安全记录机器码，不能保存未验证 payload；
- 当前只记录终态，尚不能表达实时流水线进度或状态修正；
- CI Run 的用户读取权限、Nexus View 形状和 external URL 展示尚未冻结；
- receipt 必须纳入备份和未来保留策略，否则删除后会缩短幂等保证。

## 迁移与验证

forward-only migration `003_jenkins_ci_run_core.sql` 增加 EntityType、Component、CI Run、receipt、约束、不可变 trigger、Workspace 解析和索引。当前没有生产实例或存量数据，不需要回填。

已实际验证：

```text
./scripts/check-server.sh             PASS
./scripts/check-server-postgres.sh    PASS
./scripts/check-m0-core-contracts.sh  PASS
./scripts/check-repo.sh               PASS
```

真实 PostgreSQL 用例覆盖 migration 重复执行、首次原子写入、顺序与并发重复 delivery、digest 冲突、不同 delivery 的 external run 冲突、事件冲突整单回滚、receipt 不可变、Activity 重建和零 Deployment 事件。

引入真实 HTTP adapter、来源签名、失败审计、运行中更新、多 CI provider、CI Run 用户读取或 Deployment 前，必须继续沿相应专题冻结边界；不得通过放宽本 ADR 的验证责任或保存原始 payload 绕过设计。
