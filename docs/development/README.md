# 开发指南

RadishNexus 当前处于产品定义、架构基线与仓库建设阶段。本目录定义实现开始后仍适用的跨语言工程约束，不提前承诺尚未选型的框架和目录结构。

## 入口

- [工程标准](engineering-standards.md)
- [总体架构](../architecture/overview.md)
- [领域模型](../domain-model.md)
- [Golden Path](../golden-path.md)
- [仓库治理](../governance/repository-governance.md)
- [贡献指南](../../CONTRIBUTING.md)

## 当前最小验证

从仓库根运行：

```bash
./scripts/check-repo.sh
python3 -m unittest discover -s scripts/tests -p 'test_*.py'
```

PowerShell 环境可以运行：

```powershell
./scripts/check-repo.ps1
```

随着实现进入仓库，各语言的真实构建和测试会加入统一质量门；在此之前不维护虚假的占位命令。

当前 M0 核心契约实验已经提供独立的 Go 与 PostgreSQL 定向入口：

```bash
./scripts/check-m0-core-contracts.sh
./scripts/check-m0-core-contracts-postgres.sh
```

该目录是可丢弃技术实验，不代表正式服务工程已经建立；范围、依赖和清理方式见 [M0 核心契约实验](../../experiments/m0-core-contracts/README.md)。
