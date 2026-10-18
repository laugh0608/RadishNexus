# 2026-10-18 提交回顾与文档收尾

审阅日期：2026-10-18（Asia/Shanghai）。所有者要求回顾今天全部提交，按修改的代码核对相关文档，将下一步建议写入当前状态，最后单独提交文档并结束工作。

## 当日提交

按本地全部 Git 引用的当日提交时间核对，收尾前共五笔提交，均在 `dev` 串行完成；当日起点为 `fab5561`。本次未 fetch，不将本地跟踪引用解释为实时远端状态。

| 提交 | 修改与结果 | 证据与限制 |
| --- | --- | --- |
| `19290dc` `docs: 确认 Project 决策只读发现方案` | ADR-0040 确认最小列表、当前权限、分页与页面恢复边界 | 由后续实现记录承接，不包含拒绝 / 替代 / 复核 |
| `5ddcc1c` `feat: 实现 Project 决策只读发现` | Project decisions GET、首页入口、canonical 列表与详情返回；无新增 migration 或索引 | [发现记录](2026-10-18-project-decision-discovery.md)：Go、真实 PostgreSQL、查询测量、Web 216 项测试、普通成员 / 25＋2 分页 / 受限来源 / 撤权恢复浏览器 |
| `333c4ce` `feat(authn): 实现自部署本地账户受控恢复` | 独立 CLI、migration 017、密码版本复核、全账户旧 Session 撤销、不可变 operator 审计与历史查询；修正迟到失败登录计数与代理日志过滤 | [恢复记录](2026-10-18-controlled-account-recovery.md)：32 项分层矩阵、真实提交确认丢失、016 旧工件空目标恢复、当前 Compose HTTPS / SSE 与浏览器；不是自助找回或完整失陷处置 |
| `171527f` `feat(authz): 实现 Workspace owner 主动交接` | migration 018、双角色原子交接、代次与邀请任期、不可变 Audit / receipt、本人历史查询及首页 UI；查询未确认时阻止新交接 | [交接记录](2026-10-18-workspace-owner-handoff.md)：36 项分层矩阵、Go race / vet、真实 PostgreSQL、017 旧工件恢复、Web 228 项测试、Compose 与桌面 / 手机浏览器；不转移对象级权限 |
| `506b0e5` `docs: 明确受控试用与升级失败恢复设计` | 支持说明 8 项准入、评估授权 12 项待决、ADR-0043 的 28 项故障与恢复验收矩阵 | 仅设计；三份文档仍待确认，新矩阵未执行，无授权签发或运行权授予 |

本记录所在的第六笔提交仅收尾文档。收尾后 `dev` 相对本地 `origin/dev` 跟踪引用领先六笔；没有 push、PR、发布或业务实例部署。文档提交不代表 ADR-0043 获得接受或实施授权。

## 源码与文档核对

围绕当日实现的最终代码及对应测试、实施记录核对：

- discovery service / PostgreSQL / HTTP 与 `ProjectDecisionsPage`：同一 repeatable-read 快照先检查 governing Project 权限再分页，不逐条读取 evidence；DTO 只接受 proposed / accepted。Project 标题与列表共同清理、重读并丢弃迟到请求。根 README 和决策基线仍有遗漏，已修正；server / Web README 与 ADR-0040 一致。
- recovery service / Store / CLI 与 migration 017：仅已有 active 本地凭证；事务外 KDF、账户 → 凭证锁内复核、全账户旧 Session 撤销与 operator 审计原子提交；已有 operation ID 不再次应用。历史 status 不证明当前密码，`not_observed` 不排除在途提交，旧备份可能带回旧 verifier。恢复不改变角色和业务权限，Web 不提供恢复 API。
- owner transfer service / Store / HTTP 与 migration 018：排序账户 → Session → 排序 membership → Workspace 锁，等待后重新复核有效期、近期认证和预期代次；双角色、Audit、receipt 同事务。历史成功只允许有效成员查询本人事实；当前管理权另读，不把历史成功当作仍有 owner 权限。
- invitation 创建与本地 / OIDC Store 准入：邀请绑定创建者 `role_revision`；旧任期邀请不会因重新接任而复活，已加入成员不受影响。018 升级把有效旧邀请绑定代次 1、失效邀请设为 0，不修复零 owner。对象级与 Deployment 显式权限均不随交接转移。
- `OwnerTransferPanel` 与 App：原请求重试保持 payload / operation ID；只有 ID 的恢复查询遇到未观察或网络失败时，保留未确认 ID 并阻止新交接；当前 Session / ownership 刷新与换账户清理独立于历史结果。未使用 Web Storage。提交前新增的两项状态回归已随 `171527f` 纳入最终 Web 228 项检查。
- Dockerfile / Compose / Caddy 与备份恢复：恢复 CLI 只进入 operations 镜像；显式 `account-recover` service 复用文件 Secret，默认不运行；访问日志与默认运行日志过滤自定义 CSRF Header。匹配版本工具恢复至空目标，Session / 邀请 / OIDC 事务数据不恢复；017 / 018 不新增 Activity 投影版本，当前仍为 v7。

本次修订：

1. 根 README 补 Project 决策发现、账户恢复和 Workspace owner 交接，把仍缺的管理权交接明确为 Project admin。
2. 决策基线 D-019 补 ADR-0041 路由，D-021 删除“Decision 尚无独立列表”的过期表述，并补当日变更原因；不新增产品或授权决定。
3. 架构概览补 ADR-0040 的权限、最小 DTO 与页面恢复边界；部署说明的一次性 operation 清单补 `account-recover`。
4. 当前状态更新本日收尾，将“确认并实施单机停机升级失败恢复”列为下一技术切片，同时保留支持 / 授权选择与准入条件；修正开放问题中笼统的管理员交接表述。
5. 文档索引和当前状态补当日三份实施记录与本收尾记录入口。

领域模型、server / Web README、ADR-0040 / 0041 / 0042、支持草案和授权待决文件的相关合同已随前述提交同步，本次无需重复改写。历史记录中的旧 schema、测试数量、未提交状态及当时未完成的能力保留原义；最新完成线以当前状态与后续记录为准。根 LICENSE、Golden Path、路线图和完整团队持续使用门槛不变。

## 验证与结束边界

- 本轮仅重新执行文档范围验证：`./scripts/check-repo.sh` 通过（534 个文件），`git diff --check` 与暂存差异检查通过；暂存区仅包含预期 Markdown 文件。
- 上表 Go、数据库、升级 / 恢复、Web 与浏览器结果引用当日已有实施记录，收尾未重新执行这些测试，也未将新 ADR-0043 的矩阵标记通过。
- 下一技术切片、确认事项与停止线只维护在[当前状态](../current.md#接下来的事项)。今天不开始手册实现、故障演练、授权模板签发或团队试用，不创建自动任务或次日聊天。
- 真实 Jenkins ADR-0035 / ADR-0037 B、原生中文 IME 人工验收继续暂缓。普通团队持续使用、生产运维与结构化导出仍需各自验收。
- 先前隔离验收资源已按各实施记录清理；本次未启动服务、安装依赖、修改系统配置、操作真实实例或写入远程状态。完成本地文档提交后结束工作。
