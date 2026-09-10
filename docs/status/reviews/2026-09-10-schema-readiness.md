# 2026-09-10 schema readiness

基线：`684d37d`。本地账户与邀请改动已提交，Radish OIDC 按项目所有者要求延后；本切片按授权继续落实当前执行顺序中的 schema readiness。

## 结果

- 正式 Go server 装配只读 migration history checker，代替原 Ping；复用既有 runner 的工件加载与已应用历史校验。
- 只接受当前二进制的完整连续序号、名称和 checksum；不匹配或读取失败返回通用 `503`，匹配返回 `204`，每次重新检查、2 秒预算、`no-store`。
- 检查不执行迁移、初始化或修复。`/health/live` 继续独立于数据库；完整合同见 [ADR-0024](../../adr/0024-read-only-schema-readiness.md)。

## 已执行验证

- `./scripts/check-server.sh`：Go race、vet、模块图一致性与模块校验通过。
- `./scripts/check-server-postgres.sh`：真实临时 PostgreSQL 全部集成包通过；专用隔离数据库验证空库不创建历史表、完整迁移后成功、只读事务成功、空历史 / 待迁移 / 缺号 / 名称漂移 / checksum 漂移 / 新版本 / 缺表拒绝、锁等待遵守 deadline、解除锁后恢复、关闭池拒绝。隔离数据库与测试容器按测试及脚本清理。
- HTTP 测试验证重复请求不复用旧状态、不泄露数据库明细、2 秒预算和存活探针不依赖数据库。
- `git diff --check` 与 `./scripts/check-repo.sh` 通过，仓库检查覆盖 253 个文件。

## 边界

没有新增依赖、修改 migration SQL 或对用户实例执行操作。本轮没有启动新的正式服务或重跑 Compose 镜像演练，未复验 Web 浏览器页面；这些不计入本切片通过证据。历史匹配不能证明 bootstrap、旧账户映射、手工 DDL 一致性、升级协调或完整业务可用。

下一切片为最小使用入口，列表协议与成员配置范围仍需独立审查。
