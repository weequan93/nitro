# 暂停包 pause-review-20260929-105018 审阅记录

结论：用户报告的分叉测试通过；本地独立重编码与 EIP-712 SafeTxHash 计算相符。仅为交易内容审阅，不是生产执行授权或当前链上状态证明。

## 本次明确绑定的交易

| 字段 | 值 |
| --- | --- |
| 网络 | Arbitrum One，42161 |
| 治理 Safe | `0xfbb37c66372f7b40361fbc8c8a235ae92711399d` |
| Safe nonce | `10` |
| Safe to | `0x1333480e92de9511dc9bb01f70901ff3ee94f613` |
| value / operation | `0` / `CALL (0)` |
| 外层函数 | `executeCall(address,bytes)`，selector `0xbca8c7b5` |
| target | `0xa113e2e9620a3bc088a681ebb2c234fdbeb85e21` |
| 内层 | `pause()`，`0x8456cb59` |
| safeTxGas / baseGas / gasPrice | 全部为 `0` |
| gasToken / refundReceiver | 全部为零地址 |
| SafeTxHash | `0x95c613b5f67c0b1392088f9c4f3ac08f73041594faa3511098b201b74c64dadb` |
| 用户报告的 packageIdentity | `9ebac56f76390043e52a0305da651c3d6be46927203136b2a374b10f8a0c834b` |

核对脚本：`check-pause-105018.py`。从函数签名重新编码 calldata，再使用 chainId42161、治理 Safe 和完整交易字段计算 EIP-712 摘要；两项断言通过。不请求 RPC、不签名、不广播。没有取得服务器包完整文件，因此 packageIdentity 仅记录用户报告，不称为本地重新验算结果。

## 分叉证据

用户报告：exact_package_fork_passed；thresholdNegativeTested、exactSafeFieldsTested、exactImportTransactionsTested、receiptDecoderTested 为 true；ownedForkStopped 为 true。ArbSys 日志补丁未使用。原生 Arbitrum、真实生产签名均未测试。

本次单调用暂停没有退款、修改 WASM、创建/确认恢复节点或恢复运行。工具自动生成的 REVIEW.md 含恢复阶段通用说明（如精确 node hash、Inbox 增长、A→B），这些不是此次 pause() 的调用参数或链上约束。暂停不冻结整个 L3 或清除 Fast Safe 的旧授权。

## 准备状态与后续

工具包校验和10项单元测试已由服务器报告通过；历史恢复包证据核对与分叉恢复/回滚/resume/三账户退款通过；本暂停包内容及同字段私有分叉通过。当前准备阶段无需重复长重放或数据库复制。

执行窗口前重新核对链上状态、Safe nonce 与治理/Fast Safe 的待执行及离线授权，协调旧 validator 提交。新镜像与配置切换由用户安排。此刻没有停止节点或暂停生产的授权。

已有 preflight 对 confirmed/created/stakers 等也做严格比较；旧节点继续提交可能触发 Followup state changed。发生时重新生成暂停包并复测，不绕过检查。临近实际签署才做最终 preflight。

Safe Global UI 的实际外层 to/value/data/operation、nonce、全部 Safe 费用字段及 SafeTxHash 都应等于该包。若 UI 包装为 MultiSend 或字段有变化，需重新审阅和模拟；不能沿用本文件的 hash。普通交易 gas limit 不属于 SafeTxHash。

暂停成功后才能固定最终生产 A/B、跨度、退款基线与恢复节点参数，生成新恢复包并审阅。历史43008/10578及历史模拟包不能直接导入生产。
