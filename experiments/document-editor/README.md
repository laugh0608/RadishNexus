# Document editor 可丢弃实验

本目录执行 [ADR-0021](../../docs/adr/0021-document-editor-and-collaboration-foundation.md) 阶段 A：用同一版本化 Markdown corpus 对照 Tiptap 3 / ProseMirror 与 Lexical 的 headless 解析、内部表示和确定性往返。

当前 headless 实测结论见 [RESULTS.md](RESULTS.md)。

边界：

- 这是独立 private npm package，不进入正式 `web/package.json` 或 Web bundle；
- 依赖精确锁定，安装时禁用 lifecycle script；
- 不接入 React UI、Yjs、WebSocket、SSE 或浏览器持久化；
- 结果只用于 ADR 选型，不构成 Document 公共合同。

运行门禁：

```sh
npm run check
npm run report
```

`npm run report` 输出包含每个 case 的输入 Markdown、完整内部 JSON、规范化 Markdown、第二次往返、结构摘要、SHA-256 和丢失诊断。浏览器专属 case 会单列为未执行，不用 headless 结果冒充 UI 验收。

`node_modules` 是可删除的本地安装产物；`package.json`、`package-lock.json`、corpus、测试与结果报告命令是实验的可复验输入和证据。
