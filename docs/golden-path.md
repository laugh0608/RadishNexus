# RadishNexus Golden Path

状态：M0.5 纵向原型基线

日期：2026-09-05

## 目的

Golden Path 用最薄的可运行产品验证 RadishNexus 的核心价值，而不是提前证明它能够分别实现聊天、工单、文档或 Jenkins 集成。

原型必须回答一个问题：当讨论、决策、执行、构建和部署共享同一上下文后，真实研发团队是否能够更快理解现状和原因，并减少复制、粘贴与人工追问。

## 演示故事

一个小型研发团队讨论“为登录接口增加速率限制”：

1. 成员在 Project 频道发起讨论；
2. Thread 中形成结论，并创建一个 Proposed Decision；
3. 决策人补充理由后将 Decision 标记为 Accepted；
4. 从该 Decision 创建一个 Ticket，不复制原始讨论正文；
5. Ticket 关联 `auth-service` Component 和设计 Document；
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
- 创建、接受 Decision 和创建 Ticket 后，通过正式读取入口验证 Timeline；若采用异步投影，先明确可接受延迟与失败可见性，再在该边界内验收。重建测试另行证明修复能力，不能代替正常更新测试。
- 退出再进入原 Thread / Decision，仍可发现后续对象；撤销目标权限后，反向关系、数量、标题和时间等不得扩大既有可发现性。
- Jenkins 验收包含经过真实来源验证的 delivery、重复与冲突输入、外部故障；仅调用 `VerifiedJenkinsDelivery` 内部 service 不算 Webhook 接入完成。staging 记录必须对应外部已完成事实，不执行或伪造部署。
- 每项结论标记自动化、真实数据库、浏览器人工验收或团队使用观察；尚未建立的通知、搜索等消费者标记为未覆盖，不把缺少入口写成安全验收通过。

### 从演示到持续使用

首轮聚焦一个有自部署能力的小型研发团队、一个 Project 与一个真实需求。正式试用前完成路线图中的必要身份、导航、评估授权和运维条件。参与规模不构成许可证限制，也不代表已证明支持 200 人并发。

试用按事先约定的观察周期记录以下问题；优先比较同一团队原流程与新流程，不以消息量、对象数量或测试数量作为价值指标：

| 观察问题 | 记录方式 |
| --- | --- |
| 新成员能否独立解释某次决定的原因 | 是否找到证据、用时、额外询问次数 |
| 从讨论进入执行是否减少重复背景 | 人工复制或重新整理的次数、发生在哪一步 |
| 能否从原讨论发现执行与交付结果 | 独立完成 / 需要帮助 / 无法完成及阻塞位置 |
| 能否持续使用而无需维护者修复 | 重试、权限变化、重新进入、管理员干预与直接改库次数 |
| 哪些能力值得继续投入 | 成员是否主动用于下一项工作，保留 / 修改 / 延后的具体理由 |

证据只保留经授权且脱敏的观察结果。演示通过与持续使用结论分开记录；试用失败应产生明确改进或收窄决定，不能靠增加功能数量掩盖原链路的问题。

## 原型结果的决策用途

Golden Path 完成后再冻结：

- Project、Initiative 与 Component 的首版导航方式；
- Decision 与 Ticket 的转换交互；
- EntityLink 和 Activity API；
- Jenkins 集成需要的最小 Host API；
- 正式 Web Shell 的信息架构；
- 哪些原型代码保留，哪些只作为验证产物丢弃。

Golden Path 不是提前建设生产 MVP。它是对产品差异化、领域边界和最小技术链路的一次联合验证。
