# 7838位置跨度治理参数演练

状态：操作者回传governance-span-mock-20260928-081940，governance_span_rejoin_passed。
本地归档reported-results/governance-span-mock-20260928-081940.json，注明是操作者回传，未直接连接服务器复核。
此前9项本地测试通过，包括已知普通节点哈希算法一致性、兄弟节点参与哈希、
跨度变更影响哈希、治理调用传入7838及精确非零expectedNodeHash。

## 已回传结果

- 7838份已有重放记录校验通过，摘要链dfdc67af2b21cb262d39a297e7517c482cba94219c5aa19bbe6ea1fd8f482435。
- governanceNumBlocks=7838，恢复节点42886，exactGovernanceNodeHashChecked=true。
- 精确治理节点哈希：0x1c44f95dd83a8e24c4794528d57b32b053a516204151418fce05e8a75e670b7b。
- Safe顺序恢复、两个历史账户退款支付及重加入同一普通节点42887通过，sameNodeStakeCount=2。
- normalAssertionNumBlocks=64，普通创建与确认通过。
- 创建：0xc365894e5c75b61a3503011cf98f205247561dc40da759961efe5def29d52dec。
- 第二账户加入：0x2d9d7b19585d5c2333b369a6082d8592b0ca01825f05031a1ae9aac3eaeb61d9。
- 普通确认：0x5ce5a62290cb228c5587e396d6dd4ddcbca8475d47d62952448d2b1989b908b8。
- ownedForkStillRunning=false，dockerAccessed=false，nodeDatabaseAccessed=false。

此次没有重新执行7838条消息，使用已通过的重放记录；生产退款范围不变。
旧numBlocks1的原子批次、真实Nitro自动签名/重启测试不能直接计为7838版本通过。
无需重复本轮已通过的顺序合约用例。

## 测试理由与边界

本地A′→B的7838条消息已按新WASM重放通过。父链A与A′仍为不同区块哈希。
本测试将治理numBlocks=7838解释为跨越的消息位置范围，检验该约定的合约兼容性。
它不是父链A→B执行证明；1和7838均尚未被批准为生产参数。
这轮参数与旧轮不同，因此旧numBlocks1测试不能直接当作这轮的通过结果。

使用更新的rehearse-two-staker-rejoin.py；原默认1模式保留，可选7838模式必须提供
跨度审计和完整重放目录。脚本逐条检查7838份记录、manifest、连续状态及摘要链后才创建fork。
所需replay-retained-message-span.py及既有依赖已在服务器同目录。

```sh
export PATH="$HOME/.foundry/bin:$PATH"
export ARCHIVE_RPC
cd /data_new/scripts

python3 rehearse-two-staker-rejoin.py \
  candidate-fork-126237291 \
  --previous-pass nitro-restart-mock-20260928-010622/summary.json \
  --governance-span 7838 \
  --span-audit recovery-message-span-20260928-013625/summary.json \
  --span-replay retained-span-replay-20260928-014843 \
  --out "governance-span-mock-$(date +%Y%m%d-%H%M%S)"
```

无需重跑7838条消息，无需启动Docker或访问节点数据库。需要有效ARCHIVE_RPC启动独立本地fork。
本轮仍为双账户额外用例，不改变生产退款范围。

## 验收

1. 检查父链旧状态承诺和新root；计算挑战executionHash，再按是否存在兄弟节点选择lastHash。
2. 计算nodeHash时纳入跨度7838、Inbox累加器、新root，forceCreateNode传精确非零expectedNodeHash。
3. Safe顺序执行恢复治理后，比较实际创建节点哈希与独立计算值。
4. 两历史账户退款支付、zombie清理、普通64消息B→C创建/加入/确认继续通过。
5. status=governance_span_rejoin_passed，governanceNumBlocks=7838，exactGovernanceNodeHashChecked=true。
6. normalAssertionNumBlocks仍为64；ownedForkStillRunning=false。

保存governance-commitment.json、步骤回执、参与者状态和summary.json。
模拟身份、顺序Safe治理调用；不是新参数原子批次或真实Nitro自动运行的测试。
productionNumBlocksApproved=false、parentConfirmedAToBProven=false、readyForProduction=false。

## 下一步已备妥

rehearse-atomic-span-recovery.py 将同一参数接入原子批次和最后一步失败回滚测试。
操作与验收见 ATOMIC-SPAN-MOCK.md；服务器atomic-span-mock-20260928-084057已通过。
接续Nitro自动提交及重启脚本见NITRO-SPAN-RESTART.md，运行结果待回传。
