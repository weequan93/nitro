# 诊断与候选采集

这些工具覆盖账户/权限、实际镜像、父链 RPC、候选漂移、数据库来源、WASM 验证控制及进度。

先阅读所选脚本的 docstring 和 `--help`。多数是只读检查；`audit-safe-pause-live.py` 也包含自有 Anvil 的暂停演练，不能把整个目录视为仅执行 RPC 读取。离线 witness 导出/诊断工具也可能写新报告。

常用入口：

- `probe-recovery-authority.py` / `probe-production-participants.py`：父链角色和节点。
- `inspect-recovery-operator.py` / `inspect-remote-validator.py`：操作员运行映射。
- `inspect-snapshot-cutover-config.py` / `inspect-snapshot-validation-gates.py`：snapshot 配置与控制。
- `check-snapshot-validation-progress.sh` / `probe-snapshot-next-validation.sh`：后台进度与单消息新 root 验证。
- `inspect-data-new-reuse.py`：以前测试数据的使用情况。

依赖的早期契约 helper 在 `../legacy/tools`；正式治理流程使用 [toolkit](../toolkit/START-HERE.md)。故障判断见 [排障记录](../../../docs/validator-recovery/TROUBLESHOOTING.md)。
