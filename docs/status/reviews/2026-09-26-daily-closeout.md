# 2026-09-26 提交回顾与文档收尾

审阅日期：2026-09-26。当日实现与设计范围：`1705a36..486a330`，共八笔本地提交。项目所有者要求先提交工作区，再回顾当天全部提交、按代码更新相关文档并记录明天事项，最后单独提交文档结束工作。

## 当日提交

| 提交 | 修改与用户结果 | 证据与边界 |
| --- | --- | --- |
| `1503511` `feat(document): 接通最小 Markdown 文档闭环` | Ticket 创建 Document，Project 分页发现，显式保存、冲突处理、历史恢复；migration 010、Project 权限、receipt、单一服务端安全解析与 Activity 版本 2 | [Document 记录](2026-09-26-markdown-document.md)。正式 Go / PostgreSQL / Web、备份恢复和隔离 HTTPS 流程有证据；原生中文 IME 暂缓 |
| `92f4a7f` `docs(design): 建立工作台与 Document 首轮视觉稿` | 明确 Radish 家族视觉方向，建立 Pen 原生设计源、共享组件与桌面 / 手机代表页 | [视觉方向](../../design/visual-direction.md)与[设计说明](../../design/workbench-v1.md)；静态设计不代表产品功能或浏览器验收 |
| `1f76d19` `docs(design): 优化工作台 v1.1 并记录视觉认可` | 根据 AFFiNE、AppFlowy、Mattermost 实际页面或官方配图优化层次，补桌面信息展开与手机信息页，形成 10 个画板 | 所有者认可静态方向；未下载第三方画面资产，也未引入其技术栈 |
| `b19b722` `feat(web): 落实共享工作台与 Document 视觉设计` | 正式 React 接入共享导航、手机抽屉、集中阅读 / 编辑和按需上下文，保留业务状态机与键盘交互 | [视觉落地记录](2026-09-26-workbench-ui.md)。实际 DOM 与虚构 API 浏览器检查；该批次不替代真实后端、原生 IME 或真机软键盘验收 |
| `a394ed6` `feat(ci-run): 接通正式读取接口与页面` | CI Run Session GET、安全 DTO、正式页面、已知 ID 入口和 Deployment 来源跳转 | [读取记录](2026-09-26-jenkins-next-slice.md)。当前 Component 权限与来源脱敏复用，不把构建成功显示为部署成功 |
| `0001a82` `feat(jenkins): 接通受控终态签名与有限重试交付` | 来源绑定、文件 Secret、HMAC / 重放窗口、终态映射、脱敏运行日志与有限重试 sender | [adapter 记录](2026-09-26-jenkins-adapter.md)。隔离 TLS、失败矩阵与真实数据库验证；不引入通用插件运行时或自动 Deployment |
| `be780b7` `test(jenkins): 完成隔离实例三态构建与交付验证` | 固定 controller / agent 镜像完成成功、失败、取消构建，finalized 快照经已有 sender 写入隔离 Nexus | [真实 Jenkins 记录](2026-09-26-real-jenkins-lab.md)。一次性接收与重复交付已验证；不是持久采集或团队业务实例部署 |
| `486a330` `feat(deployment): 接通 staging 显式记录与精确重试` | 成功 CI Run 选择已授权 staging 环境，确认外部终态并回读 Deployment；migration 011 扩展 receipt，未知结果可精确重试 | [staging 记录](2026-09-26-staging-deployment-recording.md)。真实数据库并发 / 撤权、备份恢复、桌面 / 手机和真实登录浏览器流程通过；配置与授权管理仍缺 |

上述提交之后，本记录所在文档提交仅修正说明与接续清单，没有继续业务开发。

## 代码与文档核对

以当日提交差异和当前源码核对了 Document 注册表、migration 010、关系读取、Activity projector、备份分类、正式 HTTP 装配与页面 allowlist；核对共享工作台及 Document 实现与设计映射；核对 Jenkins 来源 adapter、有限重试 sender、隔离 Compose 与采集入口；核对 staging service、权限事务、migration 011、公共端点和表单。

修正的文档漂移：

- 根 README、领域模型、决策基线和架构文档仍把 Document 描述为尚未实施；同步为正式实现已接通，保留原生 IME 与持续使用的未验收状态。
- 核心契约补齐 `document / doc_` 注册、Ticket 来源关系和已实现 HTTP / Web 边界；服务端总览同步 Activity 投影版本 2 与 Document 事件，避免与后文实现说明冲突。
- 决策基线及核心契约同步已确认的 ADR-0029 / 0030 / 0031，明确独立 Session 记录、当前授权和精确 receipt 重试；不新增决策，不改写被部分替代 ADR 的历史。
- 服务端 README 与 Jenkins 操作说明移除“真实 Jenkins 联调尚未验收”的过期断言，链接独立实验记录，保留持久采集、来源配置与安全 Audit 的缺口。
- Web README 更新正式页面清单、CI Run 已知 ID 入口和 staging 显式记录动作，消除“页面不提供部署写操作”与新实现的矛盾。
- 架构中的完整 Compose 证据明确不覆盖 migration 008–011 和后续入口；今天的隔离 browser fixture 与 Jenkins 实验不冒充当前业务镜像的完整自部署演练。
- 当前状态重排近期顺序，记录 9 月 27 日接续事项；文档索引加入当日收尾入口。修正 9 月 14 日收尾记录指向已删除标题的链接，保留原批次事实。

设计说明已区分静态稿、正式 React 和浏览器证据，无需改写设计源。Golden Path、路线图、插件原则和部署操作流程仍符合长期边界，不因今天若干切片完成而降低独立配置、完整关系、可移植导出、运维或持续使用门槛。历史验收记录中的“当时尚未完成”保持原批次含义，不逐条改成当前进度。

## 验证与结束边界

- 当日各实现批次的实际验证以对应记录为准。最终 staging 切片已有 Go race / vet、真实 PostgreSQL、备份恢复、Web 21 个文件 / 131 项测试、格式 / Lint / 类型 / 构建，以及隔离 HTTPS 浏览器证据。
- 本轮文档收尾只运行 `./scripts/check-repo.sh` 与 `git diff --check`，不重复启动数据库、浏览器或业务服务，不把历史验证记作本轮重跑。
- 今日新增正式 schema 为 migration 010 / 011；没有迁移业务实例。旧完整 Compose 演练不证明当前版本已部署通过。
- 各轮临时服务已按对应记录收尾。Jenkins 隔离目录中的持久 controller 数据与快照按用户要求保留；最后一轮 staging fixture 的容器、代理卷、状态与临时浏览器工件已清理，不扩大清理到历史资源。
- 所有提交只在本地 `dev`，没有 push、PR、发布或业务部署；不核验或修改远端规则。原生 IME 人工验收继续暂缓。
- 明天事项统一维护在[当前状态](../current.md#近期执行顺序)，以最小配置与显式环境授权管理的范围提案为首项。本轮结束，不继续实施下一切片，也不自动启动次日任务。
