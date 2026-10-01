# 本次故障、判断和处置

每个结论对应本次日志或仓库代码；运行参数变化后要重新核对。下列90000、RPC端口、账户和节点只是本次已观察值。

## 新WASM匹配本地历史，父链承诺仍不同

只有设置`setWasmModuleRoot`不能改变旧已确认node承诺，也不能修复不同历史的连接。比较完整GlobalState、原生/WASM版本和保存输入；本次采用受信任治理检查点迁移，旧Outbox继续保留。A′不是按目录或旧WASM自动定义的错误状态，而是当前保留本地历史在与A相同位置的状态。

## `cast`找不到、Archive RPC被当成目录

每次独立shell执行`export PATH="$HOME/.foundry/bin:$PATH"`并`command -v cast`。Anvil `failed to get fork block ... with /data_new/scripts/`曾来自RPC值错误；核对变量非空并读取chain-id42161后才启动fork。共享输出不要打印Archive凭据。

## snapshot-sync同步前进，验证checkpoint不动

曾观察head约127053335，但`arb_latestValidated`仍在125022702；初始诊断配置`validation-sent-limit=0`有意阻止后台发送。单消息`arbdebug_validateMessageNumber`结果valid=true证明那次显式root重放成功，不推进后台checkpoint。`fix-snapshot-validation-limit.py --apply`将limit改8，正常重启snapshot，并保存旧validator不变的身份报告。

后续性能试验包括prerecorded4/8/16/32、forward-blocks、workers。只比较实际持续吞吐、CPU、available memory、swap-in/out、I/O等待；参数增大不保证加速。`Unexpected preparation setting`是脚本对基线不匹配的保护，不应绕开。读取metrics时确认服务/端口对应实际进程，启动后的全零样本不能单独说明验证停止。

生产父链仍使用旧root时，snapshot通过显式新`current-module-root`做本地验证是可能的；它没有自动改变父链治理root。同期snapshot staking关闭，验证成功不等于提交断言获准。

## `STAKER_IS_ZOMBIE`

强制退款留下旧zombie，本机无法重新stake。恢复确认到43126且Rollup已resume后，由65fa EOA提交`removeOldZombies(0)`成功；`isZombie=false`、`zombieCount=0`和普通node43127均有pinned查询。调用受`onlyValidator`与暂停条件限制，先检查部署合约和模拟。该cleanup步骤应在实际运行交接中明确列出，不能只在演练做过就略去生产。

`docker top -eo comm`曾被Docker拒绝缺PID列；正确为`-eo pid,comm`。容器PID1为tail时，检查真正Nitro并通过其正常关闭流程释放数据库。

## 没有新的`validated execution`

第一次普通assertion结束于block127108784、Batch324111/0。当时Bridge的sequencerMessageCount也是324111，后续batch324111未发布。公共sequencer仍出块127164219，但validator没有下一批已发布数据，先修poster。FastSafe缺其他owners批准是确认阶段问题，不能自动归因为WASM验证停滞。

## 大小限制有两层

1. `0x4634691b = DataTooLarge(uint256,uint256)`：本次108465字节含40字节头，合约limit104857，原poster配置110000过大。
2. 改100000后，98,628字节calldata加交易封装仍被父链`eth_sendRawTransaction`拒绝`oversized data`。Nitro sequencer默认接受的完整transaction size为95000，不能只考虑合约maxDataSize。

本次改为`node.batch-poster.max-calldata-batch-size=90000`后，calldata路径边界为89960，batch324151在父块510014968收到成功回执。AnyTrust writer可有独立90000限制，日志`limit=90000`不等于calldata配置已经改变。共用sequencer进程时还需保持`execution.sequencer.max-tx-data-size`比calldata批次至少小5000；任何新改动都与实际已接受消息大小一起审阅。

`Batch overflow: compressed size limit exceeded`表示构建时停止追加、切分批次，不能单独当成失败。判定发布结果读取`DataPoster sent`、`batch sent`、实际success receipt和Bridge进度。提高gas不能修复字节限制。

## 配置热加载

本版本[LiveConfig](../../cmd/genericconf/liveconfig.go)注册`SIGUSR1`；`max-calldata-batch-size`标记`reload:hot`。修改实际`--conf.file`加载的JSON后，校验语法，向唯一Nitro主进程发USR1。CLI参数仍覆盖文件值；不可热更新字段有变化会报错。

在poster服务器执行，先由操作员完成配置改动：

```bash
(
set -euo pipefail
python3 -m json.tool /data/poster/config/nodeConfig.json >/dev/null
docker ps --format 'table {{.Names}}\t{{.Image}}'
read -rp 'Poster 容器名称: ' POSTER
PIDS=$(docker top "$POSTER" -eo pid,comm |
  awk 'NR>1 && ($2=="nitro" || $2=="nitro-node") {print $1}')
COUNT=$(printf '%s\n' "$PIDS" | awk 'NF {n++} END {print n+0}')
test "$COUNT" -eq 1 || { echo '未找到唯一Nitro主进程'; exit 1; }
kill -USR1 "$PIDS"
)
```

检查`Configuration reload triggered by SIGUSR1.`、无parsing/updating live config错误，以及下一批实际边界。`docker kill --signal USR1`只发给PID1，本次PID1是tail，不能用于这项重载。本次poster日志显示“Disabling data poster storage...Arbitrum chain without a mempool”；错误重试会清空内存中的building并重建，未删除DB或Redis队列。

## DAS RPC连接拒绝与回退

本次`47.237.169.89:8976`从poster连接3ms即refused，尚未进入BLS签名处理。先在DAS宿主机核对服务状态、实际RPC监听、Docker端口映射，再检查云安全组/防火墙和public-IP映射；不要把REST读取端口当RPC写入端口。

只读检查：

```bash
# poster服务器
curl --noproxy '*' -sS -v --connect-timeout 5 --max-time 10 \
  http://47.237.169.89:8976/ -o /dev/null

# DAS宿主机
sudo ss -lntp 'sport = :8976'
docker ps -a --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
curl --noproxy '*' -sS -v --connect-timeout 5 --max-time 10 \
  http://127.0.0.1:8976/ -o /dev/null
```

HTTP400/404/405只能证明HTTP服务可达。完整验收需要batch存储成功、正确BLS身份与链上keyset、后续validator成功读取。

日志当时为N=3、assumed-honest=1，需`N+1-H=3`后端响应。失败触发10批EthDA回退，ArbitrumOne上直接发布压缩完整数据，`use4844=false`。批次data参数首字节`0x00`代表Brotli；`0x88`代表AnyTrust tree证书。整笔transaction.input仍以ABI函数selector开头，二者不能混读。回退通常提高发布成本。

`lack of L1 balance prevents posting transaction with desired fee cap`曾与随后成功发送/回执同时出现，表示目标cap超过可分配余额后cap被限制；日志的targetMaxCost不是实际手续费。关注实际余额、费用和回执，不按warning推断所有发布失败。

## `NoSuchKeyset`：证书产生后被父链拒绝

最新09-29 14:46 UTC日志：`0x00f20c5d = NoSuchKeyset(bytes32)`，证书keyset为`0xefecd070797fd8154e149a2fb8b13aceb0e8e3dac51f514cc4e33e79b7dfb8b6`。回退窗口结束后poster得到0x88证书，但此keyset未被当前SequencerInbox视为有效。

只读核对：

```bash
export PATH="$HOME/.foundry/bin:$PATH"
RPC=http://10.1.2.16:8547
INBOX=0xe79283a775f6a1de250cb3284e7ff3541ff7668a
KEYSET=0xefecd070797fd8154e149a2fb8b13aceb0e8e3dac51f514cc4e33e79b7dfb8b6
test "$(cast chain-id --rpc-url "$RPC")" = 42161
cast call "$INBOX" 'isValidKeysetHash(bytes32)(bool)' "$KEYSET" --rpc-url "$RPC"
cast call "$INBOX" 'dasKeySetInfo(bytes32)(bool,uint64)' "$KEYSET" --rpc-url "$RPC"
```

这两项命令的结果尚未收到。核对修复过程中是否删除后端、更换BLS公钥、改变顺序或assumed-honest、重新生成密钥、加载错误配置，或链上原keyset已失效。不能把只改变URL与改变委员会成员等同。

优先对照原来成功的委员会配置与链上事件；有意迁移委员会才审阅完整keysetBytes、公钥和门槛并执行治理登记。不能只登记报错hash、手动改证书头或降低签名门槛凑成功。存储阶段的DA失败会触发回退，证书产生后的合约NoSuchKeyset不保证触发同一回退机制。

## 启动兼容性日志

`validation_capacity`方法不存在后回退room、随后连接jit-cranelift成功，是兼容性路径。BoLD检测selector`0x3be680ea`调用`challengeGracePeriodBlocks()`时pre-BoLD实现revert，不能据此判断本次普通legacy staker坏了。`Removing old bloom bits database`是旧查询索引清理，不等于删除共识区块或状态数据；日志完成与否和后续故障分别检查。
