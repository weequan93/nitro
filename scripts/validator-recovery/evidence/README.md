# 证据归档

这里保存报告、Safe import 审阅、执行回执、参与者查询、历史 fork 结果和对应核对脚本。整理时保留 JSON / 回执原字节；其中的服务器路径与观察时刻仍表示当时事实。

| 路径 | 来源 |
| --- | --- |
| `reported-results/` | 用户粘贴的服务器报告，包含来源说明和部分报告标记 |
| `presign-window-20260929-154302-review/` | 实际预签窗口的 import 与外层字段审阅 |
| `executed-recovery-03e938/` | 实际恢复交易的独立父链回执/状态检查 |
| `zombie-cleanup-57531e3c/` | 实际 EOA 清理交易与 pinned 账户状态 |
| `first-recovered-fast-confirm.json` | 当时的 Fast Safe 批准查询，不是最终确认回执 |
| `toolkit-v2-validation/` | v2 私有 fork 校验，区分本机日志 shim 与原生路径 |
| `safe-ui-prep-20260929/` | 暂停交易的 UI/权限准备审阅资料 |

这里的 Python 核对脚本有固定交易和历史前置条件，部分会访问父链 RPC、生成或覆盖其报告。它们不属于统一离线测试，不应为刷新页面而批量运行。

最新交接结论在 [工作记录](../../../docs/validator-recovery/README.md)。文件 hash 只证明本地文件一致，不会扩大它本来的证据范围。
