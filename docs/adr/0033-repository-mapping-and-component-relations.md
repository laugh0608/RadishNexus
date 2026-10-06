# ADR-0033：Repository 最小映射与 Component 关联

状态：已接受

日期：2026-10-06

关联：[ADR-0002](0002-stable-entity-reference-and-event-projection.md)、[ADR-0022](0022-transactional-activity-and-incoming-relations.md)、[ADR-0030](0030-authenticated-jenkins-delivery-adapter.md)、[ADR-0032](0032-component-environment-configuration-and-authorization.md)。本提案扩展 Repository 实体与配置能力，不替代 Jenkins 来源绑定或环境授权合同。

## 背景与目标

审阅基线为 `5abc6a9`，工作区初始干净。Component / staging Environment 已有正式创建、发现和显式授权入口；Repository 尚未进入正式 Go EntityRef 注册表、数据库实体表或 Web 配置。Jenkins source 仍由部署者配置，固定绑定 Workspace、Component 与 job。

[领域模型](../domain-model.md#repository)已定义 Repository 为外部 Git 代码库映射，[Golden Path](../golden-path.md)要求它参与最小交付场景。本切片让管理者通过正式入口建立映射、明确关联 Component，并让普通成员双向发现；不把映射存在当作外部连接成功、代码可读或构建来源已经验证。

项目所有者已于 2026-10-06 确认本提案并授权实施；按文末顺序同步真相源与实现。实现与验收完成线以当前状态及独立证据为准，接受方案不表示已完成验收。

## 建议决定与范围

| 边界 | 建议合同 | 原因与影响 |
| --- | --- | --- |
| 归属 | Repository 独立属于 Workspace，不挂 Project 或单个 Component 外键 | 同一代码库可支持多个长期软件资产，关联继续由 EntityLink 承担 |
| 外部身份 | `(workspace_id, provider, provider_origin, external_id)` 唯一 | 不把名称、URL、默认分支或 Jenkins source 当作稳定仓库身份 |
| 关联基数 | Component 与 Repository 多对多；同一对只允许一条 active 关系 | 兼容 monorepo 与一个 Component 使用多个仓库，不引入“主要仓库”排名 |
| 当前权限 | active 账户且为当前 Workspace active owner 才能创建映射、关联和解除；active 成员可读取全部映射元数据 | 复用现有共享 Component 配置边界；外部私有仓库权限不自动同步 |
| 关系来源 | 只开放人工 asserted / user 关系，记录操作者、时间和移除证据 | 防止把人工配置误认为 Jenkins 或 provider 证实的构建事实 |
| 最小修正 | 允许解除错误关联；重新关联生成新 EntityLink | 日常误操作有正式修正入口，原始来源与旧重试不可改写历史 |
| 交付范围 | Go、PostgreSQL、Session API、React 创建 / 发现 / 双向关联与恢复验证 | 形成可独立操作的最小切片，不止建立表或内部 service |

本批不开放 Repository 元数据编辑、归档、删除、批量导入、外部仓库同步、连接检查或 Git 托管；不新增 Secret、provider API 客户端或依赖。不新增 Ticket / CI Run / commit / Deployment 关系，不修改 Jenkins payload、source 配置或持续采集。本批仍不执行构建和部署，也不授予环境记录权。

## Repository 身份与字段

注册 `repository / rep_`，复用现有不透明 ID 生成与 EntityRef 校验。数据库新增 `repositories`，包含 `id`、`workspace_id`、`name`、`provider`、`provider_origin`、`external_id`、`web_url`、`default_branch`、`created_by_kind / created_by_id`、`created_at / updated_at`；不新增重复的本地 key、Component ID 数组或凭据字段。创建者固定为当前 user。

| 输入 | 本批校验与语义 |
| --- | --- |
| `name` | 复用配置名称规则：trim 后 1..120 Unicode 字符、最多 480 bytes，拒绝控制字符；名称不唯一 |
| `provider` | 显式选择 `github / gitlab / gitea`；只表示身份命名空间，不表示已安装连接器或已验证服务类型 |
| `provider_origin` | HTTPS origin，含 host 和可选端口，不含用户信息、路径、query 或 fragment；允许根 `/` 输入，规范化后不保留；最多 512 bytes |
| `external_id` | 由管理者从外部服务取得的稳定不透明 ID，1..255 UTF-8 bytes，无空白或控制字符；大小写敏感，不 trim、不数字化、不以路径推导 |
| `web_url` | 外部仓库浏览地址，最多 2048 bytes；绝对 HTTPS，同规范化 `provider_origin`，须有非根路径；不允许用户信息、query、fragment、反斜杠、空白和控制字符 |
| `default_branch` | 管理者明确填写的默认分支提示，1..255 ASCII bytes；采用本批受限语法，见下文，不自动填 `main`，不冒充远端当前值 |

URL 使用标准库解析，规范化 scheme / ASCII hostname 小写和默认端口 443，保留非默认合法端口，拒绝空 host、尾点 host、非 ASCII host、非法端口及模糊转义。国际化域名本批只接受明确的 ASCII 编码输入；不安装 IDN 依赖。浏览地址拒绝路径中的点段、重复分隔符、编码分隔符、编码控制字符或多层编码歧义；规范化规则由同一函数用于入库、digest 和去重。路径大小写、`.git` 后缀与外部 ID 均不推断或静默改写；仅校验语法，不声称证明 URL 指向对应仓库。

`provider_origin` 标识一套外部 ID 命名空间。本批不支持同一 origin 下多套通过不同子路径区分的独立 provider 实例；单实例仓库浏览地址可以包含服务前缀路径。该限制应在表单说明中可见，不通过增加 path 到 external_id 绕过身份规则。

分支提示采用应用自身的保守子集：`[A-Za-z0-9][A-Za-z0-9._/-]{0,254}`，拒绝空路径段、连续 `..`、以 `.` 开始或结束的路径段、以 `.lock` 结束的路径段和末尾 `/`。它不是完整 Git ref 校验器，也不验证远端分支存在；不执行 `git` 或访问外部网络。

身份四元组是唯一去重依据；不同 operation 创建相同身份返回冲突，即使名称或 URL 不同。不同身份但相同 `web_url` 不自动合并，URL 不替代外部稳定 ID。普通成员可看到 URL 与外部 ID，因此 owner 创建前应看到“此映射元数据对当前 Workspace 活跃成员可见”的说明，私有代码库的访问仍由外部服务决定。

本批元数据创建后不可修改，数据库保护身份和 provenance，不允许删除映射。仓库改名、迁移、外部 ID 误填或默认分支变化不应通过另造同身份映射修补；未来需独立的显式修正合同。误填映射可先解除关联并保留历史，本批不宣称覆盖完整映射生命周期。

## Component 关联与修正

新增唯一允许方向 `component --source-repository--> repository`，表示“管理者声明该资产使用此代码库”。不复用 `built-from`，后者需要具体构建与 commit 来源；也不使用泛化 `relates-to` 丢失领域含义。

- 只允许同 Workspace、存在的两端。Component 生命周期为 `active` 时可新建关联；planned / deprecated / retired 仍可读取并解除已有关系。Repository 本批无生命周期字段。
- 关系写入既有 `entity_links`：`assertion=asserted`、`origin=user`、`origin_ref=null`、当前用户与时间、`metadata={}`。直接命令与自己的关系事件同事务创建，`source_event_id=null`；事件另保存准确的关系 ID 和两端引用，不伪造先前来源事件。
- 新增限定于此类型方向的 active 部分唯一索引。它仅约束本批人工配置关系，不扩大为其他关系的全局去重规则；未来插件来源需独立审查，不得静默挤占人工关系。
- 新建关联请求显式确认两端；同一 operation 精确重试只返回原处理结果。不同 operation 遇到既有 active 关系返回 `409 conflict`，提示刷新，不重写来源。
- 解除关系使用精确 `link_id`，服务端核对 Workspace、Component、关系类型和 Repository；只允许 active → removed，填写当前移除者、时间和固定原因 `owner-unlinked`，保留原创建 provenance。无自由文本备注输入，避免把私密说明塞进关系 metadata。
- 不同 operation 再次解除已 removed 关系返回 `409 conflict`；原 operation 精确重试返回原成功结果。重新关联须由用户重新确认并用新 operation 创建新 `lnk_` ID；旧解除请求只能指向旧关系，不能伤及新的 active 关系。
- 旧关联 receipt 在解除后重试不复活关系；旧解除 receipt 在重新关联后重试不移除新关系。写响应只表示“该请求已处理”，客户端随后刷新当前权威关系。

解除只影响当前关联展示，不删除 Repository、Component、历史 Activity、CI Run 或 Deployment，不改动外部 Git 服务。Repository 列表在无关联时仍可发现映射。

## 权限与读取

在正式 Session 之外，service / store 事务内继续检查当前账户与 Workspace membership。Project admin、责任 Team、外部 provider 身份和 Jenkins source 不获得配置权；关系也不授予任一端权限或部署权限。

Repository 使用与 Component 一致的当前 active Workspace 读取规则，接入统一实体存在性、权限和标题解析。无权限与不存在统一 not-found；关联列表先检查来源，再检查每个目标，反向不可读目标完全隐藏，不泄漏数量或标题。后续若收紧 Repository 权限，所有消费者须继续通过该入口，不能因今日共享可见而绕过目标检查。

Repository 与 Component 配置页提供双向列表；核心关系读取白名单增加 Repository 对此单一关系的 incoming 方向。继续复用安全投影，不全面开放所有类型的反向关系。本批不新增完整 Component / Repository Nexus View 与 Timeline 页面，后台 Activity 正常写入与重建证据单独验收。

## 公共 API 与 Web

所有路径以下表后缀拼接 `/api/v1/workspaces/{workspace_id}`。沿用现有可信 Host / proxy、Session、写入 Origin / CSRF、严格 JSON、32 KiB 请求上限、禁止写 query、错误 envelope、`private, no-store` 与 `Vary: Cookie`。客户端不能指定角色、actor、provenance、生成 ID 或时间。

| 方法与后缀 | 请求 / 用途 | 权限 |
| --- | --- | --- |
| `GET /repositories` | 分页发现映射 | active 成员 |
| `POST /repositories` | `{client_operation_id,name,provider,provider_origin,external_id,web_url,default_branch}` | active owner |
| `GET /repositories/{repository_id}/configuration` | 元数据与当前能力 | active 成员 |
| `GET /components/{component_id}/repositories` | 当前关联 Repository，含精确 `link_id` | active 成员，两端复核 |
| `GET /repositories/{repository_id}/components` | 当前关联 Component，含精确 `link_id` | active 成员，两端复核 |
| `POST /components/{component_id}/repositories` | `{client_operation_id,repository_id,confirmed:true}`，创建人工关系 | active owner，active Component |
| `DELETE /components/{component_id}/repository-links/{link_id}` | `{client_operation_id,confirmed:true}`，解除这一条关系 | active owner，Component 可非 active |

- Repository 安全 DTO 为 `ref,name,provider,provider_origin,external_id,web_url,default_branch`；配置详情沿用 `capabilities`，本批无元数据修改动作。创建成功返回安全 DTO，首次 `201`、精确重试 `200`。Component 配置详情增加 `can_link_repository`，仅当前 owner 且 Component active 时为 true；关联列表每项另有 `can_unlink`，仅当前 owner 为 true。这些能力只服务展示，提交时重新检查。
- 关联列表返回 `{items:[{link_id,target}],next_cursor}`，target 使用对应对象的安全 DTO。关系 ID 为精确解除前置条件，不带 Audit / receipt / Secret。关联写入首次 `201`、精确重试 `200`，解除成功 `200`，`data:{link_id,applied:true}`；不能将 applied 解释为当前 active 状态。
- 列表复用 `limit / after`、默认 25 / 最大 50、稳定 ID 升序和 canonical base64url 版本化游标；Repository 列表按 Repository ID，关联列表按目标 ID。游标绑定 Workspace、kind 和来源对象；每页重查权限，不返回总数，不承诺跨页快照。空列表仍返回空 items，不自动创建或关联任何对象。
- 未登录 `401`；不可发现 Workspace / 两端 / 关系为 `404`；已可读但缺 owner 配置权为 `403`；重复身份、已有 active 关系、已移除关系、非 active Component 新关联或 digest 冲突为 `409`；非法输入、未知字段、未确认、非法游标为 `400`。冲突判断在当前授权之后，不返回 SQL 或敏感输入。
- 外链只在用户点击时打开，使用明确外部站点标识和 `noopener noreferrer` / no-referrer；不嵌入 iframe、不抓取预览、不自动验证、不发送 Session 或服务端凭据。URL 校验不等于信任外部站点。

Web 复用现有工作台与配置区：owner 创建 Repository；从 Component 页用分页 Repository 选择器明确关联；两端可互相跳转，owner 在关联列表确认解除。选择器显示 name、provider 和外部身份供消歧，不要求输入内部 ID。创建映射与关联是两个明确动作，失败后保留可重试的独立状态；不新增默认 Component 或 Jenkins source。

覆盖 loading / empty / error、分页、表单字段错误、重复映射冲突、双击、网络结果不明、桌面 / 手机和键盘。未知写入结果保留同一 operation 与原 payload，只允许显式重试；不得更换 operation 乐观宣告成功。Workspace / 对象切换和失权时清理相关表单与数据，丢弃迟到响应。

## 事务、receipt、Audit 与 Activity

### 复用配置命令

沿用 configuration service / store 的封闭分发，新增 `repository.create`、`component.repository.link`、`component.repository.unlink`；不新增通用实体或关系写 API。

创建 scope 为 Workspace、subject 为空、result 为 Repository ID；关联 scope 为 Component、subject 为 Repository ID、result 为新 EntityLink ID；解除 scope 为 Component、subject 与 result 均为被解除的 EntityLink ID。receipt 唯一范围沿用 Workspace / actor / command / scope / subject / operation。

为新命令增加独立可省略输入分支，不向旧 digest 编码注入零值字段；以已有七类基础命令与 ADR-0032 四类命令的固定输入验证历史摘要兼容。新摘要包含全部规范化业务字段与 confirmed，操作身份和路径作用域遵循既有规则。receipt 命中前重新检查操作者、两端可读性和对应命令权限；关联重试还检查 Component 仍 active，解除重试允许非 active Component。

成功 Audit 沿用既有表：创建 `before_state='' / after_state=''`，关联 `'' / active`，解除 `active / removed`，均 `changed=true`。关联 / 解除 result 指向确切关系，scope 与 subject 的类型组合进入数据库白名单；验证 result 的 Workspace、两端与方向一致。旧授权关联列保持 null，成员清理数组为空，不拿数组存关系明细。不新增无变化成功记录，失败不写成功 Audit，精确重试不追加记录。

### 并发与原子性

- 固定锁序为操作者 account → Workspace membership → Workspace → Component → Repository → EntityLink。新命令均在 Workspace 配置锁下串行化 receipt、映射身份及关系创建 / 移除，优先采用已有低频配置锁，不新增 advisory lock 服务。
- 创建映射只需前三级；关联继续锁定 Component 与 Repository，解除按请求 Component 定位关系目标，再按同一顺序锁定并复核关系，不先锁 EntityLink 后反向等待 Component。所有锁定查询都受同 Workspace 条件约束。
- 数据库唯一约束与同 Workspace 端点触发器是最终防线。新关系专用约束限制 assertion、origin、actor 和空 metadata；现有不可变 provenance、禁止删除及 removed 不可复活规则继续生效。
- 映射 / 关系状态、成功 Audit、receipt、事件、Outbox 与正常 Activity 同事务提交。故障注入须证明整体回滚；HTTP 响应丢失后通过原 operation 恢复，不能重复创建事实。
- 与现有配置和身份路径核对锁序，验证双 owner 竞争、账户禁用 / membership 撤销与写入的两种合法串行结果。撤权提交后的新请求与重试不得借 receipt 返回成功。

### 事件与投影

新增 schema_version=1 事件：`repository.created` 的 primary entity 为 Repository，payload 为 `{}`；`component.repository-linked` 与 `component.repository-unlinked` 的 primary entity 为 Component，payload 只含 `repository` EntityRef、`link_id` 和 `state`。actor 为当前 user，source 为 web，关联服务器 request correlation；外部 ID、URL、分支、名称与管理明细不进入事件副本。

正常投影与全量重建使用同一白名单，Activity projection version 建议从当前 3 升至 4。创建投影 safe facts 为空；关联事件只投影状态与受控实体引用。Repository 引用进入权限 subjects，读取时同时复核 Component 与 Repository；移除后保留历史事件，但不能将历史状态解释为当前关系。只投影到主要对象，不扩展为跨对象 Timeline 聚合，不补造既有对象历史。

## 迁移、恢复与兼容影响

建议新增 `013_repository_mapping.sql`，实施前重新核对编号。保留 001～012 原文及历史 checksum，不从 Jenkins sources 文件、现有 CI Run 或 URL 猜测并回填 Repository。

1. 新增 Repository 类型、表、唯一身份和同 Workspace 唯一键；保护本批不可变字段与删除边界，扩展 `entity_workspace`，完整保留既有所有分支。
2. 注册 `source-repository` 方向，增加本批 active 部分唯一索引、专用来源约束和服务双向读取所需的窄索引；保留旧关系规则。关联继续以 EntityLink 为唯一权威记录，不另造 Component / Repository join 表。
3. 扩展配置 Audit / receipt CHECK 和必要的引用校验，不放宽旧命令约束，不改写历史 digest 或成功证据。Repository、关系与对应事件必须在恢复时保持相同 ID 与 provenance。
4. 同步正式 Go EntityRef、读取 / 标题解析、HTTP / Web 类型、事件投影、migration registry、schema readiness、数据库契约和备份分类。实验原型只保留其既有历史合同，不为追平正式实现无目的复制一套代码。
5. Repository 表属于权威备份；已有 EntityLink、事件、Audit 和 receipt 继续备份，Activity 按既有重建流程验证。全新目标恢复后复核 active / removed 历史及重试行为，不把整库恢复称作 `.nexus` 导出验收。

新 Go / Web / schema 按匹配版本交付。旧二进制遇到 migration 013 应拒绝 readiness；不承诺滚动兼容窗口。先验证从 012 升级、全新安装和恢复；修复优先采用 forward repair。确需整版本回退时，使用升级前备份恢复至全新目标并配套旧工件，明确备份后数据损失；不删除新表或关系来伪造旧版本兼容。

提案接受只确认上述代码、文档和迁移文件范围。业务实例迁移、依赖、长期服务、真实外部系统写入、提交或远程操作沿根协作约定单独确认。需要隔离数据库或 HTTPS 浏览器验收时，在启动前列明实例、端口、数据目录及停止 / 清理方式，已有当前任务明确授权的动作不重复申请。

## 验收与实施顺序

| 层级 | 退出证据 |
| --- | --- |
| Service / HTTP | 外部身份与 URL / 分支校验、规范化去重、旧 digest 兼容；Session / Origin / CSRF、严格 JSON、游标隔离；owner / 普通成员 / Project admin / plugin / 跨 Workspace / 暂停 / 禁用矩阵 |
| 真实 PostgreSQL | 同身份并发创建仅一条映射；同对并发关联仅一条 active；解除再关联保留不同 link ID 和原来源；旧关联不复活、旧解除不伤新关系；跨 Workspace 和非法方向拒绝 |
| 原子性与恢复 | Audit / receipt / 事件 / Outbox / Activity 故障整体回滚；未知响应精确重试；012 升级、精确 readiness、备份恢复、正常投影与重建一致 |
| Web | 从正式 bootstrap 和配置入口创建 Component 与 Repository、明确关联、两端刷新可发现、解除与重新关联；普通成员只读、撤权清空及旧页面拒绝；桌面 / 手机 / 键盘与分页 |
| 既有交付回归 | Component / Environment 配置、旧授权与 receipt 不变；Jenkins 原来源与 CI Run / staging 记录合同继续通过定向测试；没有自动生成构建或 Deployment |

验收不依赖手写 SQL 生成被验收的 Repository 或关系；测试故障注入可直接操作隔离数据库，但须单列为测试条件。浏览器不得把外部链接打开或虚构数据当成 provider 连接成功。真实 Jenkins 全链、持续采集、原生 IME 与真实团队使用仍是独立未覆盖项。

实施顺序：

1. 确认外部身份、共享读取、多对多关系、解除与迁移范围；将 ADR 标为已接受，同步领域模型、核心契约和必要决策基线。
2. 完成 migration、实体注册、配置事务、receipt / Audit / Activity 和恢复验证，先证明关键失败与竞争边界。
3. 接通正式 Session API、双向发现与 React 配置交互，执行定向 Go / Web 检查及经授权的隔离验收。
4. 运行仓库检查，更新当前状态与实施证据，明确已执行、未执行和仍需授权内容；不以本切片完成宣称 M0.5 / M1 退出。

## 替代方案与后果

- 用 Jenkins source 作为 Repository：混淆 job 接入凭据与代码身份，无法表达多个 job 使用同一仓库，不采用。
- Repository 保存一个 Component 外键，或 Component 保存一个 repository_id：实现较少，但限制 monorepo / 多仓库关系且丢失 EntityLink 来源，不采用。
- 以 URL 或仓库路径唯一去重：方便录入，但不能表达独立实例的 ID 命名空间和未来改名，不采用。选择稳定外部 ID 会增加首批人工配置成本，表单应明确要求，不用假 ID 降低门槛。
- 首批直接接入 provider API / Secret 与自动同步：可减少录入，但引入新权限、依赖、网络故障和来源验证合同，留到真实需求驱动的独立切片。
- 只建立关联而不允许解除：范围更小，但无法修正误关联；本提案增加精确关系解除和新记录重建，保留历史且不改变两端对象。

采用方案新增一个正式实体、三个窄命令与一条有来源的关系类型。代价是元数据暂不可修正、provider 身份由管理者声明、共享读取需要提前明确；收益是移除仓库映射与关联对数据库预置的依赖，并为后续受验证的 CI Run / commit 关系保留独立边界。
