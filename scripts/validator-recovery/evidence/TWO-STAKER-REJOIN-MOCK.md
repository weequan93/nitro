# 两个历史质押账户共同恢复演练

状态：操作者回传two-staker-rejoin-mock-20260928-012314，two_staker_rejoin_passed。
本地未直接连接服务器复查；此前7项离线参数/状态/ABI检查通过。

## 本次结果

两个历史账户的退款ETH支付、旧zombie清理和重新质押全部通过。
5cda创建普通节点42887，D38e加入同一节点，sameNodeStakeCount=2；随后D38e普通确认42887。
治理恢复节点42886，后续普通断言numBlocks=64。

- 创建：0x54cffb519c3397d007d121718143d46140d85f83c28777ee439672855ca57c20。
- 第二账户加入：0x37c3c19dae4bcf816ee9a504373606a0e88633896f86978544246cf887b4b91a。
- 普通确认：0x5ce5a62290cb228c5587e396d6dd4ddcbca8475d47d62952448d2b1989b908b8。

两账户在该历史fork的addressHasCode均为false；交易采用模拟身份，未使用原私钥。
ownedForkStillRunning=false，dockerAccessed=false，nodeDatabaseAccessed=false。
originalAccountRuntimeTested=false，productionSigningTested=false，freshExecutionReplayTested=false。
生产退款范围未改变，readyForProduction=false。无需重复同一固定用例。

## 目的与范围

额外检查固定历史快照内的两个账户都被退款后，能否重新质押到同一个普通断言并完成普通确认。

- 活跃账户：0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a，原节点42885。
- 旧账户：0xd38e969ae2947019e0dec0e46937e6aff651834d，原节点158。
- 仍使用candidate-fork-126237291固定父链区块，不刷新成生产候选。
- 两账户均使用Anvil模拟身份；Safe使用模拟owner的approveHash授权。
- 自动选择新的本地端口，结束时停止本轮Anvil；不会调用Docker或打开Nitro数据库。
- 不改变生产方案目前仅退款5cda账户的范围。

## 执行

将rehearse-two-staker-rejoin.py保存到服务器/data_new/scripts。
同目录需有已使用过的resume-original-staker-current-fork.py、rehearse-safe-recovery.py，
以及现有mock-validator-recovery.py（也可位于recovery-mock-tools子目录）。
当前终端须已有非空ARCHIVE_RPC；脚本不会显示该值。

```sh
export PATH="$HOME/.foundry/bin:$PATH"
export ARCHIVE_RPC
cd /data_new/scripts

python3 rehearse-two-staker-rejoin.py \
  candidate-fork-126237291 \
  --previous-pass nitro-restart-mock-20260928-010622/summary.json \
  --out "two-staker-rejoin-mock-$(date +%Y%m%d-%H%M%S)"
```

## 验收

1. 历史staker集合恰好是上述两个账户，无挑战、仍在白名单。
2. Safe暂停、同时退款、治理恢复到42886、恢复运行；分别检查退款ETH入账和credit清零、zombie清理。
3. 5cda以newStakeOnNewNode创建42887；D38e以newStakeOnExistingNode加入相同nodeHash。
4. 两账户质押金额、位置、挑战状态正确；全局stakerCount、普通节点stakerCount、父节点childStakerCount均为2。
5. 推进fork的EVM区块时钟，由D38e调用普通confirmNextNode；确认42887及Outbox终点。
6. 输出status=two_staker_rejoin_passed且ownedForkStillRunning=false。

主要记录：summary.json、steps.json、participants-before.json、participants-after-refund.json、
participants-after-rejoin.json、participants-final.json和三笔普通质押/确认交易记录。

## 证据限制

采用已有Nitro重启报告中的B→C验证终点；此轮不重新执行64条消息。
治理A→B仍采用合成numBlocks=1，尚未形成生产参数或A→B执行证明。
模拟账户交易不证明原密钥、原机器或合约钱包owner调用路径可用；脚本记录addressHasCode供解释结果。
不进行新的完整提现审计，不测试两个独立Nitro进程并行运行。
运行报错时保留输出目录，不将部分PASS视为完整通过。
