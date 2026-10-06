# 本机 Jenkins 隔离联调

状态：2026-09-26 经所有者确认，已在指定独立 Docker 目录完成真实 Jenkins 三态构建、终态采集、HTTPS 签名交付与 PostgreSQL / Session 读取验证。测试服务已停止，数据与快照保留。基线为 Jenkins adapter 提交 `0001a82`。精确证据见仓库 `docs/status/reviews/2026-09-26-real-jenkins-lab.md`。

## 文件与数据布局

本目录保留受版本管理的配置模板；运行副本放入项目所有者指定的独立 Docker 目录。运行副本包含 `compose.yaml`、`bootstrap.groovy`、`agent.sh`、`Agent.java`、`lab.sh` 和本说明，所有挂载均相对 Compose 目录解析，不引用 RadishNexus 源码目录。

- `./data/jenkins_home/`：controller 的配置、账号、凭据、job 和构建历史。
- `./data/snapshots/`：收集的终态快照。
- agent 的凭据和工作目录继续使用容器 tmpfs，不持久化。

不使用 Docker named volume。`data/` 已被同目录 `.gitignore` 排除；启动脚本与收集脚本仅在本目录下创建数据目录。

## 要验证的链路

专用 Freestyle job 在独立 agent 上执行三个无外部副作用的探针：SUCCESS（退出 0）、FAILURE（退出 1）、ABORTED（等待期间由 controller 中断）。controller 的受控 `RunListener.onFinalized` 从已完成 Run 生成不可由 agent 改写的快照，宿主收集该文件，再通过现有 Go sender、真实 TLS 接口、一次性 PostgreSQL 和 Session GET 验证 CI Run / Activity。

这验证真实 Jenkins 事实的采集与交付，不是长期插件、生产通知部署或普通用户独立配置能力。试验不克隆任何仓库，不执行 production / staging 部署。基础 Workspace / Component 与用户权限来自既有数据库测试 fixture；不会预置 CI Run 或 Activity。当次联调尚无 Component 正式配置入口，不能将该基础 fixture 宣称为从空产品独立配置成功。此后 Component / Environment 配置、Repository 映射及 Ticket / Component 人工关联已分别接通，见[当前状态](../../docs/status/current.md)；尚未以新配置对象重跑这条真实 Jenkins 整链。

## 具体资源与授权范围

| 资源 | 具体范围 |
| --- | --- |
| Docker project | `radishnexus-jenkins-lab`，与默认自部署分开 |
| controller | 官方 Jenkins 2.568.3 / JDK 21，0 个构建 executor，最多 1.5 GiB / 1 CPU |
| agent | 官方 inbound-agent / JDK 21，1 个 executor，最多 768 MiB / 1 CPU |
| 访问 | 仅宿主 `127.0.0.1:18080` 提供 Jenkins 测试页；agent 不暴露宿主端口 |
| 网络 | 独立 internal Docker network；agent 与 controller 内部通信，镜像下载由宿主执行 |
| 凭据 | 启动时在测试数据目录中生成独立 admin 密码与 agent secret；agent secret 经 stdin 传输并由 Java 读取，值不进入命令参数或输出 |
| 采集 | `nexus-ci-probe` 单 job，三个受控结果；只收集 `build-1.json` 至 `build-3.json` |
| Nexus 交付 | 现有 Go 集成测试中的临时 TLS receiver 与随机内存 HMAC，写入一次性 PostgreSQL；测试 Session 读取核验三态与唯一 Activity |
| 清理 | 完成或失败后停止本 project 容器；数据库测试自动清理，TLS listener 自动关闭；保留 controller 测试数据目录与忽略目录快照供复核，不自动删除数据 |

本轮已确认并执行的范围：拉取下面两个固定官方镜像，启动这两个测试容器，生成仅供本实验使用的凭据，运行三个探针，收集快照并向临时 Nexus 测试接收端发送，最后停止测试服务。约束来源为根 [AGENTS.md](../../AGENTS.md) 的“安装、升级或移除依赖”“启动长期运行的服务”与“部署或发送外部消息”须先说明目标、副作用及清理范围。

不将 HMAC 密钥挂载到 Jenkins；controller 和 agent 都没有 Docker socket、特权模式、宿主源码或业务凭据。Jenkins 页面使用仅 loopback 暴露的 HTTP，容器内也仅在隔离网络通信；Nexus delivery 仍走验证证书的 HTTPS。实验不改全局证书或系统服务。

## 镜像来源与锁定

2026-09-26 查询官方 registry manifest 并按以下 digest 拉取运行：

| 镜像 | 固定 OCI index digest |
| --- | --- |
| `jenkins/jenkins:2.568.3-jdk21` | `sha256:c1e4c349365f6d16d88595b2c5f7e8ff39b8ae1d061f62420bac193b4b9616d0` |
| `jenkins/inbound-agent`（查询时 `jdk21`） | `sha256:6a31d728c22ad74adbb6b9a1a210eb9ec2599f644b95edd7f8a3b8a71d20772c` |

两者都提供 linux/arm64 与 linux/amd64，Compose 使用 digest 而非浮动版本。controller 是官方当日 LTS 页面列出的 2.568.3，含 2026-09-02 安全修复。Jenkins 核心使用 MIT；镜像包含 JDK 和操作系统各自许可证，应保留官方镜像 notices。本实验不安装额外 Jenkins 插件，不修改项目依赖或 lockfile；digest 锁定与官方来源核验不等于完整镜像漏洞扫描或生产安全认证。

- [官方 LTS changelog](https://www.jenkins.io/changelog-stable/)
- [官方 Docker 安装](https://www.jenkins.io/doc/book/installing/docker/)
- [Controller isolation](https://www.jenkins.io/doc/book/security/controller-isolation/)
- [Jenkins MIT License](https://www.jenkins.io/license/)
- [RunListener.onFinalized](https://javadoc.jenkins.io/hudson/model/listeners/RunListener.html#onFinalized(R))：Run 已处于 COMPLETED，相关记录已写入磁盘。

## 操作入口

以下在独立 Docker 运行目录中执行。只有 `config` 为只读检查；其余动作按上述授权范围执行。

```text
bash lab.sh config
bash lab.sh pull
bash lab.sh start
```

`start` 在启动前拒绝已有实验状态目录或 project 容器，避免读取旧 ready / complete 标记；初始化只接受新测试数据目录，创建管理员、关闭匿名读取并保留 CSRF。引导自动等待 agent（最多 3 分钟），顺序执行三个 probe（每项开始和完成均有超时），由完成监听器生成快照。出现超时或 `failed` 标记时保留环境诊断，不手写快照绕过错误。

确认容器内 `nexus-lab/complete` 存在后收集：

```text
bash lab.sh collect
```

仅复制三份终态 JSON 到同目录 `./data/snapshots`，不导出 admin 密码、agent secret、Jenkins HOME 或日志。输出目录存在旧快照时拒绝覆盖。

将 `RADISHNEXUS_JENKINS_LAB_SNAPSHOTS` 设置为运行副本 `data/snapshots` 的绝对路径后，回到 RadishNexus 仓库运行现有 `./scripts/check-server-postgres.sh`。未设置时常规检查不会读取任何联调文件，也不会伪造真实 Jenkins 证据。设置后，新增可选断言必须验证三种真实结果、首次与重复发送、Session 读取、三份唯一 CI Run / receipt / event / Activity，以及 Deployment 数量不变。

普通脚本成功输出不含测试日志；需保留每个构建对应的 CI Run ID 时，可在同样受控数据库环境用 Go `-v` 输出测试自身的安全证据行。原始凭据与 Jenkins 全量日志不进入提交或对话。

```text
bash lab.sh stop
```

`stop` 只移除该 project 的容器与网络，保留同目录 `./data/jenkins_home` 和 `./data/snapshots`。再次从空状态运行需先明确授权删除该实验数据及旧快照，不能用通配清理代替确认。

## 已执行与尚未执行

已执行：官方版本与 registry digest 核验、固定镜像拉取、Compose 配置解析、shell 语法检查、Groovy 引导、Java agent 连接、三个真实探针与 finalized listener、收集三份 JSON、带快照环境变量的完整 `check-server-postgres.sh`、仓库与差异卫生检查、测试服务清理。重复发送没有增加 CI Run / receipt / event / Activity，Deployment 数量不变。

尚未覆盖：持续运行的采集与自动转发、真实业务仓库构建、生产 Jenkins 插件、普通管理员独立配置、Web 展示本轮事实和 staging Deployment 写入口。构建提交后响应丢失、有限重试与认证拒绝仍由既有自动化矩阵验证，本轮真实构建未注入这些网络故障。Jenkins 数据和快照保留在独立 Docker 目录，临时 Nexus 数据库已自动清理，不能把本轮测试 ID 当作长期产品实例中的可访问对象。
