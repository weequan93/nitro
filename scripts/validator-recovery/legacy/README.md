# 历史演练与工具包

`tools/` 保留早期 Safe、原子恢复、退款、重新质押、Nitro 自动提交/重启、withdrawals 及固定消息重放工具。它们通过 `importlib` 加载公共 helper，保持这些模块在同一目录；对诊断和测试数据的跨目录引用已调整。

`tests/` 保存对应离线测试，receipt fixture 位于 `tests/fixtures`。统一检查入口为 `../tests/run-offline.py`。文件 `test-original-staker-rejoin.py` 是需要自有 Anvil 的 CLI 演练，名字带 test，但不是离线 unittest，统一检查不会运行它。

`packages/` 保存原始 v1 工具包及分发时副本，内容保持原样。它们用于回查历史安装内容；当前正式入口是 [toolkit](../toolkit/START-HERE.md)，不是取多个旧副本拼接成新的恢复包。

固定历史节点、numBlocks、服务器输出目录、临时 signer 或 Anvil 端口只属于那次实验。其成功不能代替新窗口采集、生产签名或完整运行验收。根因和协调历史文档移至 [history](../../../docs/validator-recovery/history/)。
