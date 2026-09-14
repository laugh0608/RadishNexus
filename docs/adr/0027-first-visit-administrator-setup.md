# ADR-0027：部署后首次访问创建管理员

状态：已接受（2026-09-14）

日期：2026-09-14

## 背景

项目所有者要求部署后首次访问能够创建第一个管理员或超级管理员。当前 `nexus-bootstrap` 已通过数据库事务锁创建唯一首位账户、Workspace 和 owner，但浏览器只提供登录与邀请兑换。基础配置已按 [ADR-0026](0026-foundation-configuration-and-membership.md) 接通，不能继续用运维预置账户代替首次访问体验。

现有领域没有实例级超级管理员。Workspace owner 是工作区管理者，不自动获得 restricted Project / Channel / Thread 或 Deployment 权限。引入跨 Workspace 或能穿透私密对象的角色会改变领域、安全与审计合同，不能作为初始化页面的隐含副作用。

## 决定

首位管理员采用现有 Workspace owner。部署者提供一次性初始化码，浏览器填写初始化码、邮箱、展示名、密码与 Workspace 名称，一次事务创建首位管理员和工作区。成功后进入正式登录流程，登录后由 owner 显式创建 Team / Project / Channel。

初始化码用于证明部署控制权，防止公网部署后被最先访问的陌生人注册占有。通过独立受保护 Secret 文件配置，禁止默认码、URL 查询参数、页面回显、日志输出和浏览器持久化。该文件仅用于初始化，不进入业务备份或 `.nexus` 导出；账户建立后入口永久关闭，即使配置文件仍存在也不能再使用。

## 接口与流程

1. 同源只读状态入口 `GET /api/v1/setup` 返回最小状态 `required / unavailable / complete`：数据库无账户且初始化码可用为 required，无账户但未配置为 unavailable，已有账户为 complete。数据库或 schema 错误必须明确失败，不能当作空实例。无账户时只提示部署者完成初始化配置，不提供无验证注册。
2. 首页在稳定未登录状态下查询初始化状态。required 展示创建管理员表单；complete 使用既有邮箱登录；unavailable 提示部署者配置初始化码或使用既有 CLI。不会覆盖已登录用户，也不通过后台轮询反复探测。
3. `POST /api/v1/setup` 使用受控 JSON 字段 `setup_code / email / display_name / password / workspace_name`。验证精确 HTTPS Origin、Host、可信代理与客户端限流，拒绝未知字段和超限正文；没有 Session 时不能沿用登录后的 CSRF token，使用既有未认证入口的同源防护。比较初始化码时不得回显原值或不同失败详情。
4. 复用正式 bootstrap 的密码规则、哈希、账户约束和数据库 advisory lock。状态 GET 不授予写入权；POST 在事务内再次检查全库账户状态。并发请求、多进程请求及 CLI / Web 竞争只有一个成功，重复请求不生成第二个账户或工作区。
5. 创建成功只返回已完成标记，清理浏览器中的初始化码与密码，再由用户正式登录。不附带超级权限、默认业务对象或自动 Session。网络结果不明确时重新读取初始化状态；不得自动用新资料覆盖、重置密码或重复注册。
6. 已有账户后禁止初始化写入，不因无 Session、owner 被暂停、配置文件改变或浏览器缓存而重新开放。管理员恢复是独立流程，不能借首次初始化重新夺取管理权。

公共响应仅 `{status}`：GET 成功 `200`，POST 成功 `201`，不返回邮箱、ID、Secret 或 Session。错误初始化码与未配置 POST 均为 `403 forbidden`；已完成为 `409 setup_complete`；字段错误 `400`、正文超限 `413`、限流 `429`，保留 `Retry-After`；数据库 / readiness 错误进入通用错误，不伪造 `required`。两种请求均 `private, no-store`，不接受 query；POST 正文上限 4 KiB，重复 / 缺失 / 未知字段与 null 拒绝。

配置键 `RADISHNEXUS_SETUP_CODE_FILE` 只接受绝对文件路径。文件包含 32 个密码学随机字节的 canonical unpadded base64url 编码（43 字符），可带单个 LF / CRLF，读取或格式错误让启动失败且不回显路径或内容。未配置时服务可启动并返回 unavailable / complete。启动后服务只保留码的 SHA-256，验证采用常数时间比较；配置变更需要重启，关闭入口仍以数据库账户事实为准。部署步骤见 [Compose 说明](../../deploy/README.md)。

初始化 POST 与登录共享已有 IP 次数 / 并发密码运算限制，不新增依赖。readiness 和处理均有超时；单进程限流不声称分布式抗滥用能力。

## 备选方案

- 无初始化码的首访注册：对外暴露空实例时有被抢先占有的风险，不建议。
- 仅 CLI bootstrap：已有可靠底层能力，但不满足本次首次访问体验要求。
- 新增实例级超级管理员：先明确实例运维权限、跨 Workspace 范围、私密数据边界、最后管理员与恢复机制，再单独设计；不以 owner 别名悄悄实现全局权限。

## 实施与验证

复用 authn bootstrap，补初始化状态查询、文件 Secret 配置、专用同源 handler 和首访 Web 表单；不新增依赖或预置业务对象。按风险覆盖：空实例成功、错误码和未配置拒绝、严格字段、Host / Origin / 限流、双请求与 CLI 竞争单胜者、事务失败无半成品、初始化完成与备份恢复后入口关闭、模糊失败恢复、密码与码不入响应或日志，以及部署后首次访问到登录的真实 HTTPS 浏览器流程。

实现与实际证据见 [首次初始化记录](../status/reviews/2026-09-14-first-visit-setup.md)。实际基础配置交付见 [本轮记录](../status/reviews/2026-09-14-foundation-configuration.md)。
