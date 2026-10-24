# Project 上下文包迁移

本手册执行 [ADR-0044](../docs/adr/0044-portable-project-context-package.md) 的 `project-collaboration-v1` 首片。它迁移单 Project 的当前可读协作闭包，目标为同版本、完整匹配当前 migration history（当前为 schema 020）且已正常初始化的空业务实例；不替代 PostgreSQL 整库备份，不合并已有项目。

## 准备与权限

- 源账号必须是 active Workspace member 和该 Project 的显式 admin，并能读取所有必须保留的来源。Workspace owner 不会越过私密 Channel / Thread。
- 目标账号必须是 active owner，并通过正常初始化 / 登录建立。允许预先邀请成员，但不得创建任何业务对象。prepare 和 apply 均重查目标，apply 对业务表加锁。
- 源与目标使用相同版本程序；migration 由显式 runner 完成，不由导入 CLI 自动执行。目标在 apply 期间暂停其他业务写入口，保留本次受认证 API 通道。
- 使用获准的受控目录保存包和计划。Markdown 可能含用户主动写入的敏感内容，校验和不证明来源真实性，也不提供加密或自动脱敏。

在 `server/` 执行 `go build -o /secure/nexus-context ./cmd/nexus-context`，使用已有依赖；当前 CLI 支持 macOS / Linux 的文件权限边界，尚未交付 Windows 版本，也未打包到 Compose operations 镜像。不需要数据库 DSN。CLI 仅连接明确 `--origin` 的 HTTPS，验证系统 CA 或通过 `--ca` 指定的 CA；拒绝重定向，没有跳过 TLS 校验选项。

为源和目标各准备仅当前 OS 用户可读写的 JSON 凭据文件，字段是 `email` 和 `password`；不要把实际凭据写入命令行、历史、仓库或报告。文件必须是当前用户所有、无组 / 其他用户权限的普通单链接文件。CLI 每次创建临时 Session，结束后注销；若注销未确认，处理提示后再交接。

## 导出、审阅与应用

以下路径与 ID 是占位示意，替换为受控实际值，不将真实包提交到仓库。

```sh
nexus-context export \
  --origin https://source.example.test \
  --credentials-file /secure/source-login.json \
  --workspace wrk_source --project prj_source \
  --output /secure/project.nexus

nexus-context inspect --input /secure/project.nexus

nexus-context prepare \
  --origin https://target.example.test \
  --credentials-file /secure/target-login.json \
  --workspace wrk_target --input /secure/project.nexus \
  --operation 00000000-0000-4000-8000-000000000001 \
  --output /secure/import-plan.json
```

导出从单个一致快照构造包；权限行保持共享锁直到导出审计提交。先发生的撤权导致不交付，后发生的撤权等待该次提交。必要依赖不可读时整体拒绝，对外不列隐藏对象的 ID、类型或数量；非必要跨 Project / 研发关系按固定 scope 排除。

`inspect` 不连接服务器。包目录包含 manifest、对象 / 作者 / EntityLink / 历史 / 文档 revision JSONL、Markdown 与固定 scope 说明；不含凭据、成员名单、授权、源命令 receipt、投递状态或 DDL。未知字段 / 版本、重复记录 / JSON key、缺失历史、摘要错误、未列文件、链接和特殊文件均拒绝。

`prepare` 将完整映射保存在新建的私有计划文件；标准输出仅给出计数、权限政策和摘要，apply / result 也不打印逐项映射。`prepare` 不写目标，生成固定对象映射及绑定包 digest、目标 identity、目标 Workspace、操作者、operation 和权限政策的计划。审阅完整私有计划和以下变化：

- 所有对象和事件生成新 ID，不按源 ID、邮箱或名称匹配既有对象 / 人员。
- 历史作者生成 disabled account 与 suspended membership，没有凭据或 Session；普通账号管理不能启用它们。
- 新 Project 强制 restricted，只有本次负责人获得 admin；包内 private Channel / Thread 初始仅负责人可见，并按 [ADR-0045](../docs/adr/0045-membership-lifecycle-and-management-handoff.md) 取得局部 manager；源管理角色不迁移，历史作者不可激活或选任。其他既有成员之后经正式管理入口授权。
- accepted Decision、Ticket 状态与原始时间是接纳的历史声明；Web 标记导入来源，不代表目标刚完成审批或执行。Markdown 文本链接原样保留，可能仍指向旧实例。

明确确认 prepare 输出的精确 `plan_digest` 后应用：

```sh
nexus-context apply \
  --origin https://target.example.test \
  --credentials-file /secure/target-login.json \
  --workspace wrk_target --input /secure/project.nexus \
  --plan /secure/import-plan.json --confirm-digest PLAN_SHA256

nexus-context result \
  --origin https://target.example.test \
  --credentials-file /secure/target-login.json \
  --workspace wrk_target --operation 00000000-0000-4000-8000-000000000001
```

## 受认证 API

以下路由均在 `/api/v1/workspaces/{workspace_id}` 下，响应为 `private, no-store`。所有入口先验证 Session 与当前 Workspace membership；POST 另查同源 Origin / CSRF。导出要求 Project admin 与近期认证，prepare / apply 要求当前 owner 与近期认证；result 允许仍为 active member 的原操作者读取本人的最小成功摘要，不要求其继续担任 owner，也不返回逐项映射；来源读取复用对象权限，无权和不存在均为 404。

| 方法与路径 | 请求 / 结果 |
| --- | --- |
| `POST /projects/{project_id}/context-export` | 空 JSON object；返回 Bundle |
| `POST /context-imports/prepare` | `bundle`、`operation_id`；返回完整 Plan |
| `POST /context-imports/apply` | `bundle`、`plan`、`confirmed_digest`；返回 Result |
| `GET /context-imports/{operation_id}` | 本人操作的 `applied` / `not_observed`；不查询他人 receipt |
| `GET /context-source/{entity_type}/{entity_id}` | 当前可读对象的 `imported` 与可选 `applied_at` |

Bundle 仅是 HTTP 传输外壳，文件字节使用 base64；磁盘仍是可读 JSONL / Markdown。prepare / apply HTTP body 上限 96 MiB，包本身 64 MiB 限制同时适用。DTO 由 `server/internal/contextpackage/types.go` 定义，拒绝未知字段、重复 JSON key、尾随 JSON 与 query 参数；包格式版本和数据库 schema 版本独立。

## 失败、重试与交接

apply 原子写入业务对象、历史身份、映射、历史来源、Activity 与成功 receipt。历史事件外壳的操作者和时间属于本次导入；原作者 / 时间 / 类型保存在受约束来源记录中。导入不创建历史 Outbox，数据库拒绝为导入历史添加投递。

同 operation、同输入和同计划的精确重试返回原结果；更改输入冲突，同包新 operation 也不能重复导入。任何未收到完整成功响应的 apply 都先查询 result。`not_observed` 只表示当前未观测到提交，须确认原事务和锁已结束，再重试原 operation；不要据此删除目标或生成新 operation。

输出路径必须不存在，不覆盖原包 / 计划。输出中断会保留不完整工件；manifest 最后写入，不把半包标成成功。清理由操作者针对已确认的专属路径执行，CLI 不自动删除输入、目标数据或失败输出。

交接前在正式 Web 验证：负责人登录并发现项目、Channel / Thread 与来源关系、accepted / proposed Decision、Ticket 状态历史、文档所有版本及恢复来源；普通成员默认拒绝，经正式邀请 / 授权后能继续发消息、改状态和保存文档，撤权后不保留可操作页面。只有实际完成这些验证后再开放目标使用。

## 自动化入口与限制

`./scripts/check-server-context-package.sh` 使用本机已有 PostgreSQL 镜像和 Go 缓存，在专属临时容器内运行 `TestContext*` 数据库测试，退出时清理容器。启动资源、故障注入和清理须先获得当前任务授权；脚本不下载依赖。

增加 `--browser-review` 会构建正式 Web，保留合成源 / 目标与回环 HTTPS 端点等待页面复验；必须按 fixture 检查列表实际操作后才写完成标记。测试 CA 仅用于隔离会话，不加入系统信任；未使用此参数的自动化运行不计浏览器证据。

首片输入上限为 64 MiB、10,000 个对象 / 历史作者、50,000 条历史 / 关系 / 文档版本、1 MiB manifest 和 JSONL 单行、256 KiB 单版 Markdown；来源链最多 8 层，导入新增一层，因果链最多 256 层。各限制同时适用，不构成生产容量承诺。profile 不包含研发实体、附件、完整 Workspace、已有项目合并、作者认领或源 ACL 等价迁移。

首片仅支持现有正式协作入口产生的 proposed / accepted Decision 和四类 asserted 关系；不支持的业务形状整体拒绝，不截断成可导入成功。最多 8 层来源链的包仍可 inspect，但再次导入会新增第 9 层，因此 prepare 拒绝。实际往返、恢复及部分覆盖范围见[2026-10-24 实施记录](../docs/status/reviews/2026-10-24-portable-context-package.md)。
