# 单宿主数据库身份拆分验收

日期：2026-11-08。范围为本机合成数据与独立 Compose 项目。宿主为 macOS arm64，Docker Engine 29.8.2（linux/arm64）、Compose 5.5.0、固定 PostgreSQL 17.10 和 Caddy。HTTPS 仅发布回环端口，CA 只由本轮验收客户端信任。

验证版本为 `4f2c12e`（schema 024、旧共用凭据 Compose）与 `380df67` 加本轮工作区改动（schema 025、Activity 13）。本轮只修改部署与运维装配，未修改 migration、业务 API、备份格式或依赖锁文件。角色和切换合同见 [ADR-0051](../../adr/0051-single-host-database-roles.md) 和[部署手册](../../../deploy/database-roles.md)。

| 验收工件 | app（含 Web）image ID | operations image ID |
| --- | --- | --- |
| 旧工件 | `sha256:cd9a160f6a964b123e4f65317d952e2737d0bcd5f8aab01841eb302b89397081` | `sha256:b800eba3db703e03f332479152880d501f13bb19bbc03b349289b398e890faee` |
| 拆分后的工件 | `sha256:840179b31ce71b7d9fbd9bc3d628f39bfca983d1adb7d30420103aee28184ec2` | `sha256:b9d9706593292b7a9fb04760a1d25feb34c98b3dc827217e2dccfc4a16c0f88f` |

## 通过项

| 场景 | 实际证据 |
| --- | --- |
| 新实例与身份边界 | 正式 Compose 检查通过角色准备、显式迁移、HTTPS readiness、首次初始化、登录、SSE、账号恢复和 owner 交接。应用可读 history 并锁定上下文导入所需审计表，不能执行 DDL、更新 history 或改写已有审计；备份身份无法写业务表。静态核对确认 app 只挂载应用 Secret |
| 旧备份和所有权切换 | 024 旧库经正式 HTTPS 建立受限 Project，完成讨论、Decision、工单状态与两版文档。停写后以匹配工件备份并恢复到独立 024 空目标。原库交接应用表所有权前后逐项核对历史和审计摘要。运行中的源库拒绝重复执行角色准备 |
| 024→025 中断及续迁 | 在隔离源库持有 `collaboration_command_receipts` 的 `ACCESS EXCLUSIVE` 锁，并临时为 owner 设置一秒 `lock_timeout`。正式迁移报锁超时，history 保持 024，025 复核函数未出现。清除测试设置与阻断连接后，同一迁移命令续跑到 025 并应用 ACL |
| 切换后继续使用与凭据轮换 | 切换后从 HTTPS 复核既有来源、工单状态、文档版本、普通成员权限与越权 404；精确重试复用原写入，继续提交复核和新版文档。轮换管理员 Secret 后通过 Compose 数据网络确认新密码成功、旧密码被拒 |
| 新备份与独立空库恢复 | 025 专用备份身份成功生成备份。新目标以自己的角色和 Secret 恢复，并重新应用 ACL。40 张权威表的逐表摘要及重建后的 10 条 Activity 与源一致，Session、未兑换邀请和 OIDC 事务均为空。正式 HTTPS 拒绝旧 Session，新登录后仍可读第 3 版 Document 与原来源，越权仍为 404，精确重试原 Decision 复核返回 200 |
| Project 上下文包 | 使用拆分后的 app 身份导出带复核历史的 v6 包，在独立新实例通过正式 HTTPS API 完成 prepare、apply 和结果查询。目标指纹、导入表锁、历史写入和结果回读均通过 |

演练修正了 PostgreSQL 就绪判断。官方镜像首次初始化期间会启动只监听 Unix socket 的临时服务，原健康检查可能在该阶段提前通过。新探针使用 TCP，后续验收在主进程就绪后运行角色准备和恢复。

源码验证运行 `./scripts/check-self-hosted-compose.sh`、`./scripts/check-repo.sh`、Shell 语法检查和 `git diff --check`。升级故障演练复用了[历史演练](../../../scripts/check-upgrade-recovery.py)的隔离 Compose、HTTPS 和快照工具，使用上表的新旧工件。退出后核对演练专属容器、网络、volume、镜像标签和临时材料均已清理，保留构建缓存。

独立操作者 U27、T04 真人复验及正式试用宿主的技术准入仍分别待验证。本轮小样本用于核对权限和恢复行为。
