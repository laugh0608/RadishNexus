# 2026-09-26 CI Run 正式读取与 Jenkins 后续接入

基线：`b19b722`。已按所有者要求提交共享工作台与 Document 页面改版，未 push。所有者随后明确确认先实施 CI Run 读取切片；本记录区分已实施读取与尚未开始的 Jenkins 来源接入。

## 已有与缺口

| 环节 | 已有实现 | 本次判断 |
| --- | --- | --- |
| 外部来源 → 核心 | `VerifiedJenkinsDelivery`、终态映射后的 service 入口 | HTTP 来源认证、Secret、重放与失败审计尚未定义，不能直接将原始 webhook 标为 verified |
| 记录事务 | 不可变 delivery receipt、CI Run、事件、Outbox、正常 Activity 更新 | 复用现有 service 与 PostgreSQL 事务，不能另建旁路写表 |
| 用户读取 | Component 作用域权限、内部 `GetNexusView`、静态 Web 代表组件 | 本轮按已接受 ADR-0029 接通正式 HTTP DTO / 路由、Web canonical 接线及 Deployment 来源跳转 |
| staging 记录 | 显式 Environment 授权与终态 Deployment service、只读页面 | 公共写入口、外部事实确认与关联发现仍缺；CI Run 成功不创建 Deployment |

源码入口：[CI Run service](../../../server/internal/goldenpath/ci_run.go)、[内部 query](../../../server/internal/goldenpath/postgres/nexus_view.go)、[现有 Deployment transport](../../../server/internal/platform/httptransport/deployment_nexus_view.go)、[Web model](../../../web/src/nexus-view/model.ts)。

## 是否现在安装 Jenkins

读取切片的开发与内部契约测试不需要 Jenkins。真实来源联调需要一个可控实例：如已有独立测试实例可复用；没有时再准备隔离实例。Jenkins 不是 Nexus 核心启动依赖，不把它加入默认自部署拓扑。

建议部署范围仅为测试用 controller、独立构建 agent、专用测试 job 和测试凭据。官方 Docker 文档提供容器路径；controller isolation 文档建议构建移至 agent。具体 LTS、JDK、镜像 digest、插件锁定、许可证与供应链在安装授权时核验，不在此以浮动版本建立可执行默认值。

外部参考（2026-09-26 查阅）：[Jenkins Docker 安装](https://www.jenkins.io/doc/book/installing/docker/)、[Controller 隔离](https://www.jenkins.io/doc/book/security/controller-isolation/)、[Credentials Binding](https://www.jenkins.io/doc/pipeline/steps/credentials-binding/)。凭据遮罩只是降低误输出风险；有权修改 Pipeline 的人应视为能够使用其绑定凭据。

安装前需确定目标机器、已有实例情况与网络可达性。拟采用只向本机或受控测试网络开放端口、固定版本、专用数据目录和账号；不挂载宿主 Docker socket、不使用生产仓库 Secret。结束后停止测试容器并撤销临时凭据；删除测试卷 / 数据须在建环境时明确授权或另行确认。不会静默修改全局证书、系统服务或正式 Jenkins 配置。

## 推进顺序

1. 已确认并实施 [ADR-0029](../../adr/0029-session-scoped-ci-run-nexus-view.md)：实现 CI Run 的正式只读接口、页面与已有 Deployment 来源跳转；复用权限，不增加数据模型或依赖。
2. 冻结 Jenkins 来源 adapter 协议：受控 source 到 Workspace / Component 的绑定、凭据装配与轮换、签名与时间窗口、delivery / external run 身份、终态映射、体积与超时限制、重复 / 冲突、失败审计及有界重试。公开 endpoint 与验收发送方成套设计，不把 Jenkins 泛称 webhook 当作现成统一签名协议。
3. adapter 合同与测试就绪后，再选择已有实例或安装隔离 Jenkins，执行真实成功 / 失败 / 取消、重复交付、坏签名、过期与超时场景；接入失败不阻断讨论与文档业务。
4. 独立完成 staging 终态事实的显式记录入口及后续关联。此流程不执行生产部署，不把 CI 成功当部署完成。

## 本轮实现与验证

- Go 新增只读 handler，复用 Workspace Session 解析、Component 授权、错误与缓存合同；正式 server、Web 路径 allowlist 和现有浏览器 fixture 装配同一入口。公开 DTO 显式排除来源、凭据和内部投影，检测字段 / 事件 / 引用 / 时间漂移。
- React 新增 CI Run adapter 与页面，复用已有展示模型和工作台；支持空开始时间、重读 / 重试、焦点复权、请求取消及迟到保护。Deployment 提供来源跳转，首页次级工具支持 `cir_`；Component 无页面时仅展示名称与引用。
- 核对源码后修正提议中的时间口径：已有 `ci-run.recorded` 发生时间是构建完成时间，读取端延续该语义；`recorded_at` 仍是 Nexus 接收记录时间，没有改写领域事件。
- `./scripts/check-server.sh` 通过：race 测试、vet、无修改 tidy 与模块校验。
- `./scripts/check-server-postgres.sh` 通过：固定镜像的一次性容器，正常 CI Run service 写入 → 重复 delivery → 正式 Session HTTP 读取；不运行投影重建。验证唯一 Activity、空开始时间、来源脱敏、跨 Workspace / 未知对象及 membership 暂停后的 404。
- Web 检查覆盖 19 个测试文件、121 项测试，以及 Prettier、Oxlint、严格 TypeScript、生产构建与 152 个锁定 package 的依赖基线；包含公共形状漂移、敏感字段注入、错误响应、失权与迟到请求、来源跳转和 ID 校验。

- 隔离 Chromium 使用虚构 API 响应验证正式 React 页面：实际点击 Deployment 来源链接进入 CI Run；1440 / 1024 / 768 / 390 / 320px 无水平溢出；Enter 刷新和焦点重读后的 404 清空通过。已查看桌面与手机截图，文件位于忽略目录 `output/playwright/ci-run-*.png`。
- `./scripts/check-repo.sh` 与 `git diff --check` 通过。最后的 DTO 诊断文案调整后补跑 CI Run / WebApp 定向 Go 测试与 HTTP 层 vet，通过。
- 一次性数据库容器已自动清理，临时 Vite 已停止，隔离浏览器已关闭。前次界面工作提交为 `b19b722`；本次 CI Run 读取改动保留在工作区，未追加提交或 push。

浏览器 mock 不能证明 Jenkins 来源认证或完整 browser → HTTPS → PostgreSQL 链路；本轮数据库 → Session HTTP 与浏览器 → React 分别验证。没有真实 Jenkins 构建送达、真机软键盘或完整读屏验收。Document 的原生中文 IME 人工复核仍暂缓。

安装 Jenkins 不属于本轮范围；没有新依赖、数据库迁移、权限扩展或远程状态写入。下一来源协议仍需明确确认，不能把本轮只读端点授权扩展为 webhook、凭据或部署授权。
