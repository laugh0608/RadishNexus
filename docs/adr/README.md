# 架构决策记录（ADR）

ADR 记录会长期影响多个模块、协作方式或安全边界的工程取舍。产品已确认事项仍以[决策基线](../decision-baseline.md)为准；ADR 负责解释工程层面的背景、选项和后果。

## 索引

| 编号 | 状态 | 决策 |
| --- | --- | --- |
| [ADR-0001](0001-branch-and-pr-governance.md) | 已接受 | 分支与 PR 治理 |
| [ADR-0002](0002-stable-entity-reference-and-event-projection.md) | 已接受 | 稳定实体引用与事件投影边界 |
| [ADR-0003](0003-go-service-foundation.md) | 已接受 | Go 服务端基础栈与数据访问 |
| [ADR-0004](0004-project-scoped-collaboration-permissions.md) | 已接受 | Project 作用域下的协作对象与权限 |
| [ADR-0005](0005-forward-only-postgresql-migrations.md) | 已接受 | Forward-only PostgreSQL migration runner |

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
