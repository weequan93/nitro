# Deriw 恢复工具包 v2

**这份包用于准备、只读核对和隔离模拟。尚未授权生产暂停或恢复。**

用户负责新镜像/配置切换；治理 Safe 团队负责执行窗口协调。工具不停止容器、不复制数据库、不读取 `/data`，也不读取生产私钥。

## 固定角色

| 用途 | 地址 |
|---|---|
| 治理 Safe，Arbitrum One，3/4，官方 Safe Global UI | `0xfbb37c66372f7b40361fbc8c8a235ae92711399d` |
| Fast confirmation Safe，3/3 | `0x5e16561173ea0422549c3de12b68c8a7d3a76672` |
| 当前服务器，后续质押 | `0x65fa6c1d1efc338dfe8f07d5de84b3e7b76cbc95` |
| 云 A，后续质押 | `0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a` |
| 云 B，后续质押 | `0x21d4ea822a07f737c5e69f7951d517e5f2974849` |
| 已退休，仅退款，不重新质押 | `0xd38e969ae2947019e0dec0e46937e6aff651834d` |

本次原始退款集合为 **5cda、d38e、65fa**。后续质押集合为 **65fa、5cda、21d4**。不向 Fast Safe 退款。链上参与者变化会使生成器停止，不能用旧集合覆盖新状态。

新镜像（用户已安排）：`fuhua-container.tencentcloudcr.com/deriw/deriw:v1.3.1.2.v3.10.0-16c17ee3a9c4`。本地 image ID 与 registry digest 不是同一字段。

## 文件入口

| 文件 | 功能 |
|---|---|
| `RUNBOOK.md` | 完整执行次序、命令和中止条件 |
| `recovery.py followup pause` | 生成实时核对后的单调用暂停 Safe JSON |
| `collect.py` | 暂停后固定父链 A、候选 B、定位本地 A′ |
| `replay-span.py` | 通用、可续跑的 A′→B 新 WASM 重放 |
| `recovery.py decision-template/build` | 参数审阅模板、绑定证据、恢复 Safe 包 |
| `verify-fork.py` | 同包私有分叉成功/失败回滚测试 |
| `recovery.py preflight/accept` | 签名前状态检查、实际 Safe 回执验收 |
| `recovery.py followup resume` | 恢复成功后生成独立 resume 包 |
| `fast-safe-check.py` | Fast Safe 3/3身份只读核对 |
| `owner-check.py` | 原账户退款回执、最终节点状态只读核对 |
| `withdraw-refund.sh` | 默认只读；只有账户主人明确 `broadcast` 才签名提款 |
| `tests/` | 单元测试、契约回归测试；不可作生产候选 |
| `reference/` | 之前的暂停审阅、计划、角色及准备证据 |
| `TEST-RESULTS.md` | 本次实际测试覆盖和限制 |
| `MANIFEST.sha256` | 安装文件校验清单 |

Python 3.10+、Foundry `cast`/`anvil`、curl；不需要 pip。默认命令使用环境中的 RPC，不把 Archive URL 写入交付包。Anvil 自己的诊断日志可能含 URL，只保存在操作员私有输出目录。

## “准备完成”的含义

工具及输入格式可以先交付；最终父块、A/B、跨度、退款基线、node hash、Safe nonce/hash 必须在执行窗口生成。全部文件经过校验也不等于签署批准。

A 是父链已确认的承诺；A′ 是本地保留链在相同消息位置的状态，两者存在差异。A′→B 重放不能证明 A→B。治理迁移使用的 numBlocks 是经审阅的位置跨度约定，程序不会将它写成已证明的执行跨度。

不要反复跑旧的 42886/43008、7838/10578 固定脚本准备生产。历史包仅保留为机制证据。

**本轮验证限制：** 本机分叉使用显式 ArbSys 日志块号模拟；未标为原生 Arbitrum 验证通过。服务器历史完整输入复测命令已备好，见 TEST-RESULTS.md / RUNBOOK.md。
