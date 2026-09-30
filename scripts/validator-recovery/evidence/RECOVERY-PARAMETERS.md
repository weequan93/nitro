# 恢复断言参数审阅（2026-09-27）

状态：审阅草案，不是生产签名包。治理跳转A->B演练使用synthetic numBlocks=1；
后续普通断言B->C已用实际验证的64条消息测试numBlocks=64。两者不能混用。
依据为仓库 contracts-legacy 源码及历史 fork 接受结果；没有在本轮完成源码编译产物
与部署字节码的可复现匹配，不将源码分析单独视为部署版本证明。

## 固定历史候选

父链0x1e58e750；A=confirmed42883，位置(320161,0)，inboxMaxCount320161。
B=本地block126237291，位置(320188,225)，SendRoot
0xac44727106df865ba77137bcea131330935688cf48174d77b2b41a65613b03cf。
B的必需批次数为320189。该值不等于prevNodeInboxMaxCount，也不是numBlocks。

## 字段及来源

| 字段 | 含义和必须核对的来源 |
|---|---|
| prevNode | 执行时latestConfirmed，不能硬编码旧42883 |
| prevNodeInboxMaxCount | A创建时记录的inboxMaxCount，与A的before-state承诺一起验证；不能用当前bridge计数替代 |
| beforeState | 父链A实际确认的BlockHash、SendRoot、Batch、Pos和machineStatus |
| afterState | 保留链B经过核对的上述字段；单消息重放只支持本地前置状态到B |
| required batches | FINISHED且Pos>0时Batch+1，否则Batch；必须不超过当前bridge计数 |
| new node inboxMaxCount | createNewNode读取当时bridge.sequencerMessageCount，参与新节点stateHash；父链继续收批次可使其变化 |
| WASM root | 节点机器文件根与治理设置根一致；修改根本身不会修复A到B历史 |
| numBlocks | 挑战执行跨度，进入executionHash；不能从28个批次或两个分支高度差推导正确执行 |
| expectedNodeHash | 原子演练已使用精确非零值；生产需按实际兄弟节点、inbox累加器、root和numBlocks重新核定，不能复用历史值 |
| forceConfirm参数 | 必须与新节点confirmData绑定的B.BlockHash、B.SendRoot一致 |

## 为什么合约接受1不代表正确跨度

RollupAdminLogic.forceCreateNode绕过普通用户stakeOnNewNode的最低跨度/时间等检查，
但保留createNewNode的旧状态承诺、inbox单调性和批次可用性检查。
RollupLib.executionHash把numBlocks放入challenge state哈希；forceConfirmNode
跳过期限/质押/zombie检查，confirmNode只核对confirmData等合约条件。
因此强制创建并确认成功，不是执行证明。

当前无证据显示新WASM可以从旧分支A执行得到保留链B。若仍选择该恢复路线，必须
在治理提案中明确这是受信任检查点替换，说明跨过的范围和原因；不能声称numBlocks=1
代表真实单步执行。当前不推荐凭空换成另一个数，也不将1批准为生产值。
需完成部署语义核对、恢复承诺约定及独立审阅，再在最终字段下完整演练。

## 执行前状态变动

生产父链继续确认时，A可能赶上或超过旧B。此时旧候选不再适用于向前恢复，不能
删掉检查、回退父链或复用旧calldata。保留历史演练证据，重新选择前方B并核对。
check-recovery-candidate-drift.py只读采集当前A并比较B位置和本地B哈希。
暂停Rollup不等于冻结Inbox批次；生产交易顺序/原子性与每步状态校验另行审阅。

## 已通过与未覆盖

已通过Safe approved-hash 3/4路径、8120条spent保留、8119恢复前后真实模拟到账、
8118及8119重复领取拒绝。旧候选另有validator继续验证、普通创建确认、原EOA重加入证据。
本候选已补足暂停Watchtower的64条消息验证、原账户脚本重加入及普通创建/确认，
以及原子恢复成功/内层回滚。未覆盖完整历史提现审计、原validator实际自动签名提交、
生产Safe ECDSA签名和最终参数批准。

## 2026-09-28：消息跨度核对已完成

后续临时账户Nitro自动签名/提交、正常重启和两个原地址模拟重加入均已通过，
详见SIMULATION-ACCEPTANCE.md；原机器真实签名与最终治理参数仍未验收。

源码核对：

- staker/legacy/l1_validator.go:createNewNodeAction以validatedCount-startCount计算普通numBlocks。
- staker/block_validator.go:GlobalStateToMsgCount先由Batch/Pos计算消息计数，随后同时比较BlockHash和SendRoot；
  相同位置但哈希不同会报ErrGlobalStateNotInChain，不会仅按计数认可该状态。
- arbutil/block_message_relation.go定义块高与消息索引的固定genesis偏移。
- contracts-legacy/src/rollup/RollupLib.sol:executionHash将跨度加入挑战承诺；stateHash与confirmHash不含跨度。
- RollupCore.createNewNode验证旧状态承诺和Inbox条件，不执行A到B的WASM计算。

因此当前先运行audit-recovery-message-span.py，回答以下事实问题，而不直接选择生产numBlocks：

1. 固定父链A的stateHash是否仍与归档RPC的历史节点存储一致？
2. 本地相同(Batch,Pos)对应哪条消息、哪一个BlockHash/SendRoot？
3. 两个状态是否一致；本地该位置到B的消息计数差是多少？

脚本通过arb_findBatchContainingBlock查批次边界，仅为A位置记录一次validation input，
核对返回Id、ExpectedEndState和规范块头；B使用已保存输入及当前本地固定块头核对。
不把eth_getBlockByHash未找到单独作为分歧证明，不把位置差写成A到B执行证明。
记录验证输入可能产生CPU/I/O和内部缓存活动；不修改链状态、不直接打开数据库、不调用Docker。

```sh
export PATH="$HOME/.foundry/bin:$PATH"
export ARCHIVE_RPC
cd /data_new/scripts

python3 audit-recovery-message-span.py \
  candidate-fork-126237291 \
  --node-rpc http://127.0.0.1:8349 \
  --out "recovery-message-span-$(date +%Y%m%d-%H%M%S)"
```

需要同目录现有resume-original-staker-current-fork.py及其依赖。
本地6项离线边界/禁止写RPC检查通过。操作者已回传
recovery-message-span-20260928-013625，status=message_span_audited；本地未连接服务器复查。
readyForProduction及productionNumBlocksApproved始终为false。

### 实测状态与跨度

固定父链区块0x1e58e750、确认节点42883。父链A的承诺与历史节点存储校验通过。

| 状态 | 消息计数/位置 | BlockHash |
|---|---|---|
| 父链已确认A | Batch320161/Pos0；该报告未证明其可映射为本地有效执行状态 | 0x5e4014f6b16243713005202c40d2ebfe0eb9cdd7f4adf025545d2975d833ace6 |
| 保留链同位置A′ | 本地消息126229453之后，count126229454；Batch320161/Pos0 | 0x2de7415d0a7d5e4fe6339a83ffe2cb43da57d6cd4c7118e4eea584151f0451c2 |
| 候选B | 本地消息126237291之后，count126237292；Batch320188/Pos225 | 0x2654eb1fc5b10d89fa6e3a75b5ec06641aa9c2856b39c0c07468c113b2014189 |

三者SendRoot均为0xac44727106df865ba77137bcea131330935688cf48174d77b2b41a65613b03cf。
confirmedBlockHashMatchesLocal=false，confirmedSendRootMatchesLocal=true，
confirmedGlobalStateMatchesLocal=false。

计数差126237292-126229454=7838只描述本地A′→B区间。
这不是父链A→B的执行证明，不能自动用7838替换1作为已批准生产值。
SendRoot相同不证明区块或全部账户状态相同；本报告也没有定位造成两块哈希不同的具体字段。

### 后续工作边界

retained-span-replay-20260928-014843已重放消息126229454至126237291（两端包含，共7838条），
使用明确的新WASM根核对从A′出发的状态连续性及B终点，7838条全部通过。
这项重放只证明保留链区间，不会将父链A变成A′或补足治理A→B证明。
若继续采用受信任检查点恢复，需单独明确其治理承诺和跨度约定；当前1与7838均未被批准为生产参数。
本次无WASM重放、无链状态修改，不改变先模拟后生产的顺序。

上句“本次无WASM重放”指message_span_audited这次位置审计；后续7838条重放已单独通过。
下一步仅在历史fork测试治理numBlocks=7838作为位置跨度约定，明确保留A→B受信任检查点性质。
同时计算精确expectedNodeHash并检查部署合约生成值、后续普通断言路径；见GOVERNANCE-SPAN-MOCK.md。
更改numBlocks会影响executionHash/nodeHash，不能复用旧1版本的恢复节点哈希或Safe calldata。

### 7838位置跨度约定：顺序合约演练通过

governance-span-mock-20260928-081940已回传governance_span_rejoin_passed。
精确治理节点哈希0x1c44f95dd83a8e24c4794528d57b32b053a516204151418fce05e8a75e670b7b通过部署合约校验。
Safe顺序恢复、双账户退款及重新质押、64消息普通断言创建/确认通过。
该证据支持7838这一位置跨度约定在本历史用例中的合约兼容性，不构成A→B执行证明或生产批准。
新参数的原子批次和真实Nitro运行尚未在此轮验证；详见GOVERNANCE-SPAN-MOCK.md。
