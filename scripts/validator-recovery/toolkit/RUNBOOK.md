# 执行手册：先准备，获得具体授权后才进入生产窗口

## 0. 安装及角色协调（现在可做）

从交付的 Mac 安装器完整复制到服务器终端；安装器只写 `/data_new/scripts/production-recovery-toolkit-v2`。若已有不同版本，它拒绝覆盖；不要跳过校验。

```sh
export PATH="$HOME/.foundry/bin:$PATH"
cd /data_new/scripts/production-recovery-toolkit-v2
sha256sum -c MANIFEST.sha256
python3 -m unittest discover -s tests -v
export PARENT_RPC=http://10.1.2.16:8547
export NODE_RPC=http://127.0.0.1:8349
read -rsp 'Archive RPC URL: ' ARCHIVE_RPC
printf '\n'
export ARCHIVE_RPC
```

命令中的输出目录每次用新名字。**所有 shell 块逐段运行**，不要把本手册当一键执行文件。数据库复制/容器切换由用户独立安排，此处没有相应命令。

协调事项：三台旧 MakeNodes 停止提交的实际时点、Fast Safe 旧提案/已批准 hash 的处理、治理 Safe 未执行队列及离线签名确认。禁止旧 validator 在恢复后继续提交旧 WASM 断言。节点停止和链上 pause 是两件不同的事，停止节点不能防止别人确认旧提案。

只读 Fast Safe 身份核对：

```sh
python3 fast-safe-check.py --parent-rpc "$PARENT_RPC" --out "/data_new/scripts/fast-safe-check-$(date +%Y%m%d-%H%M%S).json"
```

## 1. 准备暂停包（只读，可以现在运行）

```sh
PAUSE="/data_new/scripts/pause-review-$(date +%Y%m%d-%H%M%S)"
python3 recovery.py followup pause --parent-rpc "$PARENT_RPC" --out "$PAUSE"
python3 verify-fork.py --package "$PAUSE" --parent-rpc "$ARCHIVE_RPC" --out "${PAUSE}-fork"
```

检查 package.json、REVIEW.md、safe-import.json、分叉 summary。暂停是 **Safe CALL → UpgradeExecutor.executeCall(Rollup,pause())**，value0；只有一个调用。不要改成 UpgradeExecutor.execute 或外层 DELEGATECALL。

临近签署，再运行：

```sh
python3 recovery.py preflight --package "$PAUSE" --parent-rpc "$PARENT_RPC" --out "${PAUSE}-preflight-$(date +%H%M%S)"
```

**到此仍未暂停。** 得到明确生产窗口授权后，由治理团队在官方 Safe Global UI 导入，并核对地址、链、nonce、value、operation、data、全部 Safe gas 字段及 SafeTxHash。程序不连接 Safe 服务、不提案、不签署。

Safe UI 的实际外层交易必须等于 package.json.safeFields。pause/resume 单笔是 CALL；恢复多笔应是指定 MultiSendCallOnly 的 DELEGATECALL。UI 若包装方式不同，停止签署，重新生成/试跑，不凭内层显示相同就签。

Safe 队列列表不是完整授权清单；离线签名、链上 approvedHashes 不会必然显示在 UI。由团队确认并协调旧快速确认流程。

## 2. 验收暂停，固定实际 A/B（仅在暂停交易实际执行后）

```sh
PAUSE_TX=填写实际42161交易hash
PAUSE_ACCEPT="${PAUSE}-accepted"
python3 recovery.py accept --package "$PAUSE" --transaction "$PAUSE_TX" --parent-rpc "$PARENT_RPC" --out "$PAUSE_ACCEPT"

WINDOW="/data_new/scripts/recovery-window-$(date +%Y%m%d-%H%M%S)"
python3 collect.py --parent-rpc "$PARENT_RPC" --node-rpc "$NODE_RPC" --out "$WINDOW"
```

collect 默认选择本地 head-64，可用 `--block` 指定候选块。它验证输入 Id/区块映射、批次可用、A 承诺和 B 本地 canonical header。无法定位 A′、B 不在 A 前方或母链参与者变化时停止。不是继续修改旧报告。

如果需要先做纯历史演练，collect/build 可用 `--simulation`，生成包永久标注 simulation-only，生产 preflight/receipt 验收拒绝这种包。

## 3. 重放和具体参数审阅

```sh
REPLAY="${WINDOW}-replay"
python3 replay-span.py --audit "$WINDOW/audit/summary.json" --node-rpc "$NODE_RPC" --out "$REPLAY"
```

中断后使用相同路径加 `--resume`。不要重新创建时间戳冒充续跑；先确认没有其他实例持有 run.lock。

```sh
python3 replay-span.py --audit "$WINDOW/audit/summary.json" --node-rpc "$NODE_RPC" --out "$REPLAY" --resume
python3 recovery.py decision-template --candidate "$WINDOW/candidate" --audit "$WINDOW/audit/summary.json" --out "$WINDOW/decision.json"
```

审阅 decision.json：绑定 candidate/audit 的 SHA256、跨度、迁移性质及审阅记录。模板两个 acknowledgement 默认 false，理解后才能改 true；模拟审阅保留 mode=review-only。正式参数审批由治理团队记录后改 approved-parameters，**这只是审阅记录，不是签名，也不建立 A→B 证明**。

```sh
PACKAGE="${WINDOW}-package"
python3 recovery.py build --candidate "$WINDOW/candidate" --audit "$WINDOW/audit/summary.json" --replay "$REPLAY" --decision "$WINDOW/decision.json" --parent-rpc "$ARCHIVE_RPC" --node-rpc "$NODE_RPC" --out "$PACKAGE"
python3 verify-fork.py --package "$PACKAGE" --parent-rpc "$ARCHIVE_RPC" --out "${PACKAGE}-fork"
```

生成器逐条检查保存的重放链；不再次执行 WASM。分叉执行与 safe-import.json 相同的四个 CALL：退款→设置新 WASM→精确创建恢复节点→确认恢复节点。**成功后仍 paused=true；不会在同一批里 resume。**

负向测试改变最后一步的确认 hash，证明内层状态回滚；Safe 失败可消耗 nonce。工具在自己的分叉恢复快照后，以原始包 nonce 执行正向测试。分叉使用 owner impersonation + approveHash，不是生产 ECDSA。

## 4. 签署前审阅及执行恢复

```sh
python3 recovery.py preflight --package "$PACKAGE" --parent-rpc "$PARENT_RPC" --out "${PACKAGE}-preflight-$(date +%H%M%S)"
```

同时检查 decision.mode=approved-parameters、具体治理审批记录、同包分叉通过、Safe UI hash 与包一致。预检只读，并不是“授权执行”。

链上现有约束：prevNode 必须等于 latestConfirmed；A/stateHash 必须一致；精确 expectedNodeHash 绑定 sibling/执行参数/accumulator/WASM；确认使用固定 nodeNum/B。

**不是完整原子 guard：** 角色、代码、退款金额等核对发生在链下。执行窗口应排他协调治理交易；若其他治理操作改变状态则中止重签。Inbox 仍可增长，createNewNode 会把执行时的实际 Inbox 总数写入 stateHash；我们允许增长，并在回执解析 NodeCreated 核验实际值。它不会改变已绑定的目标批次 accumulator，但后续 validator 必须使用实际新节点承诺。

不要仅凭 status=1 判断成功。Safe 可能记录 ExecutionFailure。执行完：

```sh
RECOVERY_TX=填写实际42161恢复交易hash
ACCEPT="${PACKAGE}-accepted"
python3 recovery.py accept --package "$PACKAGE" --transaction "$RECOVERY_TX" --parent-rpc "$ARCHIVE_RPC" --out "$ACCEPT"
```

验收检查实际 execTransaction 字段、SafeTxHash、Safe nonce 事件、ExecutionSuccess、canonical receipt、节点/WASM/退款信用/Outbox。检查的是回执所在块结束时状态；若同块有后续动作导致状态不符，应人工核对，不能忽略失败。

## 5. 暂停状态运行验收，再单独 resume

在用户安排好的新镜像/配置下，由独立升级计划完成启动与 Watchtower/验证端点核验。**不要拿 `/data_new` 模拟数据库启动生产，也不要把 `/data_mock` 连到 Anvil。** 正式数据切换由用户计划负责。

只读检查可重复，对每台实际 RPC 执行：

```sh
python3 owner-check.py --package "$PACKAGE" --acceptance "$ACCEPT" --parent-rpc "$PARENT_RPC" --node-rpc "$NODE_RPC" --out "${PACKAGE}-runtime-$(date +%H%M%S).json"
```

单次输出仅证明观察时新 WASM 验证端点至少到 B，不证明三台持续健康或私钥签名成功。还需核对各台 txSender 地址、父链42161、MakeNodes配置、新root与验证进度；退休 d38e 不启动。

具体运行验收与恢复授权完成后，生成 resume：

```sh
RESUME="${PACKAGE}-resume"
python3 recovery.py followup resume --package "$PACKAGE" --acceptance "$ACCEPT" --parent-rpc "$PARENT_RPC" --out "$RESUME"
python3 verify-fork.py --package "$RESUME" --parent-rpc "$ARCHIVE_RPC" --out "${RESUME}-fork"
python3 recovery.py preflight --package "$RESUME" --parent-rpc "$PARENT_RPC" --out "${RESUME}-preflight"
```

治理 Safe 团队按同样规则导入签署；然后：

```sh
RESUME_TX=填写实际42161恢复运行交易hash
python3 recovery.py accept --package "$RESUME" --transaction "$RESUME_TX" --parent-rpc "$ARCHIVE_RPC" --out "${RESUME}-accepted"
```

## 6. 原账户提款、清理与继续质押

**这里是 validator 质押退款，不是用户 L3→L2 跨链提款。** 退款仍付给原账户，不能代填别的收款地址。5cda/65fa/d38e 分别由账户主人操作，密钥不提供给助手。

将工具和本次 PACKAGE/ACCEPT 交给对应操作员；OWNER_ADDRESS 填其账户，KEYSTORE 仅在其自己的安全签名环境设置。不要让助手读取 `/data` 中的密钥。

```sh
export RECOVERY_PACKAGE="$PACKAGE"
export RECOVERY_ACCEPTANCE="$ACCEPT"
export OWNER_ADDRESS=填写5cda或65fa或d38e完整地址
bash withdraw-refund.sh check
```

**以下仅在该账户提款获授权后手动运行：**

```sh
export KEYSTORE=填写本机单个加密keystore文件路径
bash withdraw-refund.sh broadcast
```

脚本会解锁确认实际地址、重新检查信用、要求输入 WITHDRAW 才广播。不会处理新质押、不导出私钥。结束后填写实际 TX，再核对：

```sh
WITHDRAW_TX=填写实际退款交易hash
python3 owner-check.py --package "$PACKAGE" --acceptance "$ACCEPT" --parent-rpc "$ARCHIVE_RPC" --address "$OWNER_ADDRESS" --transaction "$WITHDRAW_TX" --expect-empty
```

后续质押由已准备好的新 Nitro MakeNodes 流程提交，不手工制作普通断言；以运行日志和链上 `isStaked/latestStakedNode` 确认。云 B 21d4 需要其自己的新 stake 资金，不能算成 d38e 退还的资金。若 zombie 阻止重入，先检查具体地址和节点；新 Nitro 正常清理机制已有历史演练，不盲目循环清理或增加强制治理动作。

验收：最终三个运行账户均在正确分支、新 WASM 下持续验证、普通断言创建/确认成功；d38e 无新 stake；Fast Safe 仅在旧提案清理后恢复新流程；按现有跨链提款工具抽检。不得把“已退款信用”写成“已支付”或把“有stake”写成“真实进程正常”。

## 7. 失败处理与资料保存

- pause失败：保持当前状态，检查 Safe receipt/nonce，不假设已暂停。
- 恢复批次失败：核实 ExecutionFailure 和 Safe nonce；完整重新采集/预检，禁止只重发旧 nonce。
- 恢复成功但运行不通过：保持 paused，保留证据，协调修复；不要自动 resume。
- 恢复不等于可逆数据库操作；不能把反向设置旧 root 当成“回滚方案”。
- 每阶段保留 package、checksums、fork报告、实际 tx/hash/receipt、审批记录和运行观察。不要发布含 Archive RPC 凭据的 Anvil 日志。
- 出现额外 staker、challenge、代理升级、Safe owner/module/guard变化时，固定基线检查停止；重新审阅后修改基线并测试，不能关掉检查。

## 分叉环境限制与历史复测

本机 Anvil 1.2.3 不实现 ArbSys.arbBlockNumber，直接恢复会在第三步（创建节点）失败。验证器默认识别并停止。这不是生产 Rollup 已失败，也不是 late-failure 测试通过。

仅为检查相同 Solidity 调用、回滚和回执，可显式加 `--allow-arbsys-log-shim`：它在自有31337分叉把 **0x64 上唯一 arbBlockNumber selector** 的返回值设为 EVM BLOCKNUMBER，其他 selector 仍 revert。它不实现 ArbOS，也不证明真实 L1/Arbitrum 双时钟、费用或预编译环境。报告状态带 `with_arbsys_log_shim`，不能改名成原生验证通过。恢复/退款/生产合约代码不修改。

可选 `--test-followups` 在同一私有分叉额外做 Safe resume 和三账户退款余额核对，再撤销这些后续测试，最终仍停留在恢复后暂停状态。签名仍为 fork impersonation。

历史候选完整证据仍在服务器，下面只生成 simulation-only 包；把此前保存的 decision-template 两项声明按模拟审阅填写，**不要把此包导入生产 Safe**：

```sh
HIST=/data_new/scripts/three-staker-fork-20260928-090354
HIST_AUDIT=/data_new/scripts/three-staker-span-20260928-090354/summary.json
HIST_REPLAY=/data_new/scripts/three-staker-span-replay-20260928-091247
HIST_DECISION="/data_new/scripts/historical-v2-decision-$(date +%Y%m%d-%H%M%S).json"
python3 recovery.py decision-template --candidate "$HIST" --audit "$HIST_AUDIT" --out "$HIST_DECISION"
```

填好历史模拟 decision 后：

```sh
HIST_PACKAGE="/data_new/scripts/historical-v2-package-$(date +%Y%m%d-%H%M%S)"
python3 recovery.py build --candidate "$HIST" --audit "$HIST_AUDIT" --replay "$HIST_REPLAY" --decision "$HIST_DECISION" --parent-rpc "$ARCHIVE_RPC" --node-rpc "$NODE_RPC" --simulation --out "$HIST_PACKAGE"
python3 verify-fork.py --package "$HIST_PACKAGE" --parent-rpc "$ARCHIVE_RPC" --test-followups --out "${HIST_PACKAGE}-fork"
```

如果服务器也提示缺少 ArbSys，可在新输出目录显式加上述日志模拟选项，保留环境限制。带模拟补丁的测试不足以消除生产参数、实际签署、真实运行验收等边界。公共 RPC 也可能没有历史状态，历史复测需真正的 Archive RPC。
