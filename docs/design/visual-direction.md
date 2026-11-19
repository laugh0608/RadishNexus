# RadishNexus 视觉方向与页面参考

建立：2026-09-26；参考清单更新：2026-11-19。

状态：视觉语言以 Radish 家族规范为基础。共享工作台与 Document 的首轮设计参考了 AFFiNE、Mattermost、AppFlowy，设计稿与取舍见[工作台代表页设计](workbench-v1.md)。Zulip、Huly、RustChat 列入后续页面设计的参考清单。

## 参考来源与职责

| 来源 | 参考职责 | Nexus 的转译建议 |
| --- | --- | --- |
| RadishX `docs/design/family-ui/` | 家族气质、语义色、字体、组件形态、密度与响应式 | 以 Workbench 工作面为基础，温润、克制、近白暖纸底、墨色正文、低饱和印色 |
| [AFFiNE Docs](https://affine.pro/pagedoc) | 文档创作、阅读与编辑界面 | Document 内容区、阅读宽度、编辑工具条、文档列表与设置分区 |
| [Mattermost Channels](https://mattermost.com/channels/) 与[讨论串说明](https://docs.mattermost.com/end-user-guide/collaborate/organize-conversations) | 频道与讨论的持续上下文 | Channel 导航、Message 信息密度、Thread 展开和返回频道的路径 |
| [AppFlowy](https://appflowy.com/) | 工作区、文档与项目内容的组织 | Workspace 切换、侧栏分组、Project 内容发现、列表与详情的关系 |
| [Zulip 话题列表](https://zulip.com/help/list-of-topics) | 在同一频道内区分和找回并行讨论 | 评估成员如何从 Channel 找到已有讨论、进入对应 Thread；不因此新增 Topic 层级 |
| [Huly 关联事项](https://docs.huly.io/task-tracking/related-issues/) | 从事项详情发现关联对象 | 评估 Ticket 详情如何用 EntityLink 区分来源 Decision 和相关 Document，让成员看清关系后再跳转 |
| [RustChat](https://rustchat.io/) | 技术团队在聊天中分享代码片段和日志 | 评估 Thread 中代码、日志摘录与普通回复的排版，让成员能辨认并找回提供证据的原消息 |

表中转译内容是 Nexus 的设计建议。新增三项仅核对了官方资料，尚未实测交互，也不代表已经采纳其功能或技术栈。各页面仍共享一套 Nexus 导航、组件和状态表达，不混用外部品牌语言。

本次读取的家族规范为 `v26.7.3`，RadishX 本地提交 `f39a3c9`，该仓库工作区干净。依据包括 `README.md`、`01-principles.md`、`02-color.md`、`03-typography.md`、`04-space-shape-elevation.md`、`06-components.md`、`07-layout-platforms.md` 和 `references.md`；观察了参考库中的 AFFiNE 中文设置页与文档账户 / 工作区菜单。具体来源均位于 RadishX 的 `docs/design/family-ui/`，未复制截图、品牌资产或运行时代码到 Nexus。

RadishX 的 `docs/design/visual-guidelines.md` 属于官网 Brand 展示面，家族规则与官网应用须区分。Nexus 使用家族工作面原则；官网的大标题、首屏留白、角色舞台与装饰纹样不直接用于日常编辑和协作。

## 建议的视觉基线

目标是适合长期阅读、讨论和执行的工作台，同时保留 Radish 家族温润的辨识度。

| 维度 | 首轮设计建议 |
| --- | --- |
| 底色 | 从 Workbench 的应用底 `#f7f4ee`、内容面 `#fffdf8`、次级面 `#f3eee5` 起步，用明度差组织区域 |
| 品牌与操作 | 先试家族参考方案：灰玉 `#5d6c57` 表达身份，墨蓝 `#435c74` 表达主操作与链接；最终分工在样稿中评审 |
| 状态 | success / warning / danger / info / neutral 独立于品牌，始终保留文字或图标，明确草稿、未保存、冲突、只读和失败 |
| 字体 | 系统无衬线；代码和 ID 用等宽。界面正文参考 14px，长文阅读可试 16px 并单独复核中文行高与行宽；不新增网络字体 |
| 密度 | 4px 间距网格；列表和表单紧凑、正文舒展；避免大标题和空白挤占首屏有效内容 |
| 形态 | 工作面控件约 8px 圆角、面板不超过 12px；弱边界、轻浮层，减少卡片嵌套 |
| 主题 | 同步考虑浅色与深色的语义映射；首轮出稿需检查文字、焦点和状态对比度，不宣称当前已支持完整新主题 |
| 响应式 | 桌面与手机分别编排；移动触控目标至少 44px，导航转抽屉，辅助信息按需展开 |

上述颜色是家族参考值，不是已冻结的 Nexus 独立配色。实际文字 / 背景组合仍须核验对比度，不能因来自 token 就视为已通过。品牌气质通过颜色、排版与细节表达，正文和编辑区保持干净。

## 页面结构建议

### 统一工作台

桌面先探索「左侧工作区 / Project 导航 + 中央主内容 + 按需展开的上下文栏」。Project 与 Channel 的关系依既有领域模型呈现；Workspace 切换有稳定位置。顶栏承载对象位置与当前操作，避免每个页面重复大幅品牌 Header 和页脚。

上下文栏用于当前对象的来源、关系、状态与 Activity。它可折叠，不能让长文或讨论区过窄；平板和手机转为抽屉、标签页或单独详情。跨对象导航仍保留 canonical 地址与清楚的返回路径。

### Document

阅读态以标题、正文和当前版本为主体，来源 Ticket 与版本历史可发现但不抢正文。编辑态突出未保存状态、预览与显式保存。冲突比较同时保留本地草稿和服务端版本；恢复须说明追加新版本，并保留明确确认。

参考 AFFiNE 的创作体验，不将块编辑、白板、图片、实时协作或自动保存带入本轮。现有 Markdown 与 revision 合同保持不变。

### Channel / Thread

参考 Mattermost 的频道与讨论分工，保持作者、时间、消息内容和回复入口清晰。Thread 与其产生的 Decision、Ticket 之间需要可追溯的入口，不能仅靠聊天正文链接。

未读、通知、全局搜索和完整 Thread 列表尚无对应正式闭环，首轮设计不得把它们画成已经可执行的功能。若探索远期方案，应与现阶段交付稿分开。

### Project / Ticket / Decision

参考 AppFlowy 的工作区与内容组织节奏。Project 文档列表适合轻量行式信息；Ticket 和 Decision 详情突出领域状态、来源及下一步有效操作，辅助元信息退后一级。

Project、Document、Ticket 与 Decision 保持各自对象语义。Decision 的提案与人工确认不能被通用文档的编辑状态代替；来源和权限语义以[领域模型](../domain-model.md)及已接受 ADR 为准。

## 首轮设计交付建议

先用现有真实功能建立一个小而完整的设计基线：

1. 共享工作台框架与基础组件：导航、按钮、输入、状态标签、菜单和抽屉。
2. Document 阅读 / 编辑代表页，覆盖桌面与手机，以及未保存、冲突、历史恢复、失权和加载失败状态。
3. Channel / Thread 代表页验证同一框架能容纳讨论，再补 Project 文档列表与 Ticket / Decision 详情。

评审时先看信息结构、阅读体验、操作层级、品牌一致性和状态完整性，再确认具体像素参数。已确认稿落实到现有 React + TypeScript，并用真实内容和浏览器复核；视觉稿通过不代替功能或原生 IME 验收。

2026-11-14 起停用 pen.dev / Pencil。新工作台结构或关键状态用 Penpot 代表稿或可编辑代码原型评审，记录源稿或实现版本、桌面 / 窄屏预览以及页面、状态和组件的对应关系。小幅沿用既有模式的改动在运行页面复核。原生 IME 及业务进度见[当前状态](../status/current.md)。

## Penpot 评审留档与恢复

新工作台界面与新交互可以选用本机自部署 Penpot（界面或 MCP），也可以用可编辑代码原型。首轮工作台 `.pen`、原有 `docs/design/previews/` 与评审记录原样原位保存。

1. **源稿。** 从 Penpot 下载完整的 `.penpot`，与评审说明 `README.md` 放在 `docs/design/reviews/<界面或交互>/<评审版本>/`。对应专题链接该版本，说明标注页面、对象、状态及关联组件，可附本机工作区入口。恢复使用仓库内的导出文件。
2. **预览。** 从同版源稿导出桌面与窄屏代表画板的原尺寸 PNG，存入同目录的 `previews/`。在说明中将 PNG 对应到画板、尺寸、适用主题、关键状态和评审结论。用合成内容表达文档、工单、决策、冲突或失权，归档材料遵守仓库的私密工作区资料边界。
3. **恢复。** 首次归档或源稿换版时，在本机 Penpot 的项目菜单将归档 `.penpot` 导入为独立副本。核对画板、尺寸、文案、字体、素材、布局与适用的组件引用。带交互时点通代表路径。在同一说明记录源文件名、SHA-256、导出与导入的 Penpot 版本、复核范围、结果与差异，以及恢复所需字体、素材或组件库的来源与版本。

设计文件恢复记录与正式 Web 运行验收分别保留。业务权限、历史恢复及原生中文 IME 继续按各自专题取证。
