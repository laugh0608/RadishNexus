# RadishNexus Web

`web/` 是 RadishNexus 第一正式产品形态的 React + TypeScript 入口。当前实现 Decision 与 CI Run Nexus View 代表原型，用来验证 Current、Relations、Timeline、安全投影与关键页面状态；默认入口展示 CI Run 的 succeeded / failed 交互，不代表业务 HTTP API、完整 Web Shell、导航或聊天能力已经开放。

## 本地运行

使用 Node 24 LTS 与 npm 11：

```bash
cd web
npm ci
npm run dev
```

从仓库根执行可信检查：

```bash
./scripts/check-web.sh
```

该检查覆盖 Prettier、Oxlint、Vitest + jsdom 状态测试、严格 TypeScript、Vite production build，以及 lockfile 来源、integrity、许可证和 lifecycle script 基线。`npm ci` 默认受 `.npmrc` 约束，不执行依赖 lifecycle scripts。

## 当前边界

- 原型只消费已经按当前主体过滤的 `NexusViewData`，不接收角色或权限集合，也不在浏览器中重新判断对象可读性。
- `restricted` 条目在类型上不携带 EntityRef、对象类型、关系类型、标题、来源或时间；`hidden` 条目不进入客户端数据。
- fixture 是明确标注的静态代表数据，不通过临时 endpoint 或无类型 `fetch` 暗示公共 API 已冻结。
- CI Run fixture 与后端安全投影同形，只包含状态、四个受控时间、当前 Component 与唯一 `ci-run.recorded`；不包含 source ID、external run key、delivery receipt、digest、Secret、原始 payload 或外部 URL。
- 状态检视器只用于人工复核 succeeded、failed、loading 与 error；Decision 的 empty 与 restricted 行为继续由组件测试覆盖。检视器不是未来产品导航。
- 当前不引入 router、状态库、组件库、图标包或远程字体。

## 依赖与许可证

生产依赖只有 React 与 React DOM。构建、测试和格式工具使用 Vite、TypeScript、Oxlint、Vitest、Testing Library、jsdom 与 Prettier；直接依赖采用 MIT，TypeScript 采用 Apache-2.0。完整锁定依赖只允许来自官方 npm registry，必须携带 SHA-512 integrity，并限定在 `scripts/check-dependencies.mjs` 已审阅的 SPDX 许可证集合内；许可证或 lifecycle script 漂移会让检查失败。
