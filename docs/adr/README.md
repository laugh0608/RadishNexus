# 架构决策记录（ADR）

ADR 记录会长期影响多个模块、协作方式或安全边界的工程取舍。产品已确认事项仍以[决策基线](../decision-baseline.md)为准；ADR 负责解释工程层面的背景、选项和后果。

## 索引

| 编号 | 状态 | 决策 |
| --- | --- | --- |
| [ADR-0001](0001-branch-and-pr-governance.md) | 已替代 | 分支与 PR 治理 |
| [ADR-0002](0002-stable-entity-reference-and-event-projection.md) | 已接受 | 稳定实体引用与事件投影边界 |
| [ADR-0003](0003-go-service-foundation.md) | 已接受 | Go 服务端基础栈与数据访问 |
| [ADR-0004](0004-project-scoped-collaboration-permissions.md) | 已接受 | Project 作用域下的协作对象与权限 |
| [ADR-0005](0005-forward-only-postgresql-migrations.md) | 已接受 | Forward-only PostgreSQL migration runner |
| [ADR-0006](0006-verified-jenkins-delivery-and-ci-run.md) | 已接受 | 已验证 Jenkins delivery 与 CI Run 原子记录 |
| [ADR-0007](0007-component-scoped-ci-run-read.md) | 已接受 | Component 作用域下的 CI Run 读取 |
| [ADR-0008](0008-dev-first-development-governance.md) | 已接受 | `dev` 优先的单维护者开发拓扑 |
| [ADR-0009](0009-explicit-staging-deployment.md) | 已接受 | 显式 staging Deployment 与环境级授权 |
| [ADR-0010](0010-verified-postgresql-backup-and-restore.md) | 已接受 | 可验证 PostgreSQL 备份与全新实例恢复 |
| [ADR-0011](0011-workspace-scoped-deployment-read.md) | 已接受 | Workspace 作用域下的 Deployment 安全读取 |
| [ADR-0012](0012-local-identity-and-session-foundation.md) | 已接受 | 本地身份与服务端 Session 基线 |
| [ADR-0013](0013-public-authentication-transport.md) | 已接受 | 公共认证 Transport 与可信代理边界 |
| [ADR-0014](0014-session-scoped-deployment-nexus-view-transport.md) | 已接受 | Session 作用域下的 Deployment Nexus View Transport |
| [ADR-0015](0015-same-origin-authenticated-web-shell.md) | 已接受 | 同源 Authenticated Web Shell 与显式静态资源装配 |

## 新建规则

1. 复制以下结构，使用下一个四位编号；
2. 写清背景、决定、替代方案、后果和迁移；
3. 在本索引加入条目；
4. 接受后不要重写历史结论，改用新 ADR 标记 `Supersedes`。

建议结构：

```markdown
# ADR-NNNN：标题

状态：提议
日期：YYYY-MM-DD

## 背景
## 决定
## 替代方案
## 后果
## 迁移与验证
```
