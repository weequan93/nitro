# 官方Safe Global UI：治理交易准备

用户已确认：治理Safe位于Arbitrum One，平时使用官方Safe Global UI提交交易。

更新：已完成独立最新链上读取及完整Safe暂停分叉复核，见PAUSE-CONTENT-REVIEW.md。调用内容通过；真实网页待签交易仍需核对nonce、费用、结构及签名。

## 本次交付

- `DRAFT-pause.safe.json`：Transaction Builder格式的单笔暂停草案。
- `review.json`：目标、完整calldata、独立编码检查结果、文件SHA256及未完成项。

当前仅离线准备，未向Safe服务创建提案、未签名、未广播；尚未在实际UI导入测试。DRAFT文字不构成执行限制，因此当前文件只用于审阅，不能把可导入理解为已获执行授权。

## 暂停交易审阅值

| 项目 | 值 |
|---|---|
| 网络 | Arbitrum One / chainId 42161 |
| 治理Safe | 0xFbB37c66372f7B40361fBC8C8A235ae92711399D |
| Safe调用目标 | 0x1333480e92de9511dc9bb01f70901ff3ee94f613（UpgradeExecutor） |
| operation | CALL / 0 |
| value | 0 |
| 外层函数 | executeCall(address,bytes) |
| 内层目标 | 0xA113e2E9620a3Bc088a681eBB2C234FDbeb85e21（Rollup） |
| 内层调用 | pause() / 0x8456cb59 |
| 签名门槛 | 此前观察为3/4，提交前重新核对 |

此文件不包括退款、WASM修改、恢复节点或resume。未选nonce，未形成SafeTxHash；不要沿用历史观察nonce10或任何fork nonce。

## 准备和正式签署的衔接

1. 先完成三机镜像/数据/自动重启准备，核定维护时段、治理Safe至少三位签名者。使用Safe网页本身不证明三位签名者均可用。
2. 在计划窗口重新核验链上状态和Safe待办队列。得到具体停旧提交/暂停的执行授权后，按计划协调普通断言及快速确认提交者。
3. 用当时完整检查结果确认或重新生成暂停包。在Safe Global中选择Arbitrum One及治理Safe，通过New Transaction中的Transaction Builder导入最终审阅文件。
4. 核对目标、value、完整data、operation、实际nonce与费用字段；若UI生成额外包装或不同调用结构，应核对其完整解码和模拟结果，不能只看名称。所有签名者应核对同一SafeTxHash。
5. 经约定签名和执行后，核验Safe对应ExecutionSuccess及Rollup paused=true。仅外层receipt.status=1不代表内层暂停成功。
6. 实际暂停后固定A/B及最终恢复参数，再形成独立原子恢复文件。其成功应保持暂停；恢复后验证通过再另行准备resume。

官方Transaction Builder支持导出/导入JSON：
https://help.safe.global/articles/4180673514-transaction-builder

本次格式核对参考官方BatchFile类型：
https://raw.githubusercontent.com/safe-global/safe-react-apps/main/apps/tx-builder/src/typings/models.ts
