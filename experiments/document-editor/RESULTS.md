# 阶段 A headless 结果

日期：2026-09-03

## 结论

Tiptap 3 / ProseMirror 与 Lexical 都通过 `radishnexus-markdown-v1` 的 12 个 headless case：重复 parse 产生相同内部 JSON，规范化 Markdown 的第二次往返稳定，声明支持的文本与结构没有丢失。该结果只保留 Tiptap 为第一候选、Lexical 为有效对照；keyboard、IME、undo / redo、focus、paste、screen reader、390px 与长文档交互尚未在真实浏览器执行，所以当前不做最终编辑器选择，也不进入 Yjs 阶段 B。

## 固定输入

- Node.js `24.16.0`、npm `11.13.0`；
- Tiptap：`@tiptap/core`、`@tiptap/pm`、`@tiptap/starter-kit`、`@tiptap/markdown`，全部 `3.31.2`；
- Lexical：`lexical`、`@lexical/headless`、`@lexical/markdown`、`@lexical/rich-text`、`@lexical/list`、`@lexical/link`、`@lexical/code`，全部 `0.50.0`；
- 独立 lockfile 共 74 个 transitive package，只来自 `registry.npmjs.org`，全部具有 SHA-512 integrity；
- transitive license 只有 MIT 与 BSD-2-Clause，lifecycle install script 为 0，`npm audit --audit-level=high` 为 0 漏洞。

按 lockfile dependency closure 计算，Tiptap 一侧为 44 个 package、约 8.82 MiB unpacked；Lexical 一侧为 30 个 package、约 20.58 MiB unpacked，其中 `@lexical/headless` 引入的 `happy-dom` 占约 16.90 MiB。这里是 headless 安装体积，不是 production browser bundle；bundle 只能在浏览器 harness 建立后测量。

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

## Schema 与安全差异

Tiptap MarkdownManager 对未注册的 `futureCallout` 节点原生返回空字符串且无错误，属于不可接受的静默丢失。实验适配层因此在 serialize 前遍历版本化 node / mark allowlist，并由自动化测试证明未知节点会被真实拒绝。Lexical 对同一未知类型原生拒绝，但正式适配层仍不能依赖错误文案或私有 schema。

raw HTML 与危险链接结果进一步确认 editor import / export 不是 sanitizer。正式 Document route 必须在服务端独立验证 Markdown 子集、链接协议和渲染输出；客户端转义或节点属性不得成为安全边界。

## 尚未完成

以下 case 只在 corpus 中登记，未被 headless 测试冒充通过：plain-text / Markdown paste、keyboard-only、中文 composition / IME、undo / redo、focus restore、copy / paste、screen reader 基本语义、390px 布局和长文档交互。

浏览器 harness 需要另行精确锁定 UI 与 build 依赖并复核 license / lockfile。该步骤完成前，ADR-0021 保持“提议”，不安装 Yjs，不新增 Document WebSocket / SSE，也不使用 Web Storage。

## 复验

```sh
cd experiments/document-editor
npm run check
npm run report
npm audit --audit-level=high
```
