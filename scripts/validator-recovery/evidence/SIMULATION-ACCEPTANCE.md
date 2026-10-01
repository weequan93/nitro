# 恢复模拟验收汇总

更新：2026-09-28。依据：操作者回传的服务器报告、交易信息及日志；本地未直接连接服务器复查。
结论：固定历史候选的主要恢复机制与临时账户 Nitro 自动提交路径已通过。
这不是全部历史数据审计或生产上线批准；readyForProduction=false。
用户指定顺序：先完成模拟，之后才开展生产操作。原签名机器的配合暂缓。

## 固定测试范围

- 父链快照：0x1e58e750；模拟chainId31337，真实Arbitrum One为42161。
- 恢复前确认节点42883，原未决42884/42885；治理恢复节点42886。
- B：本地块126237291，Batch320188/Pos225，hash2654eb1f…014189。
- C：本地块126237355，Batch320189/Pos0，hash6fef0856…011605。
- 新WASM：0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421。
- 原账户：5cda45a9ae0e52f1d5110dc3819f6fb96bade33a；模拟退款/重加入对象。
- 双账户额外用例同时覆盖d38e969ae2947019e0dec0e46937e6aff651834d（原节点158）；生产退款范围未改变。
- 保留现有Rollup/Outbox；不是新部署一条生产链。

## 已通过的项目

| 项目 | 证据/结果 | 边界 |
|---|---|---|
| 两个历史staker退款及重加入 | two-staker-rejoin-mock-20260928-012314；5cda和D38e退款入账、旧zombie清理、共同质押普通节点42887并普通确认，stakeCount=2 | 两地址模拟身份，无原机器运行/签名，不代表生产已迁移 |
| 治理恢复合约路径 | candidate-contract-mock-20260927-011402；暂停、退款、改root、创建及确认恢复断言、resume等通过 | A->B使用synthetic numBlocks1，不是执行证明 |
| Safe授权路径 | candidate-safe-mock-20260927-012720；3/4 approveHash执行，2份签名拒绝 | fork模拟owner批准，不是生产ECDSA签名 |
| 提现领取标记保留 | 检查索引0..8119共8120个标记，恢复前后一致 | 未核查全部历史消息/证明/资产与受款人 |
| 提现实际执行样本 | candidate-withdrawal-mock-20260927-013145；8118重复领取拒绝；8119前后付款、spent变化、重复拒绝通过 | 两个样本；模拟领取状态已回退，不表示真实领取 |
| 暂停期间本地验证 | paused()=true时，从B的assume-valid起点验证至C，后续64条消息带新WASM根 | B本身为可信起点；不补出A->B证明 |
| 原账户重新质押及普通确认 | paused-original-rejoin-20260928-000731；退款到账、清理zombie、普通创建/确认42887 | 原EOA模拟身份，普通交易由脚本构造 |
| Safe原子恢复及失败回滚 | atomic-recovery-mock-20260928-001611；精确expectedNodeHash，成功后保持暂停；故意使最后调用失败时内部恢复状态回滚 | 负例Safe nonce消耗；不是外层所有状态回滚 |
| 真实Nitro进程自动签名/提交 | nitro-autosubmit-retry-20260928-005748；临时密钥自动创建及普通确认42887，numBlocks64 | 测试地址未模拟身份；不是原生产机器/账户验收 |
| Nitro正常重启状态保留 | nitro-restart-mock-20260928-010622；自动创建/确认后重启，恢复验证和质押状态，90秒内无额外发送nonce或断言 | 同一活跃固定fork，正常关闭/启动；不覆盖强杀、Anvil重启或新批次持续运行 |

这些证据来自同一固定候选的不同运行/分支，不是一笔交易或一次从头到尾的全新自动运行。

## 最新自动提交证据

状态：nitro_autosubmit_passed。
测试账户：0x80518359447a87d1197e8fdd890c85ef068c4c43。
测试地址由新临时私钥签名，没有使用Anvil impersonation；资金和白名单仅存在于fork。

创建交易：0x75127bfe59830185227bfc8d7b3d3c829d31238daa13382fc7390b1c0321d69d。
确认交易：0xf365d7f9abb7401d72d994e3a63b629ff897702019840229ae4a318e166af232。
普通断言numBlocks64，latestConfirmed推进到42887；检查签名字段、发送人、chainId、
调用、精确节点承诺、验证终点与Outbox根映射通过。

终点BlockHash：0x6fef08565d967d5c57521d4784e9fe58355c60ecf306b8641d477ed5c1011605。
终点SendRoot：0xac44727106df865ba77137bcea131330935688cf48174d77b2b41a65613b03cf。
终点位置：(320189,0)。新WASM根与上文一致。

确认期限由脚本在fork中挖空块推进，非真实等待生产挑战期；普通创建/确认交易由Nitro发送。
数据库可复用前一轮的验证进度，不能将此轮写成重新重放全部64条消息。

## 当前结束状态

- 自动提交轮recovery-nitro-autosubmit及后续重启轮recovery-nitro-restart-check均已报告停止；最新重启轮ownedForkStillRunning=false（本轮18550已关闭）。
- recovery-paused-watchtower未自动重启。
- 旧18549 fork不在本次管理范围，不能凭本报告推断其当前运行状态。
- 本次脚本不停止snapshot-sync，不打开/data文件或改变重要validator。
- /data_new数据库已经用于模拟链，仍只能作为演练数据库。
- 不需要为重复观察这轮已通过的结果而启动停止后的容器；18550已关闭。

## 仍不能作出的结论

1. 不能认定治理A->B是有效的普通执行，也不能把合成numBlocks1直接批准为生产参数。
2. 双账户用例已覆盖该历史快照全部两个staker的合约退款和重加入；不能据此认定生产账户已迁移、当前实时参与者集合相同，或两个原validator进程均可运行。
3. 不能认定全部历史提现或全部后续交易正确；领取标记检查与完整提现审计不同。
4. 不能认定原机器的密钥解锁、实际配置和自动签名已验收；此次自动运行使用临时账户。
5. 不能把旧候选用于当前生产；它已落后实时确认位置。
6. 不包含长期运行、故障恢复或多个独立validator同时运行的一致性验收。

后续生产参数若改变，需用确定的新参数补做相应模拟；不无目的重复已通过的固定历史用例。
在生产事项明确授权和准备前，不启动生产暂停、质押、签名或广播。

## 正常重启模拟已通过

服务器回传nitro-restart-mock-20260928-010622，status=nitro_restart_rehearsal_passed。
测试账户0x923087020097899a17330a572c3b2224fd5d543d，临时私钥，testSignerImpersonated=false。
自动创建交易：0xc042a3c8db6615faff6f16abd998204b488e121194f166f6a509ec947b291095。
普通确认交易：0xb5f4633a59db0b82fc43614538ecdbc7dbaaff17f818ce86a9f9710f42a8bd4c。
正常确认42887、numBlocks64后，正常重启同一测试容器，验证终点恢复为上述C和新WASM。
90秒观察通过，noAdditionalSenderNonceObserved=true；不是长期稳定性或后续批次推进证明。
testContainerStopped=true，ownedForkStillRunning=false，watchtowerAutoRestarted=false。
此项现已计入通过表，不需要再重复同一固定用例。

## 双账户恢复已通过

服务器回传two-staker-rejoin-mock-20260928-012314：two_staker_rejoin_passed。
固定历史快照中的5cda与D38e共同退款、分别重新质押到同一个普通断言42887、普通确认通过。
两地址addressHasCode=false，sameNodeStakeCount=2，普通断言numBlocks64。
本轮Anvil已停止；没有Docker或节点数据库访问。
交易哈希和验收边界见TWO-STAKER-REJOIN-MOCK.md。两账户采用模拟身份，无真实私钥签名。
此项不改变生产退款范围，不替代原账户运行环境验收，也不补足治理A→B执行证明。

## A位置与消息跨度审计结果

recovery-message-span-20260928-013625完成历史只读审计：父链A和保留链同位置A′
均在(320161,0)，但BlockHash不同；SendRoot相同。
本地A′消息count126229454，B消息count126237292，差7838。
该跨度不是A→B执行证明；本次executionReplayed=false、chainMutations=false。
详见RECOVERY-PARAMETERS.md中的完整哈希与后续工作边界。治理恢复参数仍未批准。

## 本地A′→B全区间重放已通过

用户回传retained-span-replay-20260928-014843：retained_span_replay_passed，
validatedMessagesTotal=validatedMessagesThisRun=7838，executionReplayed=true，nextMessage=null。
明确新WASM根、逐条起止状态连续性、区块哈希和最终B检查通过，耗时6748.15秒。
记录摘要链dfdc67af2b21cb262d39a297e7517c482cba94219c5aa19bbe6ea1fd8f482435。
parentConfirmedAToBProven=false、productionNumBlocksApproved=false保持不变。
执行方式及证据限制见RETAINED-SPAN-REPLAY.md；不重复已通过的重放。

## 7838跨度约定的顺序治理演练已通过

更新rehearse-two-staker-rejoin.py的可选--governance-span 7838模式。
该模式先检查完整7838条重放记录链，再用位置跨度约定和精确非零expectedNodeHash
执行Safe顺序治理恢复、两账户重加入及B→C普通确认。9项本地测试通过。
服务器回传governance-span-mock-20260928-081940：governance_span_rejoin_passed。
恢复节点42886精确哈希1c44f95dd83a8e24c4794528d57b32b053a516204151418fce05e8a75e670b7b匹配；
两账户退款/重加入普通节点42887通过，numBlocks64普通确认通过。本轮fork已停止。
这不是7838的生产批准，也不是7838版本原子批次/真实Nitro运行测试。
详见GOVERNANCE-SPAN-MOCK.md。

## 7838原子批次：已回传通过

已准备 rehearse-atomic-span-recovery.py，复核7838份保存的重放记录，捕获已替换为7838及
精确哈希的治理调用后测试Safe原子批次。负例核对父节点、最新/下一节点、质押、退款、
指针和Outbox状态回滚；成功保持暂停。12项本地检查通过。
操作者回传atomic-span-mock-20260928-084057：contract_rehearsal_passed，
innerRollbackTested=true、atomicSuccessTested=true、paused=true，精确节点哈希与顺序演练一致。
见ATOMIC-SPAN-MOCK.md。

## 7838参数Nitro运行：已回传通过

nitro-span-restart-20260928-084752：nitro_restart_rehearsal_passed。
真实临时密钥自动创建/确认普通节点42887（numBlocks64），正常重启后90秒无额外nonce/断言。
治理numBlocks7838及精确恢复哈希核验通过；本轮测试容器停止，ownedForkStillRunning=false。
运行器沿用顺序Safe恢复，原子批次另有通过证据，不声称两者在本轮串联执行。
本组历史场景验收收尾，剩余生产前事项见SIMULATION-CLOSEOUT-20260928.md。
