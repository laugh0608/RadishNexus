# 2026-10-13 提交回顾与文档收尾

审阅日期：2026-10-13（Asia/Shanghai）。所有者要求提交工作区实现，回顾今天全部提交，对照修改的代码审阅相关文档，记录接下来的事项，最后单独提交文档并结束工作。

## 当日提交

按本地全部 Git 引用的当日提交时间核对，文档收尾前共六笔提交，均在 `dev` 串行完成；当日起点为 10 月 12 日的客户端连接规划 `38c42d1`。本次没有 fetch，不将本地跟踪引用解释为实时远端状态。

| 提交 | 修改与结果 | 证据 |
| --- | --- | --- |
| `3c6fe24` `docs: 提出 CI Run 仓库与提交来源设计` | ADR-0037 提案；划分接收与发现 A、真实 checkout 采集 B | 提案由后续接受状态与实现记录承接 |
| `490e93b` `feat(ci): 记录仓库 checkout 来源与双向发现` | migration 015、sources / payload v2、不可变 binding / built-from、CI 事件 schema 2、Activity 6、CI Run ↔ Repository 读取 | [来源实施记录](2026-10-13-checkout-provenance.md)：Go、真实 PostgreSQL、014→015 升级、混合来源备份恢复、Web 158 项测试与合成签名输入 HTTPS 浏览器；真实 Jenkins B 未验收 |
| `a2c6c3f` `docs: 提出 Project 工单只读发现方案` | ADR-0038 提案；冻结当前 Project 权限、最小列表与稳定分页 | 提案由后续接受状态与实现记录承接 |
| `7ace9b4` `feat(ticket): 接通 Project 工单只读发现` | Project GET 列表、首页入口、canonical 页面与详情返回；无新增 migration | [发现实施记录](2026-10-13-project-ticket-discovery.md)：Go、真实 PostgreSQL、Web 172 项测试、正式创建链 / 普通成员重新登录 / 25＋2 分页 / 撤权恢复；代表性查询证据不等于生产容量 |
| `3a2e008` `docs: 提出 Ticket 最小状态流转方案` | ADR-0039 提案；独立状态版本、转换矩阵、精确重试与读取兼容 | 提案由后续接受状态与实现记录承接 |
| `777a363` `feat(ticket): 实现最小状态流转与精确重试` | 48 个文件；migration 016、当前权限下四状态命令、原子 receipt / 事件 / Outbox / Activity 7、Web 动作及已有读取适配 | [状态实施记录](2026-10-13-ticket-status-transitions.md)：Go、真实 PostgreSQL、015→016 正常 / 异常基线、备份恢复、Web 183 项测试、双标签冲突 / 已提交响应丢失 / 手机键盘 / 撤权恢复 |

本记录所在的第七笔提交仅收尾文档。最终 `dev` 相对本地 `origin/dev` 跟踪引用领先八笔，其中一笔是前一日客户端规划；没有 push、PR、发布或业务实例部署。

## 源码与文档核对

围绕三笔实现的最终代码核对：

- `ci_checkout.go`、Jenkins payload / config 与 migration 015：管理员绑定不等于 provider 验证；版本分界不重解释旧 v1，observed 才创建不可变 built-from，unavailable 保留有效终态。Repository 反向列表复核当前 Component 权限；受限来源不显示状态、原因或 OID。一次性 sender 支持 v2，持久 worker 明确阻断 v2，未把 A 写成 B 完成。
- discovery service / PostgreSQL / HTTP 与 `ProjectTicketsPage`：列表沿 governing Project 授权、稳定 ID 排序；标题和列表在失败、刷新与页面恢复时共同清理。ADR-0038 初版只接受 open；最终 HTTP 与 Web 已由 ADR-0039 接受四状态，历史实施记录中的 open-only 是当时证据。
- `ticket_status.go`、migration 016 与 `TicketStatusActions`：当前账户 / Workspace / Project 权限先于 receipt；原操作匹配先于新转换检查。版本只随状态改变，终态只能重新打开；原 applied 结果不覆盖后续 Current，网络未知显式重试相同请求。Ticket 创建重试、Project / Component 列表、Timeline 和 Activity 重建同步接受新事实。
- 备份、readiness 与 Web handler：权威字段 / receipt 随备份恢复，Activity 从事件重建；工件与 schema 必须精确匹配。Go 启动时读取静态入口，运行中替换 Web 资源会产生旧入口 / 新文件不匹配，因此升级说明必须要求完整构建配套重启。

本次修订：

1. 根 README 更新工单发现、四状态与 checkout 接收完成线，保留真实采集和具体交付关系缺口。
2. `deploy/README.md` 从过期的 migration 014 / Activity v5 更新到 016 / v7，补充 open-only 旧基线、停写 / 备份 / 配套升级与静态构建重启要求；完整 Compose 未覆盖范围更新为 008–016。
3. 架构概览补 ADR-0037 / 0038 / 0039 路由；决策基线同步已接受的状态与发现合同并记录原因，不另立产品或授权决定。
4. server README 分开既有 v1 真实 Jenkins 三态证据与 v2 合成 checkout 证据，避免把接收成功描述成 SCM 采集完成。
5. 当前状态移除本轮“待提交”、更新收尾与接续事项，纠正旧段落继续要求执行已完成或已暂缓工作的表述；文档索引增加本日证据入口。

领域模型、核心契约、Web README 和 ADR 索引已随实现同步，继续沿用。Golden Path 与路线图的完整交付链、身份运维、结构化导出和持续使用退出条件仍成立，不因局部切片通过而改写。历史记录中的“未提交”“只支持 open”和旧投影版本保留当时含义，以当前状态和后续 ADR 指向最新完成线。

## 验证与结束边界

- 实现提交前检查工作区与精确暂存路径，`git diff --check`、`git diff --cached --check` 通过；实现验证沿用同日状态切片最终 Go、真实 PostgreSQL、备份恢复、Web 与 HTTPS 浏览器结果。
- 文档收尾 `./scripts/check-repo.sh` 通过（491 个文件），差异检查通过，提交前复核仅包含预期 Markdown 文件；不重跑未修改的 Go、数据库或浏览器测试，也不把历史验证描述为收尾重新执行。
- 下一步先形成 **Project 下 Decision 只读发现** 的独立提案：最小列表、当前权限与受限 evidence、分页、canonical 导航及失权恢复。精确接续清单只维护在[当前状态](../current.md#接下来的事项)，本次不开始实现。
- 负责人、优先级、看板及 Decision 拒绝 / 替代 / 复核不扩入本轮。ADR-0035 / ADR-0037 B 与原生中文 IME 人工验收继续暂缓，真实团队持续使用、完整 Golden Path 和生产升级仍未验收。
- 先前验收资源已清理；本次收尾没有新增依赖、启动服务、迁移业务实例、修改系统配置或操作真实 Jenkins，没有创建自动任务或次日聊天。今天到此结束。
