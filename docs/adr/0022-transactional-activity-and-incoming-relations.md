# ADR-0022：事务内 Activity 更新与协作对象反向关系

状态：已接受

日期：2026-09-10

Supersedes：部分替代 [ADR-0019](0019-session-scoped-thread-decision-ticket-transport.md) 中协作 Nexus View 仅有来源关系、Timeline 依赖显式重建的阶段边界；身份、写命令、人工确认、幂等和不可发现性保持有效。

## 背景

普通成员通过正式入口创建 Decision、接受 Decision 和创建 Ticket 后，需要重新读取即可看到对应事实，并能从原讨论发现结果。显式全量重建只能证明修复能力；仅按 EntityLink 起点读取只能追溯来源，无法完成双向发现。

首期仍为 Go 模块化单体、PostgreSQL 和同源 React Web。已有投影内容仅为少量安全引用与状态，当前没有必须异步处理的外部 I/O，因此先采用可直接验证的一致性边界。

## 决定

### Activity 更新与失败

- 白名单继续为 `decision.proposed`、`decision.accepted`、`ticket.created`、`ci-run.recorded`、`deployment.recorded`。这些正常命令在同一业务事务写入原有业务、关系、receipt、不可变事件与 Outbox，并从该事件产生 projection version 1。
- 投影成功后，同事务将 `activity-projector` delivery 标记为 `delivered`，记录实际完成时间和一次尝试；不留下并不存在的后台消费任务。Message 的其它 Outbox consumer 保持原有行为。
- 命令提交成功后开始的正式读取立即看到该对象的 Timeline。并发且早于提交开始的读取仍可能看到旧快照；页面需重新读取或重新进入，不增加实时推送或隐藏轮询。
- 投影校验、SQL 或提交失败会回滚整笔命令；不能返回业务成功并遗漏 Timeline。响应丢失后仍按现有 command receipt 重试并重新授权，不重复投影。
- Activity 仍是可删除、可重建的派生数据；不反向控制业务事实，也不承诺替代安全 Audit。

### 全量重建与并发

- 正常更新和显式重建共用事件到投影的映射与写入函数。重建继续仅读取不可变 `domain_events`，不读取 Outbox 状态或旧 Activity。
- 重建使用 `READ COMMITTED`，先取得 `activity_items` 的 `SHARE ROW EXCLUSIVE` 表锁，再读取源事件、删除当前版本并重建，最后原子提交。
- 已进入投影写入的业务事务先提交，重建等待后取得的新快照包含其事件；之后到达的投影写入等待重建提交，再写入自己的活动。重建互相串行，普通读取仍可读提交前的完整版本。
- 不能在等待锁之前取得 repeatable-read 旧快照，否则可能清除等待期间已经提交的新投影。
- 重建失败保留此前完整投影。全量重建会阻塞这五类业务写命令，应作为受控维护操作；查询或请求取消、超时按现有错误路径返回，不通过无限重试掩盖失败。

### 双向关系与权限

仅增加以下反向发现，不复制镜像 EntityLink、不创造新的关系事实：

| 当前对象 | direction | relation_type | target |
| --- | --- | --- | --- |
| Thread | outgoing | started-from | 原 Message，仅 messaging-origin Thread |
| Thread | incoming | derived-from | 从该 Thread 提出的 Decision |
| Decision | outgoing | derived-from | evidence Thread，或既有 restricted 占位 |
| Decision | incoming | implements | 实现该 Decision 的 Ticket |
| Ticket | outgoing | implements | 来源 Decision |

`target` 表示当前视图中可跳转的另一端；`relation_type` 保留权威事实的方向含义，不能因为反向展示而将 Ticket → Decision 的 `implements` 改写成 Decision 实现 Ticket。

两种方向都重新检查当前 Workspace、Project 和对象权限。不可读的反向目标使用 `hidden`，完全省略，不返回数量、受限占位、关系名、ID、标题或时间。原 Decision evidence 的 `restricted` 合同保持不变，仅返回 `visibility`，不携带 `direction`。

本切片延续完整对象关系读取：先出向、后入向，各组按 `(created_at, link_id)` 稳定排序；不截断、不返回未过滤总数、不添加分页参数或 cursor。逐目标授权和标题查询仍是线性成本，代表性数据测试记录成本，不据此宣称团队容量。游标分页、结果上限和反向索引作为后续独立兼容性切片审查。

### 公共 DTO 与 Web

现有三个协作 Nexus View 路由不变。readable relation 新增必填 `direction: outgoing | incoming`，restricted relation 仍只能是 `{"visibility":"restricted"}`。Go adapter 与 Web runtime parser 都验证方向、关系类型、对象类型、来源基数与重复入向目标；不因为增加入向结果放宽原有来源合同。

Web 在“来源与后续结果”中区分两类关系，分别提供来源与后续对象的 canonical 链接。重新进入时重新读取，不用旧列表补齐受限结果。

本切片不改变 Timeline 的目标语义：Decision 显示自身 proposal / acceptance，Ticket 显示自身 creation；Thread Timeline 仍为空。反向发现通过 Relations 完成，不将别的对象的事件复制到 Thread 或 Decision 时间线；跨对象 Activity 分发需另行冻结来源和过滤合同。

## 替代方案

- **可靠异步 worker**：需要租约、重试、停止、恢复、延迟和失败可见性合同。当前投影无外部调用且足够轻量，增加这些运行复杂度尚无必要；未来实测写延迟或独立消费需求出现后再评估。
- **每次读取都重建或客户端轮询补齐**：扩大读开销、隐藏正常写入缺口，无法证明投影一致性。
- **复制反向关系**：制造第二份事实，增加幂等、撤销与恢复分歧。
- **反向不可读目标返回占位**：来源对象没有主动断言该后续对象可被发现；占位也会泄漏存在性。

## 迁移与验证

没有业务表、migration、事件 schema、projection version 或依赖变化。旧事件与现有投影仍可由既有显式重建恢复；服务启动不自动补齐历史投影，也不修改旧 pending delivery。全新命令与旧数据补齐分别验收。

旧 Web 严格字段白名单会拒绝新增 direction，新 Web 也会拒绝缺失 direction。Go 与 Web 必须作为同一工件发布和回退，不支持混用；部署切换后旧标签页需重新加载。回退不会删除业务事实，但旧版正常命令仍有原来的 Activity 更新缺口。

验证覆盖正式 HTTP command → query（不手工重建）、幂等重试、投影错误整单回滚、重建错误保留旧投影、写入与重建交错、双向权限及撤权、Go / Web 合同漂移和 canonical 跳转。当前执行结果另见状态证据，不把内部 verified CI Run 测试当作真实 Jenkins 接入完成。
