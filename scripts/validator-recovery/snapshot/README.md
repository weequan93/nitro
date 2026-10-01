# Snapshot 数据与验证准备

这里集中 snapshot 配置准备、复制测试数据库、验证指标与性能试验。脚本默认行为和执行开关不同，先查看 `--help`、基线要求及报告。

| 脚本 | 用途 |
| --- | --- |
| `prepare-snapshot-sync.py` | 准备非质押同步配置 |
| `prepare-snapshot-diagnostics.py` | 开启按需诊断；其初始 sent-limit=0 不等于后台验证配置 |
| `replace-recovery-test-db.py` | 冻结源、更新已授权测试副本和 metadata 比较 |
| `prepare-data-new-parent-check.py` | 测试副本接真实父链的非质押检查 |
| `fix-snapshot-validation-limit.py` | 修复后台验证发送限制 |
| `snapshot-validation-metrics.py` | 指标服务配置 |
| `tune-snapshot-*.py` | 各次有明确基线及回滚条件的性能试验 |

`--apply`、`--start` 等操作可能正常停止/启动 snapshot 并改变其配置；按本次明确授权的作用范围使用。数字更大的旧调优脚本不代表最新或更快版本。实际部署、吞吐、内存/I/O 和错误日志优先。

服务器固定数据路径和保护条件保留原值。目录规划不代表自动把数据迁到新的磁盘，也不解除独占写入要求。[操作流程](../../../docs/validator-recovery/RUNBOOK.md)说明如何交接。
