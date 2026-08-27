## 目标与范围

请说明本次变更解决的问题、采用的方案、用户或维护者价值，以及明确不包含的内容。

## 关联与目标分支

- 关联 Issue / Decision / ADR / 专题：
- 目标分支：`dev` / `master`
- 如目标为 `master`，阶段性收口或 hotfix 理由：

## 变更类型

- [ ] 产品或用户功能
- [ ] 缺陷修复
- [ ] 领域模型 / 架构 / 公共协议
- [ ] 重构
- [ ] 测试
- [ ] 文档
- [ ] CI / 仓库治理
- [ ] 安全 / 依赖 / 供应链

## 影响面

- [ ] 无长期边界变化
- [ ] Decision、Ticket、Document 或 Initiative
- [ ] Workspace、权限、私密对象或 Attention Item
- [ ] Component、Repository、Environment、CI Run 或 Deployment
- [ ] API、事件、EntityLink、Activity 或数据库迁移
- [ ] Plugin Manifest、Host API、Secrets、Webhook 或外部写操作
- [ ] 自部署、升级、备份、恢复、导入或导出
- [ ] Web UI、可访问性、响应式或未来 Flutter 协议
- [ ] 许可证、第三方材料或贡献边界

影响、兼容性和迁移说明：

## 安全、隐私与失败语义

- 权限与 Workspace scope：
- Secrets、个人数据和私密内容：
- 外部系统不可用、重复请求和部分失败：
- 高风险动作、确认和审计：

## 验证记录

只填写实际执行过的命令及结果。未执行的验证写入下一节。

```text
./scripts/check-repo.sh
python3 -m unittest discover -s scripts/tests -p "test_*.py"
```

## 未验证、风险与回滚

- 未执行或受环境阻塞的验证：
- 已知风险和停止线：
- 回滚或失败恢复方式：

## 文档与治理检查

- [ ] 改动符合 `docs/status/current.md`、产品定义和领域模型
- [ ] 改变长期产品、架构、协议或治理决策时已更新专题或 ADR
- [ ] 没有把文档、原型、静态检查或模拟结果表述为生产能力
- [ ] 没有提交真实凭据、个人数据、私密工作区内容或无权提供的第三方材料
- [ ] PR 聚焦单一目标，没有夹带无关重构、格式化或依赖升级
- [ ] 所有 review conversation 已解决或记录明确结论

## master 合并后回流

目标分支为 `master` 时必须填写；合并后先把最新 `master` 回流到 `dev`，再开始下一轮开发。

- 回流负责人：
- 预期方式：`fast-forward` / `merge commit`
- 冲突解决或产生内容变化时需要补充的验证：
