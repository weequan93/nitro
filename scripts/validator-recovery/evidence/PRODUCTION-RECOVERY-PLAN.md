> 当前版本（2026-09-29）：请以 [三账户生产准备方案](PRODUCTION-PLAN-CURRENT-20260929.md) 为准。下文为历史记录；旧单账户退款范围和未完成事项不代表当前结论。

# Deriw 生产恢复准备方案

状态：准备中，禁止将旧fork交易直接用作生产Safe提案。

## 已验证和未验证

- 已验证：治理恢复检查点，新WASM有限区间续验，新测试EOA正常质押、
  提交42686及普通confirmNextNode，终点承诺一致。
- 已通过旧快照演练：原EOA退款后用相同身份重新质押/普通提交/确认的合约测试。
  test-original-staker-rejoin.py 从18547只读提取Nitro真实生成的交易，
  在单独18548重建恢复后的fork用原EOA impersonation重放。
- 未验证：原validator进程的真实签名配置、全部旧版本节点迁移、
  真实Safe多签/guard/nonce、完整历史提现承诺及持续新增批次运行。
- 不存在旧确认状态到恢复检查点的执行证明；该跨越是可信治理决策。

## A. 原账户复用演练

1. 保留18547及已有证据。用同一snapshot、archive RPC重建18548，
   沿用现有mock选定原账户退款/提款/清理流程，保留另一个旧staker。
2. 目标必须latestConfirmed=latestNodeCreated=42685，恢复stateHash/nodeHash
   和source匹配；原账户未质押、无zombie、退款余额零、白名单仍启用。
3. 原EOA仅在18548模拟资金和身份，不读取生产私钥；重放Nitro正常创建交易。
4. 验证stateHash/confirmData、原账户质押节点及金额；满足两个确认deadline
   后普通确认；保存交易回执，禁止forceConfirm替代。
5. 如当前source/snapshot条件变化，停止，重新选择一致快照和测试模板。
6. 此项通过仅证明合约接受原身份复用，不等同于原二进制兼容或原签名恢复。
   老节点必须用明确匹配新WASM的二进制和保留链检查点，另测重启后验证。

## B. 生产只读准备及证据

- 固定父链blockHash/number，以同一标签收集Rollup实现代码、管理员、权限、
  Safe owners/threshold/guard/modules/nonce、Bridge/Inbox/Outbox/ChallengeManager。
- 收集最新confirmed/created/firstUnresolved，全部staker的节点、challenge、
  金额、可提款余额、zombie；按每个账户制定保留/退款/重加入动作。
  活跃挑战不能套用无挑战退款路径，需单独fork演练。
- 选取保留链已落在父链可用batch内且不倒退的检查点，核对完整GlobalState、
  message/block映射、SendRoot、inboxMaxCount。验证检查点及后续可提交区间。
- 源码/原生镜像digest/WASM root/激活规则固定；诊断numBlocks=1不是
  生产参数，必须按部署合约语义制定并复核恢复assertion全部字段。

## C. 提现专项

- 扫描历史Outbox root更新/执行事件，列出累计提款树与各根覆盖的消息数。
- 对照保留链消息顺序、发送方/收款方/token/金额/payload，识别分歧。
- 已领取spent标记必须保留，不能重置/复制出双花入口；检查旧根持续可执行
  的风险。相同SendRoot不证明所有状态相同，几笔成功提现不等于完整审计。
- 对待领取消息逐笔证明核验并模拟，记录恢复前后root映射及领取行为。
  若旧根包含保留链不认可的消息，先设计桥接处理，不直接恢复生产提交。

## D. 协作执行窗口

- 先完成fork和Safe实际调用路径模拟，再安排操作窗口。
- 与其他validator协调停止提交和自动确认；决定Rollup pause时核实实际
  合约覆盖范围。仅停某一validator不冻结链上状态；Rollup pause也不能
  假定等于暂停sequencer、inbox入账或outbox提现。
- 窗口内重新采集生产快照并复核参数，避免方案准备期间confirmed继续推进。
- Safe提案必须明确chain42161、执行目标、calldata、value、顺序和每步后置条件；
  用当前nonce/guard/阈值路径模拟，原始fork impersonation不能替代。
- 退款账户/挑战处理→设置匹配WASM→治理恢复断言与确认→节点对齐→
  先Watchtower验收→恢复正常质押提交。准确pause/resume位置以实测依赖确定。
- 出现断言/SendRoot/权限/代码hash变化时停止并重算，不继续旧提案。

## E. 数据及发布

- 现有/data_new/validator/config被mock写入，不是未改动生产副本。
  原始快照保持不可覆盖；隔离保存模拟后副本，恢复/重建生产副本后再连接真实父链。
- 旧validator分歧数据库不能只换WASM接着跑：迁移到保留链一致快照或在隔离
  数据目录重建，核对检查点hash/SendRoot和后续验证后才启用提交。
- 串行数据库所有权；保存镜像digest和脱敏配置，不归档测试或生产私钥。
- 成功标准：所有参与节点检查点一致、新WASM验证前进、正常NodeCreated与
  NodeConfirmed、提款承诺核验通过。观察窗口和故障暂停方案需操作团队确定。
- 治理改变已确认状态后不能承诺简单回滚；失败时停止后续操作，保留证据，
  重新设计治理动作，绝不重置spent标记。

## 原账户复用验收更新

服务器 original-staker-rejoin-20260925-014625：原EOA5cda45…e33a通过
退款后重新质押创建与普通确认，交易a29dd289…c6a13 / 1a883e1a…9953f2。
A节合约账户复用测试通过；原validator运行/真实签名配置仍未测试。

## 固定生产快照的参与者决策（20260925-015317）

依据parentBlock0x1e4f539a/hashbfbd9b254abf41f1136e271572496acf05fe1c097ff74dfdf54e38a8bfce5a66。
- Safe Fbb37…399D：VERSION1.4.1，4owners/threshold3，nonce10，guard零、modules空，
  fallbackHandler fd0732dc9e303f09fcef3a7388ad10a83459ec99。EXECUTOR_ROLE有效。
  此为快照权限形态，正式nonce/owners/threshold/实现身份须窗口内重读。
- 5cda45…e33a：活跃质押在42703，金额1e12wei，无challenge/zombie/待提款。
  拟采用已演练的选定账户forceRefund→可提款→清理旧zombie→新状态重加入路径。
  钱款withdraw由账户可用授权路径处理，不能把管理员退款等同自动到账。
- D38e…834d：质押在158，金额1e12wei，无challenge/zombie/待提款。
  已通过演练的路径保留该账户质押，不把“所有staker必须退款”作为前提。
  不启动其旧数据节点自动提交；若要重新运行，另做数据对齐和质押迁移验收。
  如业务决定也退款，则需另测该账户退款/重加入，不能沿用5cda测试结论。
- pending42702→42703，为同一条分支，firstUnresolved42702，confirmed42701。
  正式动作应按窗口实时值重新确定；不直接修改节点指针或删除数据库实现回滚。

旧演练点Batch316695不再作为生产恢复候选。/data_new模拟数据目录不能切回生产
充当新检查点源；当前候选来源为原始云快照克隆后的/data_mock独立同步节点。

## 2026-09-27 固定候选演练结果与下一阶段

- 父链固定高度0x1e58e750，hash
  0xc4a99db9563e557c6544abe0e4299ae14f8c641a7c96e938178d2308b5c29c72。
- confirmed42883，pending42884→42885；5cda账户质押42885，无挑战；D38e仍在158。
- Safe3/4、nonce10、guard零、modules空，均为该历史高度结果。
- 候选block/message126237291，位置(320188,225)，blockhash
  0x2654eb1fc5b10d89fa6e3a75b5ec06641aa9c2856b39c0c07468c113b2014189。
- SendRoot为0xac44727106df865ba77137bcea131330935688cf48174d77b2b41a65613b03cf，
  与父链该次confirmed终点相同。恢复会把此根的Outbox映射更新为候选块hash；
  根相同不等同于所有状态相同，仍需记录映射变化及提款行为。
- candidate-replay-20260927-010931已通过本地前置状态到候选的单消息重放。
- candidate-contract-mock-20260927-011402通过合约演练：恢复节点42886；
  pause、选定5cda退款、改root、创建/强制确认、resume、领取退款、清理zombie。
  已核对spent8111–8114保持true，未覆盖所有提现，也未在本次演练中重测节点重启和重新质押。
- 此轮numBlocks=1为合成值，Safe被模拟身份；不能作为生产签名包。

下一阶段按顺序进行：
1. 依据部署版本语义复核恢复断言的所有字段，包括numBlocks和inboxMaxCount；
   不把差异分支两边的普通区块高度差直接当作有效执行证明。
2. 扩展Outbox核验至候选累计发送数覆盖的消息与历史可执行根，核对已领取标记、
   未领取证明及恢复前后模拟执行。最近单笔领取成功不替代完整审计。
3. 准备可审阅的Safe调用草案，并通过Safe合约实际execTransaction路径演练；
   区分模拟owner授权与生产真实3签名，不再只用Safe地址impersonation代替。
4. 在正式窗口协调停止断言提交/确认、冻结并重新采集状态后刷新候选及参数。
   只有复核通过的最终包才提交签名；42886不作为预先保证的生产节点编号。

当前无需重同步、无需反复执行同一合约mock；snapshot-sync保持不质押。

### 2026-09-27 01:27 SGT 补充验收（覆盖上文相应未完成项）

candidate-safe-mock-20260927-012720已通过Safe实际execTransaction路径（模拟owner
approveHash授权，3/4门槛与2签名拒绝检查）；全部0–8119的isSpent在恢复前后不变。
因此Safe执行路径与此候选范围的spent保留检查已完成，不需要重复同一轮测试。
尚未覆盖生产ECDSA签名、全部历史根/未领取消息执行、生产恢复字段numBlocks的
最终语义选择；不能把合成1替换成随意高度差。当前候选仍仅为固定历史演练点。
保持readyForProduction=false；生产步骤、签名包与窗口实时状态须另行核定。

### 2026-09-27 01:31 SGT 提现行为补充验收

candidate-withdrawal-mock-20260927-013145通过。8118在固定快照已领取，恢复前后
均拒绝重复领取；8119恢复前后均实际领取10.96 USDT到账，spent置位且重复领取
被拒绝。所有测试领取均回退模拟状态；真实父链资金未动。Safe门槛与全8120条
spent保留检查再次通过。不把两笔样本执行等同完整历史提款审计。

剩余生产重点：恢复断言字段（合成numBlocks=1尚无生产核定）、A到B的受信任治理
跳转范围、最终候选及当前链上状态、完整提款审计范围和节点上线验收计划。
旧候选的节点继续验证/正常提交/确认/原账户重加入测试已记录，但本候选本轮未重测。
生产Safe真实签名仍待最终参数审阅后进行，当前固定历史包不可直接广播。

### 2026-09-27 01:38 SGT 候选时效：禁止复用旧候选生产执行

当前confirmed42885位置(320189,0)已超过旧候选B(320188,225)。
旧候选126237291只保留为历史测试夹具；不用于当前生产forceCreateNode参数。
先完成最终恢复字段/治理方案审阅，再安排协调窗口。在窗口内控制自动断言提交与
确认，核对实际latestConfirmed，选取可用的前方B，完成最终参数检查和模拟。
停止自动提交本身不保证其他账号不确认；Rollup pause也不等于Inbox停止收批次。
最终calldata必须带充分状态约束，签名期间状态变化需使操作失败或重新核定。
当前不停止重要节点、不重复已通过的历史mock；readyForProduction=false。

### 2026-09-28 同一历史候选的阶段恢复闭环通过

paused-original-rejoin-20260928-000731通过Safe.resume、原账户退款领取、zombie清理、
普通重新质押创建42887与普通确认。结合暂停Watchtower的64条验证，已覆盖同一候选的
分阶段合约/验证流程。原账户为Anvil模拟，创建确认由脚本发送，真实validator签名运行
仍未测试。原子批量恢复、最终治理字段和完整提款审计尚未完成，不反复重跑此已通过闭环。

### 2026-09-28 00:16 SGT 原子包机制已通过

atomic-recovery-mock-20260928-001611验证MultiSendCallOnly代码身份、四步Safe原子恢复
成功且保持暂停、失败包内层状态回滚。exactExpectedNodeHash已启用。
负例Safe nonce会消耗，与内部恢复状态回滚分开记录。无需重跑同一历史用例。
尚待最终生产字段/治理约定、完整提款审计或明确风险接受、真实签名/runtime以及窗口实时状态。
