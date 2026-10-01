# ADR-0029：Session 作用域下的 CI Run Nexus View

状态：已接受

日期：2026-09-26

Supersedes（部分）：[ADR-0007](0007-component-scoped-ci-run-read.md) 中“不新增 HTTP route、公共响应 schema、Web 接线”的阶段性停止线；其 Component 权限与来源脱敏决定继续有效。项目所有者已确认本切片范围。

## 背景

CI Run 的终态记录事务与内部 Nexus View 已实现，但用户仍只能看到静态代表页。真实 Jenkins 来源接入、CI Run 阅读和 staging Deployment 写入是三个不同边界。先把已有安全查询接到正式 Web，能够验证用户读取路径，同时不依赖 Jenkins 安装、不改变来源认证，也不把 fixture 当作真实外部交付证据。

## 决定

### 路由、身份与错误

- 新增 `GET /api/v1/workspaces/{workspace_id}/ci-runs/{ci_run_id}/nexus-view` 和 Web 路径 `/workspaces/{workspace_id}/ci-runs/{ci_run_id}`。
- 复用现有 Host、HTTPS / trusted proxy、Session、Workspace membership 和 `GetNexusView`；当前成员必须能读取所属 Component。保留 ADR-0007 的活跃 Workspace 成员语义，不按 Project 或 owner Team 推导权限。
- 仅允许 GET，包括 HEAD 在内的其它方法返回 `method_not_allowed`。读取不改变服务端状态，不要求 CSRF。
- Session 失效返回 `unauthenticated`；当前 membership 失效、未知、跨 Workspace 或不可读对象统一 `not_found`。错误继续携带通用机器码与 request ID，不暴露内部错误。
- 成功与错误统一 `Cache-Control: private, no-store`、`Vary: Cookie`，不增加 ETag、缓存回退或跨域入口。

### 公共响应

沿用 `{"data": {"current": ..., "relations": [], "timeline": [...]}}` envelope，使用显式 DTO，不直接序列化内部 query struct。

| 位置 | 本切片公开字段 |
| --- | --- |
| Current | `ref`（`ci-run / cir_`）、`status`（`succeeded / failed / canceled`）、nullable `started_at`、`completed_at`、`recorded_at`、`updated_at`、`component`（结构化 `ref` 与当前 `title`） |
| Relations | 当前固定空数组；不在本切片增加交付反向关系或 Repository / commit 占位 |
| Timeline | 唯一 `ci-run.recorded`：`id`、`activity_type`、`actor: {"kind":"plugin"}`、`occurred_at`、`status`、`subjects` |
| Timeline subjects | 一个当前可读 Component，使用 `{"visibility":"readable","entity":{"ref":...,"title":...}}`，与 Current Component 一致 |

Current 的 Component 是本对象读取前提，在同一读取快照内不能用 restricted 占位冒充可读。缺失、状态 / 引用 / 时间不一致、多余关系、未知事件或泄漏 actor ID 等投影漂移应显式失败。终态需有完成时间；开始时间可空，存在时不得晚于完成时间。没有运行中更新时 `updated_at` 等于 `recorded_at`，Timeline 发生时间沿用已有领域事件的构建完成时间（`completed_at`），不把接收记录时间当作构建发生时间。

响应不包含 source ID、external run key、delivery ID、receipt、digest、Secret、原始 payload、Jenkins URL、角色或权限集合。不新增 CI Run 标题字段，也不从外部 key 推导标题。

### Web 与发现入口

- 复用已登录工作台、既有 Nexus View 展示组件与集中 adapter。保留正式路径的 loading、失败重试和不可用状态，绝不回退静态 fixture。
- 支持 nullable 开始时间，显示“未提供”；显示“构建成功不代表已经部署”，不提供部署按钮或伪造 Deployment。
- 从既有 Deployment 的来源 CI Run 提供 canonical 跳转；首页现有“按 ID 打开对象”可接受 `cir_`。不增加列表 API、全局搜索、Component 配置或目录。
- 焦点恢复 / 主动刷新重新读取当前权限；401 退出当前登录视图，404 清除当前内容与上下文；切换对象、请求取消与迟到响应不能复活旧数据。

## 替代方案

- 先安装 Jenkins：不能补齐用户读取入口；适合来源 adapter 就绪后的真实联调。
- 同时开放 webhook、CI Run 与 Deployment 写接口：把认证协议、Secret、外部状态映射和环境授权合为一个切片，难以独立验收。
- 直接发布内部 struct：扩大公共兼容边界，并有来源字段泄漏风险。

## 后果、迁移与回退

新增两个只读路径和响应合同，无数据库迁移、事件变更、权限扩展或新依赖。Go 与 Web 成套发布；回退使用上一组服务端 / Web 工件，不修改已有 CI Run 数据。旧客户端不受影响，新页面不能连接尚未实现该端点的旧服务端并伪装成功。

Jenkins 来源认证、重放窗口、签名、Secret 装配、失败审计和重试规则仍需下一份独立协议；本 ADR 不授权安装 Jenkins、启动长驻服务、发起外部构建或记录 Deployment。

## 验证与接受条件

验证覆盖：Session / membership / Component 权限、跨 Workspace 与撤权不可发现性；Host / 方法 / ID / 缓存合同；DTO 字段白名单与投影漂移；缺少开始时间；真实 PostgreSQL 正常命令到 HTTP query 的读取；Web runtime parser、失败与迟到请求、canonical 跳转、桌面 / 手机与键盘操作。

使用既有服务写入的测试数据只能证明内部命令 → 数据库 → HTTP → Web；真实 Jenkins 构建产生 CI Run 要在来源 adapter 与实际实例联调后单独记录。实现与实际验证结果另记当前状态，不以决策接受代替验收。
