# ADR-0016：最小 Docker Compose 自部署开发拓扑

状态：已接受

日期：2026-08-31

## 背景

ADR-0010 已建立 PostgreSQL 17 同 major 的备份恢复，ADR-0013 已冻结精确 HTTPS public origin 与可信代理边界，ADR-0015 已把 production Web build 和 API 装配到同一个 Go server。真实浏览器也已经通过临时 HTTPS listener 完成 login → Session → Deployment Nexus View → logout，但这些能力尚未形成正式的多进程自部署方式。

下一个切片需要证明全新实例能够从固定工件启动 PostgreSQL、TLS reverse proxy、Go server 和 production Web build，并由实例管理员显式执行 migration 与一次性 bootstrap。该拓扑不能为了启动便利引入自动迁移、默认账号、明文环境变量 Secret、浮动镜像、HTTP Cookie fallback、公共数据库端口或第二套业务初始化逻辑。

## 决定

### 适用范围与进程职责

- `deploy/compose.yaml` 是 M0.5 / M1 首个正式自部署开发入口，不是高可用、Kubernetes 或公网生产拓扑承诺。
- PostgreSQL 只保存权威数据并使用独立持久卷；它不发布宿主端口，只连接数据内部网络。
- Go server 包含唯一正式业务进程和 Vite production build，只连接 proxy 与数据内部网络，不发布宿主端口，也不终止 TLS。
- Caddy 是唯一发布宿主端口的进程，只负责内部 CA HTTPS、传入代理 Header 清洗、结构化访问日志和到 Go server 的转发；它不单独托管 Web 文件，也不拥有业务路由。
- migration、bootstrap、backup 与 restore 继续调用现有四个正式 CLI，通过显式 Compose operation service 运行；它们不是常驻服务，也不作为应用启动依赖自动执行。

### 网络、HTTPS 与代理信任

- 浏览器只访问 `RADISHNEXUS_PUBLIC_ORIGIN` 指定的精确 HTTPS origin。开发拓扑默认使用 `https://localhost:8443` 和 Caddy internal CA；使用者必须显式信任或向验证工具提供该实例的 CA，不提供 HTTP 或 insecure Cookie fallback。
- Caddy 与 Go server 位于专用 proxy 网络，Caddy 使用固定容器地址；Go server 的 `RADISHNEXUS_TRUSTED_PROXY_CIDRS` 只信任该地址的 `/32`，不信任整个用户网络或任意 private range。
- Caddy 保留原始 `Host`，覆盖外部传入的 `X-Forwarded-Proto`、`X-Forwarded-For` 与 `X-Forwarded-Host`。Go server 继续按 ADR-0013 从右向左解析完整客户端链并验证直接 peer。
- 容器默认删除全部 Linux capabilities；Caddy 官方二进制自身带有 `cap_net_bind_service=ep`，因此 Caddy service 只把 `NET_BIND_SERVICE` 加回 bounding set 以允许该二进制在 `no-new-privileges` 下执行。当前仍只监听和发布非特权 HTTPS 端口，不借此开放额外入口。
- PostgreSQL 只与 Go server 和显式 operation service 共享数据内部网络。Caddy 不加入数据网络，PostgreSQL 也不加入 proxy 网络。

### Secret 与配置输入

- PostgreSQL 和需要访问数据库的 RadishNexus 进程共享同一个 Compose `postgres_password` Secret；该文件只挂载给明确需要它的 service，不写入 Compose、镜像层、仓库文件或进程参数。
- PostgreSQL 使用官方镜像的 `POSTGRES_PASSWORD_FILE` 入口。RadishNexus 保留 `DATABASE_URL` 作为非密连接地址，并用 `RADISHNEXUS_DATABASE_PASSWORD_FILE` 指向绝对 Secret 路径；公共 runtime config 在内存中把密码安全编码到 URL 后交给现有 `pgx` 和备份恢复边界。
- 同时在 `DATABASE_URL` 内嵌密码并设置密码文件、使用相对 Secret 路径、空文件、多行文件或缺少数据库用户时均启动失败，避免漂移或模糊优先级。
- bootstrap 的管理员密码仍只经标准输入传入 `nexus-bootstrap --password-stdin`，与数据库 Secret 分离；不保存为 Compose Secret，不进入 shell 参数、环境变量、日志或镜像层。

### 固定镜像与构建供应链

- 所有外部基础镜像使用 Docker Official Images，并同时写出可读 patch tag 与不可变 multi-platform digest；禁止 `latest`、floating major/minor tag 和未固定下载地址。
- 当前边界采用 Caddy `2.11.4-alpine`、PostgreSQL `17.10-alpine`、Go builder `1.26.7-alpine3.23`、Node builder `24.16.0-alpine3.23` 与 Alpine runtime `3.23.5`。精确 digest 以 `deploy/compose.yaml` 与 `deploy/Dockerfile` 为自动化合同。
- Caddy 核心使用 Apache-2.0；Go、Node、PostgreSQL 与 Alpine 继续使用各自官方镜像的上游许可证组合。该选择不改变 RadishNexus 自身许可证，镜像升级必须重新检查来源、许可证、漏洞和 digest。
- Web build 只执行现有 lockfile v3 的 `npm ci` 和 `npm run build`；Go build 只使用现有 `go.mod` / `go.sum`。构建不生成或修改 lockfile，不运行 npm lifecycle scripts，不引入新的应用依赖。
- 最终 server runtime 使用固定 Alpine 基础、非 root 用户和只读 production Web build；operation runtime 基于同一固定 PostgreSQL 17 镜像以复用匹配 major 的 `pg_dump` / `pg_restore`，不另装漂移的客户端包。

### 显式初始化与失败语义

- 全新实例顺序固定为：准备 Secret → 启动并等待 PostgreSQL ready → 显式运行 `nexus-migrate` → 显式运行且只允许一次成功的 `nexus-bootstrap` → 启动 Go server 与 Caddy → 经 HTTPS 登录。
- `docker compose up` 不执行 migration、bootstrap、业务 fixture 或 restore。应用镜像缺少 Web build、public origin 非 HTTPS、可信代理配置无效、数据库 Secret 不可读等情况必须让相关进程失败并保留真实错误。
- PostgreSQL healthcheck 只证明数据库进程可接受连接；Go `/health/ready` 只证明当前进程可以 ping 数据库。migration 与 bootstrap 状态由显式 operation 的成功或失败和运行手册区分，不扩大匿名健康响应。
- backup 只写入 `deploy/local-data/backups/` 的显式新目录；restore 继续只接受全新空目标。失败实例和临时验证数据只清理由本切片脚本创建的精确 Compose project 与 `deploy/local-data/`，不删除用户提供的外部备份。

## 替代方案

### Nginx 加静态测试证书

Nginx 能完成 TLS 和反向代理，但开发拓扑还需要独立生成、续期、挂载和检查测试证书。Caddy internal CA 能在不引入脚本生成私钥的情况下建立可持久化的 HTTPS 边界，并默认忽略不受信的传入 `X-Forwarded-*` 值；因此当前选择 Caddy。未来公网证书、外部 load balancer 或用户自带证书需要新的生产部署决策。

### 由 Caddy 单独托管 Web build

这会产生第二套路由、缓存和安全 Header 配置，并破坏 ADR-0015 的单一 Go Web 装配合同。当前继续由 Go server 同源交付 Web 与 API，Caddy 只做边界代理。

### 应用启动时自动 migration 或 bootstrap

自动 migration 会把 schema 变化和常驻进程启动耦合，自动 bootstrap 则需要默认凭据或隐式 Secret。两者都会模糊失败与审计边界，违反 ADR-0005 和 ADR-0012，因此拒绝。

### 把数据库密码放入 `.env` 或完整 `DATABASE_URL`

Compose 环境变量容易被进程环境、诊断输出或错误日志意外暴露。文件 Secret 可以按 service 授权且不进入镜像或配置文本，因此当前新增最小的密码文件 overlay，而不建立第二套数据库初始化逻辑。

### 直接发布 Go server 或 PostgreSQL 端口

发布 Go HTTP 端口会绕过 TLS 和 Header 清洗，发布 PostgreSQL 端口会扩大数据面暴露。调试应使用显式 `docker compose exec/run`，不改变默认网络边界。

## 后果

正面影响：

- 新实例第一次拥有从固定容器工件、显式 migration/bootstrap 到 HTTPS Session 的可复验自部署闭环；
- reverse proxy、Go server、Web build、PostgreSQL 和运维命令保持单一职责，既有应用与权限合同不需要复制；
- 数据库密码不进入 Compose 环境变量，且同一 Secret 可同时供 PostgreSQL 与受控 application operation 使用；
- 应用与数据库没有公共端口，客户端身份与转发 Header 继续由唯一 HTTPS 边界验证；
- 备份恢复继续使用与服务端支持矩阵一致的 PostgreSQL 17 客户端，不把宿主工具版本带入工件。

成本与风险：

- 初次构建需要显式获取五类固定官方镜像和 lockfile 中的 npm / Go 依赖；离线运行前必须提前准备这些工件；
- Caddy internal CA 只适合受控开发实例，浏览器必须显式信任 CA；它不是公网证书方案；
- 固定 proxy 地址和 subnet 可能与宿主已有 Docker 网络冲突，发生冲突时必须显式调整 subnet、proxy 地址和对应 `/32` 信任值，不能放宽为整个 private range；
- Compose Secret 在本地实现为只读文件挂载，并不替代宿主磁盘加密、文件权限或外部 Secret manager；
- digest 固定意味着安全升级不会自动发生，维护者必须主动评估并提交更新。

## 迁移与验证

该决策不新增数据库 migration、不开放业务 handler，也不改变 ADR-0010～0015 的数据、认证、Cookie、权限或 Web DTO。现有直接运行方式继续支持完整 `DATABASE_URL`；只有设置 `RADISHNEXUS_DATABASE_PASSWORD_FILE` 时才启用文件 Secret overlay。

验证必须覆盖：

1. Compose 与 Caddy 配置静态校验，所有外部镜像都具有 patch tag 和完整 digest，只有 Caddy 发布宿主 HTTPS 端口；
2. 密码文件 overlay 的 URL 编码、绝对路径、单行、空值、歧义和读取失败测试，错误不得包含 Secret 内容；
3. 全新命名 volume 上 PostgreSQL ready → 显式 migration → 一次 bootstrap，第二次 bootstrap 必须失败且不改变既有账号；
4. Go server 和 Caddy 只在初始化完成后显式启动，HTTPS login / Session / logout 成功，Secure / Strict Cookie 和精确 Host / Origin 合同保持不变；
5. 直接访问 Go server / PostgreSQL 的宿主端口失败，伪造转发 Header 不影响服务端识别的客户端边界；
6. Web build 缺失、origin / proxy / Secret 错误、数据库未就绪均显式失败，不出现 HTTP、fixture 或默认 credential fallback；
7. `./scripts/check-server.sh`、`./scripts/check-web.sh`、定向 Compose 新实例演练与 `./scripts/check-repo.sh` 通过。
