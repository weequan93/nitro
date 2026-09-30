# Deriw validator 恢复协作清单

状态：诊断与演练准备，不是可签署的生产交易方案。
目标：保留 sequencer 当前历史与节点数据，使执行、WASM 验证和父链承诺重新一致。

## 已知事实

- 相同输入的单变量重放证明 AddressMap.Add 重复键返回错误/成功足以复现 125022703 的两个区块哈希。
- 新 WASM 的 4556 条消息连续验证匹配当前本地历史，但 Batch 314227/0 的终点不同于断言 42560。
- 用户 2026-09-23 18:08 快照：latestConfirmed=42580，latestNodeCreated=42582，旧 WASM root，Rollup 未暂停。这些是快照值，不能直接作为后续交易参数。
- 用户确认可协调具备执行权限的人员共同操作。实际签名地址、角色和签名机制仍需在演练前核对，不要求提供私钥。

## 操作顺序

1. 协调全部断言提交者和确认自动化，约定停止新增断言、确认及自动管理交易的维护窗口；记录生效时间与停止后的父链状态。仅停止本机 staking 不会停止其他节点。
2. 保留当前数据与诊断输出；需要数据库副本时使用停止写入后的备份或经过验证的一致性快照，不直接复制正在写入的数据库作为可靠恢复点。
3. 评估维护窗口内是否需要暂停 sequencer 接收新交易，以取得稳定的目标检查点。明确是否仅停止 assertions；不要将两种暂停混为一谈。
4. 取得管理执行地址（EOA/多签/时间锁）、部署源码与编译配置，核对代理实现和存储布局。现有角色查询只是线索，不等于实现身份认证。
5. 在隔离的 Arbitrum One fork 中复现父链状态，设计并测试衔接。重放当前历史不能自动改写已确认节点。
6. 演练通过后形成精确交易列表：chainId、目标、calldata、顺序、nonce、执行前断言、执行后检查及失败处置。由操作人员共同复核、签署和执行。
7. 恢复至少两套独立节点的验证，核对同一消息位置的完整 global state 和父链断言端点后，再分阶段恢复提交、确认与业务。

## 已核查的本地合约限制

- setWasmModuleRoot 只设置 root，不修改既有 Node 的 stateHash / confirmData。
- forceCreateNode 要求 prevNode == latestConfirmed，底层还检查 PREV_STATE_HASH。
- forceConfirmNode 仍检查 CONFIRM_DATA，不可任意传入另一个 blockHash。
- 因此不能将 pause → setWasmModuleRoot → forceConfirmNode → resume 当作已经成立的恢复方案。
- 进一步检查发现一条待演练的路径：以最新已确认节点的实际 afterState 作为 beforeState，用 forceCreateNode 创建管理员指定的新检查点，再确认新创建节点。这与覆盖旧节点不同；PREV_STATE_HASH 并不直接排除这条路径。createNewNode 不在这里执行 WASM，因此接受该调用不构成旧状态到新检查点的执行证明。这是需要明确信任与审批的管理员恢复，不能称为普通验证通过。
- 仓库 `contracts-legacy/test/contract/arbRollup.spec.ts` 的 force-create/force-confirm 测试包含管理员从 prevNode.afterState 创建新的 assertion 后强制确认的流程。这里只核查了测试源码，未运行该合约测试，也未完成部署字节码匹配或线上 fork 验证。
- 仓库 scripts/safe-wasm-root 是 root 更新工具，README 默认环境为测试网，不能直接用于本次主网历史恢复。
- Rollup pause 不能未经核查就被视为暂停 sequencer、Bridge 或已存在 Outbox 提款。

## 演练验收

- 保留的目标检查点（BlockHash、SendRoot、Batch、PosInBatch）与恢复后父链连接规则一致。
- 所用原生二进制与 WASM 的明确版本、哈希和历史规则一致；不按客户端开关随意改变共识规则。
- 未决断言、质押和挑战状态已正确处理；已确认旧承诺的处理有明确协议依据。
- Outbox 已登记的根、已提款标记及指定失败提款经过核对；不得仅凭区块哈希不同断言提款失败原因，也不能假定更新 Rollup 会删除旧 Outbox 根。
- 父链交易模拟成功不等于恢复完成；还需验证节点续跑、下一段断言和提款路径。

本清单没有执行或授权任何链上交易，不包含私钥或签名。

## 当前只读准备工具

`prepare-recovery-fork.py` 配合更新后的 `probe-recovery-authority.py`：

- 捕获父链快照，并用事件 afterState、inboxMaxCount 重新计算 stateHash，与 getNode 核对。
- 保存实际实现字节码，供后续匹配源码。
- 以历史 validation input 的 Id 和对应区块号为锚，推导近期消息编号，并用实际 input 的哈希和父哈希验证映射。
- 默认选择本地 head 后退 64 块，比较 Batch/Pos 与最新确认位置及父链 inbox 可用范围。
- 条件满足时仅验证候选消息一次；明确不证明旧确认状态到候选状态的执行路径，不声明生产就绪。
- 不构造待签名交易，不发送交易，不修改 validator 进度或数据库。

本地模拟测试已覆盖正常采集、候选落后、inbox 不足、重放失败、子链重组及事件/节点 stateHash 不符。尚未运行真实父链 fork。采集完成后需要补齐实际管理执行地址、部署源代码身份、质押/挑战与旧 Outbox 根的处理，再进行恢复演练。
