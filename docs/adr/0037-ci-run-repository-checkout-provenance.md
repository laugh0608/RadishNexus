# ADR-0037：CI Run 的 Repository 与 checkout commit 来源

状态：已接受（2026-10-13 所有者确认实施切片 A；切片 B 继续暂缓）

日期：2026-10-13

关联：[ADR-0030](0030-authenticated-jenkins-delivery-adapter.md)、[ADR-0033](0033-repository-mapping-and-component-relations.md)、[ADR-0035](0035-durable-jenkins-terminal-delivery.md)、[Golden Path](../golden-path.md)。本决策部分扩展 ADR-0030 的 payload / 来源配置、ADR-0029 的 CI Run 读取白名单及 ADR-0035 的磁盘交接版本；旧版本身份、receipt、权限和构建 / 部署分离语义继续有效。

项目所有者已确认本文并授权实施切片 A；实际完成线以当前状态及实施记录为准。切片 B 的采集 profile 仍待后续实现与真实验收，继续暂缓；本次确认不自动授权依赖安装或外部操作。

## 背景与源码基线

基线为 `38c42d1`，本地 `dev` 工作区初始干净，相对本地 `origin/dev` 领先一笔客户端规划提交；未 fetch 或核验实时远端。

- [现有 payload](../../server/internal/jenkins/protocol.go) 只含 job、build number、终态与时间；`VerifiedJenkinsDelivery` 代表来源认证与映射完成，不证明代码来源。
- [CI Run 事务](../../server/internal/goldenpath/postgres/ci_run.go) 已原子提交 receipt、CI Run、事件、Outbox 和正常 Activity；没有 Repository / commit 字段。
- Repository 外部稳定身份与 Component 人工 `source-repository` 关系已实现；Ticket 的人工 `affects` 只说明涉及组件，不证明进入某次构建。
- [collector](../../experiments/jenkins-collector/JenkinsCollector.groovy) 只采终态；既有 Jenkins probe 没有实际 Git checkout。ADR-0035 B 的真实 Linux 持续采集、挂载与重启验收仍暂缓。

本提案补齐“这次 CI Run 报告了哪个受控 checkout revision”。它不证明构建产物完全由该 commit 生成，不证明 Ticket 已完成，也不证明外部部署发生。

## 推荐决定

| 问题 | 推荐合同 |
| --- | --- |
| 仓库身份 | 继续使用 Repository 的 `(workspace, provider, provider_origin, external_id)`；部署者显式将一个 checkout 绑定到既有 Repository，不从 job、URL、分支或 Component 共属推断 |
| commit | 使用带算法的完整 Git object ID，作为来源关系的不可变值；首批仅支持 `sha1` 与 40 位小写十六进制，不新增 Commit 实体、代码托管或提交图 |
| 证据含义 | 分开表达“管理员核对的仓库身份绑定”和“controller 采集的 checkout revision”；不使用含义不明的全局 `verified=true` |
| 第一种采集 profile | `jenkins-git-single-checkout-v1`：受控 Freestyle job、单 GitSCM / 单 remote / 单次 checkout；运行配置由可信部署者管理，仓库不能改采集器或 job 配置 |
| 多仓库与合并 | 首批明确不支持多 checkout、多仓库、submodule、动态 Pipeline 或本地预合并；不得截取其中一个 commit 冒充完整来源。正常仓库中已有的 merge commit 可按实际 checkout OID 记录 |
| 来源缺失 | 有有效终态但没有满足合同的来源时，显式记录 `unavailable` 和受控原因；不创建 `built-from`。结构错误、绑定冲突或同身份内容冲突拒绝整个 delivery |
| 历史与修正 | 首次提交后来源不可补写或覆盖；旧 v1 标为 `not_collected`。错误来源不能通过改 payload 重发纠正，先停用来源并保留证据；追补 / 撤回另立合同 |
| 首个切片 | A：v2 接收、持久化、来源关系、双向发现与正式 Web 展示；用隔离协议输入验证，不宣称真实 checkout 已验收。B：固定版本真实采集与原生 Linux 验收，单独准备资源与依赖 |

## 可信边界

### 能证明什么

| 层 | 信任依据 | 可以说明 | 不能说明 |
| --- | --- | --- | --- |
| Repository 登记 | 当前 owner 在正式入口声明外部身份 | Workspace 中存在一个明确的代码库映射 | provider 已连接、URL 与 ID 自动核实、成员有外部代码权限 |
| checkout 绑定 | 可信部署者核对 provider 稳定 ID、实际 clone 目标和受控 job，固定配置版本 | 管理者声明该 checkout 对应这一 Repository | provider 签发了证明、URL 永久指向同一仓库 |
| controller 记录 | 受控 profile 的本次 Run 记录与 checkout 观察一致 | SCM checkout 完成时使用的完整 revision | 后续脚本没有修改工作树、额外下载代码、依赖或构建产物可复现 |
| HMAC delivery | ADR-0030 的来源密钥、签名、时窗与绑定检查 | 消息由该 capability 持有者发送且传输内容未被替换 | 发送者诚实、controller 未被攻陷、用户声明变成可信事实 |
| Nexus 事务 | 唯一 receipt、来源快照、关系、事件与 Activity 原子提交 | 已接受事实可幂等读取和恢复 | Ticket 已交付或 Deployment 已发生 |

controller 管理员、采集器部署者、选定 SCM 插件与 agent 的 Git 执行环境属于此 profile 的可信计算边界。任一边界被攻陷，都可能伪造观察；首批没有远程证明或供应链 attestation。仓库代码、构建参数、环境变量、console log、changelog 和仓库产出的 JSON 不作为来源权威。尤其不能把 `GIT_COMMIT`、最后一次成功构建或当前分支 HEAD 当作本次 checkout 证据。

HMAC 仅由独立 worker 持有，agent / 仓库不能读取；checkout 观察文件、spool、worker 状态与确认目录继续隔离。Nexus 不访问 payload 中的地址，不 clone、不调用 provider API、不通过外部链接补全事实。

### 仓库绑定与变更

建议 sources 配置新增版本 2；旧配置版本 1 保持原行为。v2 source 在原绑定之外增加唯一 `checkout_binding`，内容为：`binding_id`、`repository_id`、`provider`、`provider_origin`、`external_id`、`profile`、`effective_from_build`。ID 沿 source ID 的受限语法；外部身份按 ADR-0033 规范化并与数据库精确核对。`effective_from_build` 沿合法 build number 范围，记为 N。

部署者须通过 provider 管理界面或受控管理记录核对稳定 ID 和 clone 目标，不能从 URL 解析出假 ID。核对记录及精确、无凭据的 clone 地址保存在受控部署配置中，不进入 Nexus 用户 DTO。collector 的本地绑定另固定这一个 clone 地址及运行 profile；URL 仅用于与已确认配置比对，不能替代 Repository 身份。首批仅接受无 userinfo / query / fragment 的 HTTPS clone 地址；不根据 `.git`、别名、重定向或路径大小写自动合并。改名、迁移或 clone 地址变化必须停止采集后重新审阅绑定，不自动跟随。

receiver 配置是部署者显式授予来源的窄 `link-from / link-to` capability，仅允许该 source 的 CI Run 指向绑定的一个 Repository；不开放通用关系 API。Component 的人工仓库关联可用于配置核对，但不授予此 capability，也不参与历史重试授权。解除 `source-repository` 不删除构建来源或停用机器身份；停止未来写入须按 ADR-0030 撤销 source / key 并重启。操作说明必须明确这两个动作的区别。

为防止新增绑定跨重启被静默替换，建议新增不可变 `ci_source_checkout_bindings`：以 `(workspace_id, source_kind, source_id)` 唯一，保存上述非敏感绑定字段及首次记录时间；同 Workspace 引用 Component / Repository。首条有效 v2 delivery 在业务事务内插入，后续逐字段比较，不匹配为冲突；启动校验只读检查已存在行，不在 readiness 或启动时自动建记录。配置撤销后记录仍保留，恢复该身份必须匹配原绑定。此表只保存已使用 capability 的不可变证据，不承担可编辑 Integration 注册、Secret 或关系表职责。

新增来源绑定后，既有 source 的 Jenkins 实例、job、Workspace / Component 不能改变。将同一 source 从 v1 升级到 v2 只允许一次确定的 N 分界：小于 N 的 run 继续精确 v1，大于等于 N 的 run 必须使用 v2；更换密钥不改变 N。受控停机时排空正在运行的 job、核对 next build number 与全部待送输入再确定 N，不能把已经发布为 v1 的编号划入 v2。

已使用的 Repository / profile / N 变更需要新 source、新 spool 与不重叠的采集起点；旧 source 封闭采集上界，保留原积压的精确发送。跨 source 没有业务去重，禁止将同一段历史复制到新 source。首次启用前的配置真实性和 source 身份变更仍依赖部署变更管理，不宣称本表已解决完整来源治理。

## 采集证据合同

profile 的建议实现依附现有 controller 采集器；不从业务仓库加载 Groovy，不让 worker 解析任意 Jenkins 对象。

1. 运行开始时确认受控 Freestyle / GitSCM 配置与本地绑定一致，将 source / build、binding ID、profile 和配置指纹写入 controller 私有的版本化观察记录。指纹仅覆盖本 profile 白名单配置，不对包含凭据的整个 job XML 做摘要或备份。
2. 在受控 checkout 观察点取得本次 Run 的 Git 数据：唯一匹配的 `BuildData`、对应 `Build.hudsonBuildNumber == Run.number`、非空完整 `Build.revision`；不得调用一个“last revision”便假定它来自本次运行。观察 remote 必须与绑定精确一致，多个候选不任选其一。
3. 将观察结果与本次编号、binding、配置指纹持久保存；以文件同步、原子发布与父目录同步保障交接。文件不可由 agent 写入；重复相同观察可幂等读取，第二次 checkout 或变化标记为不支持，不能覆盖第一条以伪装成单次 checkout。
4. finalized 后再次核对本次 Run、观察记录和 profile；`revision` 与 `marked` 不同、配置变化、多 SCM 或额外 checkout 均不得生成首批 `observed`。普通 merge commit 只要是实际获取并 checkout 的同一 revision 可接受，不推断其父提交、PR 或包含的 Ticket。
5. 缺失 checkout 观察、checkout 未成功或无法确认属于本次运行，输出 `unavailable`；不得用当前 job 配置重建旧 run 的身份。重启后只从已落盘观察与保存的 Run 对账，不能只依赖回调。已有不可变终态快照直接重用，禁止随着远端分支变化重新计算。

SCM 回调、Run 持久化与观察落盘之间无法承诺原子事务。观察丢失时明确降为来源不可用；同一观察文件损坏、摘要不符或同步失败属于存储故障，暂停该 source，不能降级为空观察继续发布。记录采集错误不得改变 Jenkins 构建结果，也不能阻塞 Nexus 普通协作路径。

首批不检查后续工作树是否被脚本修改；UI 必须写“checkout revision”，不能写“完整构建输入已验证”。受控构建脚本不得另行切换仓库或提交；若场景需要多个 checkout、submodule、共享库、动态 Pipeline、预合并或产物证明，先扩展 profile 及证据模型，再承诺支持。已知不受支持的场景以 `unsupported_checkout` 表达，不截断成一条来源。

Git plugin 是 B 的新增外部依赖候选，既有 probe 未装配它。实施 B 前需单独固定 Jenkins core、Git / git-client 及传递依赖版本，检查维护、安全公告、许可证、checksum 和离线可复验性，按项目规则取得安装授权。本文未选定或安装任何版本，也不以在线 API 文档代替固定版本测试。

## Payload、兼容与幂等

建议沿用 `/api/v1/integrations/jenkins/{source_id}/deliveries`，用 body `version: 2` 明确区分语义；URI 的 v1 不意味着旧程序能接受新 body。保留原八个字段并新增必需 `checkout` 对象，原终态、时间、大小上限与严格 JSON 检查不变。v2 `checkout` 的封闭字段为：

| 字段 | 合同 |
| --- | --- |
| `binding_id` | 必须等于 receiver 配置的唯一 checkout binding，body 不自行选择 Repository / Workspace / Component |
| `state` | `observed` 或 `unavailable` |
| `reason` | observed 必须为 null；unavailable 必须为 `checkout_not_observed / unsupported_checkout / binding_changed_during_run` 之一；不包含自由文本 |
| `revision` | observed 为 `{algorithm:"sha1", oid:"完整的 40 位小写十六进制"}`，拒绝全零；unavailable 必须为 null |

上表是形状说明，不是可发送的构建证据。SHA-256 Git repository、短 SHA、分支、tag、PR head 或 commit message 均不能填入首批 revision。算法显式保存，便于未来扩展时不靠字符串长度猜测；未来支持 SHA-256 必须新增经过验证的 profile / 协议合同。

- sources v1 只接收 payload v1。sources v2 按 N 分界接受 v1 / v2，错误版本 `400 invalid_request`，不能自动删字段降级重试；绑定不匹配 `403 source_binding_mismatch`，已持久绑定冲突 `409 conflict`。
- 原 HMAC `v1=` 和签名输入域保持不变：它已签入精确 body 摘要，覆盖 body version；不把签名版本与业务 payload 版本混为一谈。时窗、限流和成功响应不变。
- v1 canonical digest 的编码与字节必须原样保留。v2 单独版本化编码，纳入原事实、完整绑定字段、state / reason / algorithm / oid；轮换 key、JSON 空白、重发时间不改变业务 digest。
- `(source_id, build_number)` 与 `build-{number}` 身份保持不变。同一身份从 unavailable 改 observed、替换 commit / Repository、改变时间 / result 都冲突；不生成新 delivery ID 绕过。不同 build number 即使同 commit 也产生不同 CI Run。
- 不支持的 Jenkins result 仍沿原合同拒绝，不通过来源状态把 `UNSTABLE` 或 `NOT_BUILT` 改成既有三态。来源 unavailable 只表示来源质量，不改变真实 succeeded / failed / canceled。

worker 必须仍发送导入的原始字节，不能依据当前配置给旧 payload 加来源。B 扩展 collector / worker manifest 的版本化绑定，保存 N、binding 和 profile，拒绝未知版本；保留 checkpoint / worker state / ack 各自已有版本含义，不把这些格式混用。旧 manifest 升级只能在停止双方并取得一致备份后显式执行，必须验证旧输入和 digest 不变、旧 compacted / retired 仍可读取、新程序不能重建已清理输入；不能通过清空 spool 升级。

checkout 观察是 controller 私有输入证据，纳入其离线备份与配额；配额耗尽停止采集并报告缺口。仅在对应终态已获得匹配 ack、满足保留期且通过显式清理后才能清理观察；未确认、冲突或损坏记录不自动删除。worker 原始摘要确认与 receiver canonical digest 的职责继续分离。

## 持久化、EntityLink 与事件

建议首个 migration 使用下一空闲编号 `015_ci_run_checkout_provenance.sql`（实施前复核），不修改 001～014 或历史 checksum。

1. `ci_runs` 增加不可变 `checkout_state` 和 nullable `checkout_reason`；状态为 `not_collected / observed / unavailable`。旧记录只初始化为 `not_collected`，不回填虚构来源。v2 行引用不可变 source binding；v1 不制造 binding 引用。
2. 新增上述 `ci_source_checkout_bindings`，禁止更新 / 删除；全部引用限定同 Workspace。配置 capability、Repository 外部身份与 body binding 的核对在 receipt 返回前完成，精确重试也不能绕过已撤销 source 的认证。
3. 注册 `ci-run --built-from--> repository`。首批每个 observed CI Run 恰好一条，使用 `assertion=asserted / origin=plugin`，创建者为受控 source；`origin_ref` 为 delivery ID，`source_event_id` 指向本次 `ci-run.recorded`。metadata 仅含 `schema_version:1`、`profile`、`binding_id`、`algorithm`、`oid`。这是插件对受控观察的声明，不是从 Component 推导的关系。
4. EntityLink 是来源关系及 commit 值的唯一权威记录，不另建 commit 表、CI Run / Repository join 表或在 CI Run 重复存 OID。增加本方向的唯一约束、同 Workspace 与 provenance 校验、metadata 白名单、状态与关系一致性约束；observed 必须恰好一条，其他状态必须零条。首批关系不可解除 / 覆盖，数据库与服务均守住这一边界，不开放通用关系写入。
5. v1 `ci-run.recorded` schema 1 保持原样；v2 使用 schema 2，除原 status / Component 外仅增加 checkout state / reason、绑定的 Repository 引用及 nullable link ID（仅 observed 非空），不复制 OID、URL 或配置。unavailable 的 Repository 引用仅服务绑定追溯与权限过滤，不表示存在 built-from。绑定行、CI Run、receipt、关系、事件、Outbox 与 Activity 在唯一事务内提交；事件 / link 互相校验需要同事务延迟约束或等效提交前检查，不能留下半条事实。
6. 仅针对该事件分发 schema 1 / 2，未知版本仍失败；不要放宽所有事件解析。Activity projection 从现有 5 升至 6，正常更新与全量重建使用同一投影器。来源事件仍以 CI Run 为主要对象，不自动写入 Thread、Ticket 或 Repository Timeline。safe facts 只保存状态 / 原因，Repository 为需重新授权的 subject；展示 commit 从权威关系解析。

成功追溯依靠原子 receipt、插件 actor、来源关系与领域事件，不将人工 configuration Audit 冒用为机器操作记录。本批不新增失败安全审计产品；失败日志继续使用受控机器码，禁止原始 payload、clone 地址、credential ID、绝对路径、Secret 或 Git 日志。

历史配置或 Component 人工关系改变，不改写已记录 checkout；Repository 当前可读性仍每次重新检查。若发现已接受的来源本身错误，当前可执行的处置是停用来源、保留原始记录并记录运维问题，不能在产品中将它伪装成已纠正。正式撤回 / 更正机制不在 A 内，不能在真实使用说明中隐去这个限制。

## 读取、权限与 Web

扩展既有 CI Run Nexus View 的严格 DTO，而不是直接返回内部结构。Current 增加 `checkout` 安全摘要，Relations 仅对新方向开放；通用 parser、服务端 whitelist 与 Web runtime parser 成套更新。

- observed 且 Repository 当前可读时，显示仓库当前名称、canonical Repository 跳转、完整 commit（可复制）、算法及“管理员绑定仓库；controller 观察 checkout”的说明。来源事实不可变，名称可按当前元数据解析。commit 不自动拼外部 URL，不抓取预览。
- not_collected 显示“此构建未采集代码来源”；unavailable 显示“构建结果已记录，checkout 来源不可用”及原因的固定文案。不得把未采集写成不存在代码来源，也不显示空白成功标记。
- 先检查 CI Run 所属 Component；源对象不可读统一 404。Repository 不可读时，checkout 摘要统一为 `visibility:restricted`，不返回 state、reason、ref、类型、link ID、OID、算法、时间或 binding；Relations 采用同一无内容占位，不因两个区域重复透露额外事实。全局 Activity 等无安全来源上下文的消费者完全隐藏相关不可读投影。
- Timeline schema 2 的 Repository subject 复用当前目标授权，受限时不泄漏来源状态 / 原因；source ID、external run key、delivery、binding ID、profile 内部配置、digest 和 receipt 永远不进入普通 DTO。
- Repository 配置页新增来源构建列表：建议 `GET /api/v1/workspaces/{workspace_id}/repositories/{repository_id}/ci-runs`，只查本提案的 `built-from`。先检查 Repository，再逐 CI Run 检查 Component 权限；先授权过滤再分页，不返回总数。默认 25 / 最大 50，CI Run 稳定 ID 升序，游标绑定 Workspace / Repository / kind；每页重新授权，不承诺跨页快照。
- 反向列表只返回安全 CI Run 引用、终态、完成时间和该关系的 algorithm / oid，可跳正式 CI Run 页；不提供来源内部身份。首批 CI Run 最多一条来源，不扩大核心关系全量读取范围。

Web 覆盖 loading / empty / error、旧 v1、来源不可用、受限占位、分页、桌面 / 手机 / 键盘、后退与显式刷新。Workspace / 对象切换、失权或 404 后清空数据并丢弃迟到响应；不从浏览器缓存复活来源。

既有 staging 记录仍只按成功 CI Run、当前 Environment 授权和外部终态确认判断，不新增“必须有 checkout”门槛；没有来源的历史 CI Run 也不能被误写成“无法发生部署”。Deployment 通过原 CI Run 跳转读取来源，不复制 commit，也不从同 Component 推导 Ticket 已交付。

## 失败场景与处置

| 场景 | 预期结果 / 恢复 |
| --- | --- |
| 坏签名、过期、来源停用 | 原 401，核心事务零写入；修复授权后只能同身份重试 |
| 未终态或不支持的 Jenkins result | 保持原 422；不能用 unavailable 放行构建合同错误 |
| 短 OID、全零、未知算法、未知 / 重复键、超限 | 400，不落库；不补齐、截断或改用分支 HEAD |
| body binding 不匹配配置 | 403；不接受 body 自选仓库 |
| Repository 不存在 / 跨 Workspace / 外部身份不符 | 不存在或跨 Workspace 404，已配置身份冲突 409；无关系、receipt 或成功事件残留 |
| 首次 checkout 前失败或取消、历史缺观察 | 有效三态可记录 unavailable / checkout_not_observed；零 built-from |
| 多仓库、多 checkout、submodule、Pipeline、预合并 | unavailable / unsupported_checkout；不取首项，不把 marked 冒充实际 revision |
| 构建期间配置漂移 | unavailable / binding_changed_during_run；后续采集停用并核对配置，不能用当前配置重写历史 |
| controller 观察损坏、sync 失败、磁盘满 | 暂停来源并保留缺口；修复存储后对账，不发布伪造 unavailable 快照掩盖损坏 |
| 同 source / build 的 commit、来源状态或终态变化 | 409，worker blocked，保留冲突；不改 ID、不覆盖、不自动无限重试 |
| 相同 commit 的新 build number | 新 CI Run 与新关系；旧记录不合并，重跑结果可以不同 |
| 数据库 / 投影写入失败 | 事务整体回滚，暂时故障按原有限重试；不得出现 observed 无 link 或独立成功 Activity |
| 已提交但响应丢失、worker 未写 ack 就退出 | 原 payload / delivery 重放得到原 CI Run，关系和事件各一份 |
| v1 积压、N 分界错误、旧程序读取新格式 | 不自动升级输入或降级发送；显式阻断并核对版本 / 分界，一致恢复后继续 |
| source 绑定跨重启被换仓库 / profile / N | 已持久绑定比较失败；不得因重启重置，停用配置不删除历史 |
| 撤销成员、失去 Repository / Component 读取权 | 源对象 404 或安全占位；反向列表无泄漏、迟到响应不恢复已清数据 |
| 恢复到新实例后重放旧 spool | 默认离线；先核对实例、source、绑定、receipt 与恢复点，再显式启用，不自动授予机器写权限 |

## 迁移、恢复与影响

新增权威数据为 binding、CI Run 来源状态及 EntityLink metadata；binding 表纳入备份分类，已有实体、关系、事件与 receipt 保留原 ID。Activity 按版本 6 重建并与正常写入比较。测试包含空库安装、014 → 015、已有 v1 数据、原始 v1 digest 固定向量、新旧 run 混合、精确 readiness 与全新目标恢复。

升级按 Go / Web / schema 匹配版本交付，不承诺滚动兼容。receiver 先具备 v2 能力，来源启用仍需受控配置；旧 sender / receiver 不得静默接受新 body。回退优先 forward repair；确需旧工件则恢复升级前备份到全新目标，明确备份后的数据损失，不删除新关系伪装兼容。

Nexus 数据库备份与 collector / worker 离线备份仍不是一个事务；分别记录恢复点和缺口，Secrets 均排除。绑定记录可以恢复，但不能据此重建凭据或自动开启 source。旧不可变 receipt 与新来源关系在恢复后继续抵御重复和冲突；整库恢复不等于 `.nexus` 导入验收。

接受后需同步领域模型、核心契约、必要决策基线、server / Web / worker / collector 说明及验证记录。提议阶段只增加了本 ADR、ADR 索引与当前状态；接受后的 A 实施同步对应真相源。

## 首个实现切片与退出判据

### A：正式来源接收、保存与发现（建议首次确认范围）

范围是 v2 sources / payload 校验与现有一次性 sender 兼容、binding / CI Run / EntityLink 原子事务、schema 2 事件与 Activity 6、CI Run 来源展示、Repository 反向列表、升级与恢复。沿用现有 Go / Web 依赖，不安装 Jenkins / Git 插件，不启动真实构建，不扩展持久 collector / worker 文件格式；这些进入 B。

验收须证明：

1. Go / HTTP 严格解析、认证前后顺序、v1 digest 不变、N 分界、绑定与 canonical digest、三态和 unavailable、永久 / 暂时错误分类；sender 对原始 v2 字节签名，未知结果保持原身份。
2. 真实 PostgreSQL 验证首次 / 并发重复 / 内容冲突、绑定争用与重启变更拒绝、跨 Workspace、事务各写入点回滚、来源状态与关系约束；正常 Activity 与重建一致。
3. 014 升级、旧 v1 与新 v2 混合备份、全新实例恢复、恢复后的精确重试与冲突保持原结论。
4. 经授权的隔离 HTTPS 浏览器中，通过正式配置入口建立 Repository / Component，再由测试专用 HMAC source 输入明确标注的合成 checkout delivery；验证 CI Run → Repository → CI Run、完整 OID、分页、来源不可用、后退 / 刷新撤权与桌面 / 手机 / 键盘。
5. 定向 Go / Web 与仓库检查通过，并回归旧 CI Run、Repository 人工关系、Ticket / Component 与 staging 记录。没有生成 Ticket 交付关系或自动 Deployment。

A 的完成结论仅是“来源接收与发现合同已通过自动化、真实数据库和隔离浏览器验证”；合成输入不是 Jenkins SCM 证据。真实环境启用 observed 来源必须等 B 的支持 profile 验收完成，不能把 A 上线描述成真实来源链已交付。

### B：真实 checkout 采集与持久交接（后续独立范围）

固定依赖和受控 job、实现运行绑定与 checkout 观察、扩展 collector / worker 的版本化交接及显式清理，验证 clone 目标核对、真实 revision、失败 / 取消前后、普通 merge commit、预合并拒绝、多 checkout 拒绝、配置漂移、观察丢失、重启 / 漏采与旧积压兼容。

B 继续要求原生 Linux 支持文件系统上的真实 controller / agent / worker 隔离与持久目录证据；本机 FUSE 或合成 Run 不能代替。启动前另行明确实例、插件版本、端口、目录、数据保留、停止和清理范围。当前暂缓真实联调的决定不因本提案改变。

来源合同与 B 证据明确后，再独立冻结 Ticket ↔ 具体 CI Run 的人工 / 受控证据关系、权限、解除与历史语义；最后补原 Thread / Ticket 发现交付、真实外部 staging 和整链恢复。不上来合并为一个大切片。

## 替代方案与取舍

- 从 Component 共属、job 名、URL 或 commit message 推断：成本低但无法证明来源与交付，拒绝。
- 让仓库脚本上报 SHA，再对 JSON 签名：只能得到仓库声明；无法满足本 profile，拒绝把它标为 controller 观察。
- 直接取 `BuildData.getLastBuiltRevision()`：可能缺失、存在多个 SCM 或带历史构建数据，不能独自证明本次 checkout；采用本次 build 编号与持久观察共同核对。
- 第一批引入 provider API、Commit 实体、提交图和产物 attestation：证明能力更强，但增加依赖、权限和运维面；本次选择窄 checkout 事实，明确其局限。
- 所有缺少来源的构建一律拒绝：会丢失 checkout 前失败 / 取消的有效构建终态；采用显式 unavailable，但不吞结构、绑定、冲突与存储错误。
- 一次交付任意 Pipeline / 多仓库：无法给出可验证的封闭范围；首批限定 profile，未来扩展需保留多仓库身份和每次 checkout 语义，不能靠可选数组提前承诺。

代价是新增 migration、事件版本、来源配置版本和一个受限采集 profile；收益是用户能看到可解释、可恢复的具体来源，同时不会把人工映射、签名、构建成功或 Component 关联误读为完整交付证明。

## 所有者确认范围

2026-10-13 所有者确认以下范围并授权实施 A：

1. 接受“管理员绑定 Repository + controller 观察 checkout”的证据强度与 UI 表达，首批单仓库 / SHA-1 / 单 checkout，Commit 不独立实体化。
2. 接受缺少证据时保留终态并显式 unavailable，来源首次写入不可补写 / 覆盖；旧 v1 保持 not_collected。
3. 接受 A 涉及的 sources / payload v2、不可变 binding、migration、EntityLink、事件 / Activity 和只读 API / Web 范围。
4. B 的真实采集、依赖和运行资源另行准备；Ticket 具体交付关系与真实 staging 不包含在 A 中。

本次授权包括工作区提案提交、A 实施、必要验证和文档同步；不包括 push、业务实例迁移或 B 的真实 Jenkins 采集。

## 外部依据与核验局限

2026-10-13 阅读 Jenkins 官方文档：

- [Git plugin Build](https://javadoc.jenkins.io/plugin/git/hudson/plugins/git/util/Build.html)：区分 `marked` 与 checkout 后的 `revision`，并保存构建编号。由此采用实际 revision，并将本地预合并排除在首批 profile 之外。
- [Git plugin BuildData](https://javadoc.jenkins.io/plugin/git/hudson/plugins/git/util/BuildData.html)：保存构建的 Git 数据和历史分支记录，Run 可有多个 BuildData，last revision 可为空。由此要求唯一匹配与本次 build 核对。
- [SCMListener](https://javadoc.jenkins.io/hudson/model/listeners/SCMListener.html)：提供 checkout 后的观察点；文档不承诺与独立文件交接原子完成，提案另设观察缺失与恢复边界。
- [RunListener](https://javadoc.jenkins.io/hudson/model/listeners/RunListener.html)：终态采集继续沿既有 finalized 与对账边界。

上述在线文档只支持候选采集方案的设计依据。本轮没有固定插件依赖、编译或运行新采集代码，具体版本的回调顺序、元数据持久化和故障行为必须在 B 验证；不将文档推断记作实验通过。
