# 工作台与 Document 代表页设计 v1.1

日期：2026-09-26。实现参考基线：`1503511`；上一版设计基线：`92f4a7f`。继续维护同一个 `.pen` 文件，文件名中的 `v1` 表示本轮设计系列。

本稿落实已确认的[视觉方向](visual-direction.md)，用于评审共享工作台和最小 Markdown Document 的阅读、编辑及关键失败状态。项目所有者已认可本次 v1.1 并要求提交；本稿作为后续页面实施的视觉参考，未替换正式 React 页面，也不代表功能、完整可访问性或原生 IME 验收通过。

## 设计源与参考方法

唯一可编辑源为 [radishnexus-workbench-v1.pen](radishnexus-workbench-v1.pen)。使用 Pen 原生 MCP 读取、编辑、截图和导出；不手写或解析 `.pen` 文件。`previews/` 是该源的静态 PNG 导出，按节点 ID 命名；修改画板后重新导出对应预览，避免两套设计漂移。

按所有者指示，参考了三个兄弟项目的以下材料，只借鉴对 Nexus 有实际价值的工作方法：

| 兄弟项目材料（各仓库相对路径） | 本轮采用的方法 |
| --- | --- |
| Radish：`docs/frontend/pencil-representative-page-workflow.md` | 新共享框架先做桌面 / 手机代表页；冲突、恢复和异常用局部状态表达；设计评审与运行态验收分开 |
| RadishFlow：`docs/architecture/studio-ui-topic-plan.md` 及其设计维护说明 | 维护单一活动设计源，记录源文件、画板和预览映射，使用原生工具操作 |
| RadishMind：`docs/radishmind-ui-design-spec.md`、`docs/ui-addendum.md`、`docs/designs/radishmind-web-family-ui-v1.pen` | 观察家族工作面落地方式，先整理共享变量与组件，再组合代表页 |

兄弟仓库保持只读；Nexus 文件从空白画布创建，未复制兄弟产品页面、运行时代码或参考产品品牌资产。视觉基线来自 RadishX 家族规范，具体来源与版本见视觉方向文档。

## v1.1 的画面参考与调整

本轮参考上一轮已经实际查看的界面材料，区分运行画面与官方配图，不把官网文案或静态图片当成功能体验证据：

| 参考 | 观察范围 | 本轮转译 |
| --- | --- | --- |
| [AFFiNE 公开体验页](https://app.affine.pro/) | 公开 Getting Started 的实际编辑页面：左侧分组、轻量顶栏、正文层级；观察时浏览器宽度有限，不据此推断完整响应式行为 | 桌面顶栏压到 56px，侧栏改为 224px；收起上下文后正文保持 760px 阅读栏 |
| [AppFlowy 编辑器文章配图](https://appflowy.com/blog/how-we-built-a-highly-customizable-rich-text-editor-for-flutter) | 官方带演示标注的编辑器配图：标题、属性、正文与局部菜单；不声称亲自体验其编辑流程 | 标题、版本和来源形成清楚层次；编辑标题减少外框，预览作为次级操作；不增加块编辑器 |
| [Mattermost 讨论串文档](https://docs.mattermost.com/end-user-guide/collaborate/organize-conversations) | 官方 Threads View 截图：导航、列表和讨论详情分区；截图具体客户端版本未核验 | 将信息按任务分层：阅读先聚焦正文，来源与活动按需展开；不照搬 Threads 收件箱、未读或通知 |

相较首稿，阅读页不再常驻右侧 Activity；来源 Ticket 仍保留在标题下，可直接追溯。版本使用中性标签，避免把普通版本信息画成成功告警。编辑页使用 800px 集中的标题、Markdown 输入和预览区，保留明确的未保存、放弃和保存操作。新增桌面信息展开与手机文档信息两张画板；手机账户、返回等图标补齐 44px 触控容器。

上述观察用于 Nexus 自身设计判断，保留 Radish 家族语义色与领域合同；未下载或纳入第三方画面资产。

## 画板与预览

| 画板 | 节点 | 尺寸 | 预览 |
| --- | --- | --- | --- |
| 共享组件与视觉基线 | `bi8Au` | 1440 × 460 | [组件](previews/bi8Au.png) |
| Document 阅读 · Desktop | `R2BnY` | 1440 × 900 | [桌面阅读](previews/R2BnY.png) |
| Document 编辑 · Desktop | `YbAh9` | 1440 × 900 | [桌面编辑](previews/YbAh9.png) |
| Document 阅读 · Mobile | `qnAN3` | 390 × 844 | [手机阅读](previews/qnAN3.png) |
| Document 编辑 · Mobile | `QVfn9` | 390 × 844 | [手机编辑](previews/QVfn9.png) |
| 冲突与恢复 · 局部状态 | `m1Mk9X` | 1440 × 850 | [比较与恢复](previews/m1Mk9X.png) |
| Document 冲突 · Mobile | `z2KNCZ` | 390 × 844 | [手机冲突](previews/z2KNCZ.png) |
| 请求异常与权限变化 · 局部状态 | `t9nRJ` | 1440 × 550 | [异常状态](previews/t9nRJ.png) |
| Document 阅读 · 信息展开 Desktop | `kMcDd` | 1440 × 900 | [桌面信息展开](previews/kMcDd.png) |
| Document 信息 · Mobile | `U8jKVq` | 390 × 844 | [手机文档信息](previews/U8jKVq.png) |

文档标题、正文、用户 ID 与活动记录均为设计示例，不来自真实成员数据。手机正文采用代表性节选，正式实现不得按屏宽删减内容。局部状态画板是交互状态参考，不是同时展示给用户的完整页面。

## 布局与组件

桌面使用 224px 侧栏与 56px 顶栏；默认阅读态收起信息区，760px 内容栏居中排版。文档信息展开时显示 288px 上下文区，正文在剩余宽度内重排。来源 Ticket 保留标题下的直接入口，Project 可见性与版本活动在信息区展示；编辑态集中显示 800px 编辑栏。侧栏表达 Workspace / Project / Document 的层级，不增加未读、全局搜索或 AI 功能。

手机保留品牌、返回和当前操作；导航保留菜单入口，来源 / 活动从标题附近的文档信息入口进入单独信息页，返回后保留阅读位置。阅读和编辑分别编排；冲突比较顺序展示本地草稿与最新版本，主要操作目标至少 44px。1440px 与 390px 是本轮代表尺寸，中间宽度、导航抽屉、信息页返回位置和软键盘避让仍需实现阶段验证。桌面宽度不足时先收起信息栏，正文改为可用宽度内的流式排版；固定阅读宽度是上限，不能导致横向滚动。

源文件包含 16 个语义颜色变量、1 个字体变量，以及主按钮、次按钮、状态标签和导航项 4 个可复用组件。暖纸底与灰玉身份色沿用家族气质，墨蓝集中表达主操作；状态同时使用文案或图标，不单靠颜色。

设计画布使用 Pen 可用的 `Noto Sans SC` 排版；正式 Web 仍按家族系统无衬线字体栈实施，不由此引入字体依赖或网络字体。画布使用 Lucide 图标，运行时图标方案单独评估，不自动安装组件库或图标库。

## 与真实行为的映射

业务依据为 [ADR-0028](../adr/0028-minimal-markdown-document.md)；正式实现入口为 [DocumentPage.tsx](../../web/src/document/DocumentPage.tsx)、[Document API](../../web/src/document/api.ts) 和 [MarkdownView.tsx](../../web/src/document/MarkdownView.tsx)。工作台改造涉及 [App.tsx](../../web/src/App.tsx)、[AppHeader.tsx](../../web/src/AppHeader.tsx) 与 [WorkspaceHome.tsx](../../web/src/workspace/WorkspaceHome.tsx)，本轮没有修改这些文件。

| 场景 | 设计表达与实现约束 |
| --- | --- |
| 阅读与上下文 | 正文、当前 revision、来源与 Activity 保持可发现；链接不授予权限 |
| 编辑与预览 | 标题和 Markdown 原文分开输入，保留未保存提示、显式预览和显式保存；不引入自动保存或块编辑 |
| 保存冲突 | 保留本地草稿，读取最新版本并比较；重新应用进入可编辑草稿，之后仍须显式保存，不自动覆盖 |
| 历史恢复 | 展示所选历史与当前版本，恢复需要明确确认；成功后追加新 revision，保留历史 |
| 写入结果未知 | 保留原操作和幂等上下文，冻结冲突编辑入口，只能精确重试原操作；不显示虚假保存成功 |
| 失权或不可发现 | 清除正文、草稿、历史及来源上下文，呈现不可用；不泄露目标是否存在 |
| 内容无法安全展示 | 保留可授权读取的原文与原因，不用空白冒充成功；不允许恢复不可安全展示的历史版本 |

原生菜单、抽屉、加载过程、hover / focus / disabled 的完整组件状态、长标题与长文滚动、深色主题没有全部展开出稿。本轮提供首批代表页与关键失败状态，不宣称全产品设计系统已经完备。Channel / Thread、Project 文档列表、Ticket / Decision 后续沿同一框架逐批验证。

## 本轮静态检查与评审边界

- Pen 原生布局检查覆盖 446 个展开节点：无裁切报告、未命名节点、残留 placeholder、缺少文字填色或小于 12px 的文字。
- 已查看本轮修改的阅读 / 编辑、信息展开及手机冲突原生截图，并从同一源导出 10 张 PNG，核对桌面 / 手机代表页与局部状态的排版。
- 使用 sRGB 相对亮度公式计算主要文字配对：正文 / 内容面 13.97:1，次文字 / 内容面 6.34:1，品牌 / 应用底 5.10:1，主按钮文字 / 墨蓝 6.84:1，成功、警告、危险文字 / 对应浅底分别为 4.73:1、5.36:1、5.32:1。这仅覆盖所列静态配色，不代表焦点、控件边界或完整 WCAG 验收。
- 所有者已认可本轮静态视觉稿；真实 DOM 响应式验证、键盘 / 读屏验证、完整深色映射及原生输入法复核尚未完成。

下一步建议以共享工作台与 Document 为有限实施切片，将已认可稿落实到现有 React 组件，再核对真实内容、响应式和操作状态。实际功能与原生 IME 验收独立记录，人工验收是否恢复以[当前状态](../status/current.md)为准。
