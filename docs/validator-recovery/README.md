# Deriw validator 恢复工作记录

整理日期：2026-09-30（Asia/Singapore）。代码分支：`fix/legacy-validation-125022703`，恢复镜像对应提交 `16c17ee3a9c4`。

本次已经完成新 WASM 的保留历史重放、治理恢复节点 43126、恢复运行验证，以及本机账户重新质押并创建普通节点 43127。Poster 的超大批次问题通过将 calldata 批次上限降到 90000 得到成功回执验证。最后保存的 DAS 证书提交仍报 `NoSuchKeyset`，因此整个系统的最终验收不能写成全部通过。这里记录的是截至最后一份证据的事实，并非 2026-09-30 的实时链上状态。

| 阅读目的 | 入口 |
| --- | --- |
| 了解实际做了什么、哪些有链上回执 | [执行记录](EXECUTION-RECORD.md) |
| 按次序准备、演练、执行和验收 | [执行流程](RUNBOOK.md) |
| 找到所有相关脚本、测试、安装器和历史说明 | [脚本与资料索引](SCRIPT-CATALOG.md) |
| 处理验证停滞、zombie、批次大小、DAS 与 keyset | [排障记录](TROUBLESHOOTING.md) |
| 读取结构化参数、交易与未完成事项 | [incident.json](incident.json) |
| 核验所引用文件没有丢失或变化 | [catalog.py](catalog.py)、[source-index.json](source-index.json) |
| 查看本次归档验证结果 | [验证记录](VALIDATION-RESULTS.md) |
| 按用途查脚本目录与搬迁对照 | [脚本入口](../../scripts/validator-recovery/README.md)、[目录规划](LAYOUT.md) |
| 手动领取 L3 跨链提款 | [通用脚本与操作说明](../manual-withdrawal/README.md)，与 validator 质押退款分开 |
| Safe 黑名单与 owner 交易 | [生成器与操作说明](../safe-transactions/README.md)，明确区分 blacklist owner、chain owner 和 Safe signer |

从仓库根目录核验记录：

```bash
python3 docs/validator-recovery/catalog.py --verify
```

`source-index.json` 为每份相关本地文件记录路径、分类和 SHA-256；`SCRIPT-CATALOG.md` 提供分阶段的可点击目录。脚本已按用途归入 `scripts/validator-recovery`，本地导入和 fixture 路径同步调整，正式 toolkit 内部结构、历史分发包和回执内容保持原样。索引涵盖源码、正式 v2 工具、附加模块、测试与证据；压缩包和内嵌 payload 安装器只是当时的分发快照，不能假设包含后来添加的模块。

## 环境与角色

| 项目 | 本次值 |
| --- | --- |
| 父链 | Arbitrum One，chainId 42161 |
| 子链 | Deriw L3，chainId 2886 |
| 父链读 RPC | `http://10.1.2.16:8547` |
| 公开子链 RPC | `https://rpc.deriw.com` |
| Snapshot / Watchtower RPC | `http://127.0.0.1:8349`，仅在对应服务运行时使用 |
| 后来实际 MakeNodes RPC | `http://127.0.0.1:8449` |
| 新镜像标签 | `fuhua-container.tencentcloudcr.com/deriw/deriw:v1.3.1.2.v3.10.0-16c17ee3a9c4` |
| 新镜像 ID | `sha256:b3571c0d8d4cd4e826a48895ed558927c4b003ea14050f5be3fb5ab09c62148f` |
| 实际客户端版本 | `nitro/v1.3.2.v3.10.0-16c17ee3a9c4/linux-amd64/go1.25.14` |
| Rollup | `0xa113e2e9620a3bc088a681ebb2c234fdbeb85e21` |
| SequencerInbox | `0xe79283a775f6a1de250cb3284e7ff3541ff7668a` |
| 治理 Safe | `0xfbb37c66372f7b40361fbc8c8a235ae92711399d`，3/4，官方 Safe Global UI |
| Fast confirmation Safe | `0x5e16561173ea0422549c3de12b68c8a7d3a76672`，3/3 |
| 本机后续质押账户 | `0x65fa6c1d1efc338dfe8f07d5de84b3e7b76cbc95` |
| 云 A 后续质押账户 | `0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a` |
| 云 B 后续质押账户 | `0x21d4ea822a07f737c5e69f7951d517e5f2974849` |
| 退休账户，仅退款 | `0xd38e969ae2947019e0dec0e46937e6aff651834d` |
| Batch poster 账户 | `0x34993e941d0b58b22a57a0c407589ae060d19e2d` |

## 数据路径约束

`/data` 的生产同步数据库保持受保护；用户只单独授权从 `/data/validator/keys` 复制加密 keystore。复制加密文件不等于验证密码和签名地址，密码由管理员在本机输入。

`/data_mock/validator/config` 曾以新版本连接真实父链，采样区块哈希与 sequencer 一致，后来用于实际恢复运行。`/data_mock` 这个名称本身不能说明它连接模拟链。

`/data_new/validator/config/Deriw Chain/nitro` 曾用于连接私有 Anvil 的演练，是可覆盖的测试副本。本次数据库不再复制到这里；正式 MakeNodes 准备文件挂载 `/data_mock/validator/config`。同一数据库只能有一个 Nitro 进程读写。

## 证据的适用范围

A 为父链确认的状态承诺，A′ 为相同消息位置上的本地保留链状态，B 为选定恢复点。新 WASM 的 A′→B 重放证明该保留区间与新 WASM 配对；它不能证明父链 A→B。本次采用受信任治理迁移，而非给旧父链承诺补出执行证明。

私有 Anvil 上的 owner / EOA impersonation 和 `approveHash` 演练，不构成生产签名测试。部分本机契约演练使用 ArbSys 日志 shim，服务器同包复测报告明确没有使用该 shim。Withdrawals 的 spent flags 与样本支付检查，也不构成完整历史消息审计。

历史文档中的“尚未执行”“准备阶段”以及固定节点 42886 / 43008 均属于当时状态，查当前工作结论以本目录执行记录为入口。本次原 Safe nonce 10/11/12 与导入包均已进入历史记录，不能作为下一次操作的实时输入。
