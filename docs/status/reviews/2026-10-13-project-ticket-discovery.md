# 2026-10-13 Project 工单只读发现

范围：[ADR-0038](../../adr/0038-project-ticket-discovery.md)。所有者确认提案并授权实现、必要验证和文档同步；提案先提交为 `a2c6c3f`，本轮实现基于该提交。Ticket 状态流转不纳入，真实 Jenkins 的 ADR-0035 / ADR-0037 B 继续暂缓。

## 实际交付

- 新增 `GET /api/v1/workspaces/{workspace_id}/projects/{project_id}/tickets`，复用 discovery service、当前 Project 权限与 PostgreSQL repeatable-read 事务；来源 Thread 的权限独立判断。
- 严格最小 DTO、只接受 open、默认 25 / 最大 50 的稳定 ID keyset 分页。cursor 绑定 Workspace / Project / Ticket 列表，不兼容 Component 反向列表；异常数据整页失败，不过滤或伪造状态。
- 首页 Project 增加工单入口，canonical 列表打开既有 Ticket 详情；详情可返回所属 Project 列表。支持直接地址、重新登录、刷新和页面恢复。
- Project 标题与列表共用加载代次；翻页、刷新、恢复、作用域变化或失败清空旧内容，请求取消与迟到响应保护不保留过期权限下的数据。
- 不新增业务字段、migration、依赖、事件或写命令，不改变已有 Ticket 创建、来源、组件关联和文档合同。

## 自动化与查询证据

| 检查 | 实际结果 |
| --- | --- |
| `./scripts/check-server.sh` | Go race、vet、模块图与模块校验通过；沙箱内 TLS 测试无法绑定端口，按审批流程重跑后通过 |
| `GOFLAGS=-v ./scripts/check-server-postgres.sh` | 全部真实 PostgreSQL 集成包通过，包括新列表权限、分页、真实 Session HTTP 与既有回归 |
| `./scripts/check-web.sh` | 30 个测试文件、172 个测试通过；格式、Lint、类型、production build 和 152 个锁定 package 检查通过 |
| `./scripts/check-repo.sh`、`git diff --check` | 文档同步后通过 |

数据库新增测试通过正式 Message / Thread / Decision / Ticket 命令建立工单，覆盖空页、恰好 25、26、多页、limit 1 / 50、不存在的合法边界和页间新增；固定集合遍历完整、无重复，并逐项核对既有详情可读性。覆盖普通读取角色、公开 Project 移除角色仍可读、restricted 失权、owner 不穿透、归档、跨 Workspace、不存在、暂停 / 移除 Workspace 成员、恢复授权及撤销 Session / 禁用账户。私密 Thread 不可读不阻止 Project Ticket 读取；响应不含来源 ID。专门将测试记录改成 done 后确认 HTTP 整页 500，这只是漂移探针，不是状态流转证据。

HTTP 定向测试覆盖 Session、Host / proxy、方法与 Allow、缓存头、非法 query / cursor、跨作用域 / 列表复用及非法 DTO 不返回部分结果。Web 覆盖入口、App 直接地址与登录恢复、分页、归档、空态、重试、Scope 变化、迟到响应、Session 失效，以及 focus / visibilitychange / pageshow / popstate 重读与旧标题、条目共同清理。

代表性数据为两个 Project、共 1,200 个正式命令创建的 Ticket，目标 Project 200 个、单页返回 25 个。一次本地测量为 6 次 SQL 调用（含事务与权限读取）、约 2.30 ms；`EXPLAIN (ANALYZE, BUFFERS)` 使用既有 `tickets_workspace_id_id_key`，查询执行约 0.032 ms、142 个 shared hit block、无 shared read。该证据支持本轮不增加索引；不证明更大数据规模、冷缓存、多用户或生产延迟。没有 schema / 备份合同变更，因此未另跑升级与备份恢复演练，不借旧证据宣称本轮完成这些演练。

首次集成运行因测试撤回 Workspace 成员前未删除其 Project membership 触发外键保护；修正测试顺序后全套通过。首次 App 测试使用单元素断言匹配桌面 / 手机两个同名 Workspace 节点；改为核对现有响应式节点后通过，未改产品以绕过检查。

## 隔离 HTTPS 浏览器

使用现有 Chrome、Playwright CLI 和 `RADISHNEXUS_BROWSER_FOUNDATION=1 ./scripts/run-authenticated-web-browser-fixture.sh`，只操作临时 Go / PostgreSQL / Caddy 环境与合成账户。使用本轮 production build；没有 mock API、预置最终 Ticket、系统证书修改、依赖安装、真实 Jenkins 或业务实例操作。

1. 从正式 bootstrap 的空业务工作区，通过页面创建 Team、restricted Project 和 Channel，再由 Message → restricted Thread → Proposed Decision → 显式人工 Accepted → Ticket 建立主工单；页面创建关联 Document。主工单使用长中文标题。
2. 通过正式邀请兑换与成员配置 HTTP 接口建立普通 viewer；未授予来源 Thread 成员资格。另用同一正式 Ticket 创建端点准备 26 个分页样本，总计 27 个工单。
3. 普通成员邮箱登录，从首页选择 Project、以“查看项目工单”链接进入，无需输入 ID；第一页 25 项，第二页 2 项且下一页禁用，上一页正常。该入口以键盘 Enter 激活。退出后再次登录，恢复原 canonical 列表。
4. 从列表打开长标题工单，沿来源进入 Decision，只显示“受限 evidence”，不暴露私密 Thread 标题或 ID；关联 Document 可打开并读取版本 1 正文。通过 owner 的正式接口创建并关联 Component，普通成员可从 Ticket 打开组件，再从组件反向列表返回该工单。组件测试 key 首次因使用大写被正式校验拒绝，改为既有小写合同后创建成功。
5. 检查桌面完整截图和 390×844 手机截图，长标题换行、分页按钮可见；手机 `scrollWidth = innerWidth = 390`。没有横向溢出。截图仅为合成验收数据，保存在 Git 忽略的 `output/playwright/2026-10-13-ticket-discovery-*.png`。
6. 在独立 owner 浏览器 context 中通过正式成员 DELETE 接口撤回 restricted Project viewer；普通成员刷新后，项目名称、工单、分页均清空，只显示不可访问提示。重新授权后能重新读取，再进入 Ticket，第二次撤权后浏览器后退到列表仍重新授权并清空，未恢复旧页面内容。预期 404 对应浏览器网络错误，无额外运行时错误。

验收完成后关闭隔离浏览器，触发 fixture 正常停止，测试退出为 PASS；脚本清理临时数据库、代理、卷与证书目录。浏览器实际后退覆盖当前 Chrome 路径，BFCache 的 persisted 事件分支另由 Web 自动化覆盖；不把该自动化称为跨浏览器缓存实现验收。

## 完成线与接续

本切片达到“普通成员可从正式 Project 入口找回已有 Ticket，当前权限、分页与页面恢复通过验证”。不代表完整 Golden Path、真实团队持续使用、原生中文 IME、Ticket 状态流转或 Jenkins SCM 采集完成。

下一项仅按当前状态准备 Ticket 最小状态流转的独立提案，先冻结转换、重新打开、并发和事件历史，再同步列表与详情。没有 push、PR、发布或部署；所有者随后明确授权提交本轮实现与验收文档；提案与实现分开提交。
