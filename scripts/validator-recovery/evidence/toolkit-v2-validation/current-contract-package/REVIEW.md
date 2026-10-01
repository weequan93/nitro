# recovery：需审阅，程序没有广播能力

- 用途：simulation-only；RPC chainId：42161
- Safe：`0xfbb37c66372f7b40361fbc8c8a235ae92711399d`；Arbitrum One 42161；nonce `11`
- 外层 to：`0x9641d764fc13c8b624c04430c7356c1c7c8102e2`；operation `1`；value 0
- safeTxGas/baseGas/gasPrice 均为 0；gasToken/refundReceiver 零地址。
- SafeTxHash：`0xf048e6a0e12b9dc0171d6601887a10d9cc48a6a56faeebbb38da07ec07848034`，仅适用于上面的 chainId 和全部 Safe 字段。
- safe-import.json 是 Transaction Builder 的内层 CALL 列表。UI 必须构造 package.json 中完全相同的外层交易。
- UI 若选择不同 MultiSend 地址、包装层或 Safe gas 字段：不要签，重新生成并验证。外层普通 Ethereum gas limit 不属于 SafeTxHash。
- 历史分叉/模拟包不得用于生产。readyForProduction=false 不会自动变成批准。
- 精确节点 hash/prevNode 检查是合约已有约束；Safe nonce 防重复。其他前置检查在链下，不是原子链上 guard。
- Inbox 可以继续增长，新节点 stateHash 的 inboxMaxCount 应按 NodeCreated 实际事件核验；不要照抄模拟值。
- A→B 是受信任治理迁移。A′→B 重放和消息数量不能证明父链 A→B。
- 恢复包成功后仍暂停。resume 必须在恢复回执和运行验收后单独生成。
