# 实际执行与证据

本记录整理于 2026-09-30（新加坡时间），覆盖 2026-09-23 至 2026-09-29 的诊断、演练和生产恢复。日志内的 Nitro 时间按 UTC 阅读，服务器输出目录通常采用 UTC+8；下表明确时间基准。没有补造服务器上尚未传回的输出文件或完整交易 hash。

## 根因与修复代码

首次保存的失败消息为 125022703。相同输入的单变量实验发现，AddressMap.Add 对重复地址返回错误或返回成功的变化，足以分别复现旧 WASM 与当前本地链的两个区块哈希。原生记录见 [根因及历史语义分析](history/validation-125022703-recovery-plan.md)。

该分支末尾三个提交分别为 `0285b9a92`（可选 legacy fee account preimages）、`237cc2a4d`（legacy blacklist preimages）、`16c17ee3a`（scheduled redeem witnesses）。这些记录补丁与执行语义差异应分别理解：补齐见证不能自动使两条不同历史一致。整理记录时没有追加共识语义修改。

新 WASM 对消息 125022161–125026716 共 4556 条的重放匹配本地保留链，但终点仍不同于父链 assertion 42560。相同 SendRoot 不等于相同 BlockHash 或完整 global state，因此决定继续保留 sequencer 当前历史，并演练治理检查点迁移。

## 演练和准备阶段

| 阶段 | 实际覆盖 | 可查证据 |
| --- | --- | --- |
| 单 Safe 恢复与暂停检查点 | 3/4 门槛、两签失败、退款信用、新 WASM、创建/确认恢复节点 | `legacy/tools` 的 `rehearse-safe-recovery.py`、`rehearse-paused-recovery.py`；服务器 `candidate-*-mock-*` |
| Withdrawals 保护 | 8120 个 spent flags、8118/8119 样本付款与重复/回滚 | `rehearse-withdrawal-recovery.py`；后来的三账户检查覆盖 8121 个 flags 和 6591 自然未领取样本 |
| 原子批次 | 最后一步失败时内层回滚；Safe nonce 可能被消耗；成功保持暂停 | `rehearse-atomic-recovery.py`、`rehearse-atomic-span-recovery.py`、`rehearse-three-staker-atomic.py` |
| 新 WASM 固定历史区间 | 7838 条和刷新后的 10578 条 A′→B 重放 | [7838 区间说明](../../scripts/validator-recovery/evidence/RETAINED-SPAN-REPLAY.md)、[10578 区间报告](../../scripts/validator-recovery/evidence/reported-results/three-staker-span-replay-20260928-091247-passed.json) |
| 恢复后继续执行 | B→C 48 条新重放、普通节点创建/加入/确认 | [B→C 报告](../../scripts/validator-recovery/evidence/reported-results/three-staker-followup-20260928-134148.json)、[三账户重新加入](../../scripts/validator-recovery/evidence/reported-results/three-staker-rejoin-mock-20260928-140344.json) |
| Nitro 自动提交与重启 | 临时真实密钥签名，自动创建和确认，优雅重启后观察 90 秒无额外交易 | [运行演练摘要](../../scripts/validator-recovery/evidence/reported-results/three-staker-nitro-restart-20260928-231557-partial.json)；该保存文件是部分报告 |
| 数据复制 | 冻结源后 `/data_mock`→`/data_new`，size/mtime/metadata/file-set 比较通过 | [复制报告](../../scripts/validator-recovery/evidence/reported-results/db-replacement-20260928-062110-757499.json)；不是全量 checksum |
| Safe 同包校验 | 完整交易字段、nonce/hash、门槛负例、恢复失败回滚、回执解码 | `scripts/validator-recovery/toolkit/verify-fork.py`、`presign.py verify`，原报告区分服务器与本机 shim |

前期恢复 assertion 的 `numBlocks=1` 只用于机制演练。7838 / 10578 是旧候选的位置跨度，候选被父链确认进度追上后必须刷新；它们都没有成为本次实际生产参数。

## 本次最终候选和预签包

服务器窗口：`/data_new/scripts/presign-window-20260929-154302`；重放目录为该路径加 `-replay`，三笔 Safe 包为加 `-sequence`。本地已保存 [导入内容与 EIP-712 核对](../../scripts/validator-recovery/evidence/presign-window-20260929-154302-review/review.json)。

| 参数 | 本次固定值 |
| --- | --- |
| 父链原确认节点 | 43123 |
| 父链 A 位置 | Batch 324089 / Pos 0 |
| A BlockHash | `0x42fea32f2e72edb32eb5a8fb0ec1760a2a26d41ccaf4b96712988be511f71962` |
| 重放消息区间 | 127104254–127108720，4467 条 |
| B 位置 | Batch 324110 / Pos 122 |
| B BlockHash | `0x9f6ef87c6d5ca1b9e17cc94d4c79a9b64bcf32adccc7aad29c2d4dc8e478a74f` |
| B SendRoot | `0x9a0300bf873d9ce0ea8365ac9dd758c6023d13a883ed79e4027d6095838a4603` |
| 重放完成时间 | 2026-09-29 08:47:56 UTC / 16:47:56 UTC+8 |
| 重放耗时 | 3803.07 秒，4467/4467 通过 |
| 记录链 SHA-256 | `c0c1ce88ed9d399ccddf7ba696e0a10f35790bc907e28b137a26271699cbc13e` |
| 生产治理位置跨度 | 4467；仍非父链 A→B 执行证明 |

重放最终 summary 来自用户粘贴的服务器输出；完整 4467 份逐消息记录未复制到本机。该限制记录在 [incident.json](incident.json) 中。

治理 Safe 三笔内容为：nonce 10 暂停；nonce 11 原子执行 `forceRefundStaker` → `setWasmModuleRoot` → `forceCreateNode` → `forceConfirmNode`；nonce 12 恢复。恢复包成功后仍暂停，运行验收和恢复交易之间是独立阶段。

## 生产执行

| 顺序 | 事实 | 证据来源 |
| --- | --- | --- |
| 停止旧提交者 | 操作员报告云 A、云 B、本机旧 validator 均已停止 | 对话中的操作员报告；未保存另外两台机器的完整退出日志 |
| 暂停，nonce 10 | `0x87a8bb900761a8f21f87f565ce318548d73d1200c5ae16680f86162d56d57131`，服务器 `accept` 通过，回执块 `0x1e65d6fe`，paused=true | 用户报告，验收时间 09:42:35 UTC / 17:42:35 UTC+8 |
| 恢复，nonce 11 | `0x03e938966a6b480e3b907b153e173bd3576b7b84c88a2e2387810441fc7d249e`，节点 43126，新 WASM、三个退款 credit、仍暂停 | [公开父链独立核验](../../scripts/validator-recovery/evidence/executed-recovery-03e938/summary.json)及服务器 `accept` |
| Watchtower 交接 | snapshot-sync 正常停止，启动 `validator-recovered-watchtower`；DB `/data_mock`，无签名 | 用户报告，计划 `/data_new/scripts/validator-cutover-20260929-174736` |
| Watchtower 验证 | 09:56:59 和 09:57:30 UTC 两次观察：新 root、127108784、Batch 324111/0 | 用户报告，两次值相同；证明达到 B 之后端点，不代表持续增长 |
| 配置及 keystore 准备 | `prepare-compose.py --apply`，只复制加密密钥及配置；不复制 DB、不启动 | 用户报告 `/data_new/scripts/compose-prepared-20260929-181255` |
| 恢复，nonce 12 | 后续 pinned 查询 paused=false、治理 Safe nonce=13；实际执行交易 hash 尚未归档 | [清理后状态](../../scripts/validator-recovery/evidence/zombie-cleanup-57531e3c/summary.json)；状态不能代替具体 resume 回执验收 |
| zombie 清理 | `removeOldZombies(0)`，交易 `0x57531e3cfa42afae4b76fb9dd36f237174fca48222a130b14310e250b92087c6`，由本机 validator EOA 提交 | [交易与账户状态核验](../../scripts/validator-recovery/evidence/zombie-cleanup-57531e3c/summary.json)，zombieCount=0 |
| 普通节点及质押 | 本机 65fa 重新质押，创建 43127，nodeHash `0x947f3d434693712f6e6d3f56233216ba18fada0daac6f0fd36b5404f6712c997`，普通跨度 64 | 同一 pinned 状态报告；运行日志显示 staker 交易成功，完整创建 hash 尚未保存 |
| Fast confirmation | 10:58:27 UTC 查询 Fast Safe nonce=37626，阈值3，65fa 已 approveHash，另外两账户当时为0 | [批准状态](../../scripts/validator-recovery/evidence/first-recovered-fast-confirm.json)；不是最终快确认成功证明 |

恢复节点 43126 的 hash 为 `0x6c679a7d16a4f0e6768f320ff0491e512150902e53404ce8a68974e4aee0c779`；实际 InboxMaxCount 为 324111。三个退款账户为 5cda / d38e / 65fa，每个 credit 10^12 wei。后续重新质押集合为 65fa / 5cda / 21d4，退休账户不恢复提交。

实际部署与准备文件存在差异。准备方案使用 `/data_new/validator/compose.recovered.json` 和 `/nodeconfig.json`，fast confirmation 默认 false；操作员后来在新的 `validator-nitro-1` 内手动运行 split entry，使用 `/home/user/.arbitrum/nodeConfig.json`，并显式开启 fast confirmation。需要以实际进程、挂载和链上账户为准，不能把准备报告当作最终部署证明。容器 PID 1 为 `tail`，所以容器 Up 也不能证明 Nitro 在运行或已正常退出。

## 关联 poster / DAS 排障

| UTC 日志时间 | 发现与结果 |
| --- | --- |
| 09-29 11:04 | 批次324111：`DataTooLarge(108465,104857)`，calldata配置110000超过合约限制 |
| 11:15 | 改100000后，完整交易仍被父链接收层拒绝 `oversized data`；一个 DAS RPC 连接被拒绝，触发直接 calldata 回退 |
| 11:29 | 热加载后边界为89960，说明calldata目标90000生效；批次324144已发送，未发布消息55488 |
| 11:31:40 | 批次324151得到成功回执，父块510014968；未发布消息54621，成功回退发布继续 |
| 14:46 | `0x88`证书提交批次324668被拒绝：`NoSuchKeyset(0xefecd070797fd8154e149a2fb8b13aceb0e8e3dac51f514cc4e33e79b7dfb8b6)` |

90000修复已得到回执支持；最新 keyset 错误还没有后续配置或链上查询结果。不能记录为“DAS已全面恢复”。普通批次切分日志 `Batch overflow` 不等于发布失败；`created block` 也不等于批次已提交父链。

## 仍需补齐的最终验收

1. nonce12对应的实际 resume 交易完整 hash、Safe回执和包验收。
2. 本机43127的完整创建交易、普通或快确认的完整回执、持续前进的新root状态。
3. 云A和云B新版本实际运行、各自签名地址/质押节点、同位置global state，以及3/3快确认成功证据。
4. DAS RPC恢复、实际委员会配置与链上有效keyset的一致性，最新批次持续成功回执。
5. 退休账户退款领取结果、其他原账户退款与后续质押的最终账户核对。
6. 完整withdrawal history审计仍未完成；既有spent flags和样本不能代替它。

早期报告中的 `readyForProduction=false`、`productionTransactionsSent=false` 描述的是对应程序和对应运行。只读核验程序未广播，不等于操作员没有执行生产交易。这里保留原报告字段，并明确各项的来源和观察时间。
