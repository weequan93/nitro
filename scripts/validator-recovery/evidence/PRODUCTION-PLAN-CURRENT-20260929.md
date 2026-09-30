# Deriw 三账户生产恢复准备方案

更新：2026-09-29。状态：准备与审阅；未授权或执行生产暂停、退款、WASM 修改、强制确认或恢复提交。本文件取代旧执行草案中的参与者范围及当前进度；旧文件保留为历史记录。

## 1. 恢复目标与信任边界

沿用此前目标：保留当前 Deriw L3 执行历史、现有 Rollup、Bridge、Outbox 和领取记录，通过治理确认一个经过核验的本地检查点，再让匹配的新 WASM 验证器正常提交。

- A：父链 Rollup 已确认状态。
- A′：保留的本地链在同一消息位置的状态。
- B：拟选择的保留链恢复检查点。
- C：B 之后经过新 WASM 验证的普通断言终点。

历史样本已经证明 A 与 A′ 的区块哈希不同。10578 条重放覆盖 A′→B，48 条重放覆盖 B→C；不能标记为 A→B 执行证明，也不能仅凭 SendRoot 相同推断全部账户状态相同。

因此，本方案是**受信任的治理检查点迁移**。生产审阅必须明确接受被信任的保留历史、检查点来源和审计范围。A→B 证明字段继续为 false，不靠重复同样重放改变它。若要求从 A 通过正常执行证明到 B，则需要另外调查/恢复该执行路径，本方案不能替代。

早期 DECISION.md 记录了重复地址处理行为改变可复现分歧的实验。这是已有调查记录，不等于本次已独立复核部署二进制、全部历史与每一个当前分歧点。最终证据包应关联该实验及实际镜像/WASM 来源。

## 2. 当前已报告的模拟覆盖

详见 THREE-STAKER-CLOSEOUT-20260929.md：

- A′→B 的 10578 条及 B→C 的 48 条逐消息重放。
- 三账户原子恢复成功、内层失败回滚、Safe 3/4 门槛负例。
- 三账户退款到账、重新质押同一普通断言并确认。
- 临时密钥的 Nitro 自动创建、确认和正常重启；90 秒无额外交易。
- 全部 8121 个已覆盖领取标记保持一致；已领取样本8118/8119拒绝重复领取，自然未领取样本6591付款；三个阶段均回滚试领。

这是历史固定状态上的机制证据。原生产账户自动签名环境、持续新批次运行、全部历史提现消息审计仍未由这些结果覆盖。无需无条件重做这些历史测试。

## 3. 三账户拟处置与分工

用户已确认三者均为内部节点账户。当前方案按三账户共同恢复编排；最终生产退款清单以最新盘点与具体交易包审阅为准，此文件不发送任何退款。

| 地址 | 已知情况 | 生产准备需要的资料 |
|---|---|---|
| 0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a | 用户确认在另一台机器；历史上参与新节点质押 | 负责人、实际容器/进程、签名方式、镜像、公开发送地址 |
| 0xd38e969ae2947019e0dec0e46937e6aff651834d | 历史盘点停在节点158；已通过模拟退款重加入 | 负责人、是否仍自动提交、真实签名能力及恢复顺序 |
| 0x65fa6c1d1efc338dfe8f07d5de84b3e7b76cbc95 | /data_new 旧 keystore 声明过该地址；声明不等于签名验证 | 负责人、当前实际运行位置与发送地址，不能只靠旧配置推断 |

治理负责人负责 Safe 参数及签名；恢复负责人负责检查点与回执；各节点负责人保管密钥、核对配置与上线顺序。不索取私钥、密码或完整带认证信息的环境/命令输出。

当前准备阶段只做只读核查。涉及重要节点时，必须先确认同步/RPC与质押提交能否分离；不能直接停止 validator-nitro-1。/data 保持不访问。

## 4. 现在执行：一次固定父块的只读刷新

复用 check-recovery-candidate-drift.py 获取新 authority.json，再把同一 authority.json 交给 probe-production-participants.py。后者使用同一 parentBlock/hash 读取参与者和 Safe，避免将不同时间状态混为一个快照。

采集：实际 latestConfirmed/created、当前 WASM、暂停状态、合约实现/权限、全部staker/挑战/退款额度、Safe owners/threshold/nonce/guard/modules，以及历史 B 是否仍在本地链、是否还领先当前确认点。

这次只读刷新用于排定生产窗口和识别变化，不选择最终 B、不开始新长重放、不生成生产 calldata。若历史 B 已落后，这是预期漂移，不撤销历史模拟结论。

用户已说明三台机器的角色、镜像标签和签名方式（见第9节），无需重复调查这些已知事项。可使用 inspect-recovery-operator.py 补齐尚缺的地址到容器映射及运行镜像ID。脚本要求显式指定 --expected-staker，只收集 Docker 元数据及筛选后的启动身份；默认不读取配置文件，不读取密钥，不停止节点。历史日志匹配不等于当前签名能力已验证。

## 5. 生产窗口内的拟执行顺序（当前不执行）

1. 完成具体暂停交易及影响审阅，明确哪些验证器停止提交、哪些同步/RPC继续运行，以及签名人和维护窗口。得到明确执行授权后，才协调提交进程与发送独立 Safe.pause。
2. 暂停回执后重采集真实 A、未决节点、全部账户/挑战、Safe nonce及权限。挑战非零、参与者变化、实现/root变化必须重新分析相关路径。
3. 固定最终保留链 B，验证批次可用、本地hash/SendRoot/消息位置、原机器可取得数据。确定治理跨度约定、精确expectedNodeHash与执行前置条件；生产数字不能照抄10578、43008/43009。
4. 对实际固定状态生成并针对性复核原子包：退款指定账户→新WASM→forceCreateNode→forceConfirmNode；成功保持暂停。调用目标、权限路径、Safe operation、批量组件代码、value/费用和所有参数均入审阅清单。
5. 检查恢复回执、事件、节点承诺、root、Outbox身份、领取标记和退款credit；准备好的验证器以新检查点验证后续消息。credit不等于退款已到账。
6. 验收通过后单独 Safe.resume；按经核验暂停限制执行退款提款、zombie清理及原账户重新质押。指定一个已对齐节点先自动创建/确认普通断言，再逐步恢复其余提交者。
7. 观察新批次持续处理、普通创建/确认、发送账户/nonce、提现行为；按实际节点职责观察同步/RPC可用性。最终账户角色以第14节为准，不能把三个快确认owner都默认为重新质押账户。

Rollup.pause 不保证冻结 Inbox、sequencer、提现或其他管理操作。尤其 bridge inbox 计数可能继续增长，需要规定可接受范围和执行时状态校验。不能把Safe nonce或单独getter调用当作完整状态防漂移保护。若状态变化使已审阅包失效，重新生成相关参数，而非绕过expectedNodeHash。

## 6. 失败处理

- 原子包失败：确认内部状态回滚，同时检查Safe nonce是否已消费；不能直接照原nonce重试。
- 恢复前准备失败：保持当前已确认的运行状态；若已暂停，是否resume旧配置须基于故障原因重新判断。
- 恢复后验收失败：保持暂停并保存证据，不宣称可撤销已完成的真实提款，也不把改回WASM当作完整回滚。
- 配置、签名地址、目标网络或检查点不符：停止后续动作，修正后对相关变更重新验证。

## 7. 当前交付与尚待填入

已有历史模拟结论与只读采集脚本。待当前盘点返回后填入：实时参与者/挑战变化、各账户运行负责人、生产数据与镜像/WASM核验、最终暂停包、暂停后A/B及精确恢复包、真实签名和逐步验收命令。

不需要为获取当前状态再复制数据库。/data_mock继续连接真实父链；/data_new已是演练数据，不能直接改回生产父链并作为生产恢复节点上线。所有实际生产动作留待具体包审阅和用户明确执行授权。

## 8. 20260929-010239 盘点结果（用户贴回）

固定父块 0x1e6275ef，hash 0xf9278cab4618cefe7c8ee1b2ebf260422d1d31ba4fb1938adf6ff38b47bb3f67。

- 已确认43067，已创建43069，首个未决43068。三账户仍在质押，每个1e12 wei、challenge=0、withdrawable=0、非zombie、白名单true；d38e在158，5cda和65fa在43069。
- Safe四owners与先前一致，threshold=3，nonce=10，guard=0，modules=[]，fallbackHandler=0xfd0732dc9e303f09fcef3a7388ad10a83459ec99；参与者报告errors=[]。这些是该固定块的值，签名前必须更新，不能预先锁定nonce10。
- 旧B块126693840仍在本地链，但candidateAheadOfCurrentConfirmed=false，状态candidate_not_ahead_refresh_required。旧B及10578跨度不得直接作为生产参数。
- 此前同报告尾部paused=false、仍为旧WASM。当前贴文没有完整currentConfirmedState，因此没有从节点号反推或编造该状态。

历史模拟已经覆盖机制，不因此自动再做一轮长重放/数据库复制。先完成各节点运行清单、影响范围和具体暂停交易审阅；最终检查点在受控窗口中固定。

## 9. 用户补充的运行部署（2026-09-29）

来源：用户说明，尚未独立核验各机的有效运行参数。

- 共三台纯validator，一台在当前服务器，另两台在不同云；均不承担RPC服务。用户说明当前仍运行旧validator WASM并继续断言。
- 三台镜像标签均为 fuhua-container.tencentcloudcr.com/deriw/deriw:prod。标签相同不作为三台实际image ID一致的证据；正式升级配置需固定经过演练且核验的镜像版本/摘要，不直接假设当前prod标签已是新版本。
- 使用带密码的本地EOA keystore。所贴配置为 node.staker.enable=true、strategy=MakeNodes、parent-chain-wallet.pathname=/keys。无需索取密码、解密或复制私钥；由各负责人保留其本地解锁方式。
- 尚缺三个地址与机器/容器的准确映射。尤其链上d38e在节点158，与“运行中”并不矛盾，但不能据此断言它正在成功提交最新节点；核对其职责和提交身份即可，不推定需要第四个账户。
- 纯validator角色使维护步骤更简单；现有/data禁用与validator-nitro-1不得擅停约束仍有效，本次说明不等于具体停机授权。snapshot-sync继续承担真实父链同步。

生产窗口草案：先准备三个节点对应的配置、镜像和数据来源，随后按经审阅的窗口协调停止旧断言提交、等待/核对在途交易并执行独立治理暂停。暂停后刷新真实链状态与最终检查点，复核原子恢复包。新配置先以不提交交易的验证模式检查后续执行，再恢复Rollup，安排退款提款/清理/重新质押；一个原账户先完成新普通断言，再逐台放开其余两个。不得把仅替换镜像视为已经对齐执行状态，也不得将/data_new演练库直接投入生产。

## 10. 维护准备包交付

用户确认三台旧镜像ID前缀均为becc38893aca。已准备 production-recovery-kit-20260929/RUNBOOK.md、prepare-window.py、operator-roster.json 和所需只读采集脚本。prepare-window默认只采集、核对固定块基线，并生成不可直接签署的pause-review；无Docker操作、签名或交易发送。全Safe路径和实现身份仍需审阅。

本次用户授权为“准备全部流程需要的东西”，尚不是停止当前重要容器或执行pause的授权。各机映射、升级数据方案与Safe使用方式待补齐；最终参数须在窗口中固定后针对性验证。准备包自检5项通过，覆盖基线漂移/挑战拒绝、RPC写方法拒绝、保护路径、阶段检查和pause嵌套calldata。尚未在真实服务器运行新准备脚本。

## 11. 维护前准备检查通过（20260929-082849）

用户回传preparation_inventory_passed：固定父块0x1e640172，confirmed43096/created43098，三账户范围未变、paused=false、旧root，Safe nonce10/threshold3。Safe slot0实现地址及代码哈希已与官方SafeL2 1.4.1的42161 canonical部署元数据交叉比对一致，详情见PAUSE-REVIEW-20260929.md。原服务器报告不回写，不把metadata匹配改称完整签名路径已验证。

暂停调用本地独立编码审阅页已生成；具体Safe界面/签名流程、三机映射及数据就绪方案仍需补齐。保持现有运行，无新的长重放或复制任务。

## 12. 三台运行地址与链上质押范围不同（2026-09-29 00:35 UTC）

用户提供：当前服务器65fa、云A为5cda、云B为21d4ea822a07f737c5e69f7951d517e5f2974849，三机容器均为validator-nitro-1。映射单独保存于operator-mapping/user-mapping-20260929.json，尚未用云B运行日志核对。

随后直接通过公开Arbitrum RPC在固定父块0x1e640741/hash0xa071addc7b07ce1818966d52923fe6507970674377788acaca410d3754772bb1只读查询：activeStakerCount=3，仍为d38e/5cda/65fa；5cda、65fa都在43099，各stake1e12，d38e仍在158/stake1e12，三者challenge=0。21d4为无代码地址、whitelisted=true、isStaked=false、stake=0、withdrawable=0。结果见operator-mapping/address-check-20260929-003541.json。

不能把三台运行节点名单直接替换成三账户退款清单；当前21d4无质押可退，d38e旧质押不能遗漏。需要核对云B日志的txSender/actingAsWallet，及d38e旧账户的负责人/密钥是否仍可用，再决定恢复后参与账户。此差异不代表新增第四台机器，也不自动授权变更退款范围、白名单或生产操作。已有历史模拟覆盖的是d38e/5cda/65fa合约重加入和临时EOA自动提交；不是云B当前21d4实际运行验收。

## 13. 快速确认Safe及其三位owner（2026-09-29 00:39 UTC）

通过公开父链RPC在固定块0x1e640a50/hash0x704ed0e89f6522249a1abd9a6497e9d8de4766987596f16d59e0b79622bb5333读取：Rollup.anyTrustFastConfirmer()确实为0x5e16561173ea0422549c3de12b68c8a7d3a76672。该Safe owner依次为65fa、5cda、21d4，threshold=3（3/3），nonce37626，VERSION=1.3.0，guard=0，modules=[]，自身未质押；在UpgradeExecutor上的ADMIN_ROLE和EXECUTOR_ROLE均为false。原始记录在operator-mapping/fast-confirm-safe-20260929-003917.json。

治理Safe仍是fbb37...（3/4），不能用这个快速确认Safe替代治理Safe执行暂停/修改WASM。三个运行账户恰好是快速确认Safe的三位owner；21d4虽未直接质押，仍能参与快速确认授权。是否由云B该容器实际发approveHash/execTransaction须以运行配置/日志补证，不能仅凭owner资格断言。d38e仍是另一笔旧质押，与三位owner名单分开管理。

维护草案补充：停止旧普通断言提交与快速确认的审批/执行两条路径，可能都由同三个容器承担；排查额外执行器和已批准/在途的Safe交易。旧版源码contracts-legacy/src/rollup/RollupUserLogic.sol中fastConfirmNextNode调用带whenNotPaused的_confirmNextNode；暂停边界应在针对实际部署的最终验证中覆盖，不能说停止进程即可撤销既有Safe批准。

恢复初期保持所有旧快速确认提交者不工作，先按已有普通确认路径验收新节点；之后另行核对三位快确认owner均使用匹配新状态/WASM的节点，核验Fast Safe当前nonce、待执行hash、快速确认目标及执行结果，再恢复该功能。此项是新增的生产运行角色覆盖；旧演练使用enable-fast-confirmation=false，不能标记Fast Safe自动审批/执行已验收。不需重做A′→B长重放；需要补充快确认暂停边界和后续恢复路径的针对性核验。

准备包v1的preparation_inventory_passed仅覆盖当时检查的治理Safe与质押范围，不包含该Fast Safe。下一版采集/审阅须同时固定两套Safe状态与配置。未改生产root/权限/nonce/节点。

## 14. d38e旧账户退役处置已明确

用户确认0xd38e969ae2947019e0dec0e46937e6aff651834d已不再使用，但仍持有私钥。仅记录“用户确认持有”，不读取/索取私钥、不将其标记为真实签名已经验证。无需为此账户定位运行机器或启动旧validator。

此前拟生产角色（现由第15节用户明确指示修订）：

| 地址 | 拟处理 | 恢复运行职责 |
|---|---|---|
| d38e…1834d | 纳入原三账户退款；治理产生credit后由用户本地签名提款；不重新质押 | 无，退役 |
| 5cda…de33a | 退款/提款后按恢复流程重新质押 | 云A validator及快确认owner |
| 65fa…cbc95 | 退款/提款后按恢复流程重新质押 | 当前服务器validator及快确认owner |
| 21d4…4849 | 当前无质押可退；不因Safe owner身份而自动安排新增质押 | 云B快确认owner，实际运行策略仍待核对 |

三账户退款集合未改变；恢复后不再要求“三个原账户都重新质押”。历史三账户重加入用例是能力验证，不是要求退役账户重新上线。下一次针对快确认路径的验证应覆盖d38e退出、两个活跃账户重新质押、三个快确认owner审批的实际目标角色，不要求重跑长重放。

d38e提款应在经验证的允许阶段执行；当前旧版withdrawStakerFunds要求whenNotPaused。签名前仅核对公开发送地址、链42161、Rollup目标、可提款credit及gas余额。退款credit不等于已经到账；实际提款后核验回执、到账及credit归零。提款签名留在用户机器，不导出密钥到聊天或本工具目录。尚无任何生产退款/提款授权或交易。

## 15. 用户确认恢复后的三位质押者（取代第14节关于21d4不新增质押的安排）

用户明确要求当前服务器65fa、云A5cda、云B21d4在恢复后全部质押。此为流程准备范围更新，不是现在广播生产质押交易的指令。

| 账户 | 维护时处置 | 恢复后安排 |
|---|---|---|
| 65fa…cbc95 / 当前服务器 validator-nitro-1 | 现有质押退款、核对credit及提款 | 重新质押，参与普通断言与快确认 |
| 5cda…de33a / 云A validator-nitro-1 | 现有质押退款、核对credit及提款 | 重新质押，参与普通断言与快确认 |
| 21d4…4849 / 云B validator-nitro-1 | 上次盘点无质押，无退款；窗口再次核实 | 新增质押，参与普通断言与快确认 |
| d38e…1834d / 已退役 | 现有旧质押退款，用户本地签名提款 | 不重新质押、不启动节点 |

退款集合与恢复质押集合不同：退款为d38e/5cda/65fa；恢复质押为65fa/5cda/21d4。快确认Safe3/3 owners与恢复质押集合一致。21d4须准备执行时实际要求的质押本金及父链gas；所有账户均核验执行时currentRequiredStake和stakeToken，不照搬历史1e12值。

下一次针对性验证目标：退款原三账户→d38e退出→65fa/5cda重新质押、21d4新质押同一普通断言→确认→三位现行快确认owner路径和暂停边界。旧三账户重加入及临时密钥运行测试是参考，不等同此最终角色组合已经通过。保持历史长重放证据，不因账户角色修订重新执行全量重放或复制。

## 16. 最终账户组合及快确认的补充演练已准备，尚待服务器运行

脚本：`rehearse-final-roles-fast-confirm.py`；完整安装文件：`install-final-roles-fast-confirm.sh`。本地通过7项检查及安装载荷完整性、shell/Python语法检查；这不是实际分叉执行成功报告。

使用固定历史候选three-staker-fork-20260928-090354及已通过的10578条A′→B、48条B→C记录。自行创建并关闭本地Anvil，调用上游仅供fork读取；不调用Docker、不访问节点数据库、不停止两个运行节点、不触碰/data。不改变快确认权限来迁就历史fixture；若历史快确认Safe部署/owner与预期不符则停止。

验证原三账户退款到账、d38e不重新质押、65fa/5cda/21d4同节点质押。普通确认在一个快照分支完成后回滚；再在同一待确认断言上验证Fast Safe 3/3、两签拒绝、Rollup暂停拒绝、Safe全调用暂停拒绝、恢复后在普通期限前确认。所有owner授权均为模拟EOA impersonation + approveHash；Fast Safe本身不冒充发送交易。暂停失败检查使用eth_call，不广播失败交易。

此轮补合约路径覆盖；不宣称三台实际Nitro进程自动快确认、生产签名或父链A→B证明已完成。生产维护窗、最终状态/调用数据、三机镜像与数据准备及真实签名验收仍按既定计划独立完成。

## 17. 最终角色与快确认历史演练通过（用户回传20260929-085552）

原始回传归档于reported-results/final-roles-fast-confirm-20260929-085552.json，来源为用户完整终端输出；未将其描述为本地独立复跑。结果contract_rehearsal_passed、readyForProduction=false。

原三账户退款到账，d38e退役无新质押，65fa/5cda/21d4同节点43009质押。普通确认成功分支回滚后，快确认Safe两签拒绝、暂停期间Rollup直接调用及完整Safe调用拒绝、恢复后三签确认成功。最终EVM时钟26072627小于普通截止26072763；Safe nonce37626→37627均为历史fork值。暂停期间取得的approveHash在恢复后仍有效，说明不能把暂停当成批准清除机制。

不再重复这组历史合约用例、10578/48重放或数据库复制。后续准备重点是三机实际配置/镜像/数据切换材料与治理Safe签名方式；自动快确认进程路径、真实签名、最终暂停后A/B及生产numBlocks仍未获验证或批准。

新增便携只读脚本inspect-final-validator-prep.py，按current/cloud-a/cloud-b角色输出镜像、挂载元数据、Docker重启策略、显式启动开关及当前启动以来的日志身份。不存在文件扫描或Docker exec；不读取/data内配置或数据库。不能从缺失开关推断false，不能以元数据采集代替运行配置/数据兼容性验收。

## 18. 治理签名界面已明确

用户确认治理Safe使用Arbitrum One的官方Safe Global UI。已按官方Transaction Builder BatchFile结构离线生成production-safe-ui-prep-20260929/DRAFT-pause.safe.json，并独立重新编码核对executeCall(Rollup,pause())。未选择Safe nonce或gas参数、未生成SafeTxHash、未创建服务提案、未签名/广播，实际UI导入与当时完整Safe执行模拟待做。签名者和维护窗口尚未确认就绪。后续实际恢复和resume分别准备，不复用历史10578/43008等参数。

## 19. 用户承担切换协调；暂停内容专项复核通过

用户确认新镜像标签fuhua-container.tencentcloudcr.com/deriw/deriw:v1.3.1.2.v3.10.0-16c17ee3a9c4、配置已准备，切换另行安排，治理Safe由团队协调。按此作为用户负责事项记录，不重复要求重新准备整套配置或复制数据库；不将其称为本轮独立镜像digest验证或生产执行授权。

本轮直接读取父块0x1e642930并在本机chain31337完整执行单独Safe暂停，3/4门槛及两签负测、权限/代码基线、ExecutionSuccess/Paused事件、实际SafeL2日志解码均通过。检查的WASM/节点/staker状态未改变。最新nonce观察10，Safe服务nonce>=10待办返回0；未签名未广播，独立分叉已关闭。详见production-safe-ui-prep-20260929/PAUSE-CONTENT-REVIEW.md。

暂停内容审阅已完成；正式网页实际nonce/费用/operation/data/SafeTxHash、真实ECDSA及执行时状态仍要在具体交易形成后核对。pause不绑定节点号，暂停后的A/B重新固定；旧快确认批准不会因pause消除。不要把历史模拟交易hash或gas121366直接用于生产。

## 20. 工具交付就绪状态更正

用户要求确认“除动态node外是否全部脚本和参数已准备”。实际文件核对表明尚未全部准备：prepare-window的paused分支只采集，现有atomic/rejoin演练仍锁定历史candidate/anchor/跨度/节点；没有完整通用生产恢复JSON生成器、同一生产包动态分叉验收流水线、resume/退款提款/真实回执验收的整合交付。不能把历史测试通过或文字流程等同这些工具已完成。

详见PRODUCTION-TOOLING-READINESS-20260929.md。上述工具逻辑应在暂停前完成并用历史fixture验证，实际参数再于窗口固定。此前“主要准备已齐”只适用于机制演练及暂停调用内容，不能解读为只剩暂停后填node。

## 21. 统一工具交付（2026-09-29）

当前使用 `production-recovery-toolkit-v2/START-HERE.md` 和 `RUNBOOK.md`，替代分散历史脚本作为操作入口。历史报告继续保留。v2 的测试边界和未完成的真实生产验收见其 TEST-RESULTS.md，特别是本机 Anvil ArbSys 日志模拟限制；禁止将历史/模拟包导入生产 Safe。
