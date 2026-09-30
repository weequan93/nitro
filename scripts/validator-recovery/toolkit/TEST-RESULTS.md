# v2 测试记录（2026-09-29）

## 已完成

- 10 项单元测试：禁止公共 RPC 写方法、包/导入内容篡改、已部署状态承诺、重放链连续性及错误结果、ABI 边界、Safe 回执字段/chain/ExecutionFailure、受保护输出路径。
- 所有 Python 语法检查、提款 shell 语法检查。
- 当前真实父链状态只读盘点及 Fast Safe 身份检查：固定三个 owner，3/3。
- 自有31337分叉中执行同包恢复：失败确实到达最后的 forceConfirmNode 调用，Safe ExecutionFailure、nonce消耗和内层回滚均检查；恢复快照后执行原包成功。
- 同一回执解码器在分叉中验证 Safe execTransaction 参数、实际 nonce、indexed ExecutionSuccess hash、NodeCreated 的节点/根/实际 Inbox 数量及存储承诺。
- 在恢复成功的同一分叉：单独 Safe resume、三个原账户提款、实际余额变化/信用清零测试通过；后续测试回滚后仍恢复到暂停状态。分叉已停止。

## 必须保留的测试限制

**本机 Anvil 1.2.3 不实现 ArbSys.arbBlockNumber。** 未补丁的实测在创建节点处失败。最终通过的契约测试显式使用 `--allow-arbsys-log-shim`，仅为 log lookup 模拟该 selector 的返回值，状态为 `exact_package_fork_passed_with_arbsys_log_shim`。

这不是原生 Arbitrum 预编译/双时钟/费用模型验证，不是生产 ECDSA 签名测试。测试 B 来自当前旧 WASM 的待确认断言，仅用于调用机制，不是生产恢复候选；没有对该 B 做新 WASM 重放。

公共 RPC 拒绝本次历史锚点的 storage 读取（historical state unavailable）；因此本机未重新核验服务器上 10578 条历史记录。生成器的完整历史输入链需按 RUNBOOK 的历史模拟命令在服务器 Archive RPC 上复测。不会把既有历史报告冒充本次执行。

新通用 collect/replay 的本机覆盖是代码/语法及记录校验测试；这里没有连到服务器8349运行新一轮区间重放。旧的实际 A′→B / B→C 重放证据仍是此前服务器报告。

本包没有证明父链 A→B，没有替代治理团队对生产 numBlocks、A/B 和实际签署参数的审阅。正式签署前仍须用暂停后的实际包进行针对性验证，核对模拟环境限制。

## 本次分叉证据

`reference/v2-fork-summary.json`：恢复节点43105是本次契约测试节点，**不可抄到生产**；恢复后 paused=true，三个退款 credit 各 10^12 wei。resume/提款测试随后撤销。

`reference/v2-fast-safe-check.json`：只读快确认 Safe 身份结果；不代表旧提案/离线签名已清理。

保留的本机详细记录：`validator-recovery-evidence/toolkit-v2-validation/`。其中前几次失败没有删除，最终报告单独保存。
