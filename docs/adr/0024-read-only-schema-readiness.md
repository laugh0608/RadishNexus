# ADR-0024：只读 schema readiness

状态：已接受

日期：2026-09-10

Supersedes：[ADR-0016](0016-minimal-docker-compose-self-hosting.md) 中 Go `/health/ready` 只检查数据库 Ping 的边界；显式 migration、bootstrap 与自部署拓扑保持原有职责。

## 背景

数据库连接成功不代表业务 SQL 可以运行。缺少 migration、历史漂移或数据库版本高于应用工件时，原探针仍返回成功，可能让运维误判业务可用。当前执行顺序已要求只读 schema readiness，项目所有者已授权继续该切片。

## 决定

- `/health/live` 保持进程存活检查；`/health/ready` 读取正式 migration history，与当前二进制内嵌工件逐条比较连续序号、名称和完整文件 SHA-256 checksum，并要求数量相同。
- 复用 migration runner 的历史读取与已应用前缀校验；就绪检查另外要求不存在待应用 migration。没有另建 schema 版本文件或第二套 checksum。
- 当前采用精确版本匹配，不推断向前或向后兼容窗口。数据库更新于应用工件时，旧工件不再报告 ready。
- 每次请求重新查询，沿用 2 秒请求预算，返回 `204` 或通用 `503 not ready`，增加 `Cache-Control: no-store`。匿名响应不泄露数据库错误和历史明细。
- 检查不执行 DDL、migration、bootstrap、修复或业务写入；数据库查询本身也验证连通性。迁移仍由运维显式运行，正常完成后下次探针即可恢复。

## 替代方案与后果

保留 Ping 会继续把连接成功误当作业务就绪；启动时自动迁移会把运行权限和 schema 变更耦合；缓存成功结果会掩盖运行期漂移。本轮均不采用。

本检查针对权威 migration history，不能检测保持历史不变的手工 DDL，也不证明 bootstrap、旧账户邮箱映射或端到端业务已完成。查询是即时观察，不锁住并发升级，不充当业务请求的全局拦截器；升级窗口、路由摘流、数据库最小权限、失败恢复和未来兼容窗口另行治理。

## 验证

真实 PostgreSQL 覆盖空库、空历史、待迁移、缺号、名称与 checksum 漂移、较新 schema、历史表缺失、锁等待超时及恢复、关闭连接池；只读事务验证检查不写入。HTTP 测试覆盖预算、重复探测、不可缓存、错误收敛和独立 liveness。执行证据见[本轮记录](../status/reviews/2026-09-10-schema-readiness.md)。
