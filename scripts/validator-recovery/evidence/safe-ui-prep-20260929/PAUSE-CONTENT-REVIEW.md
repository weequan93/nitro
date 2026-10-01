# 暂停交易专项复核结果

结论：暂停草案的调用内容、当前权限和完整Safe合约调用路径复核通过。尚未核对将来网页实际生成的nonce/gas/SafeTxHash及真实签名，不等于已授权生产执行。

## 本次独立检查

- 2026-09-29 01:15 UTC（新加坡09:15），直接读取公开Arbitrum One RPC，chainId42161。
- 固定父块0x1e642930，hash0xde86ab4657b057315fe4cf72224301f015f1d77e5262c7af5de1f00712f0b397。
- Safe owners仍为既定四人，threshold3，nonce10，guard为零，modules为空，fallback与基线相同。
- Safe代理、SafeL2实现、UpgradeExecutor代理/实现、Rollup两套实现的代码哈希均匹配此前基线。SafeL2实现与官方1.4.1 canonical部署资料一致。
- Rollup proxy admin确为UpgradeExecutor；治理Safe在UpgradeExecutor拥有EXECUTOR_ROLE。
- Rollup paused=false，旧WASM仍在使用，三个现有staker均无currentChallenge。
- 实际编码和反向解码都确认为executeCall(Rollup,0x8456cb59)，inner=pause()。

## 本机新建分叉的完整执行

从上述父块建立独立Anvil chain31337；模拟owner approveHash，再调用实际Safe execTransaction。未冒充Safe发交易，未访问生产数据库，未向生产广播。

通过：

1. 两签不满足3/4门槛被拒绝。
2. 三位模拟owner批准后Safe执行成功。
3. 回执包含对应ExecutionSuccess，以及Rollup的Paused事件，事件caller为UpgradeExecutor。
4. 从SafeL2实际SafeMultiSigTransaction日志解码：to、value、data、operation全部匹配本文件；CALL=0、value=0；模拟Safe费用字段为零。
5. paused由false变true；所检查的WASM、confirmed/created/unresolved节点号、staker及退款credit/zombie、Outbox地址、fast confirmer均未改变。
6. 模拟结束已停止并销毁本次Anvil进程。

模拟消耗gas121366仅作本次分叉证据，不应直接设为网页最终gas限制。真实ECDSA签名与生产SafeTxHash未测试；chain31337的hash不可拿去生产签署。

## 唯一应有的暂停调用

| 字段 | 值 |
|---|---|
| 网络 | Arbitrum One / 42161 |
| 治理Safe | 0xFbB37c66372f7B40361fBC8C8A235ae92711399D |
| to | 0x1333480e92de9511dc9bb01f70901ff3ee94f613 |
| value | 0 |
| operation | CALL / 0 |
| 函数 | executeCall(address,bytes) |
| target参数 | 0xA113e2E9620a3Bc088a681eBB2C234FDbeb85e21 |
| bytes参数 | 0x8456cb59（pause()） |

完整data在DRAFT-pause.safe.json。文件SHA256：d25f2cca92786a841d33190f5a454195b1bca116231131faa74b663b0836a21e。

## Safe队列与签名前检查

01:16 UTC经HTTPS重定向查询Safe官方服务api.safe.global/tx-service/arb1，在nonce>=10、executed=false条件下返回count0且无下一页。说明该服务当时未列出待执行提案，不证明没有未上传的离线签名或在途交易。初次308结果保留在summary，后续成功查询另存receipt-and-queue-review.json，未把历史记录改写成成功。

在维护窗口打开实际Safe网页准备交易后，再核对真实nonce、队列、to/value/data/operation及所有费用字段和SafeTxHash。nonce10只是此次观察值。若网页增加MultiSend包装或改变交易结构，须审阅实际结构，不能直接套用本次单CALL结论。

## 暂停范围与时序

pause()本身没有绑定expectedLatestNode等状态约束。旧validator或其他提交者可能在暂停上链前继续改变状态。本方案是先协调旧提交停止，再暂停，暂停成功后重新固定恢复A/B；不得使用暂停前节点号直接生成最终恢复包。

暂停不等于清除已有Fast Safe approveHash；恢复后旧批准仍可能被执行，需配合旧提交/快确认协调。此次pause不会自动退款、修改WASM或创建恢复节点，也不意味着排序器、Inbox及Outbox全部停用。

## 证据

- live-pause-audit-3/summary.json：固定块权限核对及完整fork执行。
- live-pause-audit-3/steps.json：模拟approveHash与Safe执行回执。
- live-pause-audit-3/receipt-and-queue-review.json：实际事件字段解码与队列补查。
- 本地contracts-legacy/src/rollup/RollupAdminLogic.sol pause、AdminFallbackProxy.sol路由。
- https://raw.githubusercontent.com/OffchainLabs/upgrade-executor/main/src/UpgradeExecutor.sol （executeCall使用普通CALL）
- https://raw.githubusercontent.com/safe-global/safe-deployments/main/src/assets/v1.4.1/safe_l2.json
