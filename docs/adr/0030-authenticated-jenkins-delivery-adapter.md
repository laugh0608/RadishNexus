# ADR-0030：受控 Jenkins 终态 delivery adapter

状态：已接受（项目所有者已确认按此范围实施）

日期：2026-09-26

Supersedes（部分）：[ADR-0006](0006-verified-jenkins-delivery-and-ci-run.md) 中不开放 Jenkins HTTP route 的停止线；保留其核心事务、幂等和 CI Run / Deployment 分离语义。

## 背景

CI Run 正式读取已提交为 `a394ed6`，外部来源仍不能进入已有 `RecordCompletedJenkinsRun`。下一切片补齐接收端与配套发送工具，通过实际 HTTP 验证来源、终态映射和失败恢复；Jenkins 实例安装与真实 job 联调随后单独授权。

这是 RadishNexus 自定义的版本化 delivery 协议，不是假设 Jenkins 自带统一 HMAC webhook。受控发送方持有的凭据只允许记录一个已配置来源的终态 CI Run，不能创建 Deployment 或改变用户权限。

## 决定

- 新增默认关闭的 `POST /api/v1/integrations/jenkins/{source_id}/deliveries`，使用来源 HMAC 身份，不接受 Session 作为替代授权。
- 在现有 Go 模块内建立来源配置、校验与映射，并复用唯一 CI Run service / PostgreSQL 事务；不新增依赖、数据库表、事件类型或通用插件运行时。
- 配套一次性 Go 发送命令：读取受控终态 JSON 文件、签名、有限重试、报告机器码。工具不执行构建、不读取 Jenkins 网络 API、不运行常驻轮询；真实 Jenkins 到终态文件的采集将在实际实例联调中验证，不能把手写文件当作完成该链路。
- 失败记录使用脱敏结构化运行日志；成功事实和幂等凭据仍由既有数据库持久化。本切片不声称具备数据库级安全 Audit 或防篡改日志归档。

## 来源与 Secret 配置

部署者显式设置 `RADISHNEXUS_JENKINS_SOURCES_FILE`，指向绝对路径的 JSON 文件。未设置时不注册入口；配置存在但无效时启动失败，不降级为无认证。文件 schema 为 `{"version":1,"sources":[...]}`，最多 32 个来源、总长 64 KiB，不接受未知或重复键。

每个来源包含 `source_id`、`workspace_id`、`component_id`、`job_full_name`、`keys`。`keys` 最多两个不同 `key_id` 及对应绝对路径 `secret_file`，只用于轮换重叠期。source / key ID 使用 1–64 字符的 ASCII 字母、数字、下划线和连字符，首字符为字母；source ID 在实例配置中唯一。job 使用 Jenkins 完整名称，非空、无控制字符、无首尾空白，最多 255 UTF-8 字节。

来源代表固定 Jenkins 实例中的一个 job，并绑定一个 Workspace / Component。配置文件由可信部署者维护；没有客户端自助配置入口。改变实例、job 或 Workspace / Component 必须分配新的 source ID，旧 source ID 不重用。程序检查配置内部一致性和目标 Component 所属 Workspace；跨重启的配置历史由部署变更管理保证，本切片不建立持久化 Integration 注册表。

每个密钥文件只包含 32 个随机字节的 64 位小写十六进制编码，允许一个结尾换行；禁止内联密钥、命令行密钥和普通 API 返回。配置及密钥在启动时读取，轮换通过原子替换文件并重启：先加入新 key、切换发送方、移除旧 key。停用来源后重启即可拒绝该来源的新请求；历史 CI Run 和 receipt 保留。

Secret 文件由部署者限制读取权限，不进入仓库、普通备份、`.nexus` 导出、日志或截图。恢复实例默认没有外部写权限，需重新配置来源与密钥。HMAC key 持有者能够声明该绑定内的构建事实，签名不构成对 Jenkins 内容真实性的独立证明；不能把该 key 提供给不可信仓库代码或其可修改的 Pipeline。

## 请求与签名

入口复用现有 public Host、HTTPS 和可信代理检查，拒绝 query、非 canonical 路径和不支持的方法。无 CORS，不读取 Cookie，不使用用户 CSRF；所有响应 `Cache-Control: no-store`。仅接受 `application/json`（可附 UTF-8 charset），拒绝压缩 body，body 上限 16 KiB。

四个必需 Header 各只能出现一次：

| Header | 合同 |
| --- | --- |
| `X-Nexus-Key-ID` | 该来源配置中的 key ID |
| `X-Nexus-Delivery-ID` | 固定 `build-` 加十进制构建编号；同一个来源 / build 的所有重试保持一致 |
| `X-Nexus-Timestamp` | 当前发送时刻的 Unix 秒，规范十进制正整数 |
| `X-Nexus-Signature` | `v1=` 加 64 位小写十六进制 HMAC-SHA256 |

签名输入为以下 UTF-8 行以 LF 连接，最后一行之后没有 LF：

```text
radishnexus-jenkins-v1
POST
/api/v1/integrations/jenkins/{source_id}/deliveries
{key_id}
{delivery_id}
{timestamp}
{lowercase_sha256_of_exact_body_bytes}
```

使用常量时间比较签名。时间偏差允许前后各 300 秒，超过窗口拒绝；重试使用新的时间戳和签名，但保持 delivery ID 和业务 payload。窗口内的精确重放由数据库 receipt 返回原结果；窗口外即便已记录也必须重新签名。未知来源、未知 key、坏签名与过期使用统一 `401 unauthenticated`，不返回内部绑定信息。

先执行方法、传输和大小检查，再验证头与签名，最后解析业务 payload；未验证请求不能调用核心 service。单进程此入口最多 4 个在途处理，无等待队列，满载返回 `429` 和 `Retry-After: 1`；数据库命令 deadline 为 5 秒，复用既有 HTTP 读写超时。入口资源限制与普通协作业务独立，不新增无限 key / IP 缓存。

## Payload 与终态映射

下面仅为虚构协议示例，不能作为真实构建证据：

```json
{
  "version": 1,
  "job_full_name": "example/build",
  "build_number": 42,
  "building": false,
  "in_progress": false,
  "result": "SUCCESS",
  "started_at": "2026-09-26T02:00:00Z",
  "completed_at": "2026-09-26T02:01:00Z"
}
```

字段全部必需，`started_at` 可为 null；拒绝未知键、重复键、缺失字段、额外 JSON 值和无效 UTF-8。`build_number` 是 1 至 2147483647 的整数，不能使用小数、指数或字符串。job 必须精确匹配受控配置，两个运行中标志必须都是 false。时间使用 UTC RFC3339，最多毫秒精度，存在开始时间时须不晚于完成时间，完成时间不得晚于接收时刻加 300 秒。历史补送不以构建完成时间判断请求重放。

| Jenkins result | CI Run 状态 / 行为 |
| --- | --- |
| `SUCCESS` | `succeeded` |
| `FAILURE` | `failed` |
| `ABORTED` | `canceled` |
| `UNSTABLE`、`NOT_BUILT`、null、未知值 | `422 unsupported_result`，不写 CI Run；现有三态不能无损表达，不静默降级或升级 |

运行尚未结束返回 `422 run_not_completed`，其他业务形状错误返回 `400 invalid_request`，job 不匹配返回 `403 source_binding_mismatch`。来源绑定决定 Workspace / Component；body 不允许传入这些 ID、Nexus status、外部 URL 或任意 external run key。external run key 为规范十进制 build number，delivery ID 必须等于对应的 `build-{number}`。

签名校验 raw body 的摘要与 receipt 的规范化摘要职责不同。receipt 使用固定字段顺序的内部 JSON 编码后 SHA-256：协议版本、Workspace / Component / source / job、build number、Jenkins result、UTC 归一化开始 / 完成时间；不包含签名时间、key ID 或 JSON 空白。相同事实调整格式或轮换 key 后仍可重试，同一 delivery 改结果或时间必然冲突。该规范化函数同时供接收测试使用，发送方不自行提交 receipt digest。

只在全部检查通过后构造 `VerifiedJenkinsDelivery`。后续复用现有 receipt、CI Run、领域事件、Outbox 和 Activity 事务；Component 不属于绑定 Workspace 时不留任何写入。不将原始 payload、job 或 Jenkins result 扩散到用户 DTO / Activity。

## 响应、恢复与运行记录

首次成功返回 `201`，精确重复返回 `200`，只返回 `data: {ci_run_id, duplicate}`。receipt / external run 冲突返回 `409 conflict`；绑定目标不可用返回 `404 not_found`；数据库暂时不可用或超时返回 `503 temporarily_unavailable`。错误使用现有 envelope 和服务端 request ID，不包含底层错误、密钥路径或来源配置。

一次性发送工具最多尝试 4 次，单次 HTTP deadline 10 秒，总 deadline 60 秒。仅对网络中断、408、429、500、502、503、504 重试；等待按 1 / 2 / 4 秒退避，`Retry-After` 仅接受 1–10 秒十进制值且不能突破总 deadline。拒绝重定向，必须验证 HTTPS 证书，不提供 insecure 选项。其他 4xx 立即失败，耗尽重试返回非零退出码；不把通知失败改写为构建失败，不生成新的 delivery ID 绕过冲突。响应丢失或数据库提交结果不确定时，同一 delivery 重试恢复权威结果。

发送工具从配置文件获得目标 HTTPS origin、source / key ID 和 secret 文件引用，从输入文件读取上述终态快照。输入只能由可信采集步骤生成；对 Jenkins API 的读取权限和完成状态核验须在真实联调中落实。特别不能在当前构建尚未退出的 `post` 阶段直接将中间结果声明为完成。

接收端记录结构化事件，字段限定为服务端时间、request ID、结果机器码、HTTP 状态、耗时；认证通过后可附受控 source ID 和 delivery ID，成功后可附 CI Run ID。认证失败不记录攻击者提交的来源 / delivery 文本。禁止记录 body、Header、签名、digest、外部 URL、Secret、Cookie 或未脱敏数据库错误。

日志输出由既有进程运行日志承载，保存期与收集由部署者负责；故障日志不进入 Activity，不因审计日志失败撤销已提交事实。这是本阶段的运维诊断证据，无法替代后续安全审计与生产运维验收。

## 替代方案

- 先安装 Jenkins：无法解决 Nexus 的可信入口，且提前引入长驻服务和凭据管理。
- 直接使用用户 Session / 通用 Token：扩大机器写权限，也混淆成员撤权和来源授权。
- 同时添加 source / Secret / audit 数据库模型：可支持后台管理和持久配置约束，但超出首个固定来源验证需要；有真实管理需求时另行设计。
- 接收 Jenkins 原始任意 webhook 或自动读取其提交 URL：增加 provider 差异、字段泄漏与 SSRF 风险；首期使用窄协议和受控发送程序。

## 影响、回退与验证

公共协议新增一个机器写入口和发送命令配置；权限新增部署者配置的来源 capability，用户读取权限不变；没有数据库迁移或依赖安装。新能力默认关闭，禁用配置并重启可撤回后续写入；已有合法 CI Run 保留，回退服务端工件不删除 receipt。真实来源配置和发送仍需部署授权。

实现退出条件：

1. 配置 / Secret 读取、轮换、坏签名、过期、重复 Header、跨 source 签名、Host / HTTPS、大小与并发界限测试；日志和所有错误无敏感值。
2. 严格 payload、终态映射、时间、绑定、规范化 digest 和重试预算测试；运行中、UNSTABLE / NOT_BUILT 不会伪装成成功。
3. 真实 PostgreSQL + HTTP 验证首次、重复 / 并发、内容冲突、响应丢失恢复、目标跨 Workspace、失败原子回滚和唯一正常 Activity；既有 CI Run 读取仍成立，不产生 Deployment。
4. 发送命令对隔离 TLS 测试服务验证端到端签名、重定向拒绝、有限重试与非零失败，不接触真实 Jenkins 或外部凭据。
5. Go / 仓库检查通过，同步 server 使用说明和当前状态；清楚记录自动化与真实 Jenkins 联调的证据边界。

安装 Jenkins、运行长期服务、修改真实凭据、真实外部发送、staging 写入口和生产部署均不在该实现确认范围内。

## 外部依据

2026-09-26 核验官方材料；它们说明 Jenkins 行为，不替代本项目自定义签名协议：

- [Remote Access API](https://www.jenkins.io/doc/book/using/remote-access-api/)：可按具体构建读取数据，真实采集应限定目标与字段。
- [Run API](https://javadoc.jenkins.io/hudson/model/Run.html)：构建中的 result 可以是中间结果；开始时间与排队 timestamp 也不同，不能混用。
- [Result API](https://javadoc.jenkins.io/hudson/model/Result.html)：终态结果不只有成功、失败、取消，首期拒绝无法无损表示的状态。
- [Credentials Binding](https://www.jenkins.io/doc/pipeline/steps/credentials-binding/)：日志遮罩并非安全隔离，能定义 Pipeline 的人应视为能够使用其作用域内凭据。
