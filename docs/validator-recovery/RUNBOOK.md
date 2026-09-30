# 恢复流程与后续验收

这是本次流程的整理版。已经执行的节点43126、nonce10/11/12、旧candidate和Safe导入内容均作为历史保留；新恢复窗口必须重新采集参数。日常续跑优先执行第9节的只读验收，不重做治理恢复。

## 1. 固定版本、权限和数据计划

先核对父链42161、子链2886、新镜像digest、新WASM、治理Safe3/4、FastSafe3/3、三台机器与EOA的对应关系。入口为 `probe-recovery-authority.py`、`probe-production-participants.py`、v2的 `fast-safe-check.py` 及operator inspector。

本次退款集合为5cda/d38e/65fa，后续质押集合为65fa/5cda/21d4。退休d38e只退款。管理员仅在本地解锁各自keystore；记录地址验证结果，密码不写入配置或交付文件。

数据库布局和写入者要核对实际Docker挂载：生产旧`/data`受保护，演练使用`/data_new`副本，恢复运行使用连接真实父链的`/data_mock`。不要仅按目录名判断是否隔离。`replace-recovery-test-db.py`只用于显式授权的测试副本复制，必须冻结源和释放目标；复制后的size/mtime比较不是全盘checksum。

## 2. 停止旧断言与旧快确认提交

三台操作员正常停止各自Nitro进程，保存结束日志、镜像、命令和账户。sequencer/Bridge/inbox仍可能增长。读取所有容器内进程时用：

```bash
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
docker top CONTAINER_NAME -eo pid,comm
```

Docker top输出必须包含PID列。PID1为tail的容器要检查真正Nitro进程；前台手动运行的Nitro用其前台终端正常退出并观察停止日志。不要同时启动多个使用同一数据库或账户的Nitro。

## 3. 生成暂停包并校验

服务器操作目录为 `/data_new/scripts/production-recovery-toolkit-v2`，每次终端都加入Foundry路径：

```bash
export PATH="$HOME/.foundry/bin:$PATH"
command -v cast
command -v anvil
cd /data_new/scripts/production-recovery-toolkit-v2
```

按原 [RUNBOOK](../../scripts/validator-recovery/toolkit/RUNBOOK.md) 使用 `recovery.py followup pause`、`verify-fork.py`、`recovery.py preflight`。归档当前Safe nonce、所有外层字段、import内容、链ID和SafeTxHash；官方Safe UI最终外层包装必须一致。Archive URL在操作员本地提供，不复制进共享记录。

## 4. 固定候选、重放、审阅与集中预签

按 [PRESIGN-RUNBOOK](../../scripts/validator-recovery/toolkit/PRESIGN-RUNBOOK.md) 依次使用：

1. `presign.py collect`固定window中的父链A、本地A′、候选B和质押基线。
2. `replay-span.py`对固定A′→B逐消息recording和显式root重放；中断时确认旧进程退出后对同一目录`--resume`。
3. 等待最终`retained_span_replay_passed`、全条数、终点、连续哈希链匹配。`running`不能预先视为通过。snapshot-sync是重放的RPC提供者，不能为加速重放把它停掉。
4. `recovery.py decision-template`生成审阅记录；具体确认受信任迁移、A→B未证明和位置跨度约定，不自动填写批准。
5. `presign.py build`生成pause/recovery/resume，`presign.py verify`在自有31337fork测试同一组三笔以及失败回滚。
6. 团队可以集中签不同nonce的交易，操作员按每个阶段实际回执和运行结果逐笔执行。nonce保证顺序，不保证前一笔成功。

本次的4467完整逐消息输入留在服务器；本地只保存相应摘要和导入审阅内容。原始7838/10578脚本只用于历史机制复现，不能替代新window采集。

## 5. 治理执行并保持暂停

执行pause后 `recovery.py accept`验收实际交易；`presign.py check recovery`核对新状态，再执行恢复并`accept`。恢复的四个内层调用顺序为退款、设置新WASM、创建新恢复节点、确认新节点。记录实际NodeCreated事件和InboxMaxCount，不抄演练值。

保存失败交易回执和SafeExecutionFailure。Safe可能消耗nonce而内层回滚，不能据nonce递增直接进入下一阶段。

## 6. Watchtower接入真实父链并验证B之后状态

使用 `cutover.py prepare` 和 `start-watchtower`，独占 `/data_mock` 数据库，从恢复package和实际恢复回执派生配置。保留AnyTrust读取配置、正确验证服务器、机器root和本地RPC。遗漏AnyTrust reader时使用针对缺失reader场景的`repair-da`，不要任意应用到其他故障。

`node.staker.start-validation-from-staked=true`配合这个legacy staker版本从链上恢复的检查点开始；必须核对该checkpoint的完整global state及本地区块hash。启动时打印的持久化`validated execution`不能当作本次全区间新重放。

`cutover.py check`用于这个仍暂停、退款信用未使用的阶段。恢复后节点已创建/退款已领取时，它的旧前置条件会失效；不能反复调用旧check来判断正常MakeNodes健康。

## 7. 配置、密钥和数据库交接

按 [COMPOSE-PREPARATION](../../scripts/validator-recovery/toolkit/COMPOSE-PREPARATION.md) 使用 `prepare-compose.py --apply`。本次准备路径如下：

| 文件 | 宿主机路径 | 容器内 |
| --- | --- | --- |
| Compose | `/data_new/validator/compose.recovered.json` | — |
| 配置 | `/data_new/validator/config/nodeconfig.json` | `/nodeconfig.json`，只读 |
| 加密keystore | `/data_new/validator/keys/recovered-65fa/account.json` | `/keys/account.json`，只读 |
| 数据库 | `/data_mock/validator/config` | `/home/user/.arbitrum`，单进程读写 |

管理员运行`cast wallet address --keystore ...`并在交互提示输入密码，核对65fa地址。源加密文件复制一致、V3地址头匹配与解密地址验证是不同检查。

实际操作员改用旧布局的`validator-nitro-1`、`nodeConfig.json`和`/data_mock/validator/keys/validator.keystore`。以后接班时先核对实际进程`--conf.file`、文件名大小写、挂载和EOA，再决定使用哪套入口，不能混用两套配置。

## 8. resume、zombie清理与重新质押

Watchtower通过B之后新root运行核对后，保存运行验收记录并执行resume前检查。执行resume后验收实际回执；管理员保持同EOA自动提交停止，先检查是否需要清理旧zombies。

只读检查（账户值为本机示例）：

```bash
export PATH="$HOME/.foundry/bin:$PATH"
PARENT_RPC=http://10.1.2.16:8547
ROLLUP=0xa113e2e9620a3bc088a681ebb2c234fdbeb85e21
ACCOUNT=0x65fa6c1d1efc338dfe8f07d5de84b3e7b76cbc95
cast call "$ROLLUP" 'paused()(bool)' --rpc-url "$PARENT_RPC"
cast call "$ROLLUP" 'isZombie(address)(bool)' "$ACCOUNT" --rpc-url "$PARENT_RPC"
cast call "$ROLLUP" 'zombieCount()(uint256)' --rpc-url "$PARENT_RPC"
```

若zombie=true，在核对未暂停、白名单、旧stake早于最新确认节点、账户解锁/地址及同账户自动提交已停止后，先模拟`removeOldZombies(0)`。该函数由符合部署合约`onlyValidator`条件的EOA执行即可，不要求治理Safe。实际发送由管理员单独签名；本次已完成清理，交易和完整账户结果在执行记录中。

释放Watchtower和snapshot进程，独占DB后启动MakeNodes。正确配置包括新root、block-validator、`start-validation-from-staked=true`、父链42161、实际EOA及reader。管理员输入密码；保存staking/node creation完整回执。记录maxWorkers日志，不能只凭命令行`workers=16`认定验证服务器实际16线程。

Fast confirmation是另一条批准和执行路径：3/3 owners及每个safeHash都要核对。某账户的approveHash成功不等于该assertion已确认，也不阻止新WASM验证继续进行。

## 9. 恢复后只读验收

在实际validator服务器运行，下例使用最后确认的8449 RPC：

```bash
export PATH="$HOME/.foundry/bin:$PATH"
PARENT_RPC=http://10.1.2.16:8547
NODE_RPC=http://127.0.0.1:8449
ROLLUP=0xa113e2e9620a3bc088a681ebb2c234fdbeb85e21
BRIDGE=0x53a7559d1e57e371f3d1e55fea97e9b6748418a3
test "$(cast chain-id --rpc-url "$PARENT_RPC" --rpc-timeout 30)" = 42161
test "$(cast chain-id --rpc-url "$NODE_RPC" --rpc-timeout 30)" = 2886
cast call "$ROLLUP" 'paused()(bool)' --rpc-url "$PARENT_RPC"
cast call "$ROLLUP" 'wasmModuleRoot()(bytes32)' --rpc-url "$PARENT_RPC"
cast call "$ROLLUP" 'latestConfirmed()(uint64)' --rpc-url "$PARENT_RPC"
cast call "$ROLLUP" 'latestNodeCreated()(uint64)' --rpc-url "$PARENT_RPC"
cast call "$ROLLUP" 'zombieCount()(uint256)' --rpc-url "$PARENT_RPC"
cast call "$BRIDGE" 'sequencerMessageCount()(uint256)' --rpc-url "$PARENT_RPC"
cast rpc --rpc-url "$NODE_RPC" --rpc-timeout 30 arb_latestValidated
```

多次观察应同时保存时间、父块和完整GlobalState/WasmRoots，再按返回blockHash查实际本地高度。公共sequencerhead、父链已发布的message进度和validator验证进度是三项不同指标；未发布消息不会因为增加验证workers而变成可验证。

三台分别核对账户、白名单、质押节点/挑战/退款、签名交易和持续新root进度，最后核对普通确认或FastSafe3/3实际回执。样本提款验收保存既有Outbox地址、spent flags、root、实际付款和回滚范围；完整withdrawal audit另列为待完成。

## 10. 保存交接材料

保存window/candidate/audit/decision/replay-summary/sequence/fork-pass、三笔完整Safe字段及实际回执、配置digest与镜像digest、DB交接/keystore地址验证摘要、普通assertion与快确认回执、poster成功批次与DAS keyset核对。记录文件中标明真实父链或私有fork、证据观察时间及是否只是operator报告。

新资料加入后从仓库根目录更新索引：

```bash
python3 docs/validator-recovery/catalog.py --refresh
python3 docs/validator-recovery/catalog.py --verify
```

安装器只安装文件。旧压缩payload可能早于presign/cutover/compose附加模块，安装时使用各自校验清单和依赖顺序。共享记录不包含密码、私钥、keystore内容或Archive凭据。
