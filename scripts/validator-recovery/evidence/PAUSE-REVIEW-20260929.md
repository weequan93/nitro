# 独立治理暂停：调用审阅页

状态：准备草案；没有签名，没有提案，没有广播。对应用户报告 production-window-prep-20260929-082849。

## 已核对

父链42161，报告固定块0x1e640172，hash 0xc124509961bc5961e48a691f29d1b4ad211b61289b214aff40e01aa616ad1ae3。
已确认43096，已创建43098，首个未决43097；paused=false。该报告通过三账户与代码基线检查。nonce10只是固定块观察值。

报告中Safe实现地址0x29fcb43b46531bca003ddc8fcb67ffe91900c762及代码hash
0xb1f926978a0f44a2c0ec8fe822418ae969bd8c3f18d61e5103100339894f81ff
与[Safe官方SafeL2 1.4.1部署资料](https://raw.githubusercontent.com/safe-global/safe-deployments/main/src/assets/v1.4.1/safe_l2.json)一致，42161列为canonical。
这是官方元数据与用户提供链上读取结果的比对，不是对实际Safe当前状态、签名或所有权限的完整审计。

## 调用逐层说明

| 字段 | 内容 |
|---|---|
| 执行网络 | Arbitrum One，42161 |
| 多签账户 | 0xfbb37c66372f7b40361fbc8c8a235ae92711399d |
| Safe交易to | 0x1333480e92de9511dc9bb01f70901ff3ee94f613（UpgradeExecutor） |
| Safe交易value | 0 |
| Safe交易operation | 0 / CALL |
| 外层函数 | executeCall(address,bytes) |
| 外层参数target | 0xa113e2e9620a3bc088a681ebb2c234fdbeb85e21（Rollup） |
| 内层函数 | pause() |
| 内层data | 0x8456cb59 |

这里没有MultiSend，不使用Safe DELEGATECALL，不直接以EOA调用Rollup。此次只暂停Rollup，不退款、不改WASM、不创建恢复断言、不resume。

本地重新编码结果保存于pause-call-review-20260929.json。服务器prepare-window生成的pause-review.json可与其to/value/operation/data逐项比对。

## 签名前尚待完成

1. 明确使用哪个多签界面/脚本、3位签名者和实际gas付款方；具体Safe交易全部字段（nonce、gas参数、refundReceiver等）生成后再核对hash。
2. 明确三机地址/容器对应关系和升级/数据就绪方案，选择维护窗口。纯validator角色不等于已授权停止受保护容器。
3. 窗口内复核挑战、合约和账户状态，协调旧提交者及在途交易。暂停前报告不能用来固定暂停后的A。
4. 真实签名完成后核验完整execTransaction路径并模拟，再由获授权者执行。旧fork的approveHash模拟不等于真实ECDSA签名检查。

## 执行后验收

同时核对父链交易成功、Safe对应safeTxHash的ExecutionSuccess、Rollup paused=true；不能只看receipt.status。
随后采集暂停后的固定状态，选择最终B，生成并审阅原子恢复包。本页没有授权恢复交易，也没有预先选定恢复node编号或跨度。
