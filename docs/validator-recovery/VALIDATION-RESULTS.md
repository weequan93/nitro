# 归档验证记录

日期：2026-09-30。分支：`fix/legacy-validation-125022703`，HEAD `16c17ee3a9c4`；`go-ethereum` 已与本分支指针 `e582a7d5fb4887cbd3b4c7bb4fe74d2a5b06bddc` 一致。切换前未发现受版本控制文件的未提交修改，原有未跟踪脚本和证据已保留。

## 已完成的验证

| 检查 | 结果 | 范围 |
| --- | --- | --- |
| v2 单元测试及附加模块 | 43/43 通过 | 核对交易包/篡改、Safe 回执、连续预签/漂移、Watchtower 门控/DA、Compose 与加密密钥复制；使用离线 fixture / mocks |
| 历史工具离线测试 | 65/65 通过 | 搬迁前后均通过；覆盖跨目录 helper 和 fixture 加载，合计 108 项测试 |
| Python 语法 | 120 个文件通过 | `ast.parse`，不启动恢复脚本 |
| Shell 语法 | 30 个文件通过 | `bash -n`，不执行安装器或交易 |
| JSON 格式 | 155 个本地文件通过 | 参数、报告、交易包、fixture、incident 及迁移对照，未校验所有业务字段 |
| 原 v2 manifest | 22 个条目通过 | 原 `MANIFEST.sha256` 保持原样，后续 addon 另在新索引中逐文件校验 |
| 新索引校验 | 407 个文件逐项核对 | SHA-256、字节数、范围内缺失/新增文件及生成目录内容 |
| 搬迁原字节检查 | 317 个保持原内容的文件通过 | 对照搬迁前 hash，涵盖正式 toolkit、历史 payload、报告和回执 |
| 临时副本构建 | 5 个 builder 通过，5 个生成的安装器语法通过 | 完整包 31 个 manifest 条目匹配、addon 内容匹配；没有执行安装器 |
| 记录一致性 | 通过 | incident 中候选状态、4467 数量、恢复回执/节点、退款范围、zombie 与 fast Safe 观察对照本地原报告 |
| 新文档链接 | 通过 | 工作记录与各分组 README 相对文件链接；不连接外部网站 |
| 手动 L3 提款工具 | 11/11 离线测试通过 | 已领取、不认可的 root、消息选择、事件/证明边界、native value、参数/模拟门控 |
| 提款历史个案对照 | 10 个有 proof 的个案通过 | 删除前离线重算消息 hash、Merkle root，与旧 calldata、收款方/token/raw amount 对照；生成 shell 语法通过 |
| 浏览器提款方法 | JavaScript 语法通过 | `node --check`，没有连接钱包或发送交易 |
| Safe 黑名单与 owner 生成器 | 9/9 离线测试通过 | 已知 ABI/calldata、角色/链区分、Safe owner threshold、参数拒绝、批次、输出保护及只读检查脚本语法；合计 128 项单元测试 |

初始归档通过 43 项正式工具测试和 387 个文件检查；后续完成 [目录整理](LAYOUT.md)，将 357 个文件搬迁并修正本地路径、构建输出及说明链接。正式 toolkit、历史报告/回执、旧安装 payload 和协议实现保留原内容。新 builder 在临时副本构建验证，历史基础 manifest 不变。没有启动集成演练、调用生产 RPC、访问 Docker 或发送生产交易。单元测试成功不替代各阶段链上回执和真实运行验收。

手动 L3 提款的通用方法随后归入 [docs/manual-withdrawal](../manual-withdrawal/README.md)。14 个根目录未提交个案目录（110 个文件）按用户要求清理，后续输出目录已加入 Git ignore。Recovery 的既有 receipt fixture 独立保留。

[docs/safe-transactions](../safe-transactions/README.md) 新增离线 Safe Builder 交易生成器，涵盖黑名单 from/to、blacklist owner、chain owner 与 Safe signer 的不同方法。仅验证生成内容和检查脚本；没有运行链上权限查询或真实 Safe 执行。

## 可重复命令

从仓库根目录运行：

```bash
export PATH="$HOME/.foundry/bin:$PATH"
PYTHONDONTWRITEBYTECODE=1 python3 scripts/validator-recovery/tests/run-offline.py
python3 docs/validator-recovery/catalog.py --verify
```

原 v2 基础分发清单：

```bash
(cd scripts/validator-recovery/toolkit && shasum -a 256 -c MANIFEST.sha256)
```

`catalog.py --refresh` 仅在已审阅的资料变化后重新生成索引，不是忽略校验差异的操作。它不会联网、启动容器或运行恢复脚本。

## 生产证据限制

完整 resume 回执、普通节点最终确认、另外两台实际 validator 的最终运行，以及 DAS 新 keyset 故障的解决结果，仍列在 [执行记录](EXECUTION-RECORD.md) 和 [incident.json](incident.json) 中。其状态没有因为归档测试通过而改变。
