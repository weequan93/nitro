> 当前版本（2026-09-29）：请以 [三账户生产准备方案](PRODUCTION-PLAN-CURRENT-20260929.md) 为准。下文为历史记录；旧单账户退款范围和未完成事项不代表当前结论。

# 生产恢复执行草案

**最新盘点变更：** 20260928-085449在父块509551730发现第三个质押账户65fa…bc95，
与5cda同在43007。下文“仅退款5cda、D38e不动”是旧草案，不再是完整参与者方案。
待确认65fa运行归属并重新核定每个账户的处置；不自动扩大生产退款范围。
见PRODUCTION-REFRESH-20260928.md。暂不生成可执行生产参数。

2026-09-28更新：7838参数的原子批次与Nitro自动提交/正常重启历史演练分别通过。
最新汇总及下一阶段边界见SIMULATION-CLOSEOUT-20260928.md；下方追加记录按历史时间保留，
旧段落中的“尚未验收”不代表最新汇总状态。生产参数和真实账户运行仍未批准/验收。

2026-09-27；仅供审阅。没有生成可广播calldata，没有发起暂停、部署或签名。

## 核心决定

保留当前L3历史及现有Outbox，通过受信任治理检查点恢复；不是回滚所有L3交易。
历史候选126237291已落后父链，仅用作测试。生产A/B与新node编号不得照抄。
当前阻塞为最终断言字段与治理约定、审计覆盖、生产节点验收和最终交易包核验。

## 0. 窗口前完成，不暂停生产

- 明确operator负责的staker地址与自动提交/确认进程；不能仅凭容器名判断角色。
- 各validator负责人提供拟上线镜像、WASM根、数据来源、检查点一致性检查方法。
- snapshot-sync继续使用/data_mock数据、真实父链、关闭质押；/data_new模拟数据库隔离。
- 确定恢复窗口及3位Safe签名人；保留原始云快照。
- 完成恢复承诺numBlocks约定及独立合约审阅；不把1或高度差宣称为有效执行跨度。
- 明确完整提现审计或抽样验收的业务接受范围，不能把样本通过改标为全历史审计。
- 历史候选已覆盖保持暂停、Watchtower验证后续64条消息、再resume及原账户合约重加入。
  原validator实际签名/自动提交仍未验收；合约脚本模拟不替代运行验收。

## 1. 治理暂停作为单独边界

在上述条件完成后，由Safe执行Rollup.pause，并确认receipt、链ID42161、paused=true。
在此之前协调停止受控validator自动提交/确认；无需为此停止重要节点的同步/RPC服务。
生产暂停本身是单独需执行的治理动作，本文件不是执行指令。

暂停确认后，重新采集latestConfirmed、latestNodeCreated、全部staker/challenge、Safe
nonce/owners/权限、实现代码和Outbox。暂停交易之前取得的A不直接用于最终恢复包。
暂停普通Rollup操作不意味着冻结sequencer、Inbox或Outbox提款。其他管理员仍可能修改状态。

## 2. 在暂停后的状态上确定最终A和B

A来自实际最新确认节点。B在保留链上，inbox位置应严格领先A，所需批次已在父链可用。
固定B块hash、sendRoot、message索引、Batch/Pos、机器状态与newWASM；保存重放证据。
确认所有拟恢复validator能取得B及其后续数据。
再次检查未决断言、挑战、退款对象；当前策略仅退款5cda，D38e保持不动，变化则重审。

## 3. 恢复操作原子包草案（保持暂停）

拟将以下管理员调用合为一次Safe交易：
1. forceRefundStaker([5cda...])
2. setWasmModuleRoot(newRoot)
3. forceCreateNode(A, A.inboxMaxCount, assertion(A,B,numBlocks), exactExpectedNodeHash)
4. forceConfirmNode(预期新node编号, B.blockHash, B.sendRoot)

拟通过经核验的批量调用组件承载，每条调用目标为UpgradeExecutor.executeCall(Rollup,data)，
保持管理员身份路径。历史演练已核验MultiSendCallOnly v1.4.1部署代码并测试Safe
delegatecall；正式执行前需重新核验同一组件、嵌套调用顺序和实时参数。

所有子调用失败时整包回滚，避免退款/改root成功而恢复失败的中间状态。
exactExpectedNodeHash、latestConfirmed检查和confirmData检查提供部分保护，但不是
全部前置条件检查：Safe nonce不保护Rollup状态，普通批量调用getter也不会自动断言返回值。
必须另行核定必要的执行时状态断言或受控暂停后的状态约束，不能声称现有mock已覆盖。

Inbox可继续增长：createNewNode会采用执行时bridge计数写入新节点stateHash，后续普通
断言需消费相应范围。需明确该动态值的接受条件，不能误称所有状态在pause后都冻结。

## 4. 保持暂停验收，再单独恢复

核对新latestConfirmed、B承诺、WASM根、Outbox身份/根/领取标记、质押退款credit。
先让准备好的Watchtower对齐B，用新root验证后续实际可用消息；不得共用打开的数据库。
暂停期间staker相关报错与真实block-validator失败须分开判读；历史fork此项已通过。

退款可提款额度不等于钱包已到账。提款/清理zombie的具体时机按函数暂停限制和模拟结果确定，
不能直接搬到pause期间；当前removeOldZombies要求whenNotPaused。

验收通过后另行Safe.resume。只启用已经对齐B的指定validator，原账户按核验流程重新质押。
先观察普通NodeCreated/NodeConfirmed与提款，再依次恢复其他validator提交。
未对齐的旧validator保持不提交，不用旧数据库加新root直接上线。

## 5. 失败处置

暂停后、恢复前失败：保持暂停并排查；是否resume旧配置需结合故障原因和当前状态决定。
原子恢复包回滚：应核实全部状态未变，不继续假定某一步已成功。
恢复成功后验收失败：保持暂停，保存证据；不承诺可简单回滚，尤其不能撤销真实已执行提款。
任何执行中状态与已审阅包不一致：停止后续步骤，重采集，不使用零expectedNodeHash绕过检查。

## numBlocks的新增源码依据

staker/legacy/l1_validator.go的普通断言构造使用validatedCount-startCount。
这是同一条验证执行路径上的消息计数差，不是父链区块差，也不是Batch差。
本次A来自分歧历史，B来自保留历史，仅用本地对应位置计算差值并不能证明A->B。
仓库源码分析尚未替代部署字节码可复现核对；最终字段选择仍未批准。

## 补充测试：暂停边界已通过

用户服务器已通过独立历史fork的paused_checkpoint_ready检查，恢复确认后paused=true，
退款credit保留，普通stakeOnExistingNode及removeOldZombies正确拒绝。
未执行resume/领取退款/清理，fork18549保持运行。此结果只补足合约暂停边界，
Watchtower在暂停链上的运行验收、原子包及生产字段核定仍未完成。

## 补充测试：暂停期间Watchtower验收已通过

2026-09-28用户回传paused()=true及启动日志，Watchtower以B为assume-valid起点，
messageCount从126237292增长到126237356（64条消息），均以新WASM验证，
终点(320189,0)/hash6fef0856...011605。证明保持暂停不会阻止这段本地后续验证。
B的assume-valid记录本身没有重放证明；后续验证成功也没有补出A->B的执行证明。
固定fork的后续批次缺失限制了测试跨度，不要求等待它追上实时链。
本候选resume后的原账户重加入/普通断言创建确认及原子恢复包尚未验收。

## 补充测试：同一候选的resume及原账户合约重加入通过

2026-09-28 paused-original-rejoin-20260928-000731：在18549同一fork中，暂停Watchtower
验证后，经Safe解除暂停、5cda领取退款/清理zombie、普通newStakeOnNewNode重新质押
创建42887，再普通confirmNextNode确认42887，全部通过。
普通断言numBlocks=64具有B->C已验证消息计数依据。调用由脚本构造并以原EOA模拟
身份发送，不能称为原validator自动签名/自动提交验收。生产治理A->B参数仍另行核定。

## 补充测试：原子恢复成功与内层回滚通过

atomic-recovery-mock-20260928-001611已核验官方MultiSendCallOnly v1.4.1的历史部署代码hash，
四项恢复调用经Safe delegatecall批量执行成功，使用精确expectedNodeHash并保持paused。
故意破坏最后确认的负例发出了ExecutionFailure，内部恢复状态完整回滚；Safe nonce消耗。
正例safeTxGas=0。该结果补足原子批量调用机制，不代表已审核实时生产参数及签名。

当前已验证的机制无需重复同一历史测试：Safe授权路径、退款/账户合约重加入、全候选
范围spent保留、两笔提现样本、暂停Watchtower的64条消息验证、普通断言创建确认、
原子恢复成功与内层回滚。它们在同一候选的不同fork分支/运行中完成，不能描述为一笔
生产交易或同一连续自动化测试。
下一阶段为最终治理恢复numBlocks约定、审计覆盖与风险接受、节点签名运行验收、
暂停窗口内实时候选及最终Safe交易包；readyForProduction仍false。

## 当前下一步：原validator运行配置只读核查

使用inspect-original-validator-runtime.py收集/data_new的原配置声明、容器挂载和
原debug容器历史启动地址。只报告已知字段；钱包密钥/密码/路径值、RPC URL和
命令参数值不输出，不打开keystore。重要validator只读取Docker状态元数据。
不打开/data文件、不启动或停止容器、不签名或发交易。

缺失字段视为未知；CLI、环境与入口脚本覆盖尚未解析，历史启动地址不能证明当前
签名可用。核查结果用于选择下一步的隔离运行验收方式，不能直接启动原生产配置。

2026-09-28收到original-validator-runtime-20260928-002800.json：原配置声明MakeNodes、
keystore路径，无指定account或内联私钥；debug容器已停止，CLI包含staker.enable等
覆盖开关，但本轮只采集了参数名；未发现历史启动身份记录。因此不能确认5cda是
此容器实际签名账户，也不能依据配置文件的enable=true推断旧运行时开启质押。
snapshot-sync和独立测试Watchtower仍运行，重要validator仍运行。

下一份inspect-original-wallet-metadata.py仅补查debug容器白名单布尔参数值及
/data_new/validator/keys的V3 keystore公开address头。地址头不等于解密验证，
缺少password声明不等于已证明空密码；不解锁、不签名、不改动节点。

2026-09-28收到original-wallet-metadata-20260928-003310.json：完整可读扫描仅发现
V3 keystore声明地址0x65fa6c1d1efc338dfe8f07d5de84b3e7b76cbc95，与拟退款/重加入
地址0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a不同。debug容器CLI明确
node.staker.enable=false、block-validator.enable=true、failure-is-fatal=false；
环境/入口脚本仍未完全解析。不能将原配置中的MakeNodes称作已运行的自动提交者。

此前通过的resume-original-staker-current-fork.py明确断言历史fork中5cda的
eth_getCode为空，原账户重加入证据来自模拟该EOA，不是65fa控制合约钱包的证据。
当前不打开受保护/data、不复制或解锁密钥、不更改退款对象或白名单。
下一步需要操作者确定5cda的实际签名节点/签名器归属；现有/data_new keystore
不能作为5cda的实际自动签名验收依据。若选择65fa新账户是另一条恢复路线，
需要独立核定其权限及生产角色，不能悄悄替换原账户测试。

用户已确认5cda由另一台机器管理且其负责人可以配合。新增
ORIGINAL-VALIDATOR-OPERATOR-PLAN.md和独立inspect-remote-validator.py：先在对方
机器采集筛选后的容器/可选配置/启动身份信息，再按钱包方式准备签名和隔离运行验收。
密钥不转移，生产服务本轮不启停。仓库存在staker.data-poster.external-signer能力，
但实际镜像与远端协议尚需核验。身份签名、真实签名fork交易、validator自动提交
分别记录，不能互相替代。本轮交付只读脚本与分阶段计划，无签名服务或生产交易。

用户随后明确调整顺序：先继续并完成模拟，之后才开展生产配合。
原机器真实签名核查不作为当前演练阻塞条件，协作计划暂存。
下一轮见NITRO-AUTOSUBMIT-MOCK.md：新18550 fork、临时测试私钥、真实Nitro
自动创建/确认；仅切换/data_new演练容器。此轮结果尚待服务器回传。

后续服务器回传nitro-autosubmit-retry-20260928-005748：临时账户真实Nitro自动
签名创建及确认42887通过，普通断言64条消息，相关回执/节点/Outbox核验通过。
测试容器和自有18550 fork已停止。当前模拟总表见SIMULATION-ACCEPTANCE.md。
这补足临时签名账户的自动运行路径，不替代原账户运行环境、生产Safe签名、最终参数
及全历史审计。用户要求生产操作延后，本轮没有生产操作。

nitro-restart-mock-20260928-010622进一步通过同一活跃fork内正常重启测试：
临时账户自动创建/确认后，Nitro恢复相同验证终点及质押位置，90秒内无额外发送nonce
或断言。测试容器和fork正常退出。正常重启证据不扩展为强杀恢复、长期稳定或
新批次持续流入验收；生产事项继续按用户要求延后。

two-staker-rejoin-mock-20260928-012314额外通过历史快照内两个staker共同退款、
重新质押同一普通断言42887及普通确认。覆盖5cda和旧节点158的D38e；两地址模拟身份，
未使用原私钥或原机器。此结果说明该历史分支下双账户恢复合约路径通过，
不改变本生产草案目前仅退款5cda的范围；若决定扩大范围，需另行明确当前参与者与操作计划。
本轮fork已关闭，不访问Docker或节点数据库；readyForProduction仍为false。

recovery-message-span-20260928-013625进一步证实固定历史A与保留链同位置A′
区块哈希不同、SendRoot相同。本地A′→B的消息跨度为7838，尚未在本轮重放。
因此不得直接将合成numBlocks1替换成7838并描述为父链A→B有效执行；
治理检查点承诺/跨度约定仍需明确。此审计无链状态修改，生产动作继续延后。

后续retained-span-replay-20260928-014843已完成本地A′→B的7838条重放。
governance-span-mock-20260928-081940进一步使用7838作为位置跨度约定，通过顺序Safe恢复、
精确恢复节点哈希校验、双账户退款重加入及普通确认；证据只覆盖这一历史用例。
该轮不是7838参数的原子批次或真实Nitro自动运行验收，不改变生产退款范围，
不把A′→B证据变成父链A→B证明，readyForProduction仍为false。
