# 最小 Markdown Document 实施与验收

日期：2026-09-26。基线：本地 `dev` 工作树；实施验证期间未 fetch、push、发布或部署；项目所有者在验收汇报后要求提交本轮工作区改动。实施合同为已接受的 [ADR-0028](../../adr/0028-minimal-markdown-document.md)，当前剩余事项回到[当前状态](../current.md)维护。

## 交付范围

从正式 Ticket 创建 Project 可见 Document，建立首版、来源 EntityLink、精确 receipt、领域事件、Outbox 与正常 Activity；Project 提供分页发现。Web 可阅读、编辑、服务端预览、显式保存、比较冲突并重新应用，以及查看历史、确认恢复为新版本。没有新增富文本、CRDT、图片、附件、浏览器持久化或独立权限模型。

migration 010 固定 Document 身份、同 Workspace / Project 来源与不可变 revision；保存复用 Project 权限锁，追加版本前核对当前基线。重复请求仍重新授权，相同输入返回原 applied revision，变化重放冲突。备份分类含 Document 与 revision，恢复保留旧 receipt；Activity 投影版本 2 包含新事件，失败回滚及全量重建已验证。

实现和路由见 [server README](../../../server/README.md#最小-markdown-document)、[Web README](../../../web/README.md#最小-markdown-document)。

## 依赖与解析边界

唯一新增 Go 依赖为 `github.com/yuin/goldmark v1.8.6`；未新增 Web 依赖。已读取精确模块包的 LICENSE 与 go.mod：MIT、Go 1.22，模块无传递依赖要求；完整许可声明保留于 [THIRD_PARTY_NOTICES](../../../server/THIRD_PARTY_NOTICES.md)。

- 模块 checksum：`h1:d0VcaP1sx9GkFVkoW+KtggpGi2KZ965i14b0+bDQST4=`。
- go.mod checksum：`h1:ip/1k0VRfGynBgxOz0yCqHrbZXhcjxyuS66Brc7iBKg=`。
- Go 模块校验和 `go mod tidy -diff` 通过。读取官方 Go 漏洞数据库的模块索引，匹配到 [GO-2026-5320](https://pkg.go.dev/vuln/GO-2026-5320)，影响 `< 1.7.17` 的 HTML renderer；本次固定版本不在该公告影响范围。未安装或运行 `govulncheck`，这不是全仓库可达性扫描，也不声称不存在未知漏洞。

服务端只输出封闭展示树。原文除换行归一化外不改写；拒绝无效 UTF-8、未配对 surrogate、NUL、超限、HTML、图片和危险 URL。React 使用固定元素与文本输出，未知节点失败，不注入 parser HTML。空文档、中文 / emoji、转义实体、代码、列表、引用、硬换行、危险链接和恶意长输入均有 corpus。

初始压力检查发现仅限制投影仍无法限制 parser 前置成本，因此在 goldmark block / inline hook 上增加调用预算和深度边界；只捕获本实现私有预算中断，其他 panic 不吞掉。单次最大正文基准在 Apple M5 上约为：普通文本 1.2ms / 1MB，分隔符 1.5ms / 4.8MB，重复方括号 2.1ms / 6.5MB，深嵌套拒绝 0.05ms / 0.3MB。这是本机单次分配观察，不是并发容量保证；超出预算会显式拒绝。

## 自动化证据

| 已执行检查 | 结果与主要范围 |
| --- | --- |
| `./scripts/check-server.sh` | Go 格式、静态检查、race 测试、模块一致性通过 |
| `./scripts/check-server-postgres.sh` | 真实 PostgreSQL 通过：创建链、列表 / 历史分页、重复与变化重放、并行保存单胜者、恢复、不可变约束、跨 Workspace、viewer / 归档 / 撤权、失败回滚和 Activity 重建 |
| `./scripts/check-server-backup-restore.sh` | 同 major 空目标恢复通过：首版、保存、恢复、当前指针、历史与原 receipt 均保留 |
| `./scripts/check-web.sh` | 17 个文件 / 109 项测试通过；格式、Lint、TypeScript、production build 和 152 个锁定包检查通过 |
| 定向 HTTP / Markdown 测试 | 最终无损 Unicode 拒绝、有效 emoji surrogate 和字面转义检查通过 |
| 仓库检查与 `git diff --check` | 文本、链接与差异卫生通过 |

数据库竞态测试先持有 Project 修改事务，再让保存等待锁；提交归档或撤权后，保存按最新权限失败，版本号不变。Web 测试覆盖冲突保留 / 明确重新应用、网络歧义精确重试、撤权清理、迟到读取、创建可见性提示和封闭节点 / 安全链接校验。

## 隔离浏览器证据

使用仓库一次性 PostgreSQL + Go + Caddy fixture 和已安装 Chrome，数据全部为测试内容。Playwright 隔离 context 使用 `ignoreHTTPSErrors` 访问本地 HTTPS；未向系统钥匙串安装证书。因此本轮证明 HTTPS 业务链可运行，不证明系统信任链或生产证书部署正确。

1. 使用正式页面在 Thread 提出 Decision、人工接受并创建 Ticket；从 Ticket 预览及创建文档。刷新 Ticket 后可见 Document 关系，文档可返回来源 Ticket，正常 Activity 无需手工重建。
2. 两个标签页从版本 1 编辑，第二页保存版本 2；第一页读取最新后保留原草稿，展示基线 1 / 当前 2 并禁止直接保存。明确重新应用后保存为版本 3。服务端直接 409 路径另由 HTTP / PostgreSQL / Web 测试覆盖。
3. 阅读历史版本 1，确认恢复后生成版本 4；刷新仍显示原文，Activity 保留 1～4，并标明恢复来源 1。Project 文档列表显示同一文档的当前版本。
4. 检查 1280px 桌面和 390px 手机截图，手机文档宽度与视口同为 390px，无水平溢出。截图为忽略目录中的 `output/playwright/document-desktop.png` 和 `document-mobile.png`，不是提交工件。
5. 打开草稿、预览与历史后，在临时数据库暂停当前测试成员，再主动读取最新；页面仅留下不可用提示，textarea 数量为 0，草稿文本与历史均消失。该步骤验证撤权消费，不代表完成管理员成员治理 UI。

最终仅修正文档关联入口文案并增加 HTTP Unicode 校验，随后重新通过 Web 检查与定向 Go 测试；这些改动后未重跑完整浏览器链。测试 Chrome 已关闭，fixture 正常退出并清理 PostgreSQL、Caddy、临时 volume 与状态文件。

## 尚未完成的验收

- 正式 textarea 的 **macOS 原生中文 IME**：拼音组合、候选确认、Enter 不误保存、预览后继续输入、明确保存后刷新回读。Playwright 填充中文与已有编辑器实验不能替代此项；项目所有者已明确暂缓人工验收，此项保持未验收，不标记通过。
- 真实团队持续使用、并发容量、生产证书 / Compose 部署与完整 Golden Path；本轮没有在真实环境迁移数据库。
- 可移植 `.nexus` 导出 / 导入、受控脱敏和数据生命周期仍是独立合同与实现，不因整库恢复通过而完成。

人工验收已暂缓，页面设计及 pen.dev 工具正在讨论，尚未决定采用或安装。后续业务顺位仍为真实 Jenkins 来源验证与 staging 记录链。不得把这个局部切片描述为完整 M1 或团队试用已通过。
