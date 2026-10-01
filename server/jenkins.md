# Jenkins 终态 delivery 接入

状态：按 [ADR-0030](../docs/adr/0030-authenticated-jenkins-delivery-adapter.md) 实现；自动化与真实 Jenkins 三态隔离联调已验证，持续采集和业务实例配置仍待完成。

这是 Nexus 自定义的受控机器写入口。接收端验证来源后调用已有 CI Run service，发送命令只发送一份可信终态快照；不会安装 Jenkins、触发构建、轮询 Jenkins 或创建 Deployment。构建完成到快照的可信采集由独立 Jenkins 实验验证，不能用手写 JSON 替代真实构建证据；持久运行的采集与转发仍需另行设计。

## 接收端配置

默认关闭。部署者在已有 Go server 中显式设置：

```text
RADISHNEXUS_JENKINS_SOURCES_FILE=/run/config/jenkins-sources.json
```

沿用 server 的精确 HTTPS public origin、Host 与可信代理配置；代理保留请求路径和原始 body，不将签名 Header 或 Cookie 写入访问日志。配置文件最多 64 KiB、32 个来源，以下为结构示例，ID 必须替换为该实例已存在的真实 Workspace / Component：

```json
{
  "version": 1,
  "sources": [
    {
      "source_id": "jenkins_test_auth",
      "workspace_id": "wrk_example",
      "component_id": "cmp_example",
      "job_full_name": "example/build",
      "keys": [
        {"key_id": "key_current", "secret_file": "/run/secrets/jenkins-current"}
      ]
    }
  ]
}
```

每个 source 代表固定 Jenkins 实例的一个 job，只能写入指定 Component。绑定改变或实例重建导致 build number 可能复用时，必须使用新的 source ID；旧 ID 不重新分配。来源配置不创建基础对象，不授予成员读取权限。启动时检查目标存在与 Workspace 匹配，每次记录事务再次检查并锁定目标；无效配置使启动失败，不回退为匿名或宽权限来源。

密钥文件包含随机生成的 32 字节密钥的小写十六进制编码，即 64 个字符，可带一个 LF / CRLF 结尾。由部署者在受控位置生成并限制文件读取权限，不通过命令参数、内联环境值、普通 API 或版本库传递。配置里最多两个 key，轮换顺序为：新增 key 并重启接收端，切换受控发送程序，再移除旧 key 并重启。停用来源需要更新配置并重启，不删除已记录事实。

来源是部署者授予机器的窄权限，Session / CSRF 不能代替其签名；签名持有者能够声明该 source 下的事实。密钥应留在受控通知程序中，不交给不可信仓库代码或其可修改的 Pipeline。

## 发送端配置与输入

发送配置文件不含密钥值：

```json
{
  "version": 1,
  "origin": "https://nexus.example.com",
  "source_id": "jenkins_test_auth",
  "key_id": "key_current",
  "secret_file": "/run/secrets/jenkins-current"
}
```

origin 必须是无路径、userinfo、query 或 fragment 的精确 HTTPS origin。发送工具验证证书，拒绝重定向，没有跳过证书校验选项；私有 CA 的信任装配由部署者另行管理。

输入来自可信采集步骤，必须先确认 Jenkins 构建已完全结束；不能在构建仍执行的 `post` 中把中间 `result` 写成终态。job 全名必须与来源绑定一致，build number 在该来源中不可复用。以下仅为格式示例，不代表真实事实：

```json
{
  "version": 1,
  "job_full_name": "example/build",
  "build_number": 42,
  "building": false,
  "in_progress": false,
  "result": "SUCCESS",
  "started_at": null,
  "completed_at": "2026-09-26T02:01:00Z"
}
```

所有字段必需，只有 `started_at` 可空；时间必须为 UTC RFC3339，最多毫秒精度。开始时间指实际执行开始时间，不能用 Jenkins 排队时间代替；无法可靠取得时用 null。`SUCCESS / FAILURE / ABORTED` 分别记录 `succeeded / failed / canceled`；`UNSTABLE / NOT_BUILT` 等返回明确失败，不擅自归并。

在已构建的发送程序上执行以下命令会向配置的真实 origin 发送请求，必须先获得该目标的发送授权。源码方式从 `server/` 执行：

```text
go run ./cmd/jenkins-delivery -config /run/config/jenkins-sender.json -input /run/input/completed-build.json
```

成功 stdout 只有 `{"ci_run_id":"cir_...","duplicate":false}`，精确重试的 `duplicate` 为 true；失败 stderr 只有安全机器码或配置 / 使用错误，退出码非零。输入与配置只接受绝对文件路径，分别限 16 KiB / 64 KiB，不从原始响应或 URL 提取可执行操作。

## 请求、重试和诊断

- POST `/api/v1/integrations/jenkins/{source_id}/deliveries`，仅 `application/json`，拒绝压缩、query、非规范路径及重复字段 / 签名 Header。
- HMAC-SHA256 覆盖方法、路径、key ID、固定 `build-{number}` delivery ID、发送时刻和原始 body 的 SHA-256；精确字节格式见 ADR。时间窗口为前后 300 秒，每次重试重新签名。
- receipt 使用规范化业务事实摘要；JSON 格式和 key 轮换不改变幂等身份，同 delivery 改结果或时间返回冲突。超过请求窗口的历史构建仍可由可信发送方重新签名补送。
- 单进程最多 4 个在途 delivery，无等待队列；数据库命令最长 5 秒，复用 server 读写超时。达到容量返回 429；接入不增加后台 worker 或无限重试。
- 发送最多 4 次，每次最长 10 秒，总预算 60 秒；只重试网络中断与 408 / 429 / 500 / 502 / 503 / 504。退避 1 / 2 / 4 秒，接受 1–10 秒的 `Retry-After` 且受总预算限制。
- 其他 4xx 或响应格式错误立即失败。失败后保留可信原始快照供人工修复配置后重送，不修改 delivery ID 绕过 409；通知失败不改写 Jenkins 构建结果。

| HTTP / 机器码 | 操作含义 |
| --- | --- |
| 201 / 200 | 首次记录 / 已记录的精确重复 |
| 401 `unauthenticated` | 来源、key、签名或时钟校验失败；统一错误不暴露配置 |
| 400 `invalid_request` | payload、时间、路径或 build / delivery 身份不符合合同 |
| 403 `source_binding_mismatch` | 已认证 payload 的 job 不属于来源 |
| 404 `not_found` | 配置目标当前不可用 |
| 409 `conflict` | 不可变 delivery 内容或 external run 身份冲突，需人工核对 |
| 422 `run_not_completed` / `unsupported_result` | 构建未结束 / 现有终态无法表达 |
| 429 `rate_limited` / 503 `temporarily_unavailable` | 有界容量 / 服务暂时失败，可按预算重试 |

请求均不缓存，错误带服务端 request ID。进程日志使用 `jenkins_delivery` 和固定字段集合，只记录结果、耗时与认证后的受控 ID；不记录 body、Header、签名、digest、密钥或数据库错误内容。日志收集与保留由部署者负责，此日志不是防篡改数据库安全 Audit。已提交业务不因日志故障撤销。

## 验证、恢复与当前边界

`./scripts/check-server.sh` 覆盖协议、配置、HTTP 和发送命令的 race / vet 检查；`./scripts/check-server-postgres.sh` 使用固定镜像的一次性测试数据库，覆盖并发重复、冲突、事务失败、TLS 响应丢失重试和 Session 读取正常 Activity。实际证据见[实施记录](../docs/status/reviews/2026-09-26-jenkins-adapter.md)。

没有 schema 变更。CI Run 与 receipt 按现有备份规则保留，来源配置与密钥不进入数据库或可移植导出；恢复后需重新建立来源授权。禁用配置并重启可以停止新写入，回退服务工件不删除历史记录。

真实 Jenkins controller / agent 已完成成功、失败、取消构建以及 finalized 快照交付，证据与实验边界见[隔离联调记录](../docs/status/reviews/2026-09-26-real-jenkins-lab.md)和[实验操作说明](../experiments/jenkins-lab/README.md)。该批次使用临时接收端和测试数据，不代表持久来源配置或持续采集完成。staging 显式记录另由 [ADR-0031](../docs/adr/0031-session-scoped-staging-deployment-recording.md) 的 Session 入口提供，不是 adapter 的自动行为；Component / Environment 管理页面、授权管理、production 部署、通用插件配置 UI 和默认 Compose Jenkins 服务仍未提供。
