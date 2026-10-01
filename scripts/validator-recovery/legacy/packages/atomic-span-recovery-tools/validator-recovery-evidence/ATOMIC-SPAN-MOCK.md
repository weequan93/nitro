# 7838 原子恢复演练

状态：脚本已准备，本地检查通过；等待服务器演练结果。

前置证据是 governance-span-mock-20260928-081940 的顺序治理通过结果，
以及 retained-span-replay-20260928-014843 的全部 7838 条记录。
不重新执行 WASM 重放。

本轮仅创建独立本地 Anvil，历史父块固定 0x1e58e750、chainId 31337。
ARCHIVE_RPC 仅作为历史读取源。无 Docker 操作，无节点数据库访问。
不需要操作 snapshot-sync 或 validator-nitro-1，不访问 /data。

## 操作

把 atomic-span-recovery-tools.tar.gz 上传到 /data_new/scripts，解压更新依赖。
包内均为 Python 工具及本说明；不含数据库、配置、密钥或既有演练输出。

```sh
export PATH="$HOME/.foundry/bin:$PATH"
cd /data_new/scripts
tar -xzf atomic-span-recovery-tools.tar.gz
export ARCHIVE_RPC

python3 rehearse-atomic-span-recovery.py \
  candidate-fork-126237291 \
  --span-pass governance-span-mock-20260928-081940/summary.json \
  --span-audit recovery-message-span-20260928-013625/summary.json \
  --span-replay retained-span-replay-20260928-014843 \
  --out "atomic-span-mock-$(date +%Y%m%d-%H%M%S)"
```

如果新终端没有 ARCHIVE_RPC，先通过隐藏输入设置（不要粘贴密钥或 URL 到聊天）：

```sh
read -rsp 'Archive RPC URL: ' ARCHIVE_RPC
printf '\n'
export ARCHIVE_RPC
```

## 实际测试内容

1. 校验已有重放记录摘要链和上轮 7838 顺序治理结果。
2. 在 EVM snapshot 内执行参考恢复，计算精确节点哈希，然后回滚参考状态。
3. Safe 单独 pause，再将退款、设置 WASM、创建恢复节点、确认恢复节点组成原子批次。
4. 故意让最后一步失败：检查暂停状态、指针、root、父节点、最新及下一节点存储、
   选中质押账户和退款余额、Outbox 映射回滚；Safe nonce 允许增加一次。
5. 正确批次成功后保持 paused=true，核对恢复节点及精确哈希。
6. 自动关闭本轮拥有的 Anvil。

只退款 active 0x5cda…de33a；双账户用例已另行通过，不扩大生产退款范围。
参考分支使用 Safe impersonation，实际批次使用模拟 owners approveHash + Safe execTransaction。

## 通过字段

- status=contract_rehearsal_passed
- test=atomic_safe_recovery_span_7838
- governanceNumBlocks=7838
- exactExpectedNodeHash=0x1c44f95dd83a8e24c4794528d57b32b053a516204151418fce05e8a75e670b7b
- innerRollbackTested=true、atomicSuccessTested=true、paused=true、forkRunning=false

readyForProduction=false、productionNumBlocksApproved=false、parentConfirmedAToBProven=false 保持不变。
本轮不测试 Nitro 进程自动提交或重启；待本轮通过后补测新参数的运行流程。
7838 是测试中的位置跨度约定，A 与本地 A′ 的不同哈希仍然存在。
