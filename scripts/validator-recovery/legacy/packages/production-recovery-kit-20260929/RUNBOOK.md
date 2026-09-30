# Deriw 三节点恢复：维护准备包

日期：2026-09-29。当前交付是准备包，不是已获批准的执行包。
本包不会停止容器、修改数据库、解锁钱包、创建 Safe 提案或广播交易。

## 已知部署

- 三个纯 validator，当前服务器一台、另外两台分别在其他云。不承担用户 RPC。
- 用户确认三台均运行镜像 ID 前缀 `becc38893aca`，标签为 `fuhua-container.tencentcloudcr.com/deriw/deriw:prod`。
- 本地带密码的 EOA keystore，容器内 `/keys`，`MakeNodes`，staker enabled。
- 旧链上 WASM：`0x767c9a47cced7ccc3bf419a7efdd9ffb0f23a5dba42f30f3de64f32e2f82c55f`。
- 已演练的新 WASM：`0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421`。
- 已演练镜像的本机 image ID：`sha256:b3571c0d8d4cd4e826a48895ed558927c4b003ea14050f5be3fb5ab09c62148f`。这是 image ID，不是 registry manifest digest，不能直接拼成仓库 `@sha256:...` 拉取。正式跨云分发前核验可拉取的 digest 或导出镜像校验。

## 0. 维护前现在可以做的工作

### 0A：收集只读准备报告

在已安装本包的当前服务器运行：

```sh
(
set -e
export PATH="$HOME/.foundry/bin:$PATH"
cd /data_new/scripts
OUT="production-window-prep-$(date +%Y%m%d-%H%M%S)"
python3 production-recovery-kit-20260929/prepare-window.py --out "$OUT"
)
```

脚本在同一个固定父块收集 authority/participants，再与已知合约、三个账户、Safe owners/threshold、旧 root 等基线比较。挑战非零、账户范围或代码变化会停止生成暂停草案。会记录 Safe slot0 指向的实现与代码哈希供审阅；仅通过 Safe 代理代码检查不等于验证了 Safe 实现。

产物：

- `summary.json`：采集结果及尚待完成事项。`preparation_inventory_passed`仅表示本次检查通过。
- `authority.json`、`participants.json`：固定块盘点。
- `pause-review.json`：Safe 调用 UpgradeExecutor.executeCall(Rollup,pause()) 的目标、value=0、operation=CALL、calldata 和解码。
- `files-sha256.json`：本次报告校验值。

`pause-review.json`不是Safe导入文件，不含签名、不生成SafeTxHash、不批准签署。脚本只以Safe地址作为eth_call的from测试下游调用；没有模拟Safe签名验证或整笔execTransaction。nonce只是观察值，不能直接锁定以后交易的nonce。只回传summary和pause-review，不需要回传任何RPC认证信息。

### 0B：填三节点操作表

`operator-roster.json`是资料模板，不是可执行配置。每个账户填写机器简称、容器名、负责人、当前镜像完整ID、拟用数据来源，以及维护期间如何防止编排器自动拉起旧提交者。不填密码、私钥、认证URL。三个地址不能凭旧目录中的keystore头推断机器归属。

如需收集公开元数据，将本包内 `inspect-recovery-operator.py` 交给对应负责人，在该机器工具目录运行：

```sh
docker ps -a --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
read -rp 'Expected public staking address: ' STAKER
read -rp 'Validator container name: ' CONTAINER
python3 inspect-recovery-operator.py --expected-staker "$STAKER" --container "$CONTAINER" --out "operator-inventory-$(date +%Y%m%d-%H%M%S).json"
```

默认不读取任何配置、keystore或数据库；不要使用--config读取/data。日志中的地址只能作身份线索，不能证明有效配置或签名能力已全部核验。

### 0C：提前就绪的运行材料

1. 各负责人确认本地keystore和原解锁方式可用、备份可恢复；不在聊天里发密码或解密文件。
2. 新镜像可取得并核验，WASM实际加载根匹配。分别保存旧配置及新配置审阅差异，先禁止自动提交。不要仅依赖可变prod标签。
3. 核定三机数据来源和切换办法。当前/data_mock是实际同步副本；/data_new已被模拟修改，不能直接投入生产。旧数据库能否支持新检查点需验证；不能承诺只换镜像即可。未对齐的节点可以晚些上线，不让它们误提交旧状态。
4. 确认3位Safe签名人、提案/签名/执行方式及Arbitrum One gas准备。此包不索取密钥，也不发起提案。
5. 确认接受受信任治理检查点迁移：A与本地A′不同，A′→B重放不是A→B证明。治理跨度约定和提款审计范围需在具体包中审阅，不能以历史模拟PASS代替。

在这些项目确定前不进入停机窗口。

## 1. 维护窗口开始：停止旧提交与治理暂停

本节是流程草案，不是停机命令授权。具体容器命令需在机器映射、重启策略及入口程序的退出行为确认后给出；尤其当前validator-nitro-1仍受保护。

1. 刷新挑战、Safe权限/nonce、合约和账户范围，检查是否有其他提交者。挑战非零则先评估应答责任。
2. 三台同一窗口正常关闭旧validator，给退出/落盘足够时间，核对进程确实退出及编排器未重启；不使用强杀作为默认方式。不停止snapshot-sync。
3. 记录各发送账户nonce及已知待上链交易/回执。进程退出不取消已广播交易；节点本地的pending视图也不能单独证明全网无在途交易。
4. 重新生成并核对暂停审阅草案，验证Safe实现/完整执行路径、费用与真实nonce，形成具体暂停交易包。经用户授权后由Safe单独执行pause。
5. 核对链42161、交易receipt成功、Safe的ExecutionSuccess事件与对应safeTxHash、Rollup paused=true。仅receipt.status=1不足以证明Safe内部调用成功。

停止validator不自动退质押，也不冻结链上状态。pause也不冻结Inbox、排序器、Outbox提款或其他管理员动作。

## 2. 暂停后：固定真实恢复参数

**只有链上已经暂停后**才运行下面的只读检查；它不会执行pause：

```sh
(
set -e
export PATH="$HOME/.foundry/bin:$PATH"
cd /data_new/scripts
OUT="production-paused-inventory-$(date +%Y%m%d-%H%M%S)"
python3 production-recovery-kit-20260929/prepare-window.py --phase paused --out "$OUT"
)
```

以实际暂停后A和未决节点重新确定B、消息位置、machineStatus、SendRoot、批次可用性、治理跨度约定、inboxMaxCount、预期恢复节点号和exactExpectedNodeHash。旧B126693840、跨度10578、节点43008/43009不进入生产包。

预先已通过的历史机制不反复重跑；最终真实参数必须另做针对性模拟和核对。期间Inbox可能增长，需要在具体交易中规定允许的变化及执行时保护。不能把批量getter当作返回值断言，不能仅靠Safe nonce保护Rollup状态。

## 3. 原子恢复包：复核后才签名

拟执行顺序：三账户forceRefundStaker → setWasmModuleRoot → forceCreateNode → forceConfirmNode。成功保持暂停；resume单独进行。

审阅表必须列出：

- 父链42161，Safe、Rollup、UpgradeExecutor、批量组件地址和代码身份。
- Safe nonce、所有嵌套CALL/DELEGATECALL、value、safeTxGas/baseGas/gasPrice/gasToken/refundReceiver。
- 三个退款地址与金额/credit基线，挑战与未决节点状态。
- A/B、numBlocks约定、WASM、inboxMaxCount、exactExpectedNodeHash、新节点号。
- 防漂移约束及批次增长处理、完整Safe模拟、故意晚失败回滚检查。
- 待签safeTxHash、可读解码、交易包校验值；3位签名者核对同一包。

本准备包不输出这笔恢复calldata，因为最终A/B尚未固定。签署只针对完整审阅的具体参数。

## 4. 恢复成功但仍暂停：验收

核对Safe成功事件、恢复节点承诺、latestConfirmed、root、Outbox身份及根映射、三个退款credit。对提款标记采用明确范围和快照位置；生产在两个时点出现false→true可能是期间真实提款，需用回执解释，不能直接判定恢复篡改。历史8121范围和样本不代替当前全部新消息的审计。

准备好的一台新validator以不提交交易的验证模式对齐B并验证B后实际可用消息。B作为assume-valid的日志不等于重放B；应看到新WASM下后续进展，且与固定终点一致。禁止两个进程同时打开同一个数据库。

## 5. 单独resume、退款提款和逐台恢复

验收通过，审阅新nonce的resume包并获得执行授权后再恢复。
提款与removeOldZombies等须按已核实的whenNotPaused限制安排。逐账户核对退款到账、交易费用和credit归零。恢复后credit尚未提款不等于钱已返回钱包。

先放开一台已对齐节点MakeNodes，核验实际EOA发送者、重新质押、普通NodeCreated/NodeConfirmed和新WASM终点；接着逐台恢复另两台。核验各自质押位置、挑战和nonce，避免同一EOA在多进程并行发送。未对齐者保持不提交。

持续观察真实新批次、普通断言和提款；维护验收不只看进程running。具体观察时长和成功标准在窗口计划中约定，历史90秒正常重启测试仅是机制覆盖。

## 6. 失败处理

| 时点 | 处理 |
|---|---|
| 只读准备失败 | 保留节点现状，修正缺失/漂移，不签名 |
| 停进程但未pause | 先核对在途交易和当前状态，决定继续窗口或恢复旧提交，不无条件自动重启 |
| pause成功、恢复未执行 | 保持暂停排查；恢复旧流程也需具体状态审阅 |
| Safe原子包失败 | 核对内层回滚与Safe nonce；nonce可能已消费，不照抄重试 |
| 恢复成功但验证失败 | 保持暂停，保存证据；不能把改回WASM视为完整回滚 |
| resume后单个节点异常 | 隔离该提交者、核对已发交易/挑战；不改动已领取记录 |

## 完成定义与当前空缺

历史机制演练已完成。准备包已提供只读采集与暂停下游调用草案、机器资料模板、全流程和验收表。

进入维护窗口前仍需：机器/账户/容器映射、各机升级与数据对齐方案、真实镜像分发标识、Safe签名方式/人员和维护时段，以及具体暂停执行包审阅。暂停后的A/B和最终恢复包按第2、3节生成。不能把这些待定项标成已完成，也无需因此重复所有历史演练。
