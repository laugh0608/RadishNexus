# 2026-09-26 Jenkins 来源 adapter 实施与验证

基线：`a394ed6`（CI Run 正式读取）。项目所有者确认继续按 ADR-0030 实施来源 adapter，本记录只描述本轮代码与自动化证据，不授权实际实例部署和外部发送。

## 实现结果

- 新增默认关闭的 Jenkins POST 入口，沿用 Host / HTTPS / trusted proxy；路由在 ServeMux 清理路径前分派，非规范签名路径被拒绝，不重定向。
- `internal/jenkins` 负责受控来源 / 密钥文件配置、严格 JSON、HMAC、时间窗口、终态和规范化 receipt 映射；原始输入不进入核心领域层、Activity 或普通 DTO。
- 绑定目标启动时检查；既有 CI Run 写事务在 claim receipt 前检查并锁定目标 Component，防止缺失、跨 Workspace 和并发删除产生半成品。成功写入复用已有 receipt、事件、Outbox 与正常 Activity 投影，没有 schema 变更。
- `cmd/jenkins-delivery` 从文件读取可信快照与发送配置，使用验证证书的 HTTPS、固定 delivery ID、逐次签名、有界重试和安全机器码；拒绝重定向和无限等待。
- 错误与运行日志使用字段白名单，未经认证的攻击者来源 / delivery 文本不写入日志；文件路径、Secret、签名、payload 和底层数据库错误不出现在响应或日志中。
- 使用方法与停用 / 轮换 / 恢复步骤见 [server/jenkins.md](../../../server/jenkins.md)。既有 server README 中 CI Run 仍为内部 query 的过期说明同步修正为 ADR-0029 已开放状态。

## 实际验证

- 定向 Go 测试通过：配置默认关闭 / 无效失败、嵌套重复键 / 未知键、文件上限、Secret 格式与脱敏、轮换 key、payload 缺失 / 重复 / 大小 / UTF-8、时间精度与先后、运行中 / 不支持状态、规范化 receipt。
- HTTP 测试通过：方法、Host、HTTPS、query / 编码路径、坏签名、过期、重复 Header、Session 不能替代签名、跨来源、绑定 job、不支持终态、4 请求容量与释放、数据库错误脱敏；验证拒绝请求不调用核心 service。
- 发送测试通过：真实隔离 TLS 服务、证书校验与禁止 insecure transport、重定向不跟随、可重试状态 / 网络错误最多 4 次、1 / 2 / 4 秒退避、Retry-After 校验、取消、永久失败、异常成功响应和命令非网络失败入口。
- `./scripts/check-server.sh` 通过：完整 Go race、vet、无修改 tidy 与模块 checksum 校验。
- `./scripts/check-server-postgres.sh` 通过：现有固定镜像的一次性 PostgreSQL；真实 HTTP 首次与格式等价重复、并发 duplicate、内容冲突、目标缺失 / 跨 Workspace、未认证 / 不支持终态拒绝，以及事件插入失败整单回滚后恢复。
- 数据库测试实际在首个 TLS 请求提交后断开连接，发送程序重试并取得同一 CI Run 的 duplicate 结果；6 个成功外部事实恰好对应 6 个 receipt / CI Run / event / Outbox / Activity，没有新增 Deployment。随后经正式 Session GET 读取正常 Timeline，不运行投影重建。
- `./scripts/check-repo.sh` 与 `git diff --check` 通过；最后补充签名路径清理边界测试，定向 Go race 复验通过。
- 一次性数据库由脚本退出清理；TLS 测试监听由测试关闭。没有新增长期进程、依赖或 lockfile 变化。

## 证据边界与下一步

本轮仍使用虚构来源与受控测试快照，没有真实 Jenkins API 采集、真实 job 执行或通知配置。构建中的 result 可能是中间值，真实采集需验证完成标志与实际开始 / 完成时间，不能把当前 job 的 `post` 当作已完成事实。

下一步在明确目标、网络、来源绑定、专用凭据和清理方式后选择已有或隔离实例，验证 SUCCESS / FAILURE / ABORTED、重复、坏签名、过期和通知失败恢复。UNSTABLE / NOT_BUILT 目前明确拒绝，不能据此宣称覆盖所有 Jenkins 状态。

来源配置历史仍由部署变更管理保证，运行日志不等于持久安全 Audit；没有建立通用 Integration、Secret 数据库、Component 配置 UI、staging 写入口或完整 Golden Path。Document 原生 IME 人工验收继续暂缓。新改动保留在工作区，未提交、push、安装 Jenkins 或对外发送。
