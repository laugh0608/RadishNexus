# 阶段 A 结果

日期：2026-09-03

2026-09-05 追踪说明：以下保留阶段 A 的实测与当时选型。后续推进已按 [ADR-0021](../../docs/adr/0021-document-editor-and-collaboration-foundation.md)选择 M0.5 / M1 非 CRDT 路径，阶段 B 延后；这不改变本轮实验结果或批准生产依赖。

## 结论

Tiptap 3 / ProseMirror 与 Lexical 都通过 `radishnexus-markdown-v1` 的 12 个 headless case：重复 parse 产生相同内部 JSON，规范化 Markdown 的第二次往返稳定，声明支持的文本与结构没有丢失。内置浏览器进一步验证了两者的 keyboard-only formatting、undo / redo、focus restore、plain-text 与 `text/markdown` paste、copy、ARIA 基本语义、390px 和 400-section 长文档交互；项目所有者随后用 macOS 中文输入法完成两套候选的真实 composition 复验。

阶段 A 选择 Tiptap 3 / ProseMirror 进入后续阶段 B 对照实验。两套候选都通过往返、IME 和浏览器门槛，但 Lexical 没有证明能显著降低 React、IME 或可访问性装配成本；Tiptap / ProseMirror 的 schema、transaction 与官方 Yjs binding 更贴合既定第一候选判据。这个选择不掩盖 Tiptap Markdown Beta、未知节点静默丢失和服务端 sanitizer 风险，也不等于已经批准正式 Web 依赖或协同协议；Lexical 保留为可退出的对照证据。

## 固定输入

- Node.js `24.16.0`、npm `11.13.0`；
- Tiptap：`@tiptap/core`、`@tiptap/pm`、`@tiptap/starter-kit`、`@tiptap/markdown`，全部 `3.31.2`；
- Lexical：`lexical`、`@lexical/headless`、`@lexical/markdown`、`@lexical/rich-text`、`@lexical/list`、`@lexical/link`、`@lexical/code`，全部 `0.50.0`；
- 浏览器 harness 另加 `@lexical/history@0.50.0` 与 dev-only `vite@8.2.2`，均为 MIT；
- 独立 lockfile 共 115 个 transitive package，只来自 `registry.npmjs.org`，全部具有 SHA-512 integrity；
- transitive license 为 Apache-2.0、BSD-2-Clause、BSD-3-Clause、ISC、MIT、MPL-2.0；
- 唯一 lifecycle manifest 是精确审阅的 optional `fsevents@2.3.3`，安装全程固定 `ignore-scripts=true`；`npm audit --audit-level=high` 为 0 漏洞。

按 lockfile dependency closure 计算，Tiptap 一侧为 44 个 package、约 8.82 MiB installed；Lexical 加 history 为 31 个 package、约 20.67 MiB installed，其中 `@lexical/headless` 引入的 `happy-dom` 占约 16.90 MiB。Vite 工具链为 40 个 lockfile package，其中当前平台安装 16 个、约 28.28 MiB。

## Corpus 结果

| Case                           | Tiptap         | Lexical        | 观察                                                                                                 |
| ------------------------------ | -------------- | -------------- | ---------------------------------------------------------------------------------------------------- |
| empty                          | 稳定           | 稳定           | Tiptap 用空 `doc`，Lexical 用含空 paragraph 的 root；公共合同不能依赖任一内部形状                    |
| paragraphs                     | 稳定           | 稳定           | 文本与段落保留                                                                                       |
| heading-emphasis-link-quote    | 稳定           | 稳定           | 标题、链接、引用与 inline format 保留                                                                |
| ordered-unordered-nested-lists | 稳定           | 稳定           | Lexical 规范化缩进，并把 nested ordered start 规范化为父列表的连续值                                 |
| fenced-and-inline-code         | 稳定           | 稳定           | fenced language、code block 与 inline code 保留                                                      |
| soft-and-hard-breaks           | 稳定           | 稳定           | Tiptap 将反斜杠 hard break 规范化为两个空格；Lexical 保留两种 hard-break marker                      |
| unicode-mixed-direction        | 稳定           | 稳定           | 中文、emoji、组合字符与 RTL 文本保留                                                                 |
| long-word-and-url              | 稳定           | 稳定           | 512 字符连续词和长 URL 保留                                                                          |
| raw-html                       | 稳定、转义     | 稳定、原样保留 | Tiptap 输出 `&lt;custom-widget...`；Lexical 原样输出标签。该输入继续属于 unsupported，不允许直接渲染 |
| dangerous-link                 | 稳定、保留协议 | 稳定、保留协议 | 两者内部表示和输出都保留 `javascript:`；服务端必须按版本化规则拒绝危险协议，不能依赖编辑器           |
| malformed-input                | 稳定、规范化   | 稳定、规范化   | 两者都补齐 fenced code；对未闭合 inline syntax 的解释不同，输入已显式分类为 unsupported              |
| large-document                 | 稳定           | 稳定           | 400 sections、规范化后 35,184 字符；本轮只证明确定性，不把单次 headless 耗时当浏览器性能结论         |

完整输入、内部 JSON、内部 JSON SHA-256、规范化输出、第二次往返、结构摘要、诊断和单次耗时可通过 `npm run report` 重新输出；测试不依赖已提交的生成快照。

## 浏览器结果

同一原生 DOM 页面同时装配两套候选，没有引入 React，也没有借用正式 Web bundle。最终 production build 转换 86 个 module；单一对照页 JavaScript 为 758.85 kB minified / 238.81 kB gzip，CSS 为 3.03 kB / 1.17 kB gzip。这个数值包含两套编辑器和共享 corpus，只作为实验成本上限，不是任一候选的独立 production bundle。

| 验收项                  | Tiptap | Lexical | 证据                                                                                                                                                  |
| ----------------------- | ------ | ------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| ARIA 基本语义           | 通过   | 通过    | 两个候选分别暴露命名 region、toolbar、button、multiline textbox 与 live status；初始标题、强调和列表进入可访问树                                      |
| keyboard formatting     | 通过   | 通过    | Tab 从 Bold 到 List，Enter 执行；bold / bullet Markdown 正确，命令后 focus 回到对应 editor                                                            |
| undo / redo             | 通过   | 通过    | 普通输入可撤销并恢复；Tiptap harness 的 Clear + 紧接输入会合并为恢复初始 sample，Lexical Clear 后为 empty，正式产品不得假设两者 history grouping 相同 |
| plain-text paste / copy | 通过   | 通过    | 浏览器 clipboard 写入、`ControlOrMeta+V`、全选复制和 clipboard 读回均为 `plain paste 中文 🥕`                                                         |
| `text/markdown` paste   | 通过   | 通过    | 同一 clipboard item 同时携带 plain fallback 与 Markdown MIME，均规范化为 heading + 两项列表                                                           |
| Unicode direct input    | 通过   | 通过    | 自动化直接输入 `你好`，输出文本正确                                                                                                                   |
| OS IME composition      | 通过   | 通过    | 项目所有者在 macOS 中文输入法下分别输入 `萝卜输入测试`，确认无双写、吞字或 selection 跳动；聚焦各自 editor 后一次 `⌘Z` 分别完整撤销本次输入          |
| 390px                   | 通过   | 通过    | `innerWidth`、document 和 body scroll width 均为 390；所有可见元素无横向越界，editor `clientWidth === scrollWidth === 340`                            |
| 400-section document    | 通过   | 通过    | 输出包含 `Section 400` 与 `item 400.2`；Tiptap / Lexical normalized length 分别为 35,186 / 35,184，editor 在固定高度内纵向滚动                        |
| console                 | 通过   | 通过    | 修正后在全新标签完成最终复验，warning / error 为 0                                                                                                    |

浏览器验收发现并修复了两个 Lexical harness 问题：自定义 Markdown paste 已消费事件后，默认 plain-text handler 仍继续执行，导致重复内容；Clear 后 selection format 仍保留 bold，导致新输入泄漏格式。前者在消费 `text/markdown` 后停止后续监听器，后者在 Markdown reset 同一 update 内显式清零 selection format / style。两项均在干净标签复验通过。

## Schema 与安全差异

Tiptap MarkdownManager 对未注册的 `futureCallout` 节点原生返回空字符串且无错误，属于不可接受的静默丢失。实验适配层因此在 serialize 前遍历版本化 node / mark allowlist，并由自动化测试证明未知节点会被真实拒绝。Lexical 对同一未知类型原生拒绝，但正式适配层仍不能依赖错误文案或私有 schema。

raw HTML 与危险链接结果进一步确认 editor import / export 不是 sanitizer。正式 Document route 必须在服务端独立验证 Markdown 子集、链接协议和渲染输出；客户端转义或节点属性不得成为安全边界。

## 阶段 A 判定与停止线

阶段 A 已完成。Browser 自动化本身不支持 `Input.imeSetComposition`，因此自动化 Unicode 直接输入没有被当作 IME 证据；最终通过项来自项目所有者对同一隔离页面的真实 macOS 中文输入法复验。先在两个 editor 依次输入后，第一次 `⌘Z` 只撤销仍持有焦点的第二个 editor；重新聚焦第一个 editor 后一次 `⌘Z` 也完整撤销对应 composition，证明两套 history 均按当前 editor 焦点隔离，没有错误跨 editor 撤销。

2026-09-03 当时计划是在获得精确依赖授权后，用选定的 Tiptap / ProseMirror 与 Yjs 做无网络、无 WebSocket、无 IndexedDB 的双文档内存收敛实验。该实验已于 2026-09-05 延后，不再作为最小 Document 的前置；ADR-0021 仍待 Document 业务合同审查，不因已有阶段 A 证据自动接受。具体下一步以[当前状态](../../docs/status/current.md)为准。

## 复验

```sh
cd experiments/document-editor
npm run check
npm run report
npm audit --audit-level=high
npm run dev
```
