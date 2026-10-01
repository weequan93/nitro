# Validator 恢复脚本

本仓库统一脚本入口。实际执行结果、参数与验收范围见 [工作记录](../../docs/validator-recovery/README.md)，完整文件清单见 [脚本索引](../../docs/validator-recovery/SCRIPT-CATALOG.md)。

```text
scripts/validator-recovery/
├── toolkit/                 正式 v2：治理包、重放、回执、预签、交接
├── diagnostics/             角色、RPC、候选与运行诊断
├── snapshot/                数据准备、验证进度、有限调优
├── legacy/
│   ├── tools/               早期演练及互相加载的公共 helper
│   ├── tests/               早期工具的离线测试和 fixture
│   └── packages/            原始历史工具包，保留各包内部结构
├── distribution/
│   ├── builders/            构建新工具包
│   ├── installers/          历史安装器及内嵌 payload
│   ├── bundles/             历史压缩包，保留原 checksum
│   └── generated/           新构建输出，不纳入历史索引/Git
├── evidence/                报告、交易审阅、回执和历史核对脚本
└── tests/run-offline.py      统一离线检查入口
```

## 应使用哪一组

| 工作 | 目录 / 入口 |
| --- | --- |
| 准备当前治理包、校验实际回执 | [toolkit](toolkit/START-HERE.md) |
| 核对运行状态、角色和数据来源 | [diagnostics](diagnostics/README.md) |
| 管理 snapshot 验证配置和数据副本 | [snapshot](snapshot/README.md) |
| 查看/复现早期 fork 实验 | [legacy](legacy/README.md) |
| 分发文件或重新构建 | [distribution](distribution/README.md) |
| 查实际生产证据与当时报告 | [evidence](evidence/README.md) |

从仓库根目录执行离线检查：

```bash
export PATH="$HOME/.foundry/bin:$PATH"
PYTHONDONTWRITEBYTECODE=1 python3 scripts/validator-recovery/tests/run-offline.py
python3 docs/validator-recovery/catalog.py --verify
```

例如本地运行正式工具的帮助：

```bash
python3 scripts/validator-recovery/toolkit/recovery.py --help
```

本次整理只调整本地仓库结构。服务器既有 `/data_new/scripts/production-recovery-toolkit-v2` 和平铺安装路径仍是原部署，不随仓库移动自动变化。历史安装器继续安装当时约定的路径；新旧布局的对照及逐文件 hash 在 [layout-migration.json](../../docs/validator-recovery/layout-migration.json)。

正式 toolkit 内部结构及基础 manifest 保持原样。旧演练 helper 留在 `legacy/tools` 中，以保留动态加载关系；跨目录引用和测试 fixture 已按新布局调整。外部调用使用此目录中的新路径，不保留仓库根目录的同名副本或 symlink。
