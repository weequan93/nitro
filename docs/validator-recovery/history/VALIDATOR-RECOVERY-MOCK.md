# 8149 validator 恢复演练

最新服务器演练进度见 [2026-09-25 记录](../../../scripts/validator-recovery/evidence/PROGRESS-20260925.md)。
下文“尚未执行”的描述保留为初版工具交付时的历史状态；服务器已完成合约演练
及 watchtower 从恢复检查点使用新 WASM 跨 batch 续跑，尚未完成正常质押/提交/确认。

## 交付范围

本轮是**父链合约恢复流程演练**。它验证管理员路径是否接受恢复断言、
质押是否正确退还、Outbox 是否保留。不会修改 8149 的数据库或 staking 配置，
不会向 Arbitrum One 提交交易，不需要私钥，也不会创建 Safe 待签名提案。

分三阶段完成恢复验收：

1. 本脚本：采集实际候选检查点，在私有 Anvil fork 演练合约操作。
2. 独立 validator 数据副本：对接模拟父链，验证从检查点初始化、新 WASM
   续跑，以及后续断言与重新质押。**本脚本没有实现这一阶段。**
3. 上述阶段通过，加上历史提现消息审计与实际 Safe 交易模拟，才生成生产交易包。

## 环境与文件

在能够访问 8149 和父链 RPC 的 Linux 机器执行，需 Python 3、Foundry 的
`anvil` 和 `cast`。建议在有余量的机器运行；检查点采集会占用验证服务资源。
不安装编译器，不构建 Nitro，不要求传出完整节点数据库。

以下四个文件必须在同一目录：

- run-validator-recovery-mock.sh
- mock-validator-recovery.py
- prepare-recovery-fork.py
- probe-recovery-authority.py

Mac 仓库内打包文件为 `validator-recovery-mock.tar.gz`。例如：

```bash
scp validator-recovery-mock.tar.gz root@deriw-prod-validator:/data_new/scripts/
```

服务器上：

```bash
cd /data_new/scripts
mkdir -p recovery-mock-tools
tar -xzf validator-recovery-mock.tar.gz -C recovery-mock-tools
python3 --version
anvil --version
cast --version
```

若缺少 anvil/cast，先准备这两个 Linux 可执行文件，不要跳过依赖检查。

## 一条命令执行

```bash
cd /data_new/scripts
bash recovery-mock-tools/run-validator-recovery-mock.sh \
  /data_new/scripts/validation-125022703-20260923-130444/validation-input.json \
  "/data_new/scripts/recovery-mock-$(date +%Y%m%d-%H%M%S)" \
  0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a
```

默认父链只读 RPC 为 `http://10.1.2.16:8547`，子链为 `http://127.0.0.1:8149`。
可通过 `PARENT_RPC`、`NODE_RPC` 环境变量更改。不需要停止生产节点或其他 validator。
指定的 staker 只在 fork 中退款；脚本检查其当时是否仍质押、有无挑战。

## 具体检查

采集阶段默认选择 8149 本地 head 后退 64 块，核对消息编号映射、父哈希、
完整 global state；要求候选位置超过快照的 latestConfirmed 且输入 batch 可用。
新 WASM 只重放候选消息一次。**这是局部检查，不能替代全区间验证。**

合约阶段自行启动仅监听 127.0.0.1 的 Anvil，固定采集的父链块，chain ID=31337。
不接受用户提供的写入 RPC 地址。核对 fork 块哈希、已采集字节码、Safe executor
角色与 threshold=3 后，才启用本地模拟调用。

```text
模拟 Safe 身份（不是收集或验证三位 owner 签名）
  → UpgradeExecutor.executeCall → Rollup.pause
  → forceRefundStaker：只处理命令明确指定的 staker
  → 检查可领取余额增加及原质押身份移除
  → setWasmModuleRoot
  → forceCreateNode：实际旧确认状态 → 8149 候选检查点
  → forceConfirmNode：新建的恢复节点
  → 检查 latestConfirmed / firstUnresolved / Outbox root / spent
  → resume
  → 模拟原 staker withdrawStakerFunds，核对实际 ETH 收支与 gas
  → removeOldZombies，核对已退出地址不再是 zombie
```

退款不会减少生产质押；Anvil 为模拟地址补充的 gas 余额仅存在于私有 fork。
退出时销毁 Anvil 进程，只留下日志和结果。

## 必须理解的限制

- 新旧历史之间使用**合成 numBlocks=1**，expectedNodeHash=0，以隔离验证管理员
  合约路径。它不是实际历史执行长度，也不是可签名的生产参数，不能直接照搬。
- 使用实际部署字节码执行，比只读本地源码更强；但这不是完整源码/编译配置认证。
- Anvil 是通用 EVM fork，不等同于完整 Arbitrum Nitro 父链环境。若部署逻辑依赖
  Arbitrum 特殊预编译或区块语义，需额外验证；遇到相关失败不得直接绕过。
- Safe 被模拟为调用者；Safe 的 owner 签名、guard、module 和 nonce 路径未演练。
- 没有重启 validator、没有重新质押、没有验证后续正常断言。
- 仅检查指定旧根和消息 8111–8114 的 spent 标记，不代表审计全部历史提现。
- 管理员检查点恢复不是旧分叉状态到新状态的 WASM 执行证明。

## 结果怎么看

```bash
cat /data_new/scripts/recovery-mock-时间/snapshot/summary.json
cat /data_new/scripts/recovery-mock-时间/fork/summary.json
```

成功应是 `status=contract_rehearsal_passed`，但 `readyForProduction=false`。
把两份 summary 贴回来即可；无需传出大的 checkpoint-input.json。

失败会输出 `status=stopped` 和具体错误；已完成的本地步骤保存在 `steps.json`。
Anvil 启动或历史状态读取失败查看 `anvil.log`。若父链 RPC 没有该快照的历史状态，
需要可服务该高度的父链 RPC；不是重建 8149 的理由。

若采集输出 `local_checkpoint_not_ahead_of_confirmed`，表示当前候选位置不足，
需要等待 8149 同步或重新选点。不要手改 JSON 为通过。

## 本地已完成检查

- Python 编译和 CLI 参数检查。
- 六项隔离测试：成功路径、错误 chain ID、缺少 executor 权限、挑战中的 staker、
  spent 标记改变、未确认本地 fork 身份时禁止写入。
- Foundry 编码真实 forceCreateNode 嵌套 ABI。
- 本机 Anvil 31337 启动、RPC 检查及退出。

尚未使用你的候选检查点完成真实父链 fork 演练。本机公共 RPC 的 Python 请求
返回 HTTP 403；服务器私有父链 RPC 与实际采集输入需在你的环境运行确认。
