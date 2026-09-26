# 真实 Jenkins 隔离联调记录

日期：2026-09-26。基线：`0001a82`。所有者已确认固定镜像下载、隔离容器启动、三个测试构建、快照交付和测试服务清理；运行配置与持久化目录由所有者指定。

## 结果

官方 Jenkins 2.568.3 / JDK 21 controller 与独立 inbound-agent 实际运行成功，未安装额外插件。controller 不执行构建；单个 Freestyle job 在 agent 上执行退出 0、退出 1 和等待后中断三种探针。controller 的 `RunListener.onFinalized` 从已落盘 Run 生成快照，不由 job 自报构建完成。

| Jenkins build | 实际终态 | 开始 / 完成时间（UTC） | Nexus 状态 |
| --- | --- | --- | --- |
| 1 | SUCCESS | 11:41:03.575 / 11:41:04.446 | succeeded |
| 2 | FAILURE | 11:41:04.551 / 11:41:04.638 | failed |
| 3 | ABORTED | 11:41:04.736 / 11:41:06.251 | canceled |

宿主收集三份快照后，既有 Go sender 使用随机内存 HMAC，经验证证书的临时 HTTPS 接收端写入一次性 PostgreSQL。每份首次发送成功，重复发送返回同一 CI Run；Session GET 返回正确状态与一个 Timeline 项，未暴露 source ID。该来源下 CI Run、inbound receipt、domain event 和 Activity 均各三条，Deployment 数量不变。

快照 SHA-256（快照本身保留在隔离目录，不进入仓库）：

| 文件 | SHA-256 |
| --- | --- |
| build-1.json | `526ebcc47eca20532bfe641a1fed0c2d9035814ef982cead237f6fce90c6c87b` |
| build-2.json | `e1ec53d124b55683a39823536cc15c894567d1f740a08517e137995568d64c11` |
| build-3.json | `1ed853d4ab03c661a3888f08630cd8c6dc8ef419d2e45b45b2080aebbd6e77e0` |

## 实现与验证

- [实验模板与操作说明](../../../experiments/jenkins-lab/README.md)：固定镜像、相对 bind mount、internal network、loopback 端口、凭据经 stdin / 文件传递，完整数据留在 Compose 同目录；没有 Docker socket 或宿主源码挂载。
- [可选真实快照断言](../../../server/internal/goldenpath/postgres/jenkins_lab_integration_test.go)：仅显式设置 `RADISHNEXUS_JENKINS_LAB_SNAPSHOTS` 后运行，复用既有集成测试，不在常规 CI 伪造快照。
- `check-server-postgres.sh`：带真实快照绝对路径与 `GOFLAGS=-v` 运行完整服务端数据库检查，退出码 0，真实三态交付断言通过；既有来源认证、权限与幂等失败矩阵同时通过。
- Compose 配置解析、shell 语法、`check-repo.sh` 和 `git diff --check` 通过。
- 补充启动前旧实验状态检查，阻止旧 ready / complete 标记被复用；保留数据时再次 start 明确拒绝，不启动容器。

## 清理与证据边界

Jenkins project 的两个容器和专用网络已移除。数据库测试通过 EXIT 清理其一次性容器，HTTPS listener 由测试关闭。固定 Docker 镜像与 Compose 同目录 `data/jenkins_home`、`data/snapshots` 保留；凭据、HOME 和原始日志不进入提交或对话。重新从空状态执行必须另行确认实验数据处理范围。

本轮验证真实 Jenkins 终态事实进入正式业务 service、数据库与 Session 读取接口。基础 Workspace / Component / 用户权限仍来自既有数据库 fixture，没有预置 CI Run / Activity。本轮未启动持久 Nexus 实例，未用浏览器展示三份事实，未构建真实业务仓库，也未执行 staging 或 production 部署。

持续采集 / 自动转发、普通管理员独立配置、Component / Repository 配置、持久来源管理与安全审计仍缺。提交后响应丢失与有限重试沿用自动化证据，本轮没有针对真实 Jenkins 交付注入网络故障。下一切片为 staging Deployment 正式记录入口，遵循显式确认、独立环境权限与幂等语义；新增 Session 写协议应先说明影响并确认。
