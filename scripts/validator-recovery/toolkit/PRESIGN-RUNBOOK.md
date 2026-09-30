# 一次集中签署，操作员分阶段执行

此附加流程不要求先执行 pause 才能准备恢复交易。它固定采集时的 A、B、节点编号、退款账户、WASM、Safe nonce，并在准备期间及每次执行前检查状态。

默认三个 nonce 为 N、N+1、N+2；以生成的 sequence.json 为准。已经签好的 pause 可沿用同一个包，不需要重复签名。safe-import.json 只包含内层调用，UI 必须显式选择各包要求的未来 nonce 和完整 Safe 字段。

## 性质与边界

- 预签交易内容固定；若断言、权限、质押等状态改变，不能自动适应，须重新审阅和签署。
- 所有旧断言提交及旧 Fast Confirmation 执行保持停止。Inbox 可以继续增长。
- nonce 只保证顺序，不保证前一笔成功。Safe 失败有可能消耗 nonce。
- check 命令只提供链下执行前检查，不是合约 guard；持有足够签名者可以绕过它执行。“只有操作员执行”是团队协调，不是新合约权限。
- A′→B 重放不证明父链 A→B。生产参数审阅仍需明确接受受信任治理迁移。
- presign.py 不读取任何 keystore、不发送生产交易、不接触节点数据库或 /data。只有 verify 命令在自建 loopback Anvil 31337 上发送模拟交易。
- 新附加流程的测试不能替代本次具体包在服务器上的 verify。旧恢复演练、旧 span 不作为本次签名参数。

## 1. 采集预签输入（pause 尚未执行也可做）

在服务器中运行；PARENT_RPC 为父链读节点，NODE_RPC 为 snapshot-sync：

```bash
export PATH="$HOME/.foundry/bin:$PATH"
cd /data_new/scripts/production-recovery-toolkit-v2
export PARENT_RPC=http://10.1.2.16:8547
export NODE_RPC=http://127.0.0.1:8349
PAUSE=/data_new/scripts/pause-window-20260929-152145
WINDOW="/data_new/scripts/presign-window-$(date +%Y%m%d-%H%M%S)"
python3 presign.py collect --pause "$PAUSE" --parent-rpc "$PARENT_RPC" --node-rpc "$NODE_RPC" --out "$WINDOW"
printf '%s\n' "$WINDOW" > /data_new/scripts/presign-window.current
cat "$WINDOW/audit/summary.json"
```

采集时会读取整体链上状态，但不要求 paused=true。若暂停已执行、nonce 已消耗，则使用原 RUNBOOK 的暂停后恢复流程，不改写这个预签基线。

## 2. 重放固定的本次范围，并审阅参数

```bash
WINDOW=$(cat /data_new/scripts/presign-window.current)
python3 replay-span.py --audit "$WINDOW/audit/summary.json" --node-rpc "$NODE_RPC" --out "${WINDOW}-replay"
python3 recovery.py decision-template --candidate "$WINDOW/candidate" --audit "$WINDOW/audit/summary.json" --out "$WINDOW/decision.json"
```

中断时使用同一路径加 --resume，先确认原重放进程已结束。审阅本次 A/B、span 和证据后，在 decision.json 中填写 reviewReference；明确接受两个 acknowledgement，并将 mode 改为 approved-parameters。这是参数审阅记录，不是链上签名或 A→B 证明。

## 3. 同时生成恢复和 resume，模拟整组三笔

```bash
WINDOW=$(cat /data_new/scripts/presign-window.current)
PAUSE=/data_new/scripts/pause-window-20260929-152145
SEQUENCE="${WINDOW}-sequence"
python3 presign.py build --pause "$PAUSE" --window "$WINDOW" --replay "${WINDOW}-replay" --decision "$WINDOW/decision.json" --parent-rpc "$ARCHIVE_RPC" --node-rpc "$NODE_RPC" --out "$SEQUENCE"
python3 presign.py verify --sequence "$SEQUENCE" --parent-rpc "$ARCHIVE_RPC" --out "${SEQUENCE}-fork"
```

要求 status=exact_presigned_sequence_fork_passed。此测试包含完全相同的 pause/recovery/resume Safe 字段、两签不足门槛、恢复末步失败回滚及失败恢复不能通过 resume 前置检查。模拟 owner approveHash 使用 chain31337，实际签名使用包中 chain42161 hash；不宣称验证过生产 ECDSA 或原生 ArbOS 全部行为。

暂停执行前检查整组基线：

```bash
python3 presign.py check pause --sequence "$SEQUENCE" --fork-pass "${SEQUENCE}-fork" --parent-rpc "$PARENT_RPC" --out "${SEQUENCE}-pause-check-$(date +%H%M%S)"
```

核对 sequence.json，以及 pause/、recovery/、resume/ 各自 REVIEW.md、package.json.safeFields、safe-import.json。官方 Safe UI 提交各自 nonce，三位 owner 可以集中完成签名，由操作员协调执行顺序。UI 包装或字段有变化就重新核对，不使用预期 hash 猜测实际 hash。

## 4. 操作员依次执行（签名完成不等于可立即全部执行）

1. 执行已审阅的 pause。用原 recovery.py accept 验收对应 package；保存实际链上 PAUSE_TX。
2. 执行 recovery 前运行：

```bash
python3 presign.py check recovery --sequence "$SEQUENCE" --fork-pass "${SEQUENCE}-fork" --parent-rpc "$PARENT_RPC" --node-rpc "$NODE_RPC" --pause-tx "$PAUSE_TX" --out "${SEQUENCE}-recovery-check-$(date +%H%M%S)"
```

通过后操作员执行已签 recovery。保持 paused；保存 RECOVERY_TX，验收：

```bash
python3 recovery.py accept --package "$SEQUENCE/recovery" --transaction "$RECOVERY_TX" --parent-rpc "$ARCHIVE_RPC" --out "${SEQUENCE}-recovery-accepted"
```

3. 按原 RUNBOOK 使用 owner-check.py 及各机器运行日志完成实际新版本运行验收：新根、B 或更高正确端点、父链、签名账户、配置及持续运行。保持旧 validator 停止；不将模拟数据库用于生产。验收负责人制作 JSON 记录：

```json
{
  "recoveryPackageIdentity": "填入 sequence.json 的 packages.recovery",
  "recoveryTransaction": "填入实际 RECOVERY_TX",
  "accepted": false,
  "reviewReference": "填写实际运行验收报告路径、机器/账户和验收结论；通过后将 accepted 改为 true"
}
```

这是负责人验收声明，程序不把声明视为机器运行的独立证明。随后：

```bash
python3 presign.py check resume --sequence "$SEQUENCE" --fork-pass "${SEQUENCE}-fork" --parent-rpc "$PARENT_RPC" --pause-tx "$PAUSE_TX" --recovery-tx "$RECOVERY_TX" --runtime-review "$RUNTIME_REVIEW" --out "${SEQUENCE}-resume-check-$(date +%H%M%S)"
```

通过后操作员执行已签 resume。用 recovery.py accept --package "$SEQUENCE/resume" 验收其实际交易。presign resume 包使用本附加流程 check，不使用原 recovery.py preflight，因为其预签时保存的是恢复前状态。

任何执行失败、预检失败、nonce 异常或状态改变都停止后续执行并重新审阅。不要因 nonce 已递增就执行下一笔。已分发签名没有自动撤销能力，协调所有持有人避免执行旧提案。

本附加流程不改变原账户退款支付、未来三账户质押、旧 Fast Safe 提案协调及正常断言验收要求，参见原 RUNBOOK。
