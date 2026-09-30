# 目录规划与迁移

2026-09-30 在 `fix/legacy-validation-125022703` 整理。仓库脚本统一在 [scripts/validator-recovery](../../scripts/validator-recovery/README.md)，工作记录继续位于本目录。服务器实际 `/data_new/scripts` 的安装路径及生产数据挂载保持原约定，仓库搬迁不部署到服务器。

## 规划

| 以前的仓库位置 | 新位置 |
| --- | --- |
| `production-recovery-toolkit-v2/` | `scripts/validator-recovery/toolkit/` |
| 根目录的采集、inspect、诊断脚本 | `scripts/validator-recovery/diagnostics/` |
| 根目录的 snapshot 准备、复制、调优 | `scripts/validator-recovery/snapshot/` |
| 根目录的旧演练、重放及公共 helper | `scripts/validator-recovery/legacy/tools/` |
| 根目录的早期离线测试 | `scripts/validator-recovery/legacy/tests/` |
| 早期完整工具目录 | `scripts/validator-recovery/legacy/packages/`，保留各包内部布局 |
| 根目录的 build / install / 压缩包 | `distribution/builders/`、`distribution/installers/`、`distribution/bundles/` |
| `validator-recovery-evidence/` | `scripts/validator-recovery/evidence/` |
| `production-safe-ui-prep-20260929/` | `scripts/validator-recovery/evidence/safe-ui-prep-20260929/` |
| 根目录的三个恢复历史说明 | `docs/validator-recovery/history/` |

正式 toolkit 保持原 sibling import、测试、reference 和基础 manifest。早期 helper 互相动态加载，统一放在 `legacy/tools`，测试分离至 `legacy/tests`；跨目录 probe、历史报告和 receipt fixture 引用已调整。

构建器从当前 toolkit 读取源码，写到 `distribution/generated`。完整包的 manifest 在新 archive 内生成，历史基础清单和旧 payload 不会因重新构建被覆盖。原始分发包保留校验值，缓存、日志和新构建产物通过脚本目录 `.gitignore` 排除。

## 搬迁与检查

357 个已索引文件搬迁；317 个保持原字节，40 个调整了本地路径、历史说明链接或构建目录。逐文件旧/新位置和 hash 见 [layout-migration.json](layout-migration.json)。正式 v2 及历史分发文件、JSON 报告和回执原字节保持一致。

迁移前与迁移后的早期离线测试均为 65 项通过；正式 v2 及附加模块 43 项通过。统一离线入口会选择现有 unittest，跳过名字带 test 的 Anvil CLI 演练和 integration 脚本。

```bash
export PATH="$HOME/.foundry/bin:$PATH"
PYTHONDONTWRITEBYTECODE=1 python3 scripts/validator-recovery/tests/run-offline.py
python3 docs/validator-recovery/catalog.py --verify
```

新脚本按照用途加入对应目录；不要再向仓库根目录堆放脚本。新报告保存到 evidence 的独立输出目录，保留观察时间和来源。资料变化后审阅再执行 `catalog.py --refresh`，并重跑校验。

## 手动 L3 提款方法

与恢复工具分开的通用领取方法整理在 [docs/manual-withdrawal](../manual-withdrawal/README.md)。这里保留参数化的状态/证明准备工具、CLI 与浏览器钱包方法及离线测试。根目录 14 个未提交的 `withdrawal-*` 个案输出目录已清理，后续输出默认写入该方法目录下的 `generated/`，由 `.gitignore` 排除。恢复测试所需的独立 receipt fixture 保留。

## Safe 日常管理交易

[docs/safe-transactions](../safe-transactions/README.md) 保留通用 Builder import 生成器、黑名单/owner 权限说明与离线测试。实际生成的 import、审阅文档和只读检查脚本默认写入该目录的 `generated/` 并忽略；历史 `scripts/safe-wasm-root` 草案不作为实时权限证明。
