# 单宿主数据库权限与旧实例切换

适用于 [ADR-0051](../docs/adr/0051-single-host-database-roles.md) 的专用 PostgreSQL 17 Compose 实例。角色、密码和 ACL 由部署工件和独立 Secret 管理。执行旧实例切换前先按[单机升级手册](upgrade-recovery.md)核对目标、协调停写并保留恢复材料。

## 新实例与独立恢复目标

1. 按[部署说明](README.md#首次准备)为本实例准备四个不同的随机 Secret。应用容器只读取 app Secret，运维容器以配置的 UID / GID 读取分配给自己的文件。使用宿主访问控制或 ACL 允许必要的容器 UID 读取文件。
2. 启动 PostgreSQL 后，以一次性容器执行 `docker compose -f deploy/compose.yaml run --rm -T database-roles`。保持其他数据库客户端停止。成功后执行 `migrate` 初始化新实例，或者在独立空目标执行 `restore --input /backups/<目录>`。恢复不需要预先运行 migration。
3. 命令以 `Database access applied.` 确认 ACL 生效。启动 app 与 Caddy 后检查 readiness、登录及受限对象权限。恢复还要核对审计证据、来源关系和 Activity，以及旧 Session 失效。

## 旧实例停写切换

1. 使用旧 Compose 和匹配工件停止 Caddy、app 及所有其他写入来源。核对没有其他数据库连接。用旧 backup 命令生成备份，并在独立空目标用旧工件验证恢复，保留旧配置、工件及其必要的 Secret。恢复验证与旧原库使用独立的容器、volume 和凭据。
2. 保留源库的准确 Compose project、PostgreSQL volume 和原管理员 Secret。将运维配置切到新工件，为当前实例配置三个新的角色 Secret。运行 `database-roles`。成功时应用对象所有权已交给 `radishnexus_owner`，原数据及 migration history 不变。仅允许这一步使用原管理员凭据。
3. 保持停写并运行新 `migrate`，校验目标 history 与 ACL 成功标记。在隔离 HTTPS 入口验证新 app 正常协作、权限拒绝、历史审计可读，确认正在运行的 app 仅挂载 app Secret。
4. 验收后再次停止 Caddy、app 和其他客户端。在受控维护终端对 PostgreSQL 执行 `docker compose -f deploy/compose.yaml exec postgres psql -X -U radishnexus -d radishnexus`，通过 `\password radishnexus` 轮换原管理员密码。安全更新管理员 Secret 文件并重新创建 PostgreSQL 容器以重新挂载，保留原 volume。从同一 Compose 数据网络的独立客户端连接 `postgres:5432`，确认新密码可用、原密码被拒绝。容器内本地 socket 或回环规则可能免密码，不能用于判断凭据是否失效。移除持有旧管理员 Secret 的停用 app 和运维容器，按新工件重启并验收后再开放入口。

角色准备的事务会在连接、对象所有者或角色属性检查失败时回滚。确认仍停写后，使用匹配原库的 Secret 纠正原因并复跑。切换时先备份是因为角色准备、migration、恢复与 ACL 各有独立提交边界。migration 或 ACL 失败时按实际 history 判断，只有目标 history 匹配且没有恢复步骤失败时才可复跑 `migrate` 补 ACL。恢复失败时按[失败恢复手册](upgrade-recovery.md#选择恢复旧版本时)隔离目标并诊断；已恢复权威数据但 Activity 未确认重建时保持隔离。

需要回到原运行状态时，以匹配的旧工件和旧备份恢复到独立空目标，配置该目标自己的管理员 Secret，复核恢复时点的成员资格和业务事实，再切到唯一入口。保留原库用于诊断。原 Compose 的共用凭据应限于旧工件恢复与过渡验收。
