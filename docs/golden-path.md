# RadishNexus Golden Path

状态：M0.5 纵向原型基线

日期：2026-09-05（原型基线）；最近合同同步：2026-11-02；有限检查点规划：2026-11-10（原合同不变）。

规划归属：本页是[能力专题地图](topics.md#golden-path)中的跨能力整合目标，串联各子专题的真实交付。各能力任务与细项标准仍在原合同 / 专题，整体退出只按本页与路线图判断；局部切片交付不代表整体退出，当前进度与接续见[当前状态](status/current.md)。

## 目的

Golden Path 用最薄的可运行产品验证 RadishNexus 的核心价值，而不是提前证明它能够分别实现聊天、工单、文档或 Jenkins 集成。

原型必须回答一个问题：当讨论、决策、执行、构建和部署共享同一上下文后，真实研发团队是否能够更快理解现状和原因，并减少复制、粘贴与人工追问。

## 演示故事

一个小型研发团队讨论“为登录接口增加速率限制”：

1. 成员在 Project 频道发起讨论；
2. Thread 中形成结论，并创建一个 Proposed Decision；
3. 决策人补充理由后将 Decision 标记为 Accepted；
4. 从该 Decision 创建一个 Ticket，不复制原始讨论正文；
5. Ticket 关联 `auth-service` Component 和设计 Document（人工组件关联已按 [ADR-0034](adr/0034-ticket-component-relations.md) 接通，不表示该 Ticket 已进入某次构建或部署）；
6. Jenkins Webhook 写入一次 CI Run，并在原 Thread 与 Ticket 中显示状态；
7. 成功构建后，持有目标 Environment 显式授权的成员通过受控操作记录一次到 `staging` 的 Deployment；
8. 打开任意对象的 Nexus View，都能看到当前状态、关系和完整时间线；
9. 备份并恢复到新实例后，上述关系和来源仍然存在。

## 原型范围

### 必须包含

- 一个 Workspace、少量种子用户和基础角色；
- 一个 Project、一个 Component、一个 Repository 映射和一个 Environment；
- 单个频道、消息和 Thread；
- 从 Thread 创建 Decision；
- 从 Decision 创建基础 Ticket；
- 一个简单 Markdown Document；
- Jenkins Webhook 到统一 CI Run；
- CI Run 到 Deployment 的显式关联；
- EntityLink、Activity 和最小 Nexus View；
- 基础权限过滤和审计；
- PostgreSQL 持久化；
- Docker Compose 开发部署；
- 最小备份与恢复演练。

### 明确不包含

- 完整聊天体验、表情、收藏和复杂搜索；
- 工单看板、自定义工作流和大量字段；
- 多人协作文档、CRDT 和离线编辑；
- 可安装插件市场或多语言 SDK；
- Jenkins 主动重跑、生产部署和回滚；
- Flutter 客户端；
- 图数据库、微服务或独立消息中间件；
- 自动执行 AI 生成内容。

## 产品约束

### 不复制上下文

从讨论创建 Decision 和 Ticket 时保存引用、选定摘录和来源，不把整段聊天复制成失去同步关系的新正文。

### 人工确认决策

系统可以生成 Decision 草案，但只有具备确认权限且能够读取全部 evidence 的人才能 Accepted。Project 管理角色不会自动穿透 restricted Thread；自动摘要不能被展示成已经确认的团队结论。

### 结论变化保留原决策与执行依据

最小拒绝与同 Project 单链替代分别遵循 [ADR-0048](adr/0048-decision-lifecycle-and-rejection.md) 和 [ADR-0049](adr/0049-decision-supersession.md)。拒绝需要明确理由；替代必须先分别接受新旧结论，再对照来源并明确确认。替代保留旧接受事实、双方版本与不可变关系，不自动转移 Ticket、修改 Document 或确认新的执行结果。验收须区分当前状态和未知原请求的事实核验，并检查当前权限、双向发现、双对象 Activity、备份恢复与迁移后的合法执行历史；人工复核保留结论当时的版本、证据和评估历史。到期触发属于后续切片。 [ADR-0050](adr/0050-decision-manual-review.md)

### 基础执行不替代交付事实

Ticket 的四状态、单一负责人和固定优先级分别遵循 [ADR-0039](adr/0039-ticket-status-transitions.md)、[ADR-0046](adr/0046-ticket-assignee.md) 与 [ADR-0047](adr/0047-ticket-priority-and-filtering.md)。责任记录不授予权限，优先级不自动确定执行顺序，done 不证明构建或部署成功。验收这些切片时须检查显式确认、冲突 / 原操作重试、失权清理及迁移后历史；通过这些局部检查不代表下述完整 Golden Path 已退出。

### 构建不等于部署

CI Run 和 Deployment 是两个对象。原型只能由持有目标 Environment 显式授权的成员通过受控操作记录 staging Deployment；构建成功既不自动触发 Deployment，也不证明外部部署已经发生，更不能伪造生产部署事实。

### 外部系统故障隔离

Jenkins 不可用、重复发送 Webhook 或返回异常数据时，聊天、Decision 和 Ticket 仍然可用。Webhook 处理必须幂等，并能从审计记录解释失败原因。

### 权限不穿透

从公开 Ticket 关联到私密 Thread 时，无权限用户不能看到 Thread 标题、摘要、参与者或正文。Nexus View 只显示安全占位符。

## 验收标准

Golden Path 只有同时满足以下条件才算通过：

1. 新用户可以在 15 分钟演示内理解完整链路；
2. 从讨论到部署不需要人工复制同一段背景到多个对象；
3. 对约定的对象关系，既能从结果追溯来源，也能在刷新原对象后发现后续 Decision、Ticket 和交付结果；两个方向都重新检查当前权限；
4. 重复 Jenkins Webhook 不产生重复 CI Run；
5. 私密对象不会通过关系、通知、搜索或时间线泄漏；
6. Jenkins 停止响应不会阻塞核心业务写入；
7. 备份恢复后实体、关系、事件来源和审计仍然一致；
8. Channel 历史分页顺序稳定且无重复，不返回当前不可读 Thread 的回复，权限撤销后不能继续读取；
9. 团队可以基于原型明确说出哪些能力值得继续建设，哪些只是单点工具的重复。

### 验收入口与证据

- fixture 可以建立少量测试账号、基础对象和授权；被验收的 Message、Decision、Ticket、Document 与交付事实须通过对应正式业务入口产生。演示不得靠手写 SQL、预置最终关系或手工重建 Activity 补齐流程。
- 创建、接受 / 拒绝、明确替代 Decision 和创建 Ticket 后，通过正式读取入口验证相应 Timeline；替代同时检查双方，已有执行对象继续按自身权限操作。若采用异步投影，先明确可接受延迟与失败可见性，再在该边界内验收。重建测试另行证明修复能力，不能代替正常更新测试。
- 退出再进入原 Thread / Decision，仍可发现后续对象；撤销目标权限后，反向关系、数量、标题和时间等不得扩大既有可发现性。
- Jenkins 验收包含经过真实来源验证的 delivery、重复与冲突输入、外部故障；仅调用 `VerifiedJenkinsDelivery` 内部 service 不算 Webhook 接入完成。staging 记录必须对应外部已完成事实，不执行或伪造部署。
- 每项结论标记自动化、真实数据库、浏览器人工验收或团队使用观察；尚未建立的通知、搜索等消费者标记为未覆盖，不把缺少入口写成安全验收通过。

### 从演示到持续使用

项目开发完成后，首轮真人观察聚焦一个有自部署能力的小型研发团队、一个 Project 与一个真实需求。正式试用前完成路线图中的必要身份、导航、评估授权和运维条件。参与规模不构成许可证限制，也不代表已证明支持 200 人并发。 [路线图推进原则](roadmap.md#推进原则)

试用按事先约定的观察周期记录以下问题；优先比较同一团队原流程与新流程，不以消息量、对象数量或测试数量作为价值指标：

| 观察问题 | 记录方式 |
| --- | --- |
| 新成员能否独立解释某次决定的原因 | 是否找到证据、用时、额外询问次数 |
| 从讨论进入执行是否减少重复背景 | 人工复制或重新整理的次数、发生在哪一步 |
| 能否从原讨论发现执行与交付结果 | 独立完成 / 需要帮助 / 无法完成及阻塞位置 |
| 能否持续使用而无需维护者修复 | 重试、权限变化、重新进入、管理员干预与直接改库次数 |
| 哪些能力值得继续投入 | 成员是否主动用于下一项工作，保留 / 修改 / 延后的具体理由 |

证据只保留经授权且脱敏的观察结果。演示通过与持续使用结论分开记录；试用失败应产生明确改进或收窄决定，不能靠增加功能数量掩盖原链路的问题。

<a id="finite-web-closeout"></a>

## Web 协作闭环有限检查点

2026-11-10 所有者确认本检查点，用于组织已有 Web 能力的有限验收和后续使用判断。它是完整 Golden Path 中协作部分的阶段性检查，不替代上文完整故事、九项验收标准或 M0.5 / M1 / M4 的退出条件。当前候选、执行顺位与暂缓状态只见[当前状态](status/current.md#接下来的事项)，实际证据进入日期化 reviews。

### 范围与候选

以一个 Workspace、一个 Project 和一个可解释的需求为主场景，从正常导航进入：频道讨论 → 创建与人工接受 Decision → 创建并推进 Ticket → 创建与保存 Markdown Document → 退出重进后发现对象并追溯原讨论。相关角色分离、权限拒绝、Decision 既有历史、文档版本及数据取回沿已有合同验收；被验对象通过正式入口产生，不手写 SQL 或预置最终关系。

本批使用已交付的工单状态、负责人、优先级和发现能力；「我的工单」与看板纳入时须登记所选候选及适用证据。截止日期、更多字段、完整聊天、CRDT、通用分页和生产容量不作为默认前置；真实场景暴露的必要缺陷仍须处理。单 Project 上下文包只证明其支持的协作范围，不代替全 Workspace 或研发实体迁移。

执行前固定精确提交、应用与运维 / CLI 工件、配置、schema、目标环境和数据范围，并形成一次差异清单。旧候选、单项新功能与运行实例分别登记；只读源码和文档不能证明实例已升级。复用历史证据必须说明其对新候选仍适用的依据，新增或受影响范围补验；不得一律沿用旧记录，也不无差别重跑全部历史矩阵。

### 技术出口

| 验收面 | 本批必需结论 | 合同与证据路由 |
| --- | --- | --- |
| 正常导航与协作链 | 通过正式入口完成上述主场景，重进后能找到对象、当前状态和原始讨论，无需维护者提供 ID、复制背景或改库 | [传输 ADR-0019](adr/0019-session-scoped-thread-decision-ticket-transport.md)、[工单与上下文](topics.md#tickets-context)、[Markdown ADR-0028](adr/0028-minimal-markdown-document.md) |
| 权限与历史 | 所选路径的拒绝、撤权、冲突 / 原请求重试、来源及 Activity 按原合同成立；当前状态与既有执行依据可区分，不隐式转移权限或改写事实 | [ADR-0022](adr/0022-transactional-activity-and-incoming-relations.md)、[Decision 合同链](topics.md#communication-decisions)、[成员生命周期 ADR-0045](adr/0045-membership-lifecycle-and-management-handoff.md) |
| Document 与使用环境 | 保存、冲突、版本和恢复证据适用于候选；登记实际桌面 / 移动视口与浏览器范围，中文输入场景明确原生 IME 人工结果 | [Markdown 验收与原生缺口](status/reviews/2026-09-26-markdown-document.md)、[工程规范](development/engineering-standards.md) |
| 可恢复与取回 | 匹配工件下的备份、空目标恢复及单 Project 导出 / 导入证据适用，实体、来源、历史和目标权限一致，取回范围与限制可解释 | [ADR-0043](adr/0043-single-host-upgrade-failure-recovery.md)、[ADR-0044](adr/0044-portable-project-context-package.md)、[试用准入 T03 / T06 / T07 / T08](trial-support.md#准入与退出判据) |

技术结论逐项标记通过、未通过、待验证或本批范围外，并附候选、环境、证据与影响说明。必需项未通过或缺少证据时，只报告已完成的部分和阻塞，不能宣布本批全部通过；关键权限、历史一致性和恢复真实性不得后置为可选项。暂缓的原生 IME 不因有限命名被代签；独立操作者 U27 沿原合同单列，技术演练不等于可独立运维。旧证据可复用的条件与本批未覆盖范围必须写入结论。

### 使用出口与后续决定

本检查点形成技术结论后，开发阶段继续按[当前顺位](status/current.md#接下来的事项)交付功能。项目开发完成并由所有者恢复观察安排后，再沿[试用准入](trial-support.md#准入与退出判据)复核主体、任务、候选、环境、支持、退出和适用授权。Agent 多账号操作归入技术证据，真人独立操作和 R08 依据真实参与者的观察证据记录。准入条件满足并经所有者确认后方可开放试用。

真人观察沿前述持续使用指标，关注能否解释决定原因、减少重复背景、重新发现执行结果及维护者介入情况，并补充 [R08](adr/0050-decision-manual-review.md#验收矩阵) 的决策复核与追溯。观察窗口结束时由所有者依据结果选择继续、定向修复后重试、收窄或暂停。参与者或环境未就绪时，登记具体缺项、负责角色和下一复核触发点，保持“独立使用 / 价值未验证”。

真实 Jenkins、checkout 来源、Ticket 的具体交付关系和外部 staging 事实仍按[交付链恢复顺序](status/current.md#交付链保留完成线与恢复顺序)推进。有限协作结果只能支持讨论到执行部分的价值判断，不能证明构建 / 部署不断链、完整 Golden Path 或真实团队持续使用已经通过。

## 原型结果的决策用途

Golden Path 完成后再冻结：

- Project、Initiative 与 Component 的首版导航方式；
- Decision 与 Ticket 的转换交互；
- EntityLink 和 Activity API；
- Jenkins 集成需要的最小 Host API；
- 正式 Web Shell 的信息架构；
- 哪些原型代码保留，哪些只作为验证产物丢弃。

Golden Path 不是提前建设生产 MVP。它是对产品差异化、领域边界和最小技术链路的一次联合验证。
