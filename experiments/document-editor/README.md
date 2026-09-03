# Document editor 可丢弃实验

本目录已完成 [ADR-0021](../../docs/adr/0021-document-editor-and-collaboration-foundation.md) 阶段 A：用同一版本化 Markdown corpus 和真实浏览器对照 Tiptap 3 / ProseMirror 与 Lexical 的解析、内部表示、确定性往返与交互边界。

阶段 A 已选择 Tiptap 3 / ProseMirror 进入后续内存协同实验；完整实测证据、选择理由与停止线见 [RESULTS.md](RESULTS.md)。阶段 B 尚未安装 Yjs 或开始实现。

边界：

- 这是独立 private npm package，不进入正式 `web/package.json` 或 Web bundle；
- 依赖精确锁定，安装时禁用 lifecycle script；
- 不接入 React UI、Yjs、WebSocket、SSE 或浏览器持久化；
- 结果只用于 ADR 选型，不构成 Document 公共合同。

运行门禁：

```sh
npm run check
npm run report
npm run dev
```

`npm run report` 输出包含每个 headless case 的输入 Markdown、完整内部 JSON、规范化 Markdown、第二次往返、结构摘要、SHA-256 和丢失诊断，并单列浏览器验收清单；真实浏览器与系统中文 IME 结果记录在 `RESULTS.md`，不以 headless 或 Unicode 直接输入冒充。

`node_modules` 是可删除的本地安装产物；`package.json`、`package-lock.json`、corpus、测试与结果报告命令是实验的可复验输入和证据。

`npm run dev` 只启动绑定 `127.0.0.1` 的隔离浏览器 harness。它用原生 DOM 分别装配 Tiptap 与 Lexical，不借用正式 Web bundle，不读取或写入 Web Storage。
