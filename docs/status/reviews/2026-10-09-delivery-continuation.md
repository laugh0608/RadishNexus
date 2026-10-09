# 2026-10-09 浏览器补验与持久终态发送

## 基线与范围

本地 `dev` / `origin/dev` 同为 `c81d162`，起始工作区干净；没有 fetch 或核验实时远端。所有者确认本轮临时浏览器资源与 [ADR-0035 A](../../adr/0035-durable-jenkins-terminal-delivery.md) 的实施范围后，完成 Ticket / Component 浏览器补验、持久发送 worker、失败恢复测试与运行说明。B 的 controller 对账设计保留，尚未实施或启动真实 Jenkins。

没有增加业务 schema、公共 v1 payload、权限、依赖或锁文件变更；没有扩展聊天、Document 协同或 Flutter。原生中文 IME 继续暂缓。工作区改动尚未提交、push 或部署。

## Ticket / Component 浏览器补验

使用 `RADISHNEXUS_BROWSER_FOUNDATION=1` 与现有 `scripts/run-authenticated-web-browser-fixture.sh`，在一次性 PostgreSQL / Caddy、正式 Web build 和隔离 Chrome 上验收。从正式 bootstrap 的空业务工作区，经页面创建 Team、restricted Project、Channel、Message → Thread → Decision → Ticket 和 Component，再建立关联；没有用 SQL 预置业务对象或关系。

| 用例 | 实际操作与结果 |
| --- | --- |
| 历史导航与重入 | Ticket → Component → Ticket、浏览器后退 / 前进、稳定地址重载及双向关系均通过断言；显示正确对象与当前关系 |
| 收回写权 | 临时数据库将测试成员 Project 角色改为 viewer，再显式刷新 Ticket 关联区；内容保留，关联 / 解除表单清空并呈只读 |
| 收回读权 | 恢复 contributor 后在 Component 反向列表打开待确认解除表单，再删除测试成员 Project membership，显式刷新反向关系；Ticket 链接与表单清空，浏览器返回原 Ticket 地址得到不可读状态 |
| 布局与键盘 | 1440px 桌面、390px 手机最终工件截图已查看；Ticket / Component 关系与操作无明显遮挡，名称 / 状态分隔已展示，Enter 键完成关联 |

权限变更使用当前临时数据库的定向故障注入：当前管理员交接尚无完整正式入口，不将其计作管理 UI 验收。明确刷新与浏览器历史导航有证据；CLI 标签选择没有可靠触发原生窗口焦点，因此不宣称原生焦点专项通过。临时权限注入没有触及业务实例。

截图保留于忽略目录 `output/playwright/`，主截图为 `2026-10-09-ticket-desktop.png`、`2026-10-09-ticket-mobile.png`、`2026-10-09-component-mobile.png`；它们只含本轮合成内容，不进入提交。浏览器会话关闭，fixture 经 stop 文件正常退出并清理本轮容器、卷和临时状态。此前缺口见 [10 月 6 日记录](2026-10-06-ticket-component.md)；本轮补齐其后退、显式刷新撤权和最终截图，未改变 Web 实现。

## 持久发送实现

新增 `server/cmd/jenkins-worker` 与 `server/internal/jenkins/spool*`，保留原一次性 sender CLI。使用标准库、本地 manifest、OS 独占锁、严格权限与文件限制、不可变 payload、原子同步状态和 ack；每次发送前持久预扣预算，未知结果沿原 delivery ID 重放。sender 增加安全的类型化错误，区分有限重试、单条阻塞与来源暂停，不保存原始响应或密钥。

命令提供离线 init、Linux 前台 run、无凭据 status、精确人工 retry 与只读 cleanup-plan。每轮最多 4 请求 / 60 秒，最多 12 轮 / 24 小时，重启不清预算；单条永久失败不妨碍其他到期待办。格式不完整的 payload 不误判为来源重绑；真实绑定变化、认证或 TLS 信任失败暂停来源。

容量、文件权限、状态漂移和写入失败均显式停止；同身份异字节保留原内容并报冲突，无法通过重试覆盖。确认只在成功持久化后发布；缺失确认可以重建。当前保留全部输入和状态，清理命令只报告候选数量，不删除。运维配置、停用、轮换和恢复步骤见 [worker 说明](../../../server/jenkins-worker.md)。

## 验证证据

| 验证 | 结果与范围 |
| --- | --- |
| `./scripts/check-server.sh` | 通过：Go 全量 race、vet、依赖校验及 `go mod tidy -diff`；无新增依赖 |
| 定向 race 测试 | `go test -race ./internal/jenkins ./cmd/jenkins-worker` 通过；最终补充的格式错误隔离和离线副本测试包含在内 |
| `./scripts/check-server-postgres.sh` | 通过：固定镜像一次性真实 PostgreSQL 集成回归；新增 worker HTTPS 提交后杀进程恢复用例 |
| Linux 运行平台 | 缓存的 `golang:1.26.7-alpine3.23`、容器外部禁网、源码与模块缓存只读，`go test ./internal/jenkins ./cmd/jenkins-worker` 通过；真实 CLI 启动、签名 HTTPS、送达、SIGTERM 正常退出和锁释放 |
| 文件与故障 | 覆盖重启后预算、in-flight 重放、进程突然退出、write / file sync / rename / directory sync 故障、成功后本地记账失败、ack 失败恢复、冲突、损坏、symlink / hardlink / 权限拒绝、配额、导入批次及公平处理 |
| 离线操作 | 无 Secret 初始化 / 状态；精确重试、密钥引用轮换、暂停来源；停止后复制三个目录到新路径，预算与原始字节保持，换接收实例被拒绝。未将此测试描述为真实 Jenkins 备份恢复 |
| 仓库与差异 | `./scripts/check-repo.sh`、`git diff --check` 通过 |

真实数据库用例通过正式配置 service 创建 Component，使用合成终态输入与真实 TLS receiver：receiver 提交成功但尚未返回时杀死独立 worker 测试进程，磁盘保持 `in_flight` / 第 1 轮；重启重放返回原 CI Run 并保存第 2 轮成功。随后发送 FAILURE / ABORTED，CI Run、receipt、领域事件、Outbox、Activity 各恰好 3 条，Deployment 为 0，当前权限下可回读 CI Run。此处证明持久交付与核心幂等，不代替真实 Jenkins 采集证据。

首次局部 Go 测试因沙箱不允许监听临时端口 / 访问构建缓存失败，获准在沙箱外复跑后通过；没有通过关闭证书校验或放宽断言绕过。浏览器 fixture 构建了当前 Web 工件；本轮未修改 Web，未重复执行上轮已通过的全量 Web 测试。

## 未覆盖与下一段

- A 完成仅证明在输入已经可靠发布、存储保留且 receiver receipt 正确恢复的条件下可重启送达。没有实测宿主断电、硬件损坏、NFS、多副本、生产规模容量或无人值守长期运行；Linux 文件系统白名单不是逐种硬件耐久认证。
- B 尚缺可信 controller 持续采集、启动 / 周期对账、采集心跳、历史缺口、生产者确认清理、真实 Jenkins 分别重启与权限挂载验收。当前输出明确标记 collector 未实现。
- Repository / commit / CI Run 来源与 Ticket 具体交付关系仍须独立冻结；共享 Component 不证明已构建或部署。新配置对象 → 真实 Jenkins → 浏览器记录外部 staging 的整链仍未验收。
- 本轮临时资源已退出，无业务数据库修改、真实 Jenkins 启动、系统服务安装或远程写入。后续具体执行顺序以[当前状态](../current.md)为准。
