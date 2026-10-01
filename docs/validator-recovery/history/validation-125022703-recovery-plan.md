# 125022703：执行差异证据与保留历史的修复方案

## 已确认的实验

用户于 2026-09-23 在 validator 服务器运行诊断镜像：
`sha256:a20103a7f8f3abc7c351d192bfe53317adba41829bb619aa55b429c5e4e329a9`。
输出目录：`/data_new/scripts/grant-replay-20260923-165620`。

同一保存的 validation input，原生重放只改变 AddressMap.Add 的重复键行为：

| 项目 | 16c17ee3 基线 | 恢复重复地址报错 |
| --- | --- | --- |
| BlockHash | `0x8e5465ac51d40a9e216fb39659b9271745825ec1238eb3a13fcca2a18c6c516a` | `0x9c8e05504b6482b9903a168aed23589999c6bc9b96b8faa297df171409a6b419` |
| StateRoot | `0x9791f059653e83ccf279216865b7c7f28822bb9e04a8e72277c0babed3303d39` | `0x95de7dc7f68163232267fcb4651dbc788d79ed9320f405e292e667f7a1b1a990` |
| ReceiptsRoot | `0xd8eef04eedf4176e8ab02393c50b3315877f2d80c58fdef695c990965d4eb02a` | `0xf89609af1f20635761129dd218cc1cc1ce504d4138efcdecdad5108c84da45a9` |
| GasUsed | 1132610 | 1132610 |
| exitCode | 0 | 0 |

基线精确匹配当前链，实验精确匹配此前旧 WASM 的结果。这证明该语义变化足以复现本块差异；不需要增加 preimages 才能复现。它不证明整段历史没有其他差异，也不证明旧 WASM 的完整构建来源。

## 代码历史核查

- `167a1cafb6f9a4da934a3e0e4f5390282d24701c` 的 AddressMap.Add 对已有地址返回错误。
- 合并提交 `21ec862405cfa0e12e19c9202a74e04762b2de04`（提交日期 2026-05-14）相对 Deriw 父提交 `4fc964bb9` 将它改为返回 nil；该分支没有版本判断。提交日期不是生产部署时间。
- sequencer `237cc2a4d345` 与 validator `16c17ee3` 的该执行逻辑相同。两者差异仅三个文件，涉及 legacy recording hooks、测试与说明。
- 后续提交 `1aa35665` 使用 DeriwOS 5 门控新的授权逻辑。其 BindRelationLegacy 保留 nil/no-op 行为；`TestBindRelationLegacyPreservesHistoricalChildRebindingBehavior` 明确期望 A→C、B→C、C→A 共存。因此它的 Legacy 路径并不同时覆盖更早的 duplicate-error 行为。

## 修复设计：三个时期不能混为一谈

### 为什么旧原生代码与旧 WASM 可能一致

本次重新核查 Git 对象中的执行路径：

- `167a1caf:execution/gethexec/executionengine.go` 的消息执行调用 `arbos.ProduceBlock`（735 行），随后 `appendBlock` 调用 `WriteBlockAndSetHeadWithTime`（756 行）。节点按本地执行规则生成区块，不是仅下载 sequencer 的执行结果。
- `16c17ee3a:execution/gethexec/block_recorder.go` 的 `RecordBlockCreation` 从本地父状态执行消息、采集 preimages，并检查产生的 blockHash 等于本地 canonicalHash。
- `16c17ee3a:staker/stateless_block_validator.go` 的 `ValidationEntryRecord` 再检查 recording.BlockHash 等于 entry.End.BlockHash。
- `16c17ee3a:staker/block_validator.go:1044` 比较 WASM 的 runEnd 与 DoneEntry.End；不相等才产生本次 `validation failed: got` 错误。

因此旧原生执行若对重复授权回滚，且旧 WASM 同样回滚，两者可以匹配；新原生执行若返回成功，旧 WASM 仍回滚，则比较失败。同一消息编号不保证不同执行版本生成相同区块哈希。首个分歧之后，后续父状态和由它记录的 preimages 也可能不同。

代码中的精确变化是 `167a1caf:arbos/addressMap/addressMap.go:120` 的 `return errors.New("address already present in address map")` 变为 `16c17ee3a:arbos/addressMap/addressMap.go:183` 的 `return err`；前面已处理非 nil 错误，故已有键分支实际返回 nil。合并 `21ec862405cfa0e12e19c9202a74e04762b2de04` 相对第二父提交的 diff 显示该变更，且此处未加入版本门控。

单变量实验已经证明这一变化足以解释保存输入的两个精确区块哈希。但尚未采集其他旧节点在本块的本地结果和 validated execution 日志，不能把“继续确认断言”当作这一运行事实的替代证据。

1. 早期重复地址报错、调用回滚的历史。
2. 当前链已产生的重复地址返回成功、不覆盖原值的历史。
3. 将来通过明确共识升级边界启用的一对一授权规则。

不能用“DeriwOS < 5”这个条件区分时期 1 和 2：两段历史可能都没有 DeriwOS 状态。需要部署记录、旧块重放与链上已有版本信号核查，确定是否有可复用的真实边界；若没有，需要经过审查的链级历史兼容规则。不得把本次首次观察到的失败高度当作升级高度。

生产补丁应保持历史读取顺序、计费、错误类型与回滚结果。不要全局改回 Add 报错，也不要在旧历史中自动纠正账户关系；后者会改变历史 state root。未来的一对一规则与既有不一致关系的处理需要单独定义并测试。

## 保留当前数据还需要解决的父链衔接

当前保存的节点 42560 断言覆盖 Batch 314206/0 → 314227/0，包含故障位置 314208/93 → 94。此前本地和公开 RPC 都未找到其 after BlockHash。这不是已证明可衔接的恢复点。

必须取得完整区间验证的最终 summary，比较新 WASM 在同一终点得到的完整 global state 与父链断言终点；并刷新 latestConfirmed，不能把先前 42563/42567 等查询值当成当前值。

- 若端点完全一致，进一步验证父链要求的 root、断言连接与升级流程，才能设计恢复步骤。
- 若端点不一致，当前 L2 历史与父链承诺之间还存在恢复问题。切换本地 root、跳过验证或仅 setWasmModuleRoot 均不能自动解决它。

本地 contracts-legacy 源码中 forceCreateNode 要求 prevNode == latestConfirmed，createNewNode 校验 PREV_STATE_HASH；forceConfirmNode 也走 confirmNode。不能假定拥有 admin 即能把任意本地状态直接接到已确认历史。实际部署实现、代理与管理权限仍需逐项匹配后在父链 fork 中模拟，不生成或发送未经验证的管理交易。

## 最小验证要求

- 保存本次两种行为的精确区块哈希作为回归证据；完整输入留在服务器。
- 测试无冲突授权、重复授权、跨父账户授权、撤销，以及已有不一致关系；验证 EVM 层完整回滚而不只测试映射函数返回值。
- 对历史边界前后测试原生执行与对应 WASM：区块哈希、状态根、收据根、gas、完整 global state。
- 完成包含故障块的断言区间验证；单块成功不能替代整段结论。
- 未来升级的激活前后采用相同输入对比原生/WASM，确认激活边界一致。

## 当前待补证据

### 更新：断言 42560 区间已完成

用户提供 `assertion42560-resume-20260923-172646/summary.json`：累计 4556 条消息，从 125022161 至 125026716；completedLocalInterval=true，matchesParentAssertion=false。

新 WASM `0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421` 的逐消息重放均匹配连续本地历史。终点 Batch=314227、PosInBatch=0：

- 本地验证终点 BlockHash：`0xb63c37c2c0332011291e7fab36a7a6be5ee962caf519c49547e3a65f6bff93a8`。
- 保存的父链断言终点 BlockHash：`0x58a79224012ea604b40fdc6a77d864c8474b76281c2cbb1d9a1121c630b0ea0f`。
- SendRoot 均为 `0x5fa89c8534ff17732c04c8a863123f1b3a0d1f8d7d8b23058048f7ecc8d54712`。

这排除了“只等新 WASM 把本区间验证完成就能匹配断言 42560”的方案。保持本地历史的恢复必须处理父链承诺差异。相同 SendRoot 不等于完整状态相同，也不授权替换断言。

该测试是从同一已验证起点逐消息串联本地输入的重放，不是对旧 WASM 分叉历史的独立重建。仍不能据此确定父链不同终点的完整来源、其他 validator 的配置或首次分歧位置。

1. sequencer 从老版本迁移到 v3.10.0 系列的部署时间、镜像记录、升级前后区块。
2. 已完成的区间 summary、steps 与单块诊断输出需要保留；无需重跑本区间。
3. 实际线上 Rollup 实现和最新已确认状态，用于后续恢复模拟。

当前交付是根因证据和恢复设计；没有修改生产执行语义、WASM root、validator 进度或链上状态。
