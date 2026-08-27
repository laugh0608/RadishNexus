# RadishNexus 安全策略

RadishNexus 当前处于产品定义、仓库治理和 Golden Path 预研阶段，尚未承诺长期安全支持版本或固定响应 SLA。但项目面向自部署研发团队，涉及身份、Workspace 权限、私密消息、协作文档、插件凭据和 CI/CD 外部操作，安全边界从项目早期开始按高优先级处理。

## 私下报告

请不要为未修复漏洞创建公开 Issue、Pull Request 或讨论，也不要提交真实密钥、访问令牌、个人数据、生产凭据、私密消息或未经授权取得的第三方内容。

GitHub 远端已经启用 Private vulnerability reporting。请优先通过以下入口私下提交漏洞报告：

https://github.com/laugh0608/RadishNexus/security/advisories/new

若 GitHub 私密报告入口暂时不可用，可发送邮件至 `laugh0608@foxmail.com`，主题包含 `[RadishNexus Security]`。不要因为入口不可用而改用公开 Issue、Pull Request 或讨论披露漏洞细节。

报告应尽量包含：

- 受影响的提交、版本、组件、部署模式或存储后端；
- 可复现步骤、最小输入、预期行为和实际行为；
- 对机密性、完整性、可用性、隐私或授权边界的影响；
- 已脱敏的日志、请求、响应、审计或时间线记录；
- 已知利用条件、临时缓解措施和建议披露时间；
- 是否可以公开致谢及希望使用的署名。

若复现需要敏感材料，请先说明材料类型并等待安全传输安排。

## 安全问题范围

- 身份认证、Session、OIDC、CSRF、Origin、Cookie 或邀请流程可被绕过；
- Workspace、Project、Channel、Conversation 或对象级权限缺失，导致越权读取、写入或跨租户泄漏；
- 私密对象通过 EntityLink、Activity、搜索、通知、Attention Item、导出或 AI 插件泄漏标题、摘要或正文；
- 插件权限、Secrets、网络范围、签名、升级或隔离机制可被绕过；
- Webhook、Bot、Slash Command 或 CI/CD 连接器存在伪造、重放、SSRF、路径穿越、任意代码执行或资源失控；
- Decision、审批、Deployment、回滚等高风险事实可被未授权确认、篡改或错误归属；
- CRDT、离线同步、版本恢复或权限变化产生未授权内容合并或历史泄漏；
- 备份、恢复、`.nexus` 导出包、日志或诊断包泄漏 Secrets、Token、个人数据或私密内容；
- 依赖、构建、容器、插件、发布和第三方资产存在供应链完整性风险。

普通功能缺陷、文档错误和不产生安全边界影响的性能问题可以使用公开 Issue。

## 处理与披露

当前阶段按 best-effort 方式确认、评估和修复报告。报告者与维护者应协调披露，在修复或有效缓解措施可用前避免公开可直接利用的细节。

安全修复仍通过受保护分支和 Pull Request 流程进入稳定主线；必要时可使用 GitHub Security Advisory 的临时私有分叉。直接进入 `master` 的 hotfix 合并后必须回流到 `dev`，且不得把真实秘密、个人数据或可利用载荷写入长期 fixture、日志或文档。
